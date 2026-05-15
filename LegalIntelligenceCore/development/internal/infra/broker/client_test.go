package broker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"contractpro/legal-intelligence-core/internal/config"
)

// --- Test config -----------------------------------------------------------

func testConfig() config.BrokerConfig {
	return config.BrokerConfig{
		URL:                     "amqp://guest:guest@localhost:5672/",
		ExchangeEvents:          "contractpro.events",
		ExchangeResponses:       "contractpro.responses",
		ExchangeCommands:        "contractpro.commands",
		ExchangeDLX:             "contractpro.dlx",
		ConsumerPrefetch:        10,
		ConsumerMaxRedeliveries: 3,
		ConsumerRetryTTL1:       2 * time.Second,
		ConsumerRetryTTL2:       10 * time.Second,
		ConsumerRetryTTL3:       60 * time.Second,
		PublisherConfirmTimeout: 200 * time.Millisecond,
		PublishBufferSize:       100,
	}
}

// --- Mocks -----------------------------------------------------------------

type declaredQueue struct {
	name       string
	durable    bool
	autoDelete bool
	exclusive  bool
	args       amqp.Table
}

type declaredExchange struct {
	name string
	kind string
}

type bind struct {
	queue, key, exchange string
}

type mockChannel struct {
	mu sync.Mutex

	exchangeDeclareFn func(name, kind string, durable, autoDelete, internal, noWait bool, args amqp.Table) error
	queueDeclareFn    func(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error)
	queueBindFn       func(name, key, exchange string, noWait bool, args amqp.Table) error
	qosFn             func(prefetchCount, prefetchSize int, global bool) error
	confirmFn         func(noWait bool) error
	consumeFn         func(queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args amqp.Table) (<-chan amqp.Delivery, error)
	publishFn         func(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp.Publishing) (PublishConfirm, error)
	closeFn           func() error
	notifyCloseFn     func(chan *amqp.Error) chan *amqp.Error

	exchanges []declaredExchange
	queues    []declaredQueue
	binds     []bind
	confirmed bool
	qosCount  int
	closed    bool
}

func (m *mockChannel) ExchangeDeclare(name, kind string, durable, autoDelete, internal, noWait bool, args amqp.Table) error {
	m.mu.Lock()
	m.exchanges = append(m.exchanges, declaredExchange{name: name, kind: kind})
	m.mu.Unlock()
	if m.exchangeDeclareFn != nil {
		return m.exchangeDeclareFn(name, kind, durable, autoDelete, internal, noWait, args)
	}
	return nil
}

func (m *mockChannel) QueueDeclare(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error) {
	m.mu.Lock()
	m.queues = append(m.queues, declaredQueue{
		name: name, durable: durable, autoDelete: autoDelete, exclusive: exclusive, args: args,
	})
	m.mu.Unlock()
	if m.queueDeclareFn != nil {
		return m.queueDeclareFn(name, durable, autoDelete, exclusive, noWait, args)
	}
	return amqp.Queue{Name: name}, nil
}

func (m *mockChannel) QueueBind(name, key, exchange string, noWait bool, args amqp.Table) error {
	m.mu.Lock()
	m.binds = append(m.binds, bind{queue: name, key: key, exchange: exchange})
	m.mu.Unlock()
	if m.queueBindFn != nil {
		return m.queueBindFn(name, key, exchange, noWait, args)
	}
	return nil
}

func (m *mockChannel) Qos(prefetchCount, prefetchSize int, global bool) error {
	m.mu.Lock()
	m.qosCount = prefetchCount
	m.mu.Unlock()
	if m.qosFn != nil {
		return m.qosFn(prefetchCount, prefetchSize, global)
	}
	return nil
}

func (m *mockChannel) Confirm(noWait bool) error {
	m.mu.Lock()
	m.confirmed = true
	m.mu.Unlock()
	if m.confirmFn != nil {
		return m.confirmFn(noWait)
	}
	return nil
}

