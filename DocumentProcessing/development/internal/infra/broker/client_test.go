package broker

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"contractpro/document-processing/internal/domain/port"
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

func (m *mockAMQPConn) NotifyClose(receiver chan *amqp.Error) chan *amqp.Error {
	if m.notifyCloseFn != nil {
		return m.notifyCloseFn(receiver)
	}
	return receiver
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
	closeFn              func() error
	notifyCloseFn        func(chan *amqp.Error) chan *amqp.Error
}

func (m *mockAMQPChannel) QueueDeclare(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error) {
	if m.queueDeclareFn != nil {
		return m.queueDeclareFn(name, durable, autoDelete, exclusive, noWait, args)
	}
	return amqp.Queue{Name: name}, nil
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

func (m *mockAMQPChannel) Close() error {
	if m.closeFn != nil {
		return m.closeFn()
	}
	return nil
}

func (m *mockAMQPChannel) NotifyClose(receiver chan *amqp.Error) chan *amqp.Error {
	if m.notifyCloseFn != nil {
		return m.notifyCloseFn(receiver)
	}
	return receiver
}

// --- Publish Tests ---

func TestPublish_Success(t *testing.T) {
	var capturedKey string
	var capturedBody []byte
	var capturedContentType string

	mockCh := &mockAMQPChannel{
		publishWithContextFn: func(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp.Publishing) error {
			capturedKey = key
			capturedBody = msg.Body
			capturedContentType = msg.ContentType
			return nil
		},
	}

	mockConn := &mockAMQPConn{
		channelFn: func() (AMQPChannelAPI, error) {
			return mockCh, nil
		},
	}

	client := newClientWithAMQP(mockConn, nil)
	err := client.Publish(context.Background(), "dp.events.status-changed", []byte(`{"status":"ok"}`))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedKey != "dp.events.status-changed" {
		t.Errorf("routing key = %q, want %q", capturedKey, "dp.events.status-changed")
	}
	if string(capturedBody) != `{"status":"ok"}` {
		t.Errorf("body = %q, want %q", string(capturedBody), `{"status":"ok"}`)
	}
	if capturedContentType != "application/json" {
		t.Errorf("content type = %q, want %q", capturedContentType, "application/json")
	}
}

func TestPublish_ChannelError(t *testing.T) {
	mockCh := &mockAMQPChannel{
		publishWithContextFn: func(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp.Publishing) error {
			return errors.New("channel closed")
		},
	}

	mockConn := &mockAMQPConn{
		channelFn: func() (AMQPChannelAPI, error) {
			return mockCh, nil
		},
	}

	client := newClientWithAMQP(mockConn, nil)
	err := client.Publish(context.Background(), "topic", []byte("data"))

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if port.ErrorCode(err) != port.ErrCodeBrokerFailed {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeBrokerFailed)
	}
	if !port.IsRetryable(err) {
		t.Error("channel error should be retryable")
	}
}

func TestPublish_ContextCancelled(t *testing.T) {
	mockCh := &mockAMQPChannel{
		publishWithContextFn: func(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp.Publishing) error {
			return context.Canceled
		},
	}

	mockConn := &mockAMQPConn{
		channelFn: func() (AMQPChannelAPI, error) {
			return mockCh, nil
		},
	}

	client := newClientWithAMQP(mockConn, nil)
	err := client.Publish(context.Background(), "topic", []byte("data"))

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if port.IsDomainError(err) {
		t.Error("context.Canceled should not be wrapped in DomainError")
	}
}

func TestPublish_NotConnected(t *testing.T) {
	// Create a client with nil conn so pubCh is nil.
	client := newClientWithAMQP(nil, nil)
	err := client.Publish(context.Background(), "topic", []byte("data"))

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if port.ErrorCode(err) != port.ErrCodeBrokerFailed {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeBrokerFailed)
	}
	if !port.IsRetryable(err) {
		t.Error("nil channel error should be retryable")
	}
}

// --- Subscribe Tests ---

