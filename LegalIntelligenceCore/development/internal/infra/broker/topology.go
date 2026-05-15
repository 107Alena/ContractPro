package broker

import (
	"context"
	"fmt"
	"math"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Topology constants frozen by integration-contracts.md §6.1–§6.4.
const (
	// exchangeKindTopic — ALL four LIC exchanges (events, responses,
	// commands, dlx) are topic exchanges with routing key = topic name
	// (§6.1). The DLX MUST be topic, never fanout: a fanout DLX would
	// broadcast every dead-letter to all 18 retry queues + main queues
	// simultaneously → message storm (code-architect risk 2).
	exchangeKindTopic = "topic"

	// mainQueueMessageTTLMillis — x-message-ttl for inbound main queues,
	// 24h (§6.2). 86_400_000 < math.MaxInt32, so int32 is safe and is the
	// conventional amqp.Table numeric encoding for TTLs.
	mainQueueMessageTTLMillis int32 = 86_400_000

	// mainQueueMaxLength — x-max-length anti-runaway cap (§6.2). Overflow
	// behaviour is the RabbitMQ default (drop-head); the breach alert is an
	// observability concern, not this adapter's (code-architect risk 6).
	mainQueueMaxLength int32 = 100_000

	// retrySuffix builds lic.q.<topic>.retry.<N> retry-queue names (§6.4).
	retrySuffixFmt = "%s.retry.%d"
)

// subscriptionSpec is one frozen queue→exchange→routingKey mapping from
// integration-contracts.md §6.1. The exchange names come from BrokerConfig
// (operator-overridable); the queue names and routing keys are FROZEN
// contract identifiers and are therefore literals here.
type subscriptionSpec struct {
	queue      string
	exchange   string
	routingKey string
}

// subscriptionSpecs returns the six LIC subscriptions (§6.1) with exchange
// names resolved from config. Order is deterministic so topology declaration
// and tests are reproducible.
func (c *Client) subscriptionSpecs() []subscriptionSpec {
	return []subscriptionSpec{
		{"lic.q.version-artifacts-ready", c.cfg.ExchangeEvents, "dm.events.version-artifacts-ready"},
		{"lic.q.version-created", c.cfg.ExchangeEvents, "dm.events.version-created"},
		{"lic.q.artifacts-provided", c.cfg.ExchangeResponses, "dm.responses.artifacts-provided"},
		{"lic.q.lic-persist-confirm", c.cfg.ExchangeResponses, "dm.responses.lic-artifacts-persisted"},
		{"lic.q.lic-persist-fail", c.cfg.ExchangeResponses, "dm.responses.lic-artifacts-persist-failed"},
		{"lic.q.user-confirmed-type", c.cfg.ExchangeCommands, "orch.commands.user-confirmed-type"},
	}
}

// retryTTLs returns the three DLX-loop retry TTLs in order (retry.1, retry.2,
// retry.3) from config — defaults 2s/10s/60s (§6.4).
func (c *Client) retryTTLs() []time.Duration {
	return []time.Duration{
		c.cfg.ConsumerRetryTTL1,
		c.cfg.ConsumerRetryTTL2,
		c.cfg.ConsumerRetryTTL3,
	}
}

// mainQueueArgs builds the x-arguments for an inbound main queue. It is the
// SINGLE builder used by both the startup declare and the reconnect
// re-declare so the arguments are byte-identical — any drift would make
// RabbitMQ reject the re-declare with 406 PRECONDITION_FAILED, turning a
// transient reconnect into a hard outage (code-architect risk 4).
//
// Deliberately ABSENT: x-dead-letter-routing-key. The §6.4 ASCII diagram
// annotates the main queue with "...retry.1", but the consumer-side text
// (§6.4) specifies the retry level is chosen dynamically by the consumer from
// x-death[].count (retry.1 / retry.2 / retry.3 / lic.dlq.consumer-failed). A
// static routing key here would pin every dead-letter to retry.1 and make
// multi-level escalation impossible. The escalation seam belongs to the
// consumer adapter (LIC-TASK-043), which publishes to the DLX with the
// computed key itself. This is an intentional, spec-intent-preserving
// deviation from the literal diagram annotation (code-architect Q3).
func (c *Client) mainQueueArgs() amqp.Table {
	return amqp.Table{
		"x-message-ttl":          mainQueueMessageTTLMillis,
		"x-max-length":           mainQueueMaxLength,
		"x-dead-letter-exchange": c.cfg.ExchangeDLX,
	}
}

// retryQueueArgs builds the x-arguments for a retry queue at the given TTL.
// On TTL expiry the message is dead-lettered back through the DLX with the
// ORIGINAL routing key, returning it to the main queue (which is also bound
// to the DLX on that key). Same single-builder discipline as mainQueueArgs.
func (c *Client) retryQueueArgs(ttl time.Duration, originalRoutingKey string) amqp.Table {
	return amqp.Table{
		"x-message-ttl":             ttlMillisInt32(ttl),
		"x-dead-letter-exchange":    c.cfg.ExchangeDLX,
		"x-dead-letter-routing-key": originalRoutingKey,
	}
}

// ttlMillisInt32 converts a TTL to the int32 millisecond count RabbitMQ
// expects for x-message-ttl. The config layer already bounds retry TTLs to
// 24h (security-engineer MF-2), so this is defense-in-depth: an out-of-range
// or non-positive value is clamped to the 24h main-queue TTL rather than
// being allowed to overflow int32 into a negative value (which RabbitMQ
// rejects with 406 PRECONDITION_FAILED → permanent reconnect outage).
func ttlMillisInt32(ttl time.Duration) int32 {
	ms := ttl.Milliseconds()
	if ms <= 0 || ms > int64(math.MaxInt32) {
		return mainQueueMessageTTLMillis
	}
	return int32(ms)
}

// retryQueueName builds lic.q.<topic>.retry.<level> (level is 1-based).
func retryQueueName(mainQueue string, level int) string {
	return fmt.Sprintf(retrySuffixFmt, mainQueue, level)
}

// DeclareTopology idempotently asserts the full LIC topology:
//
//   - 4 topic exchanges: events, responses, commands, dlx (durable);
//   - 6 main queues (durable, 24h TTL, max-length, DLX) bound to their
//     source exchange on the original routing key AND to the DLX on the same
//     key (the retry-return path);
//   - 18 retry queues (6 topics × 3 levels) with TTL 2s/10s/60s, each bound
//     to the DLX on lic.q.<topic>.retry.<N>, dead-lettering back to the
//     original routing key on expiry.
//
// All declarations run on a dedicated transient channel: a failed
// declaration closes its channel in AMQP, so isolating it protects the
// long-lived publish channel. Idempotent when arguments match — safe to call
// at startup and again after every reconnect (code-architect Q2/Q6).
func (c *Client) DeclareTopology(ctx context.Context) error {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	return c.declareTopologyOn(ctx, conn)
}

// declareTopologyOn runs the declaration against an explicit connection. The
// reconnect path calls this on a freshly dialed connection BEFORE adopting
// it, so a connection that cannot host the topology is discarded without
// ever being observable through the client.
func (c *Client) declareTopologyOn(ctx context.Context, conn AMQPAPI) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if conn == nil {
		return &BrokerError{Op: "DeclareTopology", Retryable: true, Cause: ErrNotConnected}
	}

	ch, err := conn.Channel()
	if err != nil {
		return mapError(err, "DeclareTopology/Channel")
	}
	defer func() { _ = ch.Close() }()

	// Exchanges first — queues/bindings reference them (ordering pitfall,
	// code-architect Q6). Deterministic order for reproducible tests.
	for _, ex := range []string{
		c.cfg.ExchangeEvents,
		c.cfg.ExchangeResponses,
		c.cfg.ExchangeCommands,
		c.cfg.ExchangeDLX,
	} {
		if err := ch.ExchangeDeclare(ex, exchangeKindTopic, true, false, false, false, nil); err != nil {
			return mapError(err, "DeclareTopology/ExchangeDeclare "+ex)
		}
	}

	ttls := c.retryTTLs()
	for _, s := range c.subscriptionSpecs() {
		// Main queue.
		if _, err := ch.QueueDeclare(s.queue, true, false, false, false, c.mainQueueArgs()); err != nil {
			return mapError(err, "DeclareTopology/QueueDeclare "+s.queue)
		}
		// Inbound binding: source exchange → main queue on the topic key.
		if err := ch.QueueBind(s.queue, s.routingKey, s.exchange, false, nil); err != nil {
			return mapError(err, "DeclareTopology/QueueBind "+s.queue+"<-"+s.exchange)
		}
		// Retry-return binding: DLX → main queue on the same topic key, so
		// a retry message whose TTL expired re-enters the main queue.
		if err := ch.QueueBind(s.queue, s.routingKey, c.cfg.ExchangeDLX, false, nil); err != nil {
			return mapError(err, "DeclareTopology/QueueBind "+s.queue+"<-dlx")
		}

		// Retry queues (3 levels).
		for i, ttl := range ttls {
			level := i + 1
			rq := retryQueueName(s.queue, level)
			if _, err := ch.QueueDeclare(rq, true, false, false, false, c.retryQueueArgs(ttl, s.routingKey)); err != nil {
				return mapError(err, "DeclareTopology/QueueDeclare "+rq)
			}
			// DLX → retry queue on its dedicated retry routing key. The
			// consumer (LIC-TASK-043) publishes to the DLX with this key
			// to escalate by level.
			if err := ch.QueueBind(rq, rq, c.cfg.ExchangeDLX, false, nil); err != nil {
				return mapError(err, "DeclareTopology/QueueBind "+rq+"<-dlx")
			}
		}
	}

	return nil
}
