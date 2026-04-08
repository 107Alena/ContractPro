package broker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"contractpro/api-orchestrator/internal/infra/observability/logger"

	amqp "github.com/rabbitmq/amqp091-go"
)

// --- Mocks ---

type mockAMQPConn struct {
	channelFn     func() (AMQPChannelAPI, error)
	closeFn       func() error
	notifyCloseFn func(chan *amqp.Error) chan *amqp.Error
	isClosedFn    func() bool
}

func (m *mockAMQPConn) Channel() (AMQPChannelAPI, error) {
	if m.channelFn != nil {
		return m.channelFn()
	}
	return &mockAMQPChannel{}, nil
}

func (m *mockAMQPConn) Close() error {
	if m.closeFn != nil {
		return m.closeFn()
	}
	return nil
}

func (m *mockAMQPConn) NotifyClose(r chan *amqp.Error) chan *amqp.Error {
	if m.notifyCloseFn != nil {
		return m.notifyCloseFn(r)
	}
	return r
}

func (m *mockAMQPConn) IsClosed() bool {
	if m.isClosedFn != nil {
		return m.isClosedFn()
	}
	return false
}

type mockAMQPChannel struct {
	queueDeclareFn       func(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error)
	consumeFn            func(queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args amqp.Table) (<-chan amqp.Delivery, error)
	publishWithContextFn func(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp.Publishing) error
	confirmFn            func(noWait bool) error
	notifyPublishFn      func(confirm chan amqp.Confirmation) chan amqp.Confirmation
	qosFn                func(prefetchCount, prefetchSize int, global bool) error
	closeFn              func() error
	notifyCloseFn        func(chan *amqp.Error) chan *amqp.Error
}

func (m *mockAMQPChannel) QueueDeclare(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error) {
	if m.queueDeclareFn != nil {
		return m.queueDeclareFn(name, durable, autoDelete, exclusive, noWait, args)
	}
	return amqp.Queue{}, nil
}

func (m *mockAMQPChannel) Consume(queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args amqp.Table) (<-chan amqp.Delivery, error) {
	if m.consumeFn != nil {
		return m.consumeFn(queue, consumer, autoAck, exclusive, noLocal, noWait, args)
	}
	ch := make(chan amqp.Delivery)
	return ch, nil
}

func (m *mockAMQPChannel) PublishWithContext(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp.Publishing) error {
	if m.publishWithContextFn != nil {
		return m.publishWithContextFn(ctx, exchange, key, mandatory, immediate, msg)
	}
	return nil
}

func (m *mockAMQPChannel) Confirm(noWait bool) error {
	if m.confirmFn != nil {
		return m.confirmFn(noWait)
	}
	return nil
}

func (m *mockAMQPChannel) NotifyPublish(confirm chan amqp.Confirmation) chan amqp.Confirmation {
	if m.notifyPublishFn != nil {
		return m.notifyPublishFn(confirm)
	}
	return confirm
}

func (m *mockAMQPChannel) Qos(prefetchCount, prefetchSize int, global bool) error {
	if m.qosFn != nil {
		return m.qosFn(prefetchCount, prefetchSize, global)
	}
	return nil
}

func (m *mockAMQPChannel) Close() error {
	if m.closeFn != nil {
		return m.closeFn()
	}
	return nil
}

func (m *mockAMQPChannel) NotifyClose(r chan *amqp.Error) chan *amqp.Error {
	if m.notifyCloseFn != nil {
		return m.notifyCloseFn(r)
	}
	return r
}

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

// --- Interface compliance ---

var (
	_ AMQPAPI        = (*mockAMQPConn)(nil)
	_ AMQPChannelAPI = (*mockAMQPChannel)(nil)
)

// testLogger creates a logger that writes to a buffer for test inspection.
func testLogger() *logger.Logger {
	return logger.NewLogger("debug")
}

// --- Publish tests ---

