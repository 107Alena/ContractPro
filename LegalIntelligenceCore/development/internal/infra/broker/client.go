// Package broker is the RabbitMQ adapter for the Legal Intelligence Core.
//
// It owns the full LIC exchange/queue topology (integration-contracts.md
// §6.1–§6.4), publisher confirms with bounded retry, manual-ack consumption
// with a per-channel prefetch, automatic reconnection with exponential
// backoff, and a lightweight Ping for /readyz.
//
// Layering: this package is pure infrastructure. It implements no domain port
// (LIC has none for the broker, mirroring DocumentProcessing). Higher-level
// adapters under internal/ingress (consumer, LIC-TASK-043/044) and
// internal/egress (publisher, LIC-TASK-039/042) build on the primitives here.
// The x-death-count → retry-level escalation logic is a consumer concern and
// deliberately lives in those adapters, not here (code-architect Q3/Q5).
//
// amqp091 types never cross the Subscribe seam: deliveries are exposed through
// the broker-local Delivery interface so consumer adapters never import
// github.com/rabbitmq/amqp091-go.
package broker

import (
	"context"
	"sync"

	amqp "github.com/rabbitmq/amqp091-go"

	"contractpro/legal-intelligence-core/internal/config"
)

// AMQPAPI covers the Connection-level methods the client needs. Declaring it
// here (consumer-side interface) keeps the dependency inverted and enables
// unit testing against an in-memory fake without a live broker.
type AMQPAPI interface {
	Channel() (AMQPChannelAPI, error)
	Close() error
	NotifyClose(receiver chan *amqp.Error) chan *amqp.Error
	IsClosed() bool
}

// AMQPChannelAPI covers the Channel-level methods the client needs across
// topology declaration, publishing (deferred publisher confirms) and
// consuming.
type AMQPChannelAPI interface {
	ExchangeDeclare(name, kind string, durable, autoDelete, internal, noWait bool, args amqp.Table) error
	QueueDeclare(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error)
	QueueBind(name, key, exchange string, noWait bool, args amqp.Table) error
	Qos(prefetchCount, prefetchSize int, global bool) error
	Confirm(noWait bool) error
	Consume(queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args amqp.Table) (<-chan amqp.Delivery, error)
	PublishWithDeferredConfirmWithContext(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp.Publishing) (PublishConfirm, error)
	NotifyClose(receiver chan *amqp.Error) chan *amqp.Error
	Close() error
}

// PublishConfirm is the subset of *amqp.DeferredConfirmation the publish path
// uses. Using deferred confirmations (rather than a shared NotifyPublish
// channel) makes each publish own its confirmation, eliminating the
// stale-confirmation correlation bug a serialized NotifyPublish channel would
// otherwise have on timeout+retry (code-architect Q4).
type PublishConfirm interface {
	// Done is closed once the broker acks or nacks the delivery.
	Done() <-chan struct{}
	// Acked reports the outcome; only meaningful after Done is closed.
	Acked() bool
}

// MessageHandler is invoked for each consumed message. The handler OWNS the
// delivery lifecycle: it MUST terminate every delivery via d.Ack, d.Nack or
// d.Reject (Option B, code-architect Q5). The consume loop does not ack/nack
// on the handler's behalf — that is what lets the consumer adapter
// (LIC-TASK-043) implement x-death-driven retry escalation. A returned error
// is advisory (surfaced to callers/tests); it does not change ack state.
type MessageHandler func(ctx context.Context, d Delivery) error

// subscription records an active queue subscription so it can be
// re-established after a reconnect. id is a monotonic handle used to roll
// back a record whose consumer failed to start (handlers are funcs and thus
// not comparable).
type subscription struct {
	id      uint64
	queue   string
	handler MessageHandler
}

