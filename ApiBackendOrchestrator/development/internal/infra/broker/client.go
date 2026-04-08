package broker

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"sync"
	"time"

	"contractpro/api-orchestrator/internal/infra/observability/logger"

	amqp "github.com/rabbitmq/amqp091-go"
)

// queuePrefix is prepended to topic names to form the queue name.
const queuePrefix = "orch.sub."

// Publish retry settings.
const (
	publishMaxAttempts = 3
	publishBaseDelay   = 100 * time.Millisecond
)

// AMQPAPI covers connection-level AMQP methods.
// Consumer-side interface for dependency inversion and testability.
type AMQPAPI interface {
	Channel() (AMQPChannelAPI, error)
	Close() error
	NotifyClose(receiver chan *amqp.Error) chan *amqp.Error
	IsClosed() bool
}

// AMQPChannelAPI covers channel-level AMQP methods.
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

// MessageHandler is invoked for each consumed message.
// Return nil = Ack, return error = Nack with requeue.
type MessageHandler func(ctx context.Context, body []byte) error

// subscription tracks an active consumer for reconnection.
type subscription struct {
	topic   string
	queue   string
	handler MessageHandler
}

// Client is a RabbitMQ client with publisher confirms, auto-reconnect,
// configurable prefetch, and graceful shutdown.
type Client struct {
	address   string
	prefetch  int
	conn      AMQPAPI
	pubCh     AMQPChannelAPI
	pubNotify chan amqp.Confirmation
	mu        sync.RWMutex
	pubMu     sync.Mutex // serializes publish+confirm cycle to prevent confirm misattribution
	subs      []subscription
	done      chan struct{}
	wg        sync.WaitGroup
	dialFn    func(addr string) (AMQPAPI, error)
	cancelFn  context.CancelFunc
	cancelCtx context.Context
	log       *logger.Logger
}

// amqpConnWrapper adapts *amqp.Connection to the AMQPAPI interface.
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

func (w *amqpConnWrapper) Close() error                                         { return w.conn.Close() }
func (w *amqpConnWrapper) NotifyClose(r chan *amqp.Error) chan *amqp.Error        { return w.conn.NotifyClose(r) }
func (w *amqpConnWrapper) IsClosed() bool                                        { return w.conn.IsClosed() }

// amqpChanWrapper adapts *amqp.Channel to the AMQPChannelAPI interface.
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
func (w *amqpChanWrapper) Close() error                                    { return w.ch.Close() }
func (w *amqpChanWrapper) NotifyClose(r chan *amqp.Error) chan *amqp.Error  { return w.ch.NotifyClose(r) }

// NewClient dials RabbitMQ, sets up publisher confirms, and starts the
// auto-reconnect loop.
func NewClient(address string, useTLS bool, prefetch int, log *logger.Logger) (*Client, error) {
	dialFn := makeDialFn(useTLS)

	conn, err := dialFn(address)
	if err != nil {
		return nil, fmt.Errorf("broker: dial: %w", err)
	}

	pubCh, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("broker: publish channel: %w", err)
	}

	if err := pubCh.Confirm(false); err != nil {
		_ = pubCh.Close()
		_ = conn.Close()
		return nil, fmt.Errorf("broker: enable confirm mode: %w", err)
	}

	pubNotify := pubCh.NotifyPublish(make(chan amqp.Confirmation, 1))

	ctx, cancel := context.WithCancel(context.Background())

	c := &Client{
		address:   address,
		prefetch:  prefetch,
		conn:      conn,
		pubCh:     pubCh,
		pubNotify: pubNotify,
		done:      make(chan struct{}),
		dialFn:    dialFn,
		cancelFn:  cancel,
		cancelCtx: ctx,
		log:       log.With("component", "broker"),
	}

	c.wg.Add(1)
	go c.reconnectLoop()

	c.log.Info(ctx, "broker connected")
	return c, nil
}

// makeDialFn returns a dial function that wraps the raw *amqp.Connection.
func makeDialFn(useTLS bool) func(string) (AMQPAPI, error) {
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

// newClientWithAMQP creates a Client with injected AMQP objects.
// Used exclusively in tests — does NOT start the reconnect loop.
func newClientWithAMQP(conn AMQPAPI, dialFn func(string) (AMQPAPI, error), prefetch int, log *logger.Logger) *Client {
	var pubCh AMQPChannelAPI
	var pubNotify chan amqp.Confirmation

	if conn != nil {
		pubCh, _ = conn.Channel()
		if pubCh != nil {
			_ = pubCh.Confirm(false)
			pubNotify = pubCh.NotifyPublish(make(chan amqp.Confirmation, 1))
		}
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Client{
		conn:      conn,
		pubCh:     pubCh,
		pubNotify: pubNotify,
		prefetch:  prefetch,
		done:      make(chan struct{}),
		dialFn:    dialFn,
		cancelCtx: ctx,
		cancelFn:  cancel,
		log:       log.With("component", "broker"),
	}
}

// Publish sends payload to the given topic with publisher confirms and retry.
// Retries up to 3 times with exponential backoff (100ms, 200ms) for transient errors.
func (c *Client) Publish(ctx context.Context, topic string, payload []byte) error {
	var lastErr error

	for attempt := 0; attempt < publishMaxAttempts; attempt++ {
		if attempt > 0 {
			delay := publishBaseDelay << (attempt - 1) // 100ms, 200ms
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-c.done:
				return errClientClosed("Publish")
			case <-time.After(delay):
			}
		}

		err := c.publishOnce(ctx, topic, payload)
		if err == nil {
			return nil
		}
		lastErr = err

		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}

		if !isRetryable(err) {
			return err
		}

		c.log.Warn(ctx, "broker: publish retry",
			"topic", topic,
			"attempt", attempt+1,
			logger.ErrorAttr(err),
		)
	}

	return fmt.Errorf("broker: Publish: %w: last error: %v", ErrPublishMaxRetriesExhausted, lastErr)
}