func (m *mockChannel) Consume(queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args amqp.Table) (<-chan amqp.Delivery, error) {
	if m.consumeFn != nil {
		return m.consumeFn(queue, consumer, autoAck, exclusive, noLocal, noWait, args)
	}
	return make(chan amqp.Delivery), nil
}

func (m *mockChannel) PublishWithDeferredConfirmWithContext(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp.Publishing) (PublishConfirm, error) {
	if m.publishFn != nil {
		return m.publishFn(ctx, exchange, key, mandatory, immediate, msg)
	}
	return ackedConfirm(), nil
}

func (m *mockChannel) NotifyClose(receiver chan *amqp.Error) chan *amqp.Error {
	if m.notifyCloseFn != nil {
		return m.notifyCloseFn(receiver)
	}
	return receiver
}

func (m *mockChannel) Close() error {
	m.mu.Lock()
	m.closed = true
	m.mu.Unlock()
	if m.closeFn != nil {
		return m.closeFn()
	}
	return nil
}

type mockConn struct {
	mu            sync.Mutex
	channelFn     func() (AMQPChannelAPI, error)
	closeFn       func() error
	notifyCloseFn func(chan *amqp.Error) chan *amqp.Error
	isClosedFn    func() bool
	closed        bool
}

func (m *mockConn) Channel() (AMQPChannelAPI, error) {
	if m.channelFn != nil {
		return m.channelFn()
	}
	return &mockChannel{}, nil
}

func (m *mockConn) Close() error {
	m.mu.Lock()
	m.closed = true
	m.mu.Unlock()
	if m.closeFn != nil {
		return m.closeFn()
	}
	return nil
}

func (m *mockConn) NotifyClose(receiver chan *amqp.Error) chan *amqp.Error {
	if m.notifyCloseFn != nil {
		return m.notifyCloseFn(receiver)
	}
	return receiver
}

func (m *mockConn) IsClosed() bool {
	if m.isClosedFn != nil {
		return m.isClosedFn()
	}
	return false
}

// fakeConfirm implements PublishConfirm.
type fakeConfirm struct {
	done  chan struct{}
	acked bool
}

func (f *fakeConfirm) Done() <-chan struct{} { return f.done }
func (f *fakeConfirm) Acked() bool            { return f.acked }

func ackedConfirm() *fakeConfirm {
	c := &fakeConfirm{done: make(chan struct{}), acked: true}
	close(c.done)
	return c
}

func nackedConfirm() *fakeConfirm {
	c := &fakeConfirm{done: make(chan struct{}), acked: false}
	close(c.done)
	return c
}

func neverConfirm() *fakeConfirm {
	return &fakeConfirm{done: make(chan struct{})} // never closed
}

// mockAcknowledger implements amqp.Acknowledger for Delivery ack tests.
type mockAcknowledger struct {
	ackFn    func(tag uint64, multiple bool) error
	nackFn   func(tag uint64, multiple, requeue bool) error
	rejectFn func(tag uint64, requeue bool) error
}

func (m *mockAcknowledger) Ack(tag uint64, multiple bool) error {
	if m.ackFn != nil {
		return m.ackFn(tag, multiple)
	}
	return nil
}
func (m *mockAcknowledger) Nack(tag uint64, multiple, requeue bool) error {
	if m.nackFn != nil {
		return m.nackFn(tag, multiple, requeue)
	}
	return nil
}
func (m *mockAcknowledger) Reject(tag uint64, requeue bool) error {
	if m.rejectFn != nil {
		return m.rejectFn(tag, requeue)
	}
	return nil
}

// --- Interface compliance --------------------------------------------------

func TestInterfaceCompliance(t *testing.T) {
	var _ AMQPAPI = (*mockConn)(nil)
	var _ AMQPChannelAPI = (*mockChannel)(nil)
	var _ PublishConfirm = (*fakeConfirm)(nil)
	var _ Delivery = (*amqpDelivery)(nil)
}

// --- Ping ------------------------------------------------------------------

