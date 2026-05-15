package broker

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

func TestBackoffDelay_Bounds(t *testing.T) {
	if d := backoffDelay(0); d <= 0 || d > 2*time.Second {
		t.Errorf("attempt 0 delay = %v, want (0, 2s]", d)
	}
	maxWithJitter := reconnectMaxDelay + time.Duration(float64(reconnectMaxDelay)*jitterFraction)
	for i := 0; i < 200; i++ {
		d := backoffDelay(i)
		if d < 0 {
			t.Fatalf("attempt %d: negative delay %v", i, d)
		}
		if d > maxWithJitter {
			t.Fatalf("attempt %d: %v exceeds max+jitter %v", i, d, maxWithJitter)
		}
	}
	if d := backoffDelay(-5); d < 0 {
		t.Errorf("negative attempt must clamp, got %v", d)
	}
}

func TestReconnectWithBackoff_RedeclaresAndResubscribes(t *testing.T) {
	newCh := &mockChannel{}
	newDeliveries := make(chan amqp.Delivery, 1)
	newCh.consumeFn = func(q, cs string, aa, ex, nl, nw bool, a amqp.Table) (<-chan amqp.Delivery, error) {
		return newDeliveries, nil
	}
	newConn := &mockConn{channelFn: func() (AMQPChannelAPI, error) { return newCh, nil }}

	var dialCount int
	dialFn := func(addr string) (AMQPAPI, error) {
		dialCount++
		if dialCount < 2 {
			return nil, errors.New("connection refused")
		}
		return newConn, nil
	}

	oldConn := &mockConn{channelFn: func() (AMQPChannelAPI, error) { return &mockChannel{}, nil }}
	c := newClientWithAMQP(testConfig(), oldConn, dialFn)

	received := make(chan []byte, 1)
	if err := c.Subscribe("lic.q.version-artifacts-ready", func(_ context.Context, d Delivery) error {
		received <- d.Body()
		return d.Ack()
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	c.reconnectWithBackoff()

	if dialCount < 2 {
		t.Fatalf("expected ≥2 dial attempts, got %d", dialCount)
	}

	c.mu.Lock()
	connReplaced := c.conn == newConn
	pubChSet := c.pubCh != nil
	c.mu.Unlock()
	if !connReplaced {
		t.Error("conn not replaced after reconnect")
	}
	if !pubChSet {
		t.Error("publish channel not re-created after reconnect")
	}
	if !newCh.confirmed {
		t.Error("re-created publish channel must be put in Confirm mode")
	}

	// Topology was re-declared on the new connection.
	if len(newCh.queues) != 24 {
		t.Errorf("topology not re-declared on reconnect: %d queues", len(newCh.queues))
	}

	// Subscription re-established: a delivery on the NEW channel reaches the handler.
	newDeliveries <- amqp.Delivery{Acknowledger: &mockAcknowledger{}, Body: []byte("after-reconnect")}
	select {
	case b := <-received:
		if string(b) != "after-reconnect" {
			t.Errorf("body = %q", b)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not receive message after reconnect")
	}

	close(newDeliveries)
	_ = c.Close()
}

// Exercises the headline auto-reconnect path end to end: a *amqp.Error
// delivered on the connection's NotifyClose channel must drive a re-dial
// (code-reviewer coverage gap).
func TestReconnectLoop_NotifyClose_TriggersRedial(t *testing.T) {
	// amqp091 contract: NotifyClose delivers the close error to the
	// receiver the caller registered. The mock hands that receiver back to
	// the test so it can fire a broker-bounce error on it.
	receiverCh := make(chan chan *amqp.Error, 1)
	oldConn := &mockConn{
		channelFn: func() (AMQPChannelAPI, error) { return &mockChannel{}, nil },
		notifyCloseFn: func(r chan *amqp.Error) chan *amqp.Error {
			select {
			case receiverCh <- r:
			default:
			}
			return r
		},
	}
	newConn := &mockConn{channelFn: func() (AMQPChannelAPI, error) { return &mockChannel{}, nil }}

	var dialCount int32
	dialFn := func(addr string) (AMQPAPI, error) {
		atomic.AddInt32(&dialCount, 1)
		return newConn, nil
	}

	c := newClientWithAMQP(testConfig(), oldConn, dialFn)
	c.wg.Add(1)
	go c.reconnectLoop()

	var r chan *amqp.Error
	select {
	case r = <-receiverCh:
	case <-time.After(2 * time.Second):
		t.Fatal("reconnectLoop never registered NotifyClose")
	}
	r <- &amqp.Error{Code: 320, Reason: "CONNECTION_FORCED"} // broker bounce

	deadline := time.After(3 * time.Second)
	for {
		c.mu.Lock()
		swapped := c.conn == newConn
		c.mu.Unlock()
		if swapped {
			break
		}
		select {
		case <-deadline:
			t.Fatal("NotifyClose did not trigger a re-dial + connection swap")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if atomic.LoadInt32(&dialCount) < 1 {
		t.Error("expected at least one re-dial")
	}

	close(c.done)
	waited := make(chan struct{})
	go func() { c.wg.Wait(); close(waited) }()
	select {
	case <-waited:
	case <-time.After(3 * time.Second):
		t.Fatal("reconnectLoop did not exit after done")
	}
}

func TestReconnectWithBackoff_StopsOnDone(t *testing.T) {
	dialFn := func(addr string) (AMQPAPI, error) { return nil, errors.New("always fails") }
	c := newClientWithAMQP(testConfig(), nil, dialFn)

	go func() {
		time.Sleep(50 * time.Millisecond)
		close(c.done)
	}()

	done := make(chan struct{})
	go func() {
		c.reconnectWithBackoff()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("reconnectWithBackoff did not stop after done closed")
	}
}

func TestReconnectLoop_ExitsOnClose(t *testing.T) {
	conn := &mockConn{channelFn: func() (AMQPChannelAPI, error) { return &mockChannel{}, nil }}
	c := newClientWithAMQP(testConfig(), conn, nil)
	c.wg.Add(1)
	go c.reconnectLoop()

	time.Sleep(30 * time.Millisecond)
	close(c.done)

	waited := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(waited)
	}()
	select {
	case <-waited:
	case <-time.After(3 * time.Second):
		t.Fatal("reconnectLoop did not exit on done")
	}
}