// Client is the RabbitMQ broker client. It is safe for concurrent use; the
// publish path serializes on mu (publisher confirms share one channel and
// must not race a reconnect swap — code-architect Q4/risk 5).
type Client struct {
	cfg config.BrokerConfig

	mu    sync.Mutex     // guards conn, pubCh, subs (short critical sections only)
	pubMu sync.Mutex     // serializes Publish (publisher confirms share one channel)
	conn   AMQPAPI        // current connection
	pubCh  AMQPChannelAPI // dedicated publish channel, in Confirm mode
	subs   []subscription // active subscriptions (re-subscribed after reconnect)
	subSeq uint64         // monotonic subscription id source (guarded by mu)

	done      chan struct{}      // closed by Close()
	wg        sync.WaitGroup     // tracks reconnect loop + consumer goroutines
	dialFn    func(addr string) (AMQPAPI, error)
	cancelCtx context.Context    // parent context for message handlers
	cancelFn  context.CancelFunc // cancels handler contexts on Close
}

// --- real amqp091 wrappers --------------------------------------------------

type amqpConnWrapper struct{ conn *amqp.Connection }

func (w *amqpConnWrapper) Channel() (AMQPChannelAPI, error) {
	ch, err := w.conn.Channel()
	if err != nil {
		return nil, err
	}
	return &amqpChanWrapper{ch: ch}, nil
}
func (w *amqpConnWrapper) Close() error { return w.conn.Close() }
func (w *amqpConnWrapper) NotifyClose(r chan *amqp.Error) chan *amqp.Error {
	return w.conn.NotifyClose(r)
}
func (w *amqpConnWrapper) IsClosed() bool { return w.conn.IsClosed() }

type amqpChanWrapper struct{ ch *amqp.Channel }

func (w *amqpChanWrapper) ExchangeDeclare(name, kind string, durable, autoDelete, internal, noWait bool, args amqp.Table) error {
	return w.ch.ExchangeDeclare(name, kind, durable, autoDelete, internal, noWait, args)
}
func (w *amqpChanWrapper) QueueDeclare(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error) {
	return w.ch.QueueDeclare(name, durable, autoDelete, exclusive, noWait, args)
}
func (w *amqpChanWrapper) QueueBind(name, key, exchange string, noWait bool, args amqp.Table) error {
	return w.ch.QueueBind(name, key, exchange, noWait, args)
}
func (w *amqpChanWrapper) Qos(prefetchCount, prefetchSize int, global bool) error {
	return w.ch.Qos(prefetchCount, prefetchSize, global)
}
func (w *amqpChanWrapper) Confirm(noWait bool) error { return w.ch.Confirm(noWait) }
func (w *amqpChanWrapper) Consume(queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args amqp.Table) (<-chan amqp.Delivery, error) {
	return w.ch.Consume(queue, consumer, autoAck, exclusive, noLocal, noWait, args)
}
func (w *amqpChanWrapper) PublishWithDeferredConfirmWithContext(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp.Publishing) (PublishConfirm, error) {
	dc, err := w.ch.PublishWithDeferredConfirmWithContext(ctx, exchange, key, mandatory, immediate, msg)
	if err != nil {
		return nil, err
	}
	return dc, nil
}
func (w *amqpChanWrapper) NotifyClose(r chan *amqp.Error) chan *amqp.Error {
	return w.ch.NotifyClose(r)
}
func (w *amqpChanWrapper) Close() error { return w.ch.Close() }

// Compile-time assertions that the real wrappers satisfy the injected APIs.
var (
	_ AMQPAPI        = (*amqpConnWrapper)(nil)
	_ AMQPChannelAPI = (*amqpChanWrapper)(nil)
)

// NewClient dials the broker, opens a dedicated publish channel in Confirm
// mode, asserts the full LIC topology, and starts the reconnect loop.
//
// Topology is declared at construction so misconfiguration (bad credentials,
// arg drift, missing vhost) fails fast at startup rather than on first
// publish. DeclareTopology is idempotent and is re-asserted on every
// reconnect.
func NewClient(cfg config.BrokerConfig) (*Client, error) {
	dialFn := func(addr string) (AMQPAPI, error) {
		conn, err := amqp.Dial(addr)
		if err != nil {
			return nil, err
		}
		return &amqpConnWrapper{conn: conn}, nil
	}

	conn, err := dialFn(cfg.URL)
	if err != nil {
		return nil, &BrokerError{Op: "Dial", Retryable: true, Cause: redactURLCredentials(err, cfg.URL)}
	}

	ctx, cancel := context.WithCancel(context.Background())
	c := &Client{
		cfg:       cfg,
		conn:      conn,
		done:      make(chan struct{}),
		dialFn:    dialFn,
		cancelCtx: ctx,
		cancelFn:  cancel,
	}

	if err := c.openPublishChannel(); err != nil {
		_ = conn.Close()
		cancel()
		return nil, err
	}

	if err := c.DeclareTopology(ctx); err != nil {
		c.mu.Lock()
		pubCh := c.pubCh
		c.mu.Unlock()
		if pubCh != nil {
			_ = pubCh.Close()
		}
		_ = conn.Close()
		cancel()
		return nil, err
	}

	c.wg.Add(1)
	go c.reconnectLoop()

	return c, nil
}