func TestPublish_Success(t *testing.T) {
	confirmCh := make(chan amqp.Confirmation, 1)
	var capturedKey string
	var capturedBody []byte
	var capturedContentType string
	var capturedDeliveryMode uint8

	ch := &mockAMQPChannel{
		publishWithContextFn: func(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp.Publishing) error {
			capturedKey = key
			capturedBody = msg.Body
			capturedContentType = msg.ContentType
			capturedDeliveryMode = msg.DeliveryMode
			confirmCh <- amqp.Confirmation{Ack: true}
			return nil
		},
		notifyPublishFn: func(confirm chan amqp.Confirmation) chan amqp.Confirmation {
			return confirmCh
		},
	}

	conn := &mockAMQPConn{
		channelFn: func() (AMQPChannelAPI, error) { return ch, nil },
	}

	c := newClientWithAMQP(conn, nil, 10, testLogger())
	defer c.Close()

	err := c.Publish(context.Background(), "test.topic", []byte(`{"key":"value"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedKey != "test.topic" {
		t.Errorf("expected topic test.topic, got %s", capturedKey)
	}
	if string(capturedBody) != `{"key":"value"}` {
		t.Errorf("unexpected body: %s", capturedBody)
	}
	if capturedContentType != "application/json" {
		t.Errorf("expected content-type application/json, got %s", capturedContentType)
	}
	if capturedDeliveryMode != amqp.Persistent {
		t.Errorf("expected persistent delivery mode, got %d", capturedDeliveryMode)
	}
}

func TestPublish_ConfirmNack(t *testing.T) {
	confirmCh := make(chan amqp.Confirmation, 1)

	ch := &mockAMQPChannel{
		publishWithContextFn: func(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp.Publishing) error {
			confirmCh <- amqp.Confirmation{Ack: false}
			return nil
		},
		notifyPublishFn: func(confirm chan amqp.Confirmation) chan amqp.Confirmation {
			return confirmCh
		},
	}

	conn := &mockAMQPConn{
		channelFn: func() (AMQPChannelAPI, error) { return ch, nil },
	}

	c := newClientWithAMQP(conn, nil, 10, testLogger())
	defer c.Close()

	err := c.Publish(context.Background(), "test.topic", []byte(`{}`))
	if err == nil {
		t.Fatal("expected error for nacked publish")
	}
	// ErrPublishNacked is retryable, so after 3 retries we get ErrPublishMaxRetriesExhausted.
	if !errors.Is(err, ErrPublishMaxRetriesExhausted) {
		t.Errorf("expected ErrPublishMaxRetriesExhausted, got: %v", err)
	}
}

func TestPublish_ConfirmChannelClosed(t *testing.T) {
	confirmCh := make(chan amqp.Confirmation, 1)
	close(confirmCh) // simulate connection loss

	ch := &mockAMQPChannel{
		publishWithContextFn: func(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp.Publishing) error {
			return nil
		},
		notifyPublishFn: func(confirm chan amqp.Confirmation) chan amqp.Confirmation {
			return confirmCh
		},
	}

	conn := &mockAMQPConn{
		channelFn: func() (AMQPChannelAPI, error) { return ch, nil },
	}

	c := newClientWithAMQP(conn, nil, 10, testLogger())
	defer c.Close()

	err := c.Publish(context.Background(), "test.topic", []byte(`{}`))
	if err == nil {
		t.Fatal("expected error for closed confirm channel")
	}
	// ErrPublishConfirmFailed is retryable, so after retries we get max retries exhausted.
	if !errors.Is(err, ErrPublishMaxRetriesExhausted) {
		t.Errorf("expected ErrPublishMaxRetriesExhausted, got: %v", err)
	}
}

func TestPublish_RetryOnTransientError(t *testing.T) {
	attempts := 0
	confirmCh := make(chan amqp.Confirmation, 1)

	ch := &mockAMQPChannel{
		publishWithContextFn: func(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp.Publishing) error {
			attempts++
			if attempts == 1 {
				return errors.New("connection reset")
			}
			confirmCh <- amqp.Confirmation{Ack: true}
			return nil
		},
		notifyPublishFn: func(confirm chan amqp.Confirmation) chan amqp.Confirmation {
			return confirmCh
		},
	}

	conn := &mockAMQPConn{
		channelFn: func() (AMQPChannelAPI, error) { return ch, nil },
	}

	c := newClientWithAMQP(conn, nil, 10, testLogger())
	defer c.Close()

	err := c.Publish(context.Background(), "test.topic", []byte(`{}`))
	if err != nil {
		t.Fatalf("unexpected error after retry: %v", err)
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}
}

func TestPublish_NoRetryOnNonRetryable(t *testing.T) {
	attempts := 0

	ch := &mockAMQPChannel{
		publishWithContextFn: func(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp.Publishing) error {
			attempts++
			return &amqp.Error{Code: 404, Reason: "NOT_FOUND"}
		},
		notifyPublishFn: func(confirm chan amqp.Confirmation) chan amqp.Confirmation {
			return make(chan amqp.Confirmation, 1)
		},
	}

	conn := &mockAMQPConn{
		channelFn: func() (AMQPChannelAPI, error) { return ch, nil },
	}

	c := newClientWithAMQP(conn, nil, 10, testLogger())
	defer c.Close()

	err := c.Publish(context.Background(), "test.topic", []byte(`{}`))
	if err == nil {
		t.Fatal("expected error for non-retryable AMQP error")
	}
	if attempts != 1 {
		t.Errorf("expected 1 attempt for non-retryable error, got %d", attempts)
	}
	var brokerErr *BrokerError
	if !errors.As(err, &brokerErr) {
		t.Fatalf("expected BrokerError, got %T", err)
	}
	if brokerErr.Retryable {
		t.Error("expected non-retryable error")
	}
}

func TestPublish_MaxRetriesExhausted(t *testing.T) {
	attempts := 0

	ch := &mockAMQPChannel{
		publishWithContextFn: func(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp.Publishing) error {
			attempts++
			return errors.New("connection closed")
		},
		notifyPublishFn: func(confirm chan amqp.Confirmation) chan amqp.Confirmation {
			return make(chan amqp.Confirmation, 1)
		},
	}

	conn := &mockAMQPConn{
		channelFn: func() (AMQPChannelAPI, error) { return ch, nil },
	}

	c := newClientWithAMQP(conn, nil, 10, testLogger())
	defer c.Close()

	err := c.Publish(context.Background(), "test.topic", []byte(`{}`))
	if !errors.Is(err, ErrPublishMaxRetriesExhausted) {
		t.Fatalf("expected ErrPublishMaxRetriesExhausted, got: %v", err)
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestPublish_ContextCancelled(t *testing.T) {
	ch := &mockAMQPChannel{
		publishWithContextFn: func(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp.Publishing) error {
			return errors.New("transient error")
		},
		notifyPublishFn: func(confirm chan amqp.Confirmation) chan amqp.Confirmation {
			return make(chan amqp.Confirmation, 1)
		},
	}

	conn := &mockAMQPConn{
		channelFn: func() (AMQPChannelAPI, error) { return ch, nil },
	}

	c := newClientWithAMQP(conn, nil, 10, testLogger())
	defer c.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := c.Publish(ctx, "test.topic", []byte(`{}`))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}
}

func TestPublish_NotConnected(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c := &Client{
		done:      make(chan struct{}),
		log:       testLogger().With("component", "broker"),
		pubCh:     nil,
		cancelCtx: ctx,
		cancelFn:  cancel,
	}

	err := c.Publish(context.Background(), "test.topic", []byte(`{}`))
	// ErrNotConnected is retryable, so after 3 retries we get ErrPublishMaxRetriesExhausted.
	if !errors.Is(err, ErrPublishMaxRetriesExhausted) {
		t.Fatalf("expected ErrPublishMaxRetriesExhausted, got: %v", err)
	}
}

func TestPublish_AfterClose(t *testing.T) {
	ch := &mockAMQPChannel{}
	conn := &mockAMQPConn{
		channelFn: func() (AMQPChannelAPI, error) { return ch, nil },
	}

	c := newClientWithAMQP(conn, nil, 10, testLogger())
	_ = c.Close()

	err := c.Publish(context.Background(), "test.topic", []byte(`{}`))
	if !errors.Is(err, ErrClientClosed) && !errors.Is(err, ErrNotConnected) {
		t.Fatalf("expected ErrClientClosed or ErrNotConnected, got: %v", err)
	}
}

func TestPublish_ContextErrorPassthrough(t *testing.T) {
	ch := &mockAMQPChannel{
		publishWithContextFn: func(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp.Publishing) error {
			return context.DeadlineExceeded
		},
		notifyPublishFn: func(confirm chan amqp.Confirmation) chan amqp.Confirmation {
			return make(chan amqp.Confirmation, 1)
		},
	}

	conn := &mockAMQPConn{
		channelFn: func() (AMQPChannelAPI, error) { return ch, nil },
	}

	c := newClientWithAMQP(conn, nil, 10, testLogger())
	defer c.Close()

	err := c.Publish(context.Background(), "test.topic", []byte(`{}`))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got: %v", err)
	}
}

// --- Subscribe tests ---

func TestSubscribe_Success(t *testing.T) {
	var declaredQueue string
	var declaredDurable bool
	var prefetchSet int

	ch := &mockAMQPChannel{
		qosFn: func(prefetchCount, prefetchSize int, global bool) error {
			prefetchSet = prefetchCount
			return nil
		},
		queueDeclareFn: func(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error) {
			declaredQueue = name
			declaredDurable = durable
			return amqp.Queue{}, nil
		},
	}

	conn := &mockAMQPConn{
		channelFn: func() (AMQPChannelAPI, error) { return ch, nil },
	}

	c := newClientWithAMQP(conn, nil, 15, testLogger())
	defer c.Close()

	err := c.Subscribe("dp.events.status-changed", func(ctx context.Context, body []byte) error {
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if declaredQueue != "orch.sub.dp.events.status-changed" {
		t.Errorf("expected queue orch.sub.dp.events.status-changed, got %s", declaredQueue)
	}
	if !declaredDurable {
		t.Error("expected durable queue")
	}
	if prefetchSet != 15 {
		t.Errorf("expected prefetch 15, got %d", prefetchSet)
	}
}

func TestSubscribe_QueueDeclareError(t *testing.T) {
	ch := &mockAMQPChannel{
		queueDeclareFn: func(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error) {
			return amqp.Queue{}, &amqp.Error{Code: 406, Reason: "PRECONDITION_FAILED"}
		},
	}

	conn := &mockAMQPConn{
		channelFn: func() (AMQPChannelAPI, error) { return ch, nil },
	}

	c := newClientWithAMQP(conn, nil, 10, testLogger())
	defer c.Close()

	err := c.Subscribe("test.topic", func(ctx context.Context, body []byte) error {
		return nil
	})
	if err == nil {
		t.Fatal("expected error for queue declare failure")
	}
	var brokerErr *BrokerError
	if !errors.As(err, &brokerErr) {
		t.Fatalf("expected BrokerError, got %T", err)
	}
	if brokerErr.Retryable {
		t.Error("expected non-retryable error for 406")
	}
}

func TestSubscribe_QosError(t *testing.T) {
	ch := &mockAMQPChannel{
		qosFn: func(prefetchCount, prefetchSize int, global bool) error {
			return errors.New("qos error")
		},
	}

	conn := &mockAMQPConn{
		channelFn: func() (AMQPChannelAPI, error) { return ch, nil },
	}

	c := newClientWithAMQP(conn, nil, 10, testLogger())
	defer c.Close()

	err := c.Subscribe("test.topic", func(ctx context.Context, body []byte) error {
		return nil
	})
	if err == nil {
		t.Fatal("expected error for QoS failure")
	}
}

func TestSubscribe_NotConnected(t *testing.T) {
	c := &Client{
		done: make(chan struct{}),
		log:  testLogger().With("component", "broker"),
		conn: nil,
	}

	err := c.Subscribe("test.topic", func(ctx context.Context, body []byte) error {
		return nil
	})
	if !errors.Is(err, ErrNotConnected) {
		t.Fatalf("expected ErrNotConnected, got: %v", err)
	}
}

func TestSubscribe_HandlerAck(t *testing.T) {
	deliveryCh := make(chan amqp.Delivery, 1)
	acked := make(chan bool, 1)

	ch := &mockAMQPChannel{
		consumeFn: func(queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args amqp.Table) (<-chan amqp.Delivery, error) {
			return deliveryCh, nil
		},
	}

	conn := &mockAMQPConn{
		channelFn: func() (AMQPChannelAPI, error) { return ch, nil },
	}

	c := newClientWithAMQP(conn, nil, 10, testLogger())
	defer c.Close()

	handled := make(chan []byte, 1)
	err := c.Subscribe("test.topic", func(ctx context.Context, body []byte) error {
		handled <- body
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	deliveryCh <- amqp.Delivery{
		Body: []byte(`{"test":true}`),
		Acknowledger: &mockAcknowledger{
			ackFn: func(tag uint64, multiple bool) error {
				acked <- true
				return nil
			},
		},
	}

	select {
	case body := <-handled:
		if string(body) != `{"test":true}` {
			t.Errorf("unexpected body: %s", body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler not called within timeout")
	}

	select {
	case <-acked:
	case <-time.After(2 * time.Second):
		t.Fatal("message not acked within timeout")
	}
}

func TestSubscribe_HandlerError_Nacks(t *testing.T) {
	deliveryCh := make(chan amqp.Delivery, 1)
	nacked := make(chan bool, 1)
	var requeueValue bool

	ch := &mockAMQPChannel{
		consumeFn: func(queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args amqp.Table) (<-chan amqp.Delivery, error) {
			return deliveryCh, nil
		},
	}

	conn := &mockAMQPConn{
		channelFn: func() (AMQPChannelAPI, error) { return ch, nil },
	}

	c := newClientWithAMQP(conn, nil, 10, testLogger())
	defer c.Close()

	err := c.Subscribe("test.topic", func(ctx context.Context, body []byte) error {
		return errors.New("processing failed")
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	deliveryCh <- amqp.Delivery{
		Body: []byte(`{}`),
		Acknowledger: &mockAcknowledger{
			nackFn: func(tag uint64, multiple, requeue bool) error {
				requeueValue = requeue
				nacked <- true
				return nil
			},
		},
	}

	select {
	case <-nacked:
		if !requeueValue {
			t.Error("expected requeue=true on handler error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("message not nacked within timeout")
	}
}

func TestSubscribe_ManualAckNotAutoAck(t *testing.T) {
	var capturedAutoAck bool

	ch := &mockAMQPChannel{
		consumeFn: func(queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args amqp.Table) (<-chan amqp.Delivery, error) {
			capturedAutoAck = autoAck
			return make(chan amqp.Delivery), nil
		},
	}

	conn := &mockAMQPConn{
		channelFn: func() (AMQPChannelAPI, error) { return ch, nil },
	}

	c := newClientWithAMQP(conn, nil, 10, testLogger())
	defer c.Close()

	err := c.Subscribe("test.topic", func(ctx context.Context, body []byte) error { return nil })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedAutoAck {
		t.Error("expected autoAck=false (manual ACK)")
	}
}

// --- Ping tests ---

func TestPing_Connected(t *testing.T) {
	conn := &mockAMQPConn{
		isClosedFn: func() bool { return false },
	}

	c := newClientWithAMQP(conn, nil, 10, testLogger())
	defer c.Close()

	if err := c.Ping(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPing_Disconnected(t *testing.T) {
	conn := &mockAMQPConn{
		isClosedFn: func() bool { return true },
	}

	c := newClientWithAMQP(conn, nil, 10, testLogger())
	defer c.Close()

	err := c.Ping()
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
	if !errors.Is(err, ErrClientClosed) {
		t.Errorf("expected ErrClientClosed, got: %v", err)
	}
}

func TestPing_NilConn(t *testing.T) {
	c := &Client{
		done: make(chan struct{}),
		log:  testLogger().With("component", "broker"),
		conn: nil,
	}

	err := c.Ping()
	if !errors.Is(err, ErrNotConnected) {
		t.Fatalf("expected ErrNotConnected, got: %v", err)
	}
}

// --- Close tests ---

func TestClose_GracefulShutdown(t *testing.T) {
	pubChClosed := false
	connClosed := false

	ch := &mockAMQPChannel{
		closeFn: func() error {
			pubChClosed = true
			return nil
		},
	}

	conn := &mockAMQPConn{
		channelFn: func() (AMQPChannelAPI, error) { return ch, nil },
		closeFn: func() error {
			connClosed = true
			return nil
		},
	}

	c := newClientWithAMQP(conn, nil, 10, testLogger())

	err := c.Close()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !pubChClosed {
		t.Error("expected publish channel to be closed")
	}
	if !connClosed {
		t.Error("expected connection to be closed")
	}
}

func TestClose_Idempotent(t *testing.T) {
	ch := &mockAMQPChannel{}
	conn := &mockAMQPConn{
		channelFn: func() (AMQPChannelAPI, error) { return ch, nil },
	}

	c := newClientWithAMQP(conn, nil, 10, testLogger())

	err1 := c.Close()
	err2 := c.Close()

	if err1 != nil {
		t.Fatalf("first close error: %v", err1)
	}
	if err2 != nil {
		t.Fatalf("second close error: %v", err2)
	}
}

func TestClose_CancelsHandlerContext(t *testing.T) {
	deliveryCh := make(chan amqp.Delivery, 1)
	handlerDone := make(chan struct{})

	ch := &mockAMQPChannel{
		consumeFn: func(queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args amqp.Table) (<-chan amqp.Delivery, error) {
			return deliveryCh, nil
		},
	}

	conn := &mockAMQPConn{
		channelFn: func() (AMQPChannelAPI, error) { return ch, nil },
	}

	c := newClientWithAMQP(conn, nil, 10, testLogger())

	err := c.Subscribe("test.topic", func(ctx context.Context, body []byte) error {
		<-ctx.Done()
		close(handlerDone)
		return ctx.Err()
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	deliveryCh <- amqp.Delivery{
		Body:         []byte(`{}`),
		Acknowledger: &mockAcknowledger{},
	}

	// Give the handler time to start blocking.
	time.Sleep(50 * time.Millisecond)

	_ = c.Close()

	select {
	case <-handlerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("handler context was not cancelled")
	}
}

// --- Error mapping tests ---

func TestMapError_AMQPRetryable(t *testing.T) {
	err := mapError(&amqp.Error{Code: 504, Reason: "CHANNEL_ERROR"}, "Publish")
	var brokerErr *BrokerError
	if !errors.As(err, &brokerErr) {
		t.Fatalf("expected BrokerError, got %T", err)
	}
	if !brokerErr.Retryable {
		t.Error("expected retryable error for 504")
	}
}

func TestMapError_AMQPNonRetryable(t *testing.T) {
	codes := []int{404, 403, 406}
	for _, code := range codes {
		err := mapError(&amqp.Error{Code: code, Reason: "test"}, "Op")
		var brokerErr *BrokerError
		if !errors.As(err, &brokerErr) {
			t.Fatalf("expected BrokerError for code %d", code)
		}
		if brokerErr.Retryable {
			t.Errorf("expected non-retryable for code %d", code)
		}
	}
}

func TestMapError_ContextErrors(t *testing.T) {
	err1 := mapError(context.Canceled, "Op")
	if !errors.Is(err1, context.Canceled) {
		t.Errorf("expected context.Canceled passthrough, got: %v", err1)
	}

	err2 := mapError(context.DeadlineExceeded, "Op")
	if !errors.Is(err2, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded passthrough, got: %v", err2)
	}
}

func TestMapError_UnknownError(t *testing.T) {
	err := mapError(errors.New("network error"), "Subscribe")
	var brokerErr *BrokerError
	if !errors.As(err, &brokerErr) {
		t.Fatalf("expected BrokerError, got %T", err)
	}
	if !brokerErr.Retryable {
		t.Error("expected retryable for unknown error")
	}
	if brokerErr.Operation != "Subscribe" {
		t.Errorf("expected operation Subscribe, got %s", brokerErr.Operation)
	}
}

// --- IsRetryable tests ---

func TestIsRetryable_BrokerError(t *testing.T) {
	retryable := &BrokerError{Retryable: true}
	nonRetryable := &BrokerError{Retryable: false}

	if !isRetryable(retryable) {
		t.Error("expected retryable")
	}
	if isRetryable(nonRetryable) {
		t.Error("expected non-retryable")
	}
}

func TestIsRetryable_SentinelErrors(t *testing.T) {
	cases := []struct {
		name      string
		err       error
		retryable bool
	}{
		{"ErrPublishConfirmFailed", ErrPublishConfirmFailed, true},
		{"ErrNotConnected", ErrNotConnected, true},
		{"ErrPublishNacked", ErrPublishNacked, true},
		{"ErrClientClosed", ErrClientClosed, false},
		{"unknown error", errors.New("unknown"), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isRetryable(tc.err); got != tc.retryable {
				t.Errorf("isRetryable(%v) = %v, want %v", tc.err, got, tc.retryable)
			}
		})
	}
}

// --- BackoffDelay tests ---

func TestBackoffDelay_ExponentialGrowth(t *testing.T) {
	prev := time.Duration(0)
	for attempt := 0; attempt < 5; attempt++ {
		d := backoffDelay(attempt)
		if attempt > 0 && d <= prev/2 {
			t.Errorf("attempt %d delay %v not growing from previous %v", attempt, d, prev)
		}
		prev = d
	}
}

func TestBackoffDelay_CapsAtMax(t *testing.T) {
	d := backoffDelay(100) // very high attempt
	maxWithJitter := reconnectMaxDelay + time.Duration(float64(reconnectMaxDelay)*jitterFraction)
	if d > maxWithJitter {
		t.Errorf("delay %v exceeds max with jitter %v", d, maxWithJitter)
	}
}

func TestBackoffDelay_NeverNegative(t *testing.T) {
	for i := 0; i < 100; i++ {
		d := backoffDelay(i % 10)
		if d < 0 {
			t.Fatalf("negative delay at attempt %d: %v", i, d)
		}
	}
}

// --- Reconnect tests ---

func TestReconnect_ReestablishesConfirmMode(t *testing.T) {
	confirmCalled := false
	newCh := &mockAMQPChannel{
		confirmFn: func(noWait bool) error {
			confirmCalled = true
			return nil
		},
	}

	newConn := &mockAMQPConn{
		channelFn: func() (AMQPChannelAPI, error) { return newCh, nil },
	}

	dialCount := 0
	dialFn := func(addr string) (AMQPAPI, error) {
		dialCount++
		return newConn, nil
	}

	c := &Client{
		address:  "amqp://test",
		prefetch: 10,
		done:     make(chan struct{}),
		dialFn:   dialFn,
		log:      testLogger().With("component", "broker"),
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.cancelCtx = ctx
	c.cancelFn = cancel

	c.reconnectWithBackoff()

	if !confirmCalled {
		t.Error("expected Confirm() to be called on new channel")
	}
	if c.pubCh == nil {
		t.Error("expected pubCh to be set after reconnect")
	}
	if c.pubNotify == nil {
		t.Error("expected pubNotify to be set after reconnect")
	}
}

func TestReconnect_ResubscribesAll(t *testing.T) {
	var subscribedQueues []string
	var mu sync.Mutex

	newCh := &mockAMQPChannel{
		queueDeclareFn: func(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error) {
			mu.Lock()
			subscribedQueues = append(subscribedQueues, name)
			mu.Unlock()
			return amqp.Queue{}, nil
		},
	}

	newConn := &mockAMQPConn{
		channelFn: func() (AMQPChannelAPI, error) { return newCh, nil },
	}

	dialFn := func(addr string) (AMQPAPI, error) {
		return newConn, nil
	}

	c := &Client{
		address:  "amqp://test",
		prefetch: 10,
		done:     make(chan struct{}),
		dialFn:   dialFn,
		subs: []subscription{
			{topic: "dp.events.status-changed", queue: "orch.sub.dp.events.status-changed", handler: func(ctx context.Context, body []byte) error { return nil }},
			{topic: "lic.events.status-changed", queue: "orch.sub.lic.events.status-changed", handler: func(ctx context.Context, body []byte) error { return nil }},
		},
		log: testLogger().With("component", "broker"),
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.cancelCtx = ctx
	c.cancelFn = cancel

	c.reconnectWithBackoff()

	mu.Lock()
	defer mu.Unlock()

	if len(subscribedQueues) != 2 {
		t.Fatalf("expected 2 re-subscriptions, got %d", len(subscribedQueues))
	}
}

func TestReconnect_StopsOnDone(t *testing.T) {
	dialFn := func(addr string) (AMQPAPI, error) {
		return nil, errors.New("connection refused")
	}

	c := &Client{
		address: "amqp://test",
		done:    make(chan struct{}),
		dialFn:  dialFn,
		log:     testLogger().With("component", "broker"),
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.cancelCtx = ctx
	c.cancelFn = cancel

	// Close after a short delay.
	go func() {
		time.Sleep(100 * time.Millisecond)
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
		t.Fatal("reconnectWithBackoff did not stop after done was closed")
	}
}

func TestReconnect_DialFailRetries(t *testing.T) {
	dialAttempts := 0
	newCh := &mockAMQPChannel{}
	newConn := &mockAMQPConn{
		channelFn: func() (AMQPChannelAPI, error) { return newCh, nil },
	}

	dialFn := func(addr string) (AMQPAPI, error) {
		dialAttempts++
		if dialAttempts < 3 {
			return nil, errors.New("connection refused")
		}
		return newConn, nil
	}

	c := &Client{
		address:  "amqp://test",
		prefetch: 10,
		done:     make(chan struct{}),
		dialFn:   dialFn,
		log:      testLogger().With("component", "broker"),
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.cancelCtx = ctx
	c.cancelFn = cancel

	c.reconnectWithBackoff()

	if dialAttempts < 3 {
		t.Errorf("expected at least 3 dial attempts, got %d", dialAttempts)
	}
}

// --- Queue prefix tests ---

func TestSubscribe_QueueNamePrefix(t *testing.T) {
	topics := []struct {
		topic    string
		expected string
	}{
		{"dp.events.status-changed", "orch.sub.dp.events.status-changed"},
		{"dp.commands.process-document", "orch.sub.dp.commands.process-document"},
		{"dm.events.version-created", "orch.sub.dm.events.version-created"},
	}

	for _, tc := range topics {
		t.Run(tc.topic, func(t *testing.T) {
			var declaredQueue string
			ch := &mockAMQPChannel{
				queueDeclareFn: func(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error) {
					declaredQueue = name
					return amqp.Queue{}, nil
				},
			}

			conn := &mockAMQPConn{
				channelFn: func() (AMQPChannelAPI, error) { return ch, nil },
			}

			c := newClientWithAMQP(conn, nil, 10, testLogger())
			defer c.Close()

			err := c.Subscribe(tc.topic, func(ctx context.Context, body []byte) error { return nil })
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if declaredQueue != tc.expected {
				t.Errorf("expected queue %s, got %s", tc.expected, declaredQueue)
			}
		})
	}
}

// --- BrokerError tests ---

func TestBrokerError_Error(t *testing.T) {
	err := &BrokerError{
		Operation: "Publish",
		Message:   "504 CHANNEL_ERROR",
		Cause:     errors.New("underlying"),
	}
	s := err.Error()
	if s != "broker: Publish: 504 CHANNEL_ERROR: underlying" {
		t.Errorf("unexpected error string: %s", s)
	}
}

func TestBrokerError_ErrorNoCause(t *testing.T) {
	err := &BrokerError{
		Operation: "Subscribe",
		Message:   "not connected",
	}
	s := err.Error()
	if s != "broker: Subscribe: not connected" {
		t.Errorf("unexpected error string: %s", s)
	}
}

func TestBrokerError_Unwrap(t *testing.T) {
	cause := errors.New("root cause")
	err := &BrokerError{Cause: cause}
	if !errors.Is(err, cause) {
		t.Error("expected Unwrap to return the cause")
	}
}

// --- Concurrent access test ---

func TestPublish_ConcurrentSafety(t *testing.T) {
	confirmCh := make(chan amqp.Confirmation, 10)

	ch := &mockAMQPChannel{
		publishWithContextFn: func(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp.Publishing) error {
			confirmCh <- amqp.Confirmation{Ack: true}
			return nil
		},
		notifyPublishFn: func(confirm chan amqp.Confirmation) chan amqp.Confirmation {
			return confirmCh
		},
	}

	conn := &mockAMQPConn{
		channelFn: func() (AMQPChannelAPI, error) { return ch, nil },
	}

	c := newClientWithAMQP(conn, nil, 10, testLogger())
	defer c.Close()

	var wg sync.WaitGroup
	errs := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := c.Publish(context.Background(), "test.topic", []byte(`{}`)); err != nil {
				errs <- err
			}
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("unexpected error in concurrent publish: %v", err)
	}
}