func TestSubscribe_Success(t *testing.T) {
	deliveryCh := make(chan amqp.Delivery, 1)
	received := make(chan []byte, 1)

	var capturedQueue string
	var capturedDurable bool

	mockCh := &mockAMQPChannel{
		queueDeclareFn: func(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error) {
			capturedQueue = name
			capturedDurable = durable
			return amqp.Queue{Name: name}, nil
		},
		consumeFn: func(queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args amqp.Table) (<-chan amqp.Delivery, error) {
			return deliveryCh, nil
		},
	}

	mockConn := &mockAMQPConn{
		channelFn: func() (AMQPChannelAPI, error) {
			return mockCh, nil
		},
	}

	client := newClientWithAMQP(mockConn, nil)

	handler := func(ctx context.Context, body []byte) error {
		received <- body
		return nil
	}

	err := client.Subscribe("dp.commands.process-document", handler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedQueue != "dp.commands.process-document" {
		t.Errorf("queue = %q, want %q", capturedQueue, "dp.commands.process-document")
	}
	if !capturedDurable {
		t.Error("queue should be durable")
	}

	// Send a delivery and verify handler receives it.
	deliveryCh <- amqp.Delivery{Body: []byte(`{"jobId":"123"}`)}

	select {
	case body := <-received:
		if string(body) != `{"jobId":"123"}` {
			t.Errorf("body = %q, want %q", string(body), `{"jobId":"123"}`)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not receive message within timeout")
	}

	// Cleanup.
	close(deliveryCh)
	_ = client.Close()
}

func TestSubscribe_QueueDeclareError(t *testing.T) {
	mockCh := &mockAMQPChannel{
		queueDeclareFn: func(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error) {
			return amqp.Queue{}, &amqp.Error{Code: 406, Reason: "PRECONDITION_FAILED"}
		},
	}

	mockConn := &mockAMQPConn{
		channelFn: func() (AMQPChannelAPI, error) {
			return mockCh, nil
		},
	}

	client := newClientWithAMQP(mockConn, nil)
	err := client.Subscribe("bad-queue", func(ctx context.Context, body []byte) error { return nil })

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if port.ErrorCode(err) != port.ErrCodeBrokerFailed {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeBrokerFailed)
	}

	_ = client.Close()
}

func TestSubscribe_HandlerError_Nacks(t *testing.T) {
	deliveryCh := make(chan amqp.Delivery, 1)
	nacked := make(chan bool, 1)

	mockCh := &mockAMQPChannel{
		queueDeclareFn: func(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error) {
			return amqp.Queue{Name: name}, nil
		},
		consumeFn: func(queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args amqp.Table) (<-chan amqp.Delivery, error) {
			return deliveryCh, nil
		},
	}

	mockConn := &mockAMQPConn{
		channelFn: func() (AMQPChannelAPI, error) {
			return mockCh, nil
		},
	}

	client := newClientWithAMQP(mockConn, nil)

	handler := func(ctx context.Context, body []byte) error {
		return errors.New("processing failed")
	}

	err := client.Subscribe("test-queue", handler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Create a mock acknowledger that tracks Nack calls.
	acker := &mockAcknowledger{nackFn: func(tag uint64, multiple, requeue bool) error {
		if !requeue {
			t.Error("expected requeue=true on Nack")
		}
		nacked <- true
		return nil
	}}

	deliveryCh <- amqp.Delivery{
		Acknowledger: acker,
		Body:         []byte("bad-data"),
	}

	select {
	case <-nacked:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("Nack was not called within timeout")
	}

	close(deliveryCh)
	_ = client.Close()
}

func TestSubscribe_HandlerSuccess_Acks(t *testing.T) {
	deliveryCh := make(chan amqp.Delivery, 1)
	acked := make(chan bool, 1)

	mockCh := &mockAMQPChannel{
		queueDeclareFn: func(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error) {
			return amqp.Queue{Name: name}, nil
		},
		consumeFn: func(queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args amqp.Table) (<-chan amqp.Delivery, error) {
			return deliveryCh, nil
		},
	}

	mockConn := &mockAMQPConn{
		channelFn: func() (AMQPChannelAPI, error) {
			return mockCh, nil
		},
	}

	client := newClientWithAMQP(mockConn, nil)

	handler := func(ctx context.Context, body []byte) error {
		return nil
	}

	err := client.Subscribe("test-queue", handler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	acker := &mockAcknowledger{ackFn: func(tag uint64, multiple bool) error {
		acked <- true
		return nil
	}}

	deliveryCh <- amqp.Delivery{
		Acknowledger: acker,
		Body:         []byte("good-data"),
	}

	select {
	case <-acked:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("Ack was not called within timeout")
	}

	close(deliveryCh)
	_ = client.Close()
}

// --- Close Tests ---

func TestClose_GracefulShutdown(t *testing.T) {
	deliveryCh := make(chan amqp.Delivery)
	var connClosed, pubChClosed bool
	var mu sync.Mutex

	mockCh := &mockAMQPChannel{
		queueDeclareFn: func(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error) {
			return amqp.Queue{Name: name}, nil
		},
		consumeFn: func(queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args amqp.Table) (<-chan amqp.Delivery, error) {
			return deliveryCh, nil
		},
	}

	// Separate pub channel mock to track its Close.
	pubCh := &mockAMQPChannel{
		closeFn: func() error {
			mu.Lock()
			pubChClosed = true
			mu.Unlock()
			return nil
		},
	}

	mockConn := &mockAMQPConn{
		channelFn: func() (AMQPChannelAPI, error) {
			return mockCh, nil
		},
		closeFn: func() error {
			mu.Lock()
			connClosed = true
			mu.Unlock()
			return nil
		},
	}

	client := newClientWithAMQP(mockConn, nil)
	// Override pubCh with our tracked mock.
	client.pubCh = pubCh

	// Start a subscriber to create a consumer goroutine.
	err := client.Subscribe("test-queue", func(ctx context.Context, body []byte) error { return nil })
	if err != nil {
		t.Fatalf("unexpected subscribe error: %v", err)
	}

	// Close should stop everything and wait for goroutines.
	done := make(chan error, 1)
	go func() {
		done <- client.Close()
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not complete within timeout")
	}

	mu.Lock()
	defer mu.Unlock()
	if !pubChClosed {
		t.Error("publish channel was not closed")
	}
	if !connClosed {
		t.Error("connection was not closed")
	}
}

func TestClose_Idempotent(t *testing.T) {
	mockConn := &mockAMQPConn{
		channelFn: func() (AMQPChannelAPI, error) {
			return &mockAMQPChannel{}, nil
		},
	}

	client := newClientWithAMQP(mockConn, nil)

	err1 := client.Close()
	if err1 != nil {
		t.Fatalf("first Close returned error: %v", err1)
	}

	// Second Close should not panic and should return nil.
	err2 := client.Close()
	if err2 != nil {
		t.Fatalf("second Close returned error: %v", err2)
	}
}

// --- Error Mapping Tests ---

func TestMapError_AMQPError(t *testing.T) {
	original := &amqp.Error{Code: 504, Reason: "CHANNEL_ERROR", Server: true, Recover: false}
	err := mapError(original, "Publish")

	if port.ErrorCode(err) != port.ErrCodeBrokerFailed {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeBrokerFailed)
	}
	if !port.IsRetryable(err) {
		t.Error("504 CHANNEL_ERROR should be retryable")
	}
	if !strings.Contains(err.Error(), "504") {
		t.Errorf("error should contain code 504, got %q", err.Error())
	}
}

func TestMapError_ContextDeadlineExceeded(t *testing.T) {
	err := mapError(context.DeadlineExceeded, "Publish")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded, got %v", err)
	}
	if port.IsDomainError(err) {
		t.Error("context.DeadlineExceeded should not be wrapped in DomainError")
	}
}

func TestMapError_UnknownError(t *testing.T) {
	original := errors.New("connection reset by peer")
	err := mapError(original, "Subscribe")

	if port.ErrorCode(err) != port.ErrCodeBrokerFailed {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeBrokerFailed)
	}
	if !port.IsRetryable(err) {
		t.Error("unknown error should be retryable")
	}
	if !strings.Contains(err.Error(), "broker: Subscribe") {
		t.Errorf("error should contain operation, got %q", err.Error())
	}
}