// newClientWithAMQP builds a Client around an injected connection for tests.
// It does NOT dial, declare topology, open a publish channel, or start the
// reconnect loop — tests drive those explicitly (mirrors the DP test seam).
func newClientWithAMQP(cfg config.BrokerConfig, conn AMQPAPI, dialFn func(string) (AMQPAPI, error)) *Client {
	ctx, cancel := context.WithCancel(context.Background())
	return &Client{
		cfg:       cfg,
		conn:      conn,
		done:      make(chan struct{}),
		dialFn:    dialFn,
		cancelCtx: ctx,
		cancelFn:  cancel,
	}
}

// openPublishChannelOn opens a fresh channel on the given connection and puts
// it in Confirm mode. The Confirm flag is per-channel and must be re-applied
// for every new channel, including after a reconnect (code-architect Q6).
// The channel is returned, NOT stored — the reconnect path validates a new
// connection fully before publishing it into c, so a connection that fails
// mid-setup is never observable and never leaks.
func (c *Client) openPublishChannelOn(conn AMQPAPI) (AMQPChannelAPI, error) {
	if conn == nil {
		return nil, &BrokerError{Op: "openPublishChannel", Retryable: true, Cause: ErrNotConnected}
	}
	ch, err := conn.Channel()
	if err != nil {
		return nil, mapError(err, "openPublishChannel/Channel")
	}
	if err := ch.Confirm(false); err != nil {
		_ = ch.Close()
		return nil, mapError(err, "openPublishChannel/Confirm")
	}
	return ch, nil
}

// openPublishChannel opens the dedicated publish channel on the current
// connection and stores it. Used at startup (NewClient).
func (c *Client) openPublishChannel() error {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()

	ch, err := c.openPublishChannelOn(conn)
	if err != nil {
		return err
	}

	c.mu.Lock()
	old := c.pubCh
	c.pubCh = ch
	c.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	return nil
}

// Ping verifies broker liveness for /readyz. IsClosed only reflects locally
// observed closure, so Ping additionally opens and immediately closes a
// transient channel: channel.open performs a real round-trip that proves the
// broker is responsive end-to-end (code-architect Q7). The live publish
// channel is intentionally not reused — a failed ping must not perturb
// publish state.
func (c *Client) Ping(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()

	if conn == nil || conn.IsClosed() {
		return &BrokerError{Op: "Ping", Retryable: true, Cause: ErrNotConnected}
	}

	// conn.Channel() takes no context: on a half-open TCP connection (broker
	// host gone, no RST) it can block until the OS TCP timeout (minutes),
	// far past the /readyz probe deadline. Run it off-goroutine and honour
	// ctx; an orphaned channel from a timed-out call is reaped when the dead
	// connection is recycled by the reconnect loop (code-reviewer S3).
	type chanResult struct {
		ch  AMQPChannelAPI
		err error
	}
	resCh := make(chan chanResult, 1)
	go func() {
		ch, err := conn.Channel()
		resCh <- chanResult{ch: ch, err: err}
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case res := <-resCh:
		if res.err != nil {
			return mapError(res.err, "Ping/Channel")
		}
		// A failed channel.close is itself a liveness signal — surface it
		// rather than reporting a falsely-ready broker (security-engineer
		// N-1).
		if err := res.ch.Close(); err != nil {
			return mapError(err, "Ping/Close")
		}
		return nil
	}
}

// Close performs a graceful, idempotent shutdown: signals goroutines to stop,
// cancels in-flight handler contexts, closes the publish channel and
// connection, and waits for all goroutines to finish.
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
