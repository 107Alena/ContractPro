package broker

import (
	"context"
	"sync"

	amqp "github.com/rabbitmq/amqp091-go"

	"contractpro/document-processing/internal/config"
	"contractpro/document-processing/internal/domain/port"
)

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

// Client is a RabbitMQ message broker client that supports publishing,
// subscribing, and automatic reconnection with exponential backoff.
//
// Client does not implement any domain port directly — it is used by
// higher-level adapters (egress/publisher, ingress/consumer) that
// implement the ports.
type Client struct {
	address  string
	conn     AMQPAPI
	pubCh    AMQPChannelAPI // dedicated channel for publishing
	mu       sync.RWMutex   // protects conn, pubCh, subs
	subs     []subscription // active subscriptions (re-subscribe after reconnect)
	done     chan struct{}   // closed on Close()
	wg       sync.WaitGroup // tracks goroutines (reconnect loop, consumers)
	dialFn   func(addr string) (AMQPAPI, error) // injectable for testing
	cancelFn context.CancelFunc // cancels handler contexts on Close
	cancelCtx context.Context   // parent context for message handlers
}

// amqpConnWrapper adapts a real *amqp.Connection to the AMQPAPI interface.
// The Channel() method wraps the returned *amqp.Channel in amqpChanWrapper
// so it satisfies AMQPChannelAPI.
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

func (w *amqpChanWrapper) Close() error {
	return w.ch.Close()
}

func (w *amqpChanWrapper) NotifyClose(receiver chan *amqp.Error) chan *amqp.Error {
	return w.ch.NotifyClose(receiver)
}

// NewClient creates a Client configured for the given BrokerConfig.
// It dials the AMQP server, creates a dedicated publish channel,
// and starts the reconnection loop.
func NewClient(cfg config.BrokerConfig) (*Client, error) {
	dialFn := func(addr string) (AMQPAPI, error) {
		conn, err := amqp.Dial(addr)
		if err != nil {
			return nil, err
		}
		return &amqpConnWrapper{conn: conn}, nil
	}

	conn, err := dialFn(cfg.Address)
	if err != nil {
		return nil, port.NewBrokerError("broker: dial failed", err)
	}

	pubCh, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, port.NewBrokerError("broker: create publish channel", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	c := &Client{
		address:   cfg.Address,
		conn:      conn,
		pubCh:     pubCh,
		done:      make(chan struct{}),
		dialFn:    dialFn,
		cancelCtx: ctx,
		cancelFn:  cancel,
	}

	c.wg.Add(1)
	go c.reconnectLoop()

	return c, nil
}

// newClientWithAMQP creates a Client with an injected AMQPAPI connection
// and dial function. Used for testing — does NOT start the reconnect loop.
func newClientWithAMQP(conn AMQPAPI, dialFn func(string) (AMQPAPI, error)) *Client {
	var pubCh AMQPChannelAPI
	if conn != nil {
		pubCh, _ = conn.Channel()
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Client{
		conn:      conn,
		pubCh:     pubCh,
		done:      make(chan struct{}),
		dialFn:    dialFn,
		cancelCtx: ctx,
		cancelFn:  cancel,
	}
}

// Publish sends a message to the given topic (routing key) via the default
// exchange. The payload is published as application/json with persistent
// delivery mode. The RLock is held for the entire publish call to prevent
// a TOCTOU race with reconnection swapping the channel.
func (c *Client) Publish(ctx context.Context, topic string, payload []byte) error {
	c.mu.RLock()
	ch := c.pubCh
	if ch == nil {
		c.mu.RUnlock()
		return port.NewBrokerError("broker: Publish: not connected (nil channel)", nil)
	}
	err := ch.PublishWithContext(ctx, "", topic, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Body:         payload,
	})
	c.mu.RUnlock()

	if err != nil {
		return mapError(err, "Publish")
	}
	return nil
}

// Subscribe declares a durable queue for the given topic, starts consuming,
// and launches a goroutine that dispatches deliveries to the handler.
// The subscription is recorded so it can be re-established after reconnect.
func (c *Client) Subscribe(topic string, handler MessageHandler) error {
	if err := c.subscribe(topic, handler); err != nil {
		return err
	}

	c.mu.Lock()
	c.subs = append(c.subs, subscription{queue: topic, handler: handler})
	c.mu.Unlock()

	return nil
}

// subscribe performs the actual QueueDeclare + Consume + goroutine launch
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