func TestPing_OK(t *testing.T) {
	conn := &mockConn{channelFn: func() (AMQPChannelAPI, error) { return &mockChannel{}, nil }}
	c := newClientWithAMQP(testConfig(), conn, nil)
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("Ping returned error: %v", err)
	}
}

func TestPing_NotConnected(t *testing.T) {
	c := newClientWithAMQP(testConfig(), nil, nil)
	err := c.Ping(context.Background())
	if !errors.Is(err, ErrNotConnected) {
		t.Fatalf("want ErrNotConnected, got %v", err)
	}
	if !IsRetryable(err) {
		t.Error("not-connected ping should be retryable")
	}
}

func TestPing_ConnectionClosed(t *testing.T) {
	conn := &mockConn{isClosedFn: func() bool { return true }}
	c := newClientWithAMQP(testConfig(), conn, nil)
	if err := c.Ping(context.Background()); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("want ErrNotConnected for closed conn, got %v", err)
	}
}

func TestPing_ContextCancelled(t *testing.T) {
	conn := &mockConn{channelFn: func() (AMQPChannelAPI, error) { return &mockChannel{}, nil }}
	c := newClientWithAMQP(testConfig(), conn, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.Ping(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

func TestPing_HonoursContextDeadlineOnBlockingChannel(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	conn := &mockConn{channelFn: func() (AMQPChannelAPI, error) {
		<-release // simulate half-open TCP: Channel() blocks indefinitely
		return &mockChannel{}, nil
	}}
	c := newClientWithAMQP(testConfig(), conn, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := c.Ping(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want context.DeadlineExceeded, got %v", err)
	}
	if time.Since(start) > time.Second {
		t.Error("Ping did not honour ctx deadline (blocked on Channel())")
	}
}

func TestPing_ReturnsChannelCloseError(t *testing.T) {
	ch := &mockChannel{closeFn: func() error { return &amqp.Error{Code: 504, Reason: "CHANNEL_ERROR"} }}
	conn := &mockConn{channelFn: func() (AMQPChannelAPI, error) { return ch, nil }}
	c := newClientWithAMQP(testConfig(), conn, nil)
	if err := c.Ping(context.Background()); err == nil {
		t.Fatal("a failing channel.close is a liveness signal — must surface")
	}
}

// --- Close -----------------------------------------------------------------

func TestClose_GracefulAndIdempotent(t *testing.T) {
	var connClosed, pubClosed bool
	var mu sync.Mutex

	pubCh := &mockChannel{closeFn: func() error {
		mu.Lock()
		pubClosed = true
		mu.Unlock()
		return nil
	}}
	conn := &mockConn{
		channelFn: func() (AMQPChannelAPI, error) { return &mockChannel{}, nil },
		closeFn: func() error {
			mu.Lock()
			connClosed = true
			mu.Unlock()
			return nil
		},
	}

	c := newClientWithAMQP(testConfig(), conn, nil)
	c.pubCh = pubCh

	if err := c.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("second Close should be a no-op nil, got %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if !pubClosed {
		t.Error("publish channel was not closed")
	}
	if !connClosed {
		t.Error("connection was not closed")
	}
}

func TestClose_CancelsHandlerContext(t *testing.T) {
	deliveryCh := make(chan amqp.Delivery, 1)
	cancelled := make(chan struct{}, 1)

	ch := &mockChannel{
		consumeFn: func(queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args amqp.Table) (<-chan amqp.Delivery, error) {
			return deliveryCh, nil
		},
	}
	conn := &mockConn{channelFn: func() (AMQPChannelAPI, error) { return ch, nil }}

	c := newClientWithAMQP(testConfig(), conn, nil)
	if err := c.Subscribe("q", func(ctx context.Context, d Delivery) error {
		<-ctx.Done()
		cancelled <- struct{}{}
		return ctx.Err()
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	deliveryCh <- amqp.Delivery{Acknowledger: &mockAcknowledger{}, Body: []byte("x")}
	time.Sleep(30 * time.Millisecond)

	go func() { _ = c.Close() }()

	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("handler context not cancelled on Close")
	}
}