func TestMapError_NotFound_NonRetryable(t *testing.T) {
	original := &amqp.Error{Code: 404, Reason: "NOT_FOUND", Server: true, Recover: false}
	err := mapError(original, "Subscribe")

	if port.ErrorCode(err) != port.ErrCodeBrokerFailed {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeBrokerFailed)
	}
	if port.IsRetryable(err) {
		t.Error("404 NOT_FOUND should not be retryable")
	}
}

func TestMapError_AccessRefused_NonRetryable(t *testing.T) {
	original := &amqp.Error{Code: 403, Reason: "ACCESS_REFUSED", Server: true, Recover: false}
	err := mapError(original, "Publish")

	if port.ErrorCode(err) != port.ErrCodeBrokerFailed {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeBrokerFailed)
	}
	if port.IsRetryable(err) {
		t.Error("403 ACCESS_REFUSED should not be retryable")
	}
}

func TestMapError_ContextCanceled(t *testing.T) {
	err := mapError(context.Canceled, "Publish")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if port.IsDomainError(err) {
		t.Error("context.Canceled should not be wrapped in DomainError")
	}
}

// --- Interface Compliance ---

func TestInterfaceCompliance(t *testing.T) {
	var _ AMQPAPI = (*mockAMQPConn)(nil)
	var _ AMQPChannelAPI = (*mockAMQPChannel)(nil)
}

