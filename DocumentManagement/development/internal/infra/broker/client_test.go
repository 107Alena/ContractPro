package broker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"contractpro/document-management/internal/config"
	"contractpro/document-management/internal/domain/port"
	"contractpro/document-management/internal/infra/concurrency"
)

// ---------------------------------------------------------------------------
// Mock infrastructure
// ---------------------------------------------------------------------------

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
	return newDefaultMockChannel(), nil
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
	confirmFn            func(noWait bool) error
	notifyPublishFn      func(confirm chan amqp.Confirmation) chan amqp.Confirmation
	qosFn                func(prefetchCount, prefetchSize int, global bool) error
	closeFn              func() error
	notifyCloseFn        func(chan *amqp.Error) chan *amqp.Error
}

func newDefaultMockChannel() *mockAMQPChannel {
	return &mockAMQPChannel{}
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

func (m *mockAMQPChannel) NotifyClose(receiver chan *amqp.Error) chan *amqp.Error {
	if m.notifyCloseFn != nil {
		return m.notifyCloseFn(receiver)
	}
	return receiver
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

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func testBrokerConfig() config.BrokerConfig {
	return config.BrokerConfig{
		Address:                        "amqp://localhost:5672",
		TLS:                           false,
		TopicDPArtifactsProcessingReady: "dp.artifacts.processing-ready",
		TopicDPRequestsSemanticTree:     "dp.requests.semantic-tree",
		TopicDPArtifactsDiffReady:       "dp.artifacts.diff-ready",
		TopicLICArtifactsAnalysisReady:  "lic.artifacts.analysis-ready",
		TopicLICRequestsArtifacts:       "lic.requests.artifacts",
		TopicREArtifactsReportsReady:    "re.artifacts.reports-ready",
		TopicRERequestsArtifacts:        "re.requests.artifacts",
		TopicDMDLQIngestionFailed:       "dm.dlq.ingestion-failed",
		TopicDMDLQQueryFailed:           "dm.dlq.query-failed",
		TopicDMDLQInvalidMessage:        "dm.dlq.invalid-message",
	}
}

func testConsumerConfig() config.ConsumerConfig {
	return config.ConsumerConfig{
		Prefetch:    10,
		Concurrency: 5,
	}
}

// newTestClient creates a Client with a mock connection that supports
// publisher confirms via a captured confirm channel.
func newTestClient(conn *mockAMQPConn, dialFn func(string) (AMQPAPI, error)) (*Client, chan amqp.Confirmation) {
	var confirmCh chan amqp.Confirmation

	// Wrap the original channelFn to capture the confirm channel.
	origChannelFn := conn.channelFn
	conn.channelFn = func() (AMQPChannelAPI, error) {
		if origChannelFn != nil {
			ch, err := origChannelFn()
			if err != nil {
				return nil, err
			}
			// Intercept NotifyPublish to capture the confirm channel.
			if mockCh, ok := ch.(*mockAMQPChannel); ok {
				origNotify := mockCh.notifyPublishFn
				mockCh.notifyPublishFn = func(confirm chan amqp.Confirmation) chan amqp.Confirmation {
					confirmCh = confirm
					if origNotify != nil {
						return origNotify(confirm)
					}
					return confirm
				}
			}
			return ch, nil
		}
		ch := newDefaultMockChannel()
		ch.notifyPublishFn = func(confirm chan amqp.Confirmation) chan amqp.Confirmation {
			confirmCh = confirm
			return confirm
		}
		return ch, nil
	}

	client := newClientWithAMQP(conn, dialFn, testBrokerConfig(), testConsumerConfig(), nil)
	return client, confirmCh
}

// ---------------------------------------------------------------------------
// Publish Tests
// ---------------------------------------------------------------------------

func TestPublish_Success(t *testing.T) {
	var capturedKey string
	var capturedBody []byte
	var capturedContentType string
	var capturedDeliveryMode uint8

	mockCh := &mockAMQPChannel{
		publishWithContextFn: func(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp.Publishing) error {
			capturedKey = key
			capturedBody = msg.Body
			capturedContentType = msg.ContentType
			capturedDeliveryMode = msg.DeliveryMode
			return nil
		},
	}

	mockConn := &mockAMQPConn{
		channelFn: func() (AMQPChannelAPI, error) {
			return mockCh, nil
		},
	}

	client, confirmCh := newTestClient(mockConn, nil)

	// Send a confirm asynchronously to unblock Publish.
	go func() {
		time.Sleep(10 * time.Millisecond)
		confirmCh <- amqp.Confirmation{Ack: true}
	}()

	err := client.Publish(context.Background(), "dm.responses.artifacts-persisted", []byte(`{"status":"ok"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedKey != "dm.responses.artifacts-persisted" {
		t.Errorf("routing key = %q, want %q", capturedKey, "dm.responses.artifacts-persisted")
	}
	if string(capturedBody) != `{"status":"ok"}` {
		t.Errorf("body = %q, want %q", string(capturedBody), `{"status":"ok"}`)
	}
	if capturedContentType != "application/json" {
		t.Errorf("content type = %q, want %q", capturedContentType, "application/json")
	}
	if capturedDeliveryMode != amqp.Persistent {
		t.Errorf("delivery mode = %d, want %d (Persistent)", capturedDeliveryMode, amqp.Persistent)
	}
}

func TestPublish_ConfirmNack(t *testing.T) {
	mockCh := &mockAMQPChannel{}
	mockConn := &mockAMQPConn{
		channelFn: func() (AMQPChannelAPI, error) {
			return mockCh, nil
		},
	}

	client, confirmCh := newTestClient(mockConn, nil)

	go func() {
		time.Sleep(10 * time.Millisecond)
		confirmCh <- amqp.Confirmation{Ack: false}
	}()

	err := client.Publish(context.Background(), "topic", []byte("data"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if port.ErrorCode(err) != port.ErrCodeBrokerFailed {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeBrokerFailed)
	}
	if !strings.Contains(err.Error(), "nacked") {
		t.Errorf("error should contain 'nacked', got %q", err.Error())
	}
}

func TestPublish_ConfirmChannelClosed(t *testing.T) {
	mockCh := &mockAMQPChannel{}
	mockConn := &mockAMQPConn{
		channelFn: func() (AMQPChannelAPI, error) {
			return mockCh, nil
		},
	}

	client, confirmCh := newTestClient(mockConn, nil)

	go func() {
		time.Sleep(10 * time.Millisecond)
		close(confirmCh)
	}()

	err := client.Publish(context.Background(), "topic", []byte("data"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if port.ErrorCode(err) != port.ErrCodeBrokerFailed {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeBrokerFailed)
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

	client := newClientWithAMQP(mockConn, nil, testBrokerConfig(), testConsumerConfig(), nil)
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
	mockCh := &mockAMQPChannel{}
	mockConn := &mockAMQPConn{
		channelFn: func() (AMQPChannelAPI, error) {
			return mockCh, nil
		},
	}

	client, _ := newTestClient(mockConn, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	err := client.Publish(ctx, "topic", []byte("data"))
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if port.IsDomainError(err) {
		t.Error("context.Canceled should not be wrapped in DomainError")
	}
}

func TestPublish_NotConnected(t *testing.T) {
	client := newClientWithAMQP(nil, nil, testBrokerConfig(), testConsumerConfig(), nil)
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

// ---------------------------------------------------------------------------
// Subscribe Tests
// ---------------------------------------------------------------------------

func TestSubscribe_Success(t *testing.T) {
	deliveryCh := make(chan amqp.Delivery, 1)
	received := make(chan []byte, 1)

	var capturedQueue string
	var capturedDurable bool
	var capturedArgs amqp.Table
	var capturedPrefetch int

	// Use a separate channel for subscribe (not the publish channel).
	subscribeCh := &mockAMQPChannel{
		queueDeclareFn: func(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error) {
			capturedQueue = name
			capturedDurable = durable
			capturedArgs = args
			return amqp.Queue{Name: name}, nil
		},
		consumeFn: func(queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args amqp.Table) (<-chan amqp.Delivery, error) {
			return deliveryCh, nil
		},
		qosFn: func(prefetchCount, prefetchSize int, global bool) error {
			capturedPrefetch = prefetchCount
			return nil
		},
	}

	callCount := 0
	mockConn := &mockAMQPConn{
		channelFn: func() (AMQPChannelAPI, error) {
			callCount++
			// First call is for publish channel (via setupPublishChannel).
			if callCount == 1 {
				return newDefaultMockChannel(), nil
			}
			// Subsequent calls are for consumer channels.
			return subscribeCh, nil
		},
	}

	client := newClientWithAMQP(mockConn, nil, testBrokerConfig(), testConsumerConfig(), nil)

	handler := func(ctx context.Context, body []byte) error {
		received <- body
		return nil
	}

	err := client.Subscribe("dp.artifacts.processing-ready", handler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedQueue != "dp.artifacts.processing-ready" {
		t.Errorf("queue = %q, want %q", capturedQueue, "dp.artifacts.processing-ready")
	}
	if !capturedDurable {
		t.Error("queue should be durable")
	}
	if capturedArgs == nil {
		t.Fatal("queue args should not be nil")
	}
	if capturedArgs["x-max-length"] != int32(standardMaxLength) {
		t.Errorf("x-max-length = %v, want %v", capturedArgs["x-max-length"], int32(standardMaxLength))
	}
	if capturedArgs["x-overflow"] != "reject-publish" {
		t.Errorf("x-overflow = %v, want %q", capturedArgs["x-overflow"], "reject-publish")
	}
	if capturedPrefetch != 10 {
		t.Errorf("prefetch = %d, want %d", capturedPrefetch, 10)
	}

	// Send a delivery and verify handler receives it.
	deliveryCh <- amqp.Delivery{
		Acknowledger: &mockAcknowledger{},
		Body:         []byte(`{"jobId":"123"}`),
	}

	select {
	case body := <-received:
		if string(body) != `{"jobId":"123"}` {
			t.Errorf("body = %q, want %q", string(body), `{"jobId":"123"}`)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not receive message within timeout")
	}

	close(deliveryCh)
	_ = client.Close()
}

func TestSubscribe_QueueDeclareError(t *testing.T) {
	callCount := 0
	mockConn := &mockAMQPConn{
		channelFn: func() (AMQPChannelAPI, error) {
			callCount++
			if callCount == 1 {
				return newDefaultMockChannel(), nil
			}
			return &mockAMQPChannel{
				queueDeclareFn: func(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error) {
					return amqp.Queue{}, &amqp.Error{Code: 406, Reason: "PRECONDITION_FAILED"}
				},
			}, nil
		},
	}

	client := newClientWithAMQP(mockConn, nil, testBrokerConfig(), testConsumerConfig(), nil)
	err := client.Subscribe("bad-queue", func(ctx context.Context, body []byte) error { return nil })

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if port.ErrorCode(err) != port.ErrCodeBrokerFailed {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeBrokerFailed)
	}
	if port.IsRetryable(err) {
		t.Error("406 PRECONDITION_FAILED should not be retryable")
	}

	_ = client.Close()
}

func TestSubscribe_HandlerError_Nacks(t *testing.T) {
	deliveryCh := make(chan amqp.Delivery, 1)
	nacked := make(chan bool, 1)

	callCount := 0
	mockConn := &mockAMQPConn{
		channelFn: func() (AMQPChannelAPI, error) {
			callCount++
			if callCount == 1 {
				return newDefaultMockChannel(), nil
			}
			return &mockAMQPChannel{
				queueDeclareFn: func(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error) {
					return amqp.Queue{Name: name}, nil
				},
				consumeFn: func(queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args amqp.Table) (<-chan amqp.Delivery, error) {
					return deliveryCh, nil
				},
			}, nil
		},
	}

	client := newClientWithAMQP(mockConn, nil, testBrokerConfig(), testConsumerConfig(), nil)

	handler := func(ctx context.Context, body []byte) error {
		return errors.New("processing failed")
	}

	err := client.Subscribe("test-queue", handler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

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

	callCount := 0
	mockConn := &mockAMQPConn{
		channelFn: func() (AMQPChannelAPI, error) {
			callCount++
			if callCount == 1 {
				return newDefaultMockChannel(), nil
			}
			return &mockAMQPChannel{
				queueDeclareFn: func(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error) {
					return amqp.Queue{Name: name}, nil
				},
				consumeFn: func(queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args amqp.Table) (<-chan amqp.Delivery, error) {
					return deliveryCh, nil
				},
			}, nil
		},
	}

	client := newClientWithAMQP(mockConn, nil, testBrokerConfig(), testConsumerConfig(), nil)

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

// ---------------------------------------------------------------------------
// DeclareTopology Tests
// ---------------------------------------------------------------------------

func TestDeclareTopology_Success(t *testing.T) {
	var declaredQueues []string
	var declaredArgs []amqp.Table

	callCount := 0
	mockConn := &mockAMQPConn{
		channelFn: func() (AMQPChannelAPI, error) {
			callCount++
			if callCount == 1 {
				return newDefaultMockChannel(), nil
			}
			return &mockAMQPChannel{
				queueDeclareFn: func(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error) {
					declaredQueues = append(declaredQueues, name)
					declaredArgs = append(declaredArgs, args)
					return amqp.Queue{Name: name}, nil
				},
			}, nil
		},
	}

	client := newClientWithAMQP(mockConn, nil, testBrokerConfig(), testConsumerConfig(), nil)

	err := client.DeclareTopology()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Expect 7 incoming + 3 DLQ = 10 queues.
	if len(declaredQueues) != 10 {
		t.Fatalf("declared %d queues, want 10", len(declaredQueues))
	}

	// Verify incoming queues (first 7).
	expectedIncoming := []string{
		"dp.artifacts.processing-ready",
		"dp.requests.semantic-tree",
		"dp.artifacts.diff-ready",
		"lic.artifacts.analysis-ready",
		"lic.requests.artifacts",
		"re.artifacts.reports-ready",
		"re.requests.artifacts",
	}
	for i, expected := range expectedIncoming {
		if declaredQueues[i] != expected {
			t.Errorf("queue[%d] = %q, want %q", i, declaredQueues[i], expected)
		}
		// Verify standard queue args.
		if declaredArgs[i]["x-overflow"] != "reject-publish" {
			t.Errorf("queue[%d] x-overflow = %v, want %q", i, declaredArgs[i]["x-overflow"], "reject-publish")
		}
	}

	// Verify DLQ queues (last 3).
	expectedDLQ := []string{
		"dm.dlq.ingestion-failed",
		"dm.dlq.query-failed",
		"dm.dlq.invalid-message",
	}
	for i, expected := range expectedDLQ {
		idx := 7 + i
		if declaredQueues[idx] != expected {
			t.Errorf("queue[%d] = %q, want %q", idx, declaredQueues[idx], expected)
		}
		// Verify quorum queue args.
		if declaredArgs[idx]["x-queue-type"] != "quorum" {
			t.Errorf("queue[%d] x-queue-type = %v, want %q", idx, declaredArgs[idx]["x-queue-type"], "quorum")
		}
	}
}

func TestDeclareTopology_NotConnected(t *testing.T) {
	client := newClientWithAMQP(nil, nil, testBrokerConfig(), testConsumerConfig(), nil)
	err := client.DeclareTopology()

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if port.ErrorCode(err) != port.ErrCodeBrokerFailed {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeBrokerFailed)
	}
}

func TestDeclareTopology_QueueDeclareError(t *testing.T) {
	callCount := 0
	declareCount := 0
	mockConn := &mockAMQPConn{
		channelFn: func() (AMQPChannelAPI, error) {
			callCount++
			if callCount == 1 {
				return newDefaultMockChannel(), nil
			}
			return &mockAMQPChannel{
				queueDeclareFn: func(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error) {
					declareCount++
					if declareCount == 3 { // Fail on 3rd queue
						return amqp.Queue{}, &amqp.Error{Code: 403, Reason: "ACCESS_REFUSED"}
					}
					return amqp.Queue{Name: name}, nil
				},
			}, nil
		},
	}

	client := newClientWithAMQP(mockConn, nil, testBrokerConfig(), testConsumerConfig(), nil)
	err := client.DeclareTopology()

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if port.ErrorCode(err) != port.ErrCodeBrokerFailed {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeBrokerFailed)
	}
}

// ---------------------------------------------------------------------------
// Close Tests
// ---------------------------------------------------------------------------

func TestClose_GracefulShutdown(t *testing.T) {
	deliveryCh := make(chan amqp.Delivery)
	var connClosed, pubChClosed bool
	var mu sync.Mutex

	pubCh := &mockAMQPChannel{
		closeFn: func() error {
			mu.Lock()
			pubChClosed = true
			mu.Unlock()
			return nil
		},
	}

	callCount := 0
	mockConn := &mockAMQPConn{
		channelFn: func() (AMQPChannelAPI, error) {
			callCount++
			if callCount == 1 {
				return pubCh, nil
			}
			return &mockAMQPChannel{
				queueDeclareFn: func(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error) {
					return amqp.Queue{Name: name}, nil
				},
				consumeFn: func(queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args amqp.Table) (<-chan amqp.Delivery, error) {
					return deliveryCh, nil
				},
			}, nil
		},
		closeFn: func() error {
			mu.Lock()
			connClosed = true
			mu.Unlock()
			return nil
		},
	}

	client := newClientWithAMQP(mockConn, nil, testBrokerConfig(), testConsumerConfig(), nil)

	err := client.Subscribe("test-queue", func(ctx context.Context, body []byte) error { return nil })
	if err != nil {
		t.Fatalf("unexpected subscribe error: %v", err)
	}

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
			return newDefaultMockChannel(), nil
		},
	}

	client := newClientWithAMQP(mockConn, nil, testBrokerConfig(), testConsumerConfig(), nil)

	err1 := client.Close()
	if err1 != nil {
		t.Fatalf("first Close returned error: %v", err1)
	}

	err2 := client.Close()
	if err2 != nil {
		t.Fatalf("second Close returned error: %v", err2)
	}
}

// ---------------------------------------------------------------------------
// IsConnected Tests
// ---------------------------------------------------------------------------

func TestIsConnected_True(t *testing.T) {
	mockConn := &mockAMQPConn{
		channelFn: func() (AMQPChannelAPI, error) {
			return newDefaultMockChannel(), nil
		},
	}
	client := newClientWithAMQP(mockConn, nil, testBrokerConfig(), testConsumerConfig(), nil)

	if !client.IsConnected() {
		t.Error("expected IsConnected=true for connected client")
	}
}

func TestIsConnected_FalseWhenNil(t *testing.T) {
	client := newClientWithAMQP(nil, nil, testBrokerConfig(), testConsumerConfig(), nil)

	if client.IsConnected() {
		t.Error("expected IsConnected=false for nil connection")
	}
}

// ---------------------------------------------------------------------------
// Error Mapping Tests
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Reconnect / Backoff Tests
// ---------------------------------------------------------------------------

func TestBackoffDelay_ExponentialGrowth(t *testing.T) {
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
			return newDefaultMockChannel(), nil
		},
	}
	client := newClientWithAMQP(oldMockConn, dialFn, testBrokerConfig(), testConsumerConfig(), nil)

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

	// Verify healthy flag is restored.
	if !client.IsConnected() {
		t.Error("expected IsConnected=true after reconnect")
	}

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

	client := newClientWithAMQP(nil, dialFn, testBrokerConfig(), testConsumerConfig(), nil)

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

func TestReconnectWithBackoff_ReEnablesConfirms(t *testing.T) {
	confirmEnabled := false

	newMockCh := &mockAMQPChannel{
		confirmFn: func(noWait bool) error {
			confirmEnabled = true
			return nil
		},
		queueDeclareFn: func(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error) {
			return amqp.Queue{Name: name}, nil
		},
	}

	newMockConn := &mockAMQPConn{
		channelFn: func() (AMQPChannelAPI, error) {
			return newMockCh, nil
		},
	}

	dialFn := func(addr string) (AMQPAPI, error) {
		return newMockConn, nil
	}

	oldMockConn := &mockAMQPConn{
		channelFn: func() (AMQPChannelAPI, error) {
			return newDefaultMockChannel(), nil
		},
	}
	client := newClientWithAMQP(oldMockConn, dialFn, testBrokerConfig(), testConsumerConfig(), nil)

	client.reconnectWithBackoff()

	if !confirmEnabled {
		t.Error("publisher confirms were not re-enabled after reconnect")
	}

	_ = client.Close()
}

// ---------------------------------------------------------------------------
// Interface Compliance Tests
// ---------------------------------------------------------------------------

func TestInterfaceCompliance(t *testing.T) {
	var _ AMQPAPI = (*mockAMQPConn)(nil)
	var _ AMQPChannelAPI = (*mockAMQPChannel)(nil)
	var _ port.BrokerPublisherPort = (*Client)(nil)
}

// ---------------------------------------------------------------------------
// Handler Context Tests
// ---------------------------------------------------------------------------

func TestSubscribe_CancelContextOnClose(t *testing.T) {
	deliveryCh := make(chan amqp.Delivery, 1)
	ctxCancelled := make(chan bool, 1)

	callCount := 0
	mockConn := &mockAMQPConn{
		channelFn: func() (AMQPChannelAPI, error) {
			callCount++
			if callCount == 1 {
				return newDefaultMockChannel(), nil
			}
			return &mockAMQPChannel{
				queueDeclareFn: func(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error) {
					return amqp.Queue{Name: name}, nil
				},
				consumeFn: func(queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args amqp.Table) (<-chan amqp.Delivery, error) {
					return deliveryCh, nil
				},
			}, nil
		},
	}

	client := newClientWithAMQP(mockConn, nil, testBrokerConfig(), testConsumerConfig(), nil)

	handler := func(ctx context.Context, body []byte) error {
		<-ctx.Done()
		ctxCancelled <- true
		return ctx.Err()
	}

	err := client.Subscribe("test-queue", handler)
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	deliveryCh <- amqp.Delivery{
		Acknowledger: &mockAcknowledger{},
		Body:         []byte("data"),
	}

	time.Sleep(50 * time.Millisecond)

	go func() {
		_ = client.Close()
	}()

	select {
	case <-ctxCancelled:
		// success
	case <-time.After(3 * time.Second):
		t.Fatal("handler context was not cancelled on Close")
	}
}

// ---------------------------------------------------------------------------
// Queue Args Tests
// ---------------------------------------------------------------------------

func TestStandardQueueArgs(t *testing.T) {
	args := standardQueueArgs()

	if args["x-max-length"] != int32(standardMaxLength) {
		t.Errorf("x-max-length = %v, want %v", args["x-max-length"], int32(standardMaxLength))
	}
	if args["x-overflow"] != "reject-publish" {
		t.Errorf("x-overflow = %v, want %q", args["x-overflow"], "reject-publish")
	}
	if args["x-message-ttl"] != int32(standardMessageTTL) {
		t.Errorf("x-message-ttl = %v, want %v", args["x-message-ttl"], int32(standardMessageTTL))
	}
}

func TestDLQQueueArgs(t *testing.T) {
	args := dlqQueueArgs()

	if args["x-queue-type"] != "quorum" {
		t.Errorf("x-queue-type = %v, want %q", args["x-queue-type"], "quorum")
	}
	if args["x-max-length"] != int32(dlqMaxLength) {
		t.Errorf("x-max-length = %v, want %v", args["x-max-length"], int32(dlqMaxLength))
	}
	if args["x-message-ttl"] != int32(dlqMessageTTL) {
		t.Errorf("x-message-ttl = %v, want %v", args["x-message-ttl"], int32(dlqMessageTTL))
	}
}

// ---------------------------------------------------------------------------
// Consumer Backpressure (BRE-007) Tests
// ---------------------------------------------------------------------------

type testLimiterLogger struct{}

func (l *testLimiterLogger) Debug(msg string, args ...any) {}
func (l *testLimiterLogger) Warn(msg string, args ...any)  {}

func TestConsumeLoop_WithLimiter_ConcurrentDispatch(t *testing.T) {
	// Verify that with a limiter, messages are dispatched concurrently.
	deliveryCh := make(chan amqp.Delivery, 10)
	var ackCount, nackCount atomic.Int64

	acker := &mockAcknowledger{
		ackFn:  func(tag uint64, multiple bool) error { ackCount.Add(1); return nil },
		nackFn: func(tag uint64, multiple bool, requeue bool) error { nackCount.Add(1); return nil },
	}

	mockConn := &mockAMQPConn{
		channelFn: func() (AMQPChannelAPI, error) {
			return &mockAMQPChannel{
				queueDeclareFn: func(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error) {
					return amqp.Queue{Name: name}, nil
				},
				consumeFn: func(queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args amqp.Table) (<-chan amqp.Delivery, error) {
					return deliveryCh, nil
				},
			}, nil
		},
	}

	limiter := concurrency.NewSemaphore(3, &testLimiterLogger{})
	client := newClientWithAMQP(mockConn, nil, testBrokerConfig(), testConsumerConfig(), limiter)

	var processing atomic.Int64
	var peak atomic.Int64
	done := make(chan struct{})

	handler := func(ctx context.Context, body []byte) error {
		cur := processing.Add(1)
		for {
			old := peak.Load()
			if cur <= old || peak.CompareAndSwap(old, cur) {
				break
			}
		}
		time.Sleep(30 * time.Millisecond)
		processing.Add(-1)
		return nil
	}

	if err := client.Subscribe("test-queue", handler); err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	// Send 6 messages.
	for i := 0; i < 6; i++ {
		deliveryCh <- amqp.Delivery{
			Acknowledger: acker,
			Body:         []byte(fmt.Sprintf("msg-%d", i)),
		}
	}

	go func() {
		// Wait for all messages to be acked.
		for ackCount.Load() < 6 {
			time.Sleep(10 * time.Millisecond)
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for messages to be processed")
	}

	if ackCount.Load() != 6 {
		t.Errorf("expected 6 acks, got %d", ackCount.Load())
	}
	if peak.Load() > 3 {
		t.Errorf("peak concurrent=%d exceeded limiter capacity=3", peak.Load())
	}
	if peak.Load() < 2 {
		t.Errorf("peak concurrent=%d; expected at least 2 concurrent handlers", peak.Load())
	}

	_ = client.Close()
}

func TestConsumeLoop_WithLimiter_HandlerError_NacksWithRequeue(t *testing.T) {
	deliveryCh := make(chan amqp.Delivery, 2)
	nackCh := make(chan bool, 2)

	acker := &mockAcknowledger{
		nackFn: func(tag uint64, multiple bool, requeue bool) error {
			nackCh <- requeue
			return nil
		},
	}

	mockConn := &mockAMQPConn{
		channelFn: func() (AMQPChannelAPI, error) {
			return &mockAMQPChannel{
				queueDeclareFn: func(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error) {
					return amqp.Queue{Name: name}, nil
				},
				consumeFn: func(queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args amqp.Table) (<-chan amqp.Delivery, error) {
					return deliveryCh, nil
				},
			}, nil
		},
	}

	limiter := concurrency.NewSemaphore(2, &testLimiterLogger{})
	client := newClientWithAMQP(mockConn, nil, testBrokerConfig(), testConsumerConfig(), limiter)

	handler := func(ctx context.Context, body []byte) error {
		return fmt.Errorf("handler error")
	}

	if err := client.Subscribe("test-queue", handler); err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	deliveryCh <- amqp.Delivery{Acknowledger: acker, Body: []byte("fail")}

	select {
	case requeue := <-nackCh:
		if !requeue {
			t.Error("expected Nack with requeue=true")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for Nack")
	}

	_ = client.Close()
}

func TestConsumeLoop_WithLimiter_GracefulShutdown_WaitsForInflight(t *testing.T) {
	deliveryCh := make(chan amqp.Delivery, 5)
	handlerStarted := make(chan struct{})
	handlerBlocking := make(chan struct{})
	ackCh := make(chan struct{}, 5)

	acker := &mockAcknowledger{
		ackFn: func(tag uint64, multiple bool) error {
			ackCh <- struct{}{}
			return nil
		},
	}

	mockConn := &mockAMQPConn{
		channelFn: func() (AMQPChannelAPI, error) {
			return &mockAMQPChannel{
				queueDeclareFn: func(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error) {
					return amqp.Queue{Name: name}, nil
				},
				consumeFn: func(queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args amqp.Table) (<-chan amqp.Delivery, error) {
					return deliveryCh, nil
				},
			}, nil
		},
	}

	limiter := concurrency.NewSemaphore(2, &testLimiterLogger{})
	client := newClientWithAMQP(mockConn, nil, testBrokerConfig(), testConsumerConfig(), limiter)

	var callCount atomic.Int64
	handler := func(ctx context.Context, body []byte) error {
		n := callCount.Add(1)
		if n == 1 {
			close(handlerStarted)
			<-handlerBlocking // block until released
		}
		return nil
	}

	if err := client.Subscribe("test-queue", handler); err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	// Send a message and wait for handler to start.
	deliveryCh <- amqp.Delivery{Acknowledger: acker, Body: []byte("blocking")}
	<-handlerStarted

	// Start Close in background — it should wait for the in-flight handler.
	closeDone := make(chan struct{})
	go func() {
		_ = client.Close()
		close(closeDone)
	}()

	// Give Close a moment to signal done.
	time.Sleep(50 * time.Millisecond)

	// Close should NOT be done yet (handler is still blocked).
	select {
	case <-closeDone:
		t.Fatal("Close returned before in-flight handler finished")
	default:
	}

	// Release the handler.
	close(handlerBlocking)

	// Now Close should complete.
	select {
	case <-closeDone:
		// success
	case <-time.After(3 * time.Second):
		t.Fatal("Close did not return after in-flight handler finished")
	}
}

func TestConsumeLoop_WithoutLimiter_SynchronousFallback(t *testing.T) {
	deliveryCh := make(chan amqp.Delivery, 3)
	var ackOrder []int
	var mu sync.Mutex
	ackDone := make(chan struct{})

	acker := &mockAcknowledger{
		ackFn: func(tag uint64, multiple bool) error {
			mu.Lock()
			ackOrder = append(ackOrder, int(tag))
			done := len(ackOrder) >= 3
			mu.Unlock()
			if done {
				select {
				case <-ackDone:
				default:
					close(ackDone)
				}
			}
			return nil
		},
	}

	mockConn := &mockAMQPConn{
		channelFn: func() (AMQPChannelAPI, error) {
			return &mockAMQPChannel{
				queueDeclareFn: func(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error) {
					return amqp.Queue{Name: name}, nil
				},
				consumeFn: func(queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args amqp.Table) (<-chan amqp.Delivery, error) {
					return deliveryCh, nil
				},
			}, nil
		},
	}

	// No limiter = synchronous.
	client := newClientWithAMQP(mockConn, nil, testBrokerConfig(), testConsumerConfig(), nil)

	handler := func(ctx context.Context, body []byte) error {
		return nil
	}

	if err := client.Subscribe("test-queue", handler); err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	// Send 3 messages.
	for i := 1; i <= 3; i++ {
		deliveryCh <- amqp.Delivery{
			Acknowledger: acker,
			DeliveryTag:  uint64(i),
			Body:         []byte(fmt.Sprintf("msg-%d", i)),
		}
	}

	select {
	case <-ackDone:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for messages to be processed")
	}

	mu.Lock()
	if len(ackOrder) != 3 {
		t.Errorf("expected 3 acks, got %d", len(ackOrder))
	}
	// In synchronous mode, acks should be in order.
	for i, tag := range ackOrder {
		if tag != i+1 {
			t.Errorf("ack[%d] = %d, expected %d (sequential order)", i, tag, i+1)
		}
	}
	mu.Unlock()

	_ = client.Close()
}

func TestConsumeLoop_WithLimiter_AcquireCancelled_RequeuesMessage(t *testing.T) {
	// When Close is called, the limiter's Acquire context is cancelled.
	// The message should be Nacked with requeue.
	deliveryCh := make(chan amqp.Delivery, 2)
	nackCh := make(chan bool, 1)

	acker := &mockAcknowledger{
		nackFn: func(tag uint64, multiple bool, requeue bool) error {
			nackCh <- requeue
			return nil
		},
	}

	// Fill limiter to capacity.
	limiter := concurrency.NewSemaphore(1, &testLimiterLogger{})

	handlerStarted := make(chan struct{})
	mockConn := &mockAMQPConn{
		channelFn: func() (AMQPChannelAPI, error) {
			return &mockAMQPChannel{
				queueDeclareFn: func(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error) {
					return amqp.Queue{Name: name}, nil
				},
				consumeFn: func(queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args amqp.Table) (<-chan amqp.Delivery, error) {
					return deliveryCh, nil
				},
			}, nil
		},
	}

	client := newClientWithAMQP(mockConn, nil, testBrokerConfig(), testConsumerConfig(), limiter)

	handler := func(ctx context.Context, body []byte) error {
		if string(body) == "blocking" {
			close(handlerStarted)
			<-ctx.Done() // block until cancelled
		}
		return nil
	}

	if err := client.Subscribe("test-queue", handler); err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	// First message fills the only slot.
	deliveryCh <- amqp.Delivery{
		Acknowledger: &mockAcknowledger{
			nackFn: func(tag uint64, multiple bool, requeue bool) error { return nil },
		},
		Body: []byte("blocking"),
	}
	<-handlerStarted

	// Second message — Acquire will block because slot is full.
	deliveryCh <- amqp.Delivery{Acknowledger: acker, Body: []byte("queued")}

	// Give time for the second message to reach the Acquire call.
	time.Sleep(50 * time.Millisecond)

	// Close cancels context → Acquire fails → Nack with requeue.
	go func() { _ = client.Close() }()

	select {
	case requeue := <-nackCh:
		if !requeue {
			t.Error("expected Nack with requeue=true on acquire cancellation")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for Nack on acquire cancellation")
	}
}

func TestConsumeLoop_WithLimiter_ReleasesSlotAfterAck(t *testing.T) {
	deliveryCh := make(chan amqp.Delivery, 5)
	var ackCount atomic.Int64

	acker := &mockAcknowledger{
		ackFn: func(tag uint64, multiple bool) error { ackCount.Add(1); return nil },
	}

	mockConn := &mockAMQPConn{
		channelFn: func() (AMQPChannelAPI, error) {
			return &mockAMQPChannel{
				queueDeclareFn: func(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error) {
					return amqp.Queue{Name: name}, nil
				},
				consumeFn: func(queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args amqp.Table) (<-chan amqp.Delivery, error) {
					return deliveryCh, nil
				},
			}, nil
		},
	}

	// Capacity 1 — only one at a time, but all should complete.
	limiter := concurrency.NewSemaphore(1, &testLimiterLogger{})
	client := newClientWithAMQP(mockConn, nil, testBrokerConfig(), testConsumerConfig(), limiter)

	handler := func(ctx context.Context, body []byte) error {
		return nil
	}

	if err := client.Subscribe("test-queue", handler); err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	// Send 5 messages — they must all complete even with capacity=1.
	for i := 0; i < 5; i++ {
		deliveryCh <- amqp.Delivery{Acknowledger: acker, Body: []byte("msg")}
	}

	// Wait for all acks.
	deadline := time.After(5 * time.Second)
	for ackCount.Load() < 5 {
		select {
		case <-deadline:
			t.Fatalf("timeout: only %d/5 messages acked", ackCount.Load())
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	if limiter.ActiveCount() != 0 {
		t.Errorf("expected 0 active slots after all messages processed, got %d", limiter.ActiveCount())
	}

	_ = client.Close()
}
