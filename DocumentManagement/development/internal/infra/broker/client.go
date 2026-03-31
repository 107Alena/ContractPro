package broker

import (
	"context"
	"crypto/tls"
	"sync"
	"sync/atomic"

	amqp "github.com/rabbitmq/amqp091-go"

	"contractpro/document-management/internal/config"
	"contractpro/document-management/internal/domain/port"
)

// Compile-time interface check.
var _ port.BrokerPublisherPort = (*Client)(nil)

// AMQPAPI covers the Connection-level AMQP methods needed.
// Defined here (consumer-side interface) to keep the dependency inverted
// and enable unit testing with a mock.
type AMQPAPI interface {
	Channel() (AMQPChannelAPI, error)
	Close() error
	NotifyClose(receiver chan *amqp.Error) chan *amqp.Error
	IsClosed() bool
}

// AMQPChannelAPI covers the Channel-level AMQP methods needed.
type AMQPChannelAPI interface {
	QueueDeclare(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error)
	Consume(queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args amqp.Table) (<-chan amqp.Delivery, error)
	PublishWithContext(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp.Publishing) error
	Confirm(noWait bool) error
	NotifyPublish(confirm chan amqp.Confirmation) chan amqp.Confirmation
	Qos(prefetchCount, prefetchSize int, global bool) error
	Close() error
	NotifyClose(receiver chan *amqp.Error) chan *amqp.Error
}

// MessageHandler is a callback invoked for each consumed message.
// Returning nil signals successful processing (Ack); returning an error
// causes a Nack with requeue.
type MessageHandler func(ctx context.Context, body []byte) error

// subscription records an active queue subscription for re-subscribe after reconnect.
type subscription struct {
	queue   string
	handler MessageHandler
}

// Queue policy constants.
const (
	standardMaxLength  = 10000     // max messages in standard queues (BRE-026)
	standardMessageTTL = 86400000  // 24 hours in milliseconds (BRE-026)
	dlqMaxLength       = 50000     // max messages in DLQ queues
	dlqMessageTTL      = 604800000 // 7 days in milliseconds
)

// Client is a RabbitMQ client that supports publishing with publisher confirms,
// subscribing with manual ack and configurable prefetch, TLS, queue topology
// declaration (including quorum queues for DLQ), and automatic reconnection.
//
// Client implements port.BrokerPublisherPort for direct use by the outbox relay.
// It is also used by the ingress consumer for subscription.
type Client struct {
	cfg         config.BrokerConfig
	consumerCfg config.ConsumerConfig
	conn        AMQPAPI
	pubCh       AMQPChannelAPI
	confirmCh   <-chan amqp.Confirmation // publisher confirm notifications
	mu          sync.RWMutex            // protects conn, pubCh, confirmCh, subs
	publishMu   sync.Mutex              // serializes publish+confirm for correct ordering
	subs        []subscription          // active subscriptions (re-subscribe after reconnect)
	done        chan struct{}            // closed on Close()
	wg          sync.WaitGroup          // tracks goroutines (reconnect loop, consumers)
	dialFn      func(addr string) (AMQPAPI, error) // injectable for testing
	cancelFn    context.CancelFunc // cancels handler contexts on Close
	cancelCtx   context.Context    // parent context for message handlers
	healthy     atomic.Bool        // health status for readiness checks
}

// amqpConnWrapper adapts a real *amqp.Connection to the AMQPAPI interface.
type amqpConnWrapper struct {
	conn *amqp.Connection
}

func (w *amqpConnWrapper) Channel() (AMQPChannelAPI, error) {
	ch, err := w.conn.Channel()
	if err != nil {
		return nil, err
	}
	return &amqpChanWrapper{ch: ch}, nil
}

func (w *amqpConnWrapper) Close() error {
	return w.conn.Close()
}

func (w *amqpConnWrapper) NotifyClose(receiver chan *amqp.Error) chan *amqp.Error {
	return w.conn.NotifyClose(receiver)
}

func (w *amqpConnWrapper) IsClosed() bool {
	return w.conn.IsClosed()
}

// amqpChanWrapper adapts a real *amqp.Channel to the AMQPChannelAPI interface.
type amqpChanWrapper struct {
	ch *amqp.Channel
}

