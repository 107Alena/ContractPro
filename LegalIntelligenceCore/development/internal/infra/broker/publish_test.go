package broker

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

func clientWithPubChannel(t *testing.T, ch *mockChannel) *Client {
	t.Helper()
	conn := &mockConn{channelFn: func() (AMQPChannelAPI, error) { return ch, nil }}
	c := newClientWithAMQP(testConfig(), conn, nil)
	c.pubCh = ch
	return c
}

func TestPublish_ConfirmAck_Success(t *testing.T) {
	var gotExchange, gotKey string
	var gotBody []byte
	var gotCT string
	ch := &mockChannel{
		publishFn: func(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp.Publishing) (PublishConfirm, error) {
			gotExchange, gotKey, gotBody, gotCT = exchange, key, msg.Body, msg.ContentType
			if mandatory {
				t.Error("mandatory must be false")
			}
			return ackedConfirm(), nil
		},
	}
	c := clientWithPubChannel(t, ch)

	err := c.Publish(context.Background(), "contractpro.events", "lic.events.status-changed", []byte(`{"status":"IN_PROGRESS"}`))
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if gotExchange != "contractpro.events" || gotKey != "lic.events.status-changed" {
		t.Errorf("routed to %q/%q", gotExchange, gotKey)
	}
	if string(gotBody) != `{"status":"IN_PROGRESS"}` {
		t.Errorf("body = %q", gotBody)
	}
	if gotCT != "application/json" {
		t.Errorf("content-type = %q, want application/json", gotCT)
	}
}

func TestPublish_NackThenExhausted(t *testing.T) {
	var attempts int32
	ch := &mockChannel{
		publishFn: func(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp.Publishing) (PublishConfirm, error) {
			atomic.AddInt32(&attempts, 1)
			return nackedConfirm(), nil
		},
	}
	c := clientWithPubChannel(t, ch)

	err := c.Publish(context.Background(), "x", "y", []byte("p"))
	if !errors.Is(err, ErrPublishNack) {
		t.Fatalf("want ErrPublishNack, got %v", err)
	}
	if !IsRetryable(err) {
		t.Error("nack should be retryable")
	}
	if got := atomic.LoadInt32(&attempts); got != maxPublishAttempts {
		t.Errorf("attempts = %d, want %d", got, maxPublishAttempts)
	}
}

func TestPublish_RetryThenSuccess(t *testing.T) {
	var attempts int32
	ch := &mockChannel{
		publishFn: func(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp.Publishing) (PublishConfirm, error) {
			if atomic.AddInt32(&attempts, 1) == 1 {
				return nackedConfirm(), nil
			}
			return ackedConfirm(), nil
		},
	}
	c := clientWithPubChannel(t, ch)

	if err := c.Publish(context.Background(), "x", "y", []byte("p")); err != nil {
		t.Fatalf("Publish should succeed on retry: %v", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 2 {
		t.Errorf("attempts = %d, want 2", got)
	}
}

func TestPublish_ConfirmTimeout(t *testing.T) {
	ch := &mockChannel{
		publishFn: func(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp.Publishing) (PublishConfirm, error) {
			return neverConfirm(), nil
		},
	}
	c := clientWithPubChannel(t, ch)
	// testConfig sets PublisherConfirmTimeout=200ms; 3 attempts + backoff.
	start := time.Now()
	err := c.Publish(context.Background(), "x", "y", []byte("p"))
	if !errors.Is(err, ErrConfirmTimeout) {
		t.Fatalf("want ErrConfirmTimeout, got %v", err)
	}
	if time.Since(start) < 200*time.Millisecond {
		t.Error("should have waited at least one confirm timeout")
	}
}

func TestPublish_ContextCancelled_PassThroughRaw(t *testing.T) {
	ch := &mockChannel{
		publishFn: func(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp.Publishing) (PublishConfirm, error) {
			return neverConfirm(), nil
		},
	}
	c := clientWithPubChannel(t, ch)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	err := c.Publish(ctx, "x", "y", []byte("p"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
	var be *BrokerError
	if errors.As(err, &be) {
		t.Error("context error must pass through raw, not wrapped in BrokerError")
	}
}

func TestPublish_NotConnected(t *testing.T) {
	c := newClientWithAMQP(testConfig(), nil, nil) // pubCh nil
	err := c.Publish(context.Background(), "x", "y", []byte("p"))
	if !errors.Is(err, ErrNotConnected) {
		t.Fatalf("want ErrNotConnected, got %v", err)
	}
}

func TestPublish_NonRetryableAMQPError_FailsFast(t *testing.T) {
	var attempts int32
	ch := &mockChannel{
		publishFn: func(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp.Publishing) (PublishConfirm, error) {
			atomic.AddInt32(&attempts, 1)
			return nil, &amqp.Error{Code: 403, Reason: "ACCESS_REFUSED"}
		},
	}
	c := clientWithPubChannel(t, ch)

	err := c.Publish(context.Background(), "x", "y", []byte("p"))
	if err == nil {
		t.Fatal("expected error")
	}
	if IsRetryable(err) {
		t.Error("403 ACCESS_REFUSED must be non-retryable")
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Errorf("attempts = %d, want 1 (fail fast, no retry)", got)
	}
}

// Close must NOT be stalled by an in-flight Publish: publish serialization
// is on pubMu, not c.mu, so a slow/dead broker cannot delay graceful
// shutdown for ~3×(confirmTimeout+backoff) (code-reviewer S1/S2).
func TestClose_NotStalledByInflightPublish(t *testing.T) {
	ch := &mockChannel{
		publishFn: func(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp.Publishing) (PublishConfirm, error) {
			return neverConfirm(), nil
		},
	}
	conn := &mockConn{channelFn: func() (AMQPChannelAPI, error) { return ch, nil }}
	c := newClientWithAMQP(testConfig(), conn, nil)
	c.pubCh = ch

	pubErr := make(chan error, 1)
	go func() { pubErr <- c.Publish(context.Background(), "x", "y", []byte("p")) }()
	time.Sleep(30 * time.Millisecond) // let the publish reach waitConfirm

	start := time.Now()
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("Close stalled %v behind in-flight publish", elapsed)
	}

	select {
	case err := <-pubErr:
		if !errors.Is(err, ErrNotConnected) {
			t.Errorf("publish should abort with ErrNotConnected on Close, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("in-flight publish did not unblock on Close")
	}
}

func TestPublish_ClientClosed(t *testing.T) {
	ch := &mockChannel{
		publishFn: func(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp.Publishing) (PublishConfirm, error) {
			return neverConfirm(), nil
		},
	}
	c := clientWithPubChannel(t, ch)
	go func() {
		time.Sleep(50 * time.Millisecond)
		close(c.done)
	}()
	err := c.Publish(context.Background(), "x", "y", []byte("p"))
	if !errors.Is(err, ErrNotConnected) {
		t.Fatalf("want ErrNotConnected after Close, got %v", err)
	}
}