// --- Reconnect / Backoff Tests ---

func TestBackoffDelay_ExponentialGrowth(t *testing.T) {
	// backoffDelay uses jitter so we test bounds rather than exact values.
	for attempt := 0; attempt <= 3; attempt++ {
		delay := backoffDelay(attempt)
		if delay <= 0 {
			t.Errorf("attempt %d: delay should be positive, got %v", attempt, delay)
		}
	}

	// Attempt 0: base=1s ± jitter → should be < 2s.
	d0 := backoffDelay(0)
	if d0 > 2*time.Second {
		t.Errorf("attempt 0: delay too large: %v", d0)
	}
}

func TestBackoffDelay_CapsAtMax(t *testing.T) {
	// High attempt should cap at reconnectMaxDelay + jitter.
	// With 25% jitter, max possible = 30s * 1.25 = 37.5s.
	for i := 0; i < 100; i++ {
		delay := backoffDelay(100)
		maxWithJitter := reconnectMaxDelay + time.Duration(float64(reconnectMaxDelay)*jitterFraction)
		if delay > maxWithJitter {
			t.Errorf("attempt 100: delay %v exceeds max with jitter %v", delay, maxWithJitter)
		}
		if delay < 0 {
			t.Errorf("attempt 100: delay should not be negative, got %v", delay)
		}
	}
}

func TestBackoffDelay_NonNegative(t *testing.T) {
	for attempt := 0; attempt < 200; attempt++ {
		delay := backoffDelay(attempt)
		if delay < 0 {
			t.Errorf("attempt %d: delay should be non-negative, got %v", attempt, delay)
		}
	}
}