func (w *amqpChanWrapper) QueueDeclare(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error) {
	return w.ch.QueueDeclare(name, durable, autoDelete, exclusive, noWait, args)
}

func (w *amqpChanWrapper) Consume(queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args amqp.Table) (<-chan amqp.Delivery, error) {
	return w.ch.Consume(queue, consumer, autoAck, exclusive, noLocal, noWait, args)
}

func (w *amqpChanWrapper) PublishWithContext(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp.Publishing) error {
	return w.ch.PublishWithContext(ctx, exchange, key, mandatory, immediate, msg)
}

func (w *amqpChanWrapper) Confirm(noWait bool) error {
	return w.ch.Confirm(noWait)
}

func (w *amqpChanWrapper) NotifyPublish(confirm chan amqp.Confirmation) chan amqp.Confirmation {
	return w.ch.NotifyPublish(confirm)
}

func (w *amqpChanWrapper) Qos(prefetchCount, prefetchSize int, global bool) error {
	return w.ch.Qos(prefetchCount, prefetchSize, global)
}

func (w *amqpChanWrapper) Close() error {
	return w.ch.Close()
}

func (w *amqpChanWrapper) NotifyClose(receiver chan *amqp.Error) chan *amqp.Error {
	return w.ch.NotifyClose(receiver)
}

// NewClient creates a Client configured for the given BrokerConfig and ConsumerConfig.
// It dials the AMQP server (with optional TLS), creates a dedicated publish channel
// with publisher confirms enabled, and starts the reconnection loop.
func NewClient(cfg config.BrokerConfig, consumerCfg config.ConsumerConfig) (*Client, error) {
	dialFn := makeDialFn(cfg.TLS)

	conn, err := dialFn(cfg.Address)
	if err != nil {
		return nil, port.NewBrokerError("broker: dial failed", err)
	}

	pubCh, confirmCh, err := setupPublishChannel(conn)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())

	c := &Client{
		cfg:         cfg,
		consumerCfg: consumerCfg,
		conn:        conn,
		pubCh:       pubCh,
		confirmCh:   confirmCh,
		done:        make(chan struct{}),
		dialFn:      dialFn,
		cancelCtx:   ctx,
		cancelFn:    cancel,
	}
	c.healthy.Store(true)

	c.wg.Add(1)
	go c.reconnectLoop()

	return c, nil
}

// makeDialFn creates a dial function that supports optional TLS (NFR-3.2).
func makeDialFn(useTLS bool) func(addr string) (AMQPAPI, error) {
	return func(addr string) (AMQPAPI, error) {
		var conn *amqp.Connection
		var err error
		if useTLS {
			conn, err = amqp.DialTLS(addr, &tls.Config{MinVersion: tls.VersionTLS12})
		} else {
			conn, err = amqp.Dial(addr)
		}
		if err != nil {
			return nil, err
		}
		return &amqpConnWrapper{conn: conn}, nil
	}
}

// setupPublishChannel creates a dedicated publish channel with publisher confirms enabled.
func setupPublishChannel(conn AMQPAPI) (AMQPChannelAPI, <-chan amqp.Confirmation, error) {
	ch, err := conn.Channel()
	if err != nil {
		return nil, nil, port.NewBrokerError("broker: create publish channel", err)
	}

	if err := ch.Confirm(false); err != nil {
		_ = ch.Close()
		return nil, nil, port.NewBrokerError("broker: enable publisher confirms", err)
	}

	confirmCh := make(chan amqp.Confirmation, 64)
	ch.NotifyPublish(confirmCh)

	return ch, confirmCh, nil
}

// newClientWithAMQP creates a Client with an injected AMQPAPI connection
// and dial function. Used for testing — does NOT start the reconnect loop.
func newClientWithAMQP(conn AMQPAPI, dialFn func(string) (AMQPAPI, error), cfg config.BrokerConfig, consumerCfg config.ConsumerConfig) *Client {
	var pubCh AMQPChannelAPI
	var confirmCh <-chan amqp.Confirmation
	if conn != nil {
		pubCh, confirmCh, _ = setupPublishChannel(conn)
	}

	ctx, cancel := context.WithCancel(context.Background())

	c := &Client{
		cfg:         cfg,
		consumerCfg: consumerCfg,
		conn:        conn,
		pubCh:       pubCh,
		confirmCh:   confirmCh,
		done:        make(chan struct{}),
		dialFn:      dialFn,
		cancelCtx:   ctx,
		cancelFn:    cancel,
	}
	c.healthy.Store(conn != nil)
	return c
}