// publishOnce sends a single message and waits for broker confirmation.
// pubMu serializes the publish+confirm cycle to prevent confirm misattribution
// when multiple goroutines publish concurrently.
func (c *Client) publishOnce(ctx context.Context, topic string, payload []byte) error {
	// Snapshot channel references under RLock, then release before I/O.
	c.mu.RLock()
	ch := c.pubCh
	notify := c.pubNotify
	c.mu.RUnlock()

	if ch == nil {
		return errNotConnected("Publish")
	}

	// Serialize the entire publish+confirm cycle so that each goroutine
	// reads the confirmation for its own message, not another's.
	c.pubMu.Lock()
	defer c.pubMu.Unlock()

	err := ch.PublishWithContext(ctx, "", topic, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Body:         payload,
	})
	if err != nil {
		return mapError(err, "Publish")
	}

	select {
	case confirm, ok := <-notify:
		if !ok {
			return fmt.Errorf("broker: Publish: %w", ErrPublishConfirmFailed)
		}
		if !confirm.Ack {
			return fmt.Errorf("broker: Publish: %w", ErrPublishNacked)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return errClientClosed("Publish")
	}
}

// Subscribe creates a consumer on queue orch.sub.{topic} with manual ACK/NACK
// and the configured prefetch count. The subscription is registered before the
// consumer starts so that a concurrent reconnect will not miss it.
func (c *Client) Subscribe(topic string, handler MessageHandler) error {
	queue := queuePrefix + topic

	// Register subscription first so a concurrent reconnect re-subscribes it.
	c.mu.Lock()
	idx := len(c.subs)
	c.subs = append(c.subs, subscription{topic: topic, queue: queue, handler: handler})
	c.mu.Unlock()

	if err := c.subscribe(topic, queue, handler); err != nil {
		// Roll back on failure.
		c.mu.Lock()
		c.subs = append(c.subs[:idx], c.subs[idx+1:]...)
		c.mu.Unlock()
		return err
	}

	return nil
}

// subscribe sets up a consumer channel with QoS, declares the queue, and starts
// the consume loop. Used by both Subscribe and reconnect.
func (c *Client) subscribe(topic, queue string, handler MessageHandler) error {
	c.mu.RLock()
	conn := c.conn
	prefetch := c.prefetch
	c.mu.RUnlock()

	if conn == nil {
		return errNotConnected("Subscribe")
	}

	ch, err := conn.Channel()
	if err != nil {
		return mapError(err, "Subscribe/Channel")
	}

	if err := ch.Qos(prefetch, 0, false); err != nil {
		_ = ch.Close()
		return mapError(err, "Subscribe/Qos")
	}

	_, err = ch.QueueDeclare(queue, true, false, false, false, nil)
	if err != nil {
		_ = ch.Close()
		return mapError(err, "Subscribe/QueueDeclare")
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

// consumeLoop processes messages from the deliveries channel until done
// is closed or the channel is disconnected.
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
				if nackErr := d.Nack(false, true); nackErr != nil {
					c.log.Warn(c.cancelCtx, "broker: nack failed", logger.ErrorAttr(nackErr))
				}
			} else {
				if ackErr := d.Ack(false); ackErr != nil {
					c.log.Warn(c.cancelCtx, "broker: ack failed", logger.ErrorAttr(ackErr))
				}
			}
		}
	}
}

// Ping checks broker connectivity for health/readiness probes.
func (c *Client) Ping() error {
	c.mu.RLock()
	conn := c.conn
	c.mu.RUnlock()

	if conn == nil {
		return errNotConnected("Ping")
	}
	if conn.IsClosed() {
		return fmt.Errorf("broker: Ping: %w", ErrClientClosed)
	}
	return nil
}

// Close performs graceful shutdown: signals goroutines, cancels handler
// contexts, closes the publish channel and connection, and waits for all
// goroutines to finish. Safe to call multiple times.
func (c *Client) Close() error {
	c.mu.Lock()

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
	c.pubNotify = nil
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