func TestReconnectWithBackoff_DialFailsThenSucceeds(t *testing.T) {
	dialAttempts := 0
	newDeliveryCh := make(chan amqp.Delivery)

	newMockCh := &mockAMQPChannel{
		queueDeclareFn: func(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error) {
			return amqp.Queue{Name: name}, nil
		},
		consumeFn: func(queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args amqp.Table) (<-chan amqp.Delivery, error) {
			return newDeliveryCh, nil
		},
	}

	newMockConn := &mockAMQPConn{
		channelFn: func() (AMQPChannelAPI, error) {
			return newMockCh, nil
		},
	}

	dialFn := func(addr string) (AMQPAPI, error) {
		dialAttempts++
		// Fail on first attempt only to keep the test fast (1s backoff delay).
		if dialAttempts < 2 {
			return nil, errors.New("connection refused")
		}
		return newMockConn, nil
	}

	// Create client with a mock that will be replaced.
	oldMockConn := &mockAMQPConn{
		channelFn: func() (AMQPChannelAPI, error) {
			return &mockAMQPChannel{}, nil
		},
	}
	client := newClientWithAMQP(oldMockConn, dialFn)

	// Register a subscription to verify re-subscribe after reconnect.
	received := make(chan []byte, 1)
	err := client.Subscribe("test-queue", func(ctx context.Context, body []byte) error {
		received <- body
		return nil
	})
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	// Trigger reconnect (this blocks until success or done).
	client.reconnectWithBackoff()

	if dialAttempts < 2 {
		t.Errorf("expected at least 2 dial attempts, got %d", dialAttempts)
	}

	// Verify that conn was replaced.
	client.mu.RLock()
	if client.conn != newMockConn {
		t.Error("conn was not replaced after reconnect")
	}
	client.mu.RUnlock()

	// Verify re-subscribe by sending a message through the new delivery channel.
	newDeliveryCh <- amqp.Delivery{Body: []byte("reconnected")}

	select {
	case body := <-received:
		if string(body) != "reconnected" {
			t.Errorf("body = %q, want %q", string(body), "reconnected")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not receive message after reconnect")
	}

	close(newDeliveryCh)
	_ = client.Close()
}

func TestReconnectWithBackoff_StopsOnDone(t *testing.T) {
	dialFn := func(addr string) (AMQPAPI, error) {
		return nil, errors.New("always fail")
	}

	client := newClientWithAMQP(nil, dialFn)

	// Close done channel after a short delay to stop the reconnect loop.
	go func() {
		time.Sleep(100 * time.Millisecond)
		close(client.done)
	}()

	// reconnectWithBackoff should return quickly after done is closed.
	done := make(chan struct{})
	go func() {
		client.reconnectWithBackoff()
		close(done)
	}()

	select {
	case <-done:
		// success
	case <-time.After(5 * time.Second):
		t.Fatal("reconnectWithBackoff did not stop after done was closed")
	}
}

func TestSubscribe_CancelContextOnClose(t *testing.T) {
	deliveryCh := make(chan amqp.Delivery, 1)
	ctxCancelled := make(chan bool, 1)

	mockCh := &mockAMQPChannel{
		queueDeclareFn: func(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error) {
			return amqp.Queue{Name: name}, nil
		},
		consumeFn: func(queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args amqp.Table) (<-chan amqp.Delivery, error) {
			return deliveryCh, nil
		},
	}

	mockConn := &mockAMQPConn{
		channelFn: func() (AMQPChannelAPI, error) {
			return mockCh, nil
		},
	}

	client := newClientWithAMQP(mockConn, nil)

	handler := func(ctx context.Context, body []byte) error {
		// Block until context is cancelled.
		<-ctx.Done()
		ctxCancelled <- true
		return ctx.Err()
	}

	err := client.Subscribe("test-queue", handler)
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	// Send a delivery to trigger the handler.
	deliveryCh <- amqp.Delivery{
		Acknowledger: &mockAcknowledger{},
		Body:         []byte("data"),
	}

	// Give handler time to start blocking.
	time.Sleep(50 * time.Millisecond)

	// Close should cancel the handler context.
	go func() {
		_ = client.Close()
	}()

	select {
	case <-ctxCancelled:
		// success — handler context was cancelled
	case <-time.After(3 * time.Second):
		t.Fatal("handler context was not cancelled on Close")
	}
}

// --- mockAcknowledger ---

// mockAcknowledger implements amqp.Acknowledger for testing Ack/Nack calls.
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