// Publish sends a message to the given topic (routing key) via the default
// exchange with publisher confirm. The payload is published as application/json
// with persistent delivery mode.
//
// The publishMu serializes publish+confirm operations to ensure each publish
// reads its own confirm from the channel (correct ordering guarantee).
// Stale confirms from previously timed-out publishes are drained before
// publishing to prevent a Publish from reading a confirm that belongs to
// an earlier message.
func (c *Client) Publish(ctx context.Context, topic string, payload []byte) error {
	c.publishMu.Lock()
	defer c.publishMu.Unlock()

	c.mu.RLock()
	ch := c.pubCh
	confirmCh := c.confirmCh
	c.mu.RUnlock()

	if ch == nil {
		return port.NewBrokerError("broker: Publish: not connected (nil channel)", nil)
	}

	// Drain stale confirms from previously timed-out publishes.
	// Without this, a context-cancelled Publish leaves its confirm in the buffer,
	// and the next Publish reads it instead of its own.
	drainConfirms(confirmCh)

	err := ch.PublishWithContext(ctx, "", topic, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Body:         payload,
	})
	if err != nil {
		return mapError(err, "Publish")
	}

	// Wait for publisher confirm.
	select {
	case confirm, ok := <-confirmCh:
		if !ok {
			return port.NewBrokerError("broker: Publish: confirm channel closed (reconnecting)", nil)
		}
		if !confirm.Ack {
			return port.NewBrokerError("broker: Publish: message nacked by broker", nil)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// drainConfirms removes any stale confirmations from the channel.
func drainConfirms(ch <-chan amqp.Confirmation) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

// Subscribe declares a durable queue with QoS prefetch for the given topic,
// starts consuming with manual ack, and launches a goroutine that dispatches
// deliveries to the handler. The subscription is recorded so it can be
// re-established after reconnect.
func (c *Client) Subscribe(topic string, handler MessageHandler) error {
	if err := c.subscribe(topic, handler); err != nil {
		return err
	}

	c.mu.Lock()
	c.subs = append(c.subs, subscription{queue: topic, handler: handler})
	c.mu.Unlock()

	return nil
}

// subscribe performs the actual QueueDeclare + Qos + Consume + goroutine launch
// without recording the subscription (used by both Subscribe and reconnect).
func (c *Client) subscribe(queue string, handler MessageHandler) error {
	c.mu.RLock()
	conn := c.conn
	c.mu.RUnlock()

	if conn == nil {
		return port.NewBrokerError("broker: Subscribe: not connected", nil)
	}

	ch, err := conn.Channel()
	if err != nil {
		return mapError(err, "Subscribe/Channel")
	}

	_, err = ch.QueueDeclare(queue, true, false, false, false, standardQueueArgs())
	if err != nil {
		_ = ch.Close()
		return mapError(err, "Subscribe/QueueDeclare")
	}

	// Set prefetch count for flow control (BRE-026).
	if err := ch.Qos(c.consumerCfg.Prefetch, 0, false); err != nil {
		_ = ch.Close()
		return mapError(err, "Subscribe/Qos")
	}

	deliveries, err := ch.Consume(queue, "", false, false, false, false, nil)
	if err != nil {
		_ = ch.Close()
		return mapError(err, "Subscribe/Consume")
	}

	c.wg.Add(1)
	go c.consumeLoop(ch, deliveries, handler)

	return nil
}

// consumeLoop processes deliveries from the channel. On handler success it
// Acks the delivery; on handler error it Nacks with requeue. The loop
// exits when the deliveries channel closes or the done channel is signalled.
func (c *Client) consumeLoop(ch AMQPChannelAPI, deliveries <-chan amqp.Delivery, handler MessageHandler) {
	defer c.wg.Done()
	defer ch.Close()

	for {
		select {
		case <-c.done:
			return
		case d, ok := <-deliveries:
			if !ok {
				return
			}
			if err := handler(c.cancelCtx, d.Body); err != nil {
				_ = d.Nack(false, true)
			} else {
				_ = d.Ack(false)
			}
		}
	}
}

// DeclareTopology declares all queues that DM operates on:
//   - 7 incoming queues with standard policies (BRE-026)
//   - 3 DLQ quorum queues (REV-025)
//
// Uses a temporary channel that is closed after declarations.
// Should be called once at startup after NewClient.
func (c *Client) DeclareTopology() error {
	c.mu.RLock()
	conn := c.conn
	c.mu.RUnlock()

	if conn == nil {
		return port.NewBrokerError("broker: DeclareTopology: not connected", nil)
	}

	ch, err := conn.Channel()
	if err != nil {
		return mapError(err, "DeclareTopology/Channel")
	}
	defer ch.Close()

	return c.declareTopologyOnChannel(ch)
}

// declareTopologyOnChannel declares all queue topology on the given channel.
// Called from DeclareTopology (startup) and reconnectWithBackoff (best-effort).
func (c *Client) declareTopologyOnChannel(ch AMQPChannelAPI) error {
	// Incoming queues with standard policies.
	incomingQueues := []string{
		c.cfg.TopicDPArtifactsProcessingReady,
		c.cfg.TopicDPRequestsSemanticTree,
		c.cfg.TopicDPArtifactsDiffReady,
		c.cfg.TopicLICArtifactsAnalysisReady,
		c.cfg.TopicLICRequestsArtifacts,
		c.cfg.TopicREArtifactsReportsReady,
		c.cfg.TopicRERequestsArtifacts,
	}

	stdArgs := standardQueueArgs()
	for _, q := range incomingQueues {
		if _, err := ch.QueueDeclare(q, true, false, false, false, stdArgs); err != nil {
			return mapError(err, "DeclareTopology/QueueDeclare/"+q)
		}
	}

	// DLQ quorum queues with extended retention.
	dlqQueues := []string{
		c.cfg.TopicDMDLQIngestionFailed,
		c.cfg.TopicDMDLQQueryFailed,
		c.cfg.TopicDMDLQInvalidMessage,
	}

	dArgs := dlqQueueArgs()
	for _, q := range dlqQueues {
		if _, err := ch.QueueDeclare(q, true, false, false, false, dArgs); err != nil {
			return mapError(err, "DeclareTopology/QueueDeclare/"+q)
		}
	}

	return nil
}

// standardQueueArgs returns the AMQP table for standard queue policies.
// BRE-026: x-max-length + x-overflow=reject-publish + x-message-ttl.
// Values use int32 to match AMQP long-int encoding and prevent 406 errors
// if queues were previously declared with int32 values by other clients.
func standardQueueArgs() amqp.Table {
	return amqp.Table{
		"x-max-length":  int32(standardMaxLength),
		"x-overflow":    "reject-publish",
		"x-message-ttl": int32(standardMessageTTL),
	}
}

// dlqQueueArgs returns the AMQP table for DLQ quorum queue policies.
// REV-025: x-queue-type=quorum for Raft replication and native x-delivery-count.
// NOTE: x-message-ttl on quorum queues requires RabbitMQ >= 3.10.
// NOTE: x-overflow is intentionally omitted — quorum queues only support drop-head.
func dlqQueueArgs() amqp.Table {
	return amqp.Table{
		"x-queue-type":  "quorum",
		"x-max-length":  int32(dlqMaxLength),
		"x-message-ttl": int32(dlqMessageTTL),
	}
}

// IsConnected returns true if the broker connection is healthy.
// Used by the health check handler for readiness probes.
func (c *Client) IsConnected() bool {
	return c.healthy.Load()
}

// Close performs a graceful shutdown: signals goroutines to stop via the done
// channel, closes the publish channel and connection, and waits for all
// goroutines to finish. Safe to call multiple times.
func (c *Client) Close() error {
	c.mu.Lock()

	// Idempotent: check if already closed.
	select {
	case <-c.done:
		c.mu.Unlock()
		return nil
	default:
	}

	close(c.done)
	c.cancelFn()

	pubCh := c.pubCh
	conn := c.conn
	c.pubCh = nil
	c.confirmCh = nil
	c.conn = nil
	c.mu.Unlock()

	var firstErr error

	if pubCh != nil {
		if err := pubCh.Close(); err != nil {
			firstErr = err
		}
	}
	if conn != nil {
		if err := conn.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	c.wg.Wait()
	return firstErr
}
