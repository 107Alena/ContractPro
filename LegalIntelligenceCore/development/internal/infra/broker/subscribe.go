package broker

import (
	amqp "github.com/rabbitmq/amqp091-go"
)

// xDeathMaxEntries / xDeathMaxRoutingKeys bound the work done decoding an
// x-death header. RabbitMQ realistically produces one entry per queue a
// message traversed (single digits) and a handful of routing keys; a hostile
// or corrupt upstream could still craft a frame-sized deeply-nested header.
// Capping keeps decode allocation O(1) in attacker input (security-engineer
// SF-1). Escalation only needs the highest count, so truncation is safe.
const (
	xDeathMaxEntries     = 64
	xDeathMaxRoutingKeys = 64
)

// XDeathEntry is an amqp091-free view of one entry in a message's x-death
// header. RabbitMQ records, per (queue, reason) pair, a cumulative Count of
// how many times the message dead-lettered through that queue. The consumer
// adapter (LIC-TASK-043) reads Count to pick the retry level per §6.4
// (0→retry.1, 1→retry.2, 2→retry.3, ≥MaxRedeliveries→lic.dlq.consumer-failed).
type XDeathEntry struct {
	Queue       string
	Reason      string
	Exchange    string
	Count       int64
	RoutingKeys []string
}

// Delivery is the broker-local view of a consumed message. It deliberately
// exposes no amqp091 types so consumer adapters under internal/ingress never
// import the AMQP SDK (code-architect Q5; mirrors how port.LLMProviderError
// shields the codebase from provider SDKs). The handler OWNS the lifecycle
// and MUST call exactly one of Ack / Nack / Reject.
type Delivery interface {
	Body() []byte
	// Header returns a single header value and whether it was present.
	Header(key string) (any, bool)
	// Headers returns a SHALLOW COPY of the header table (nil if absent).
	// A copy is returned so a downstream adapter cannot mutate the live
	// delivery state decoded from an untrusted wire message
	// (security-engineer SF-2).
	Headers() map[string]any
	// XDeath returns the decoded x-death entries (nil if the header is
	// absent or malformed) — the input to retry-level escalation.
	XDeath() []XDeathEntry
	// Ack positively acknowledges the message.
	Ack() error
	// Nack negatively acknowledges; requeue=false dead-letters the message
	// through the queue's DLX (the §6.4 retry path).
	Nack(requeue bool) error
	// Reject is Nack without the multiple flag.
	Reject(requeue bool) error
}

// amqpDelivery adapts an amqp.Delivery to the Delivery interface.
type amqpDelivery struct{ d amqp.Delivery }

var _ Delivery = (*amqpDelivery)(nil)

func (a *amqpDelivery) Body() []byte { return a.d.Body }

func (a *amqpDelivery) Header(key string) (any, bool) {
	if a.d.Headers == nil {
		return nil, false
	}
	v, ok := a.d.Headers[key]
	return v, ok
}

func (a *amqpDelivery) Headers() map[string]any {
	if a.d.Headers == nil {
		return nil
	}
	cp := make(map[string]any, len(a.d.Headers))
	for k, v := range a.d.Headers {
		cp[k] = v
	}
	return cp
}

func (a *amqpDelivery) Ack() error              { return a.d.Ack(false) }
func (a *amqpDelivery) Nack(requeue bool) error { return a.d.Nack(false, requeue) }
func (a *amqpDelivery) Reject(requeue bool) error { return a.d.Reject(requeue) }

// XDeath decodes the "x-death" header. RabbitMQ encodes it as a slice of
// tables; any entry not shaped as expected is skipped so a wire-format
// surprise degrades to "no escalation info" rather than a panic. Entry and
// routing-key counts are capped (xDeathMax*).
func (a *amqpDelivery) XDeath() []XDeathEntry {
	if a.d.Headers == nil {
		return nil
	}
	raw, ok := a.d.Headers["x-death"]
	if !ok {
		return nil
	}
	list, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]XDeathEntry, 0, min(len(list), xDeathMaxEntries))
	for _, item := range list {
		if len(out) >= xDeathMaxEntries {
			break
		}
		t, ok := item.(amqp.Table)
		if !ok {
			continue
		}
		e := XDeathEntry{}
		if s, ok := t["queue"].(string); ok {
			e.Queue = s
		}
		if s, ok := t["reason"].(string); ok {
			e.Reason = s
		}
		if s, ok := t["exchange"].(string); ok {
			e.Exchange = s
		}
		e.Count = toInt64(t["count"])
		if rks, ok := t["routing-keys"].([]any); ok {
			for _, rk := range rks {
				if len(e.RoutingKeys) >= xDeathMaxRoutingKeys {
					break
				}
				if s, ok := rk.(string); ok {
					e.RoutingKeys = append(e.RoutingKeys, s)
				}
			}
		}
		out = append(out, e)
	}
	return out
}

// toInt64 normalises the numeric encodings RabbitMQ / shovel / federation
// paths may use for x-death "count". Defaulting an UNRECOGNISED encoding to a
// large sentinel (not 0) is deliberate: the consumer (LIC-TASK-043) maps a
// high count to "retry budget exhausted → DLQ", whereas a wrong 0 would pin
// the message to retry.1 forever — the exact hot-loop §6.4 prevents
// (code-reviewer S5).
func toInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int32:
		return int64(n)
	case int:
		return int64(n)
	case uint:
		return int64(n)
	case uint32:
		return int64(n)
	case uint64:
		return int64(n)
	case float64:
		return int64(n)
	case nil:
		return 0 // absent count → first dead-letter
	default:
		return int64(1) << 62 // unknown encoding → treat as "exhausted"
	}
}

// Subscribe declares a consumer on an already-declared queue (topology is
// asserted by DeclareTopology, not lazily here), applies the configured
// prefetch, and dispatches deliveries to handler. The subscription is
// recorded BEFORE the consumer is started so a reconnect racing the very
// first Subscribe still re-establishes it; on a start failure the record is
// rolled back by id (code-reviewer S4 / golang-pro S4).
//
// auto-ack is false: the handler MUST ack/nack/reject every delivery.
func (c *Client) Subscribe(queue string, handler MessageHandler) error {
	c.mu.Lock()
	select {
	case <-c.done:
		c.mu.Unlock()
		return &BrokerError{Op: "Subscribe", Retryable: true, Cause: ErrNotConnected}
	default:
	}
	c.subSeq++
	id := c.subSeq
	c.subs = append(c.subs, subscription{id: id, queue: queue, handler: handler})
	c.mu.Unlock()

	if err := c.startConsumer(queue, handler); err != nil {
		c.mu.Lock()
		c.removeSub(id)
		c.mu.Unlock()
		return err
	}
	return nil
}

// removeSub drops the subscription with the given id. Caller holds c.mu.
func (c *Client) removeSub(id uint64) {
	for i := range c.subs {
		if c.subs[i].id == id {
			c.subs = append(c.subs[:i], c.subs[i+1:]...)
			return
		}
	}
}

// startConsumer opens a fresh channel, applies the prefetch, starts
// consuming, and launches the dispatch goroutine. Shared by Subscribe and
// the reconnect re-subscribe path. The wg.Add is performed under the same
// lock that checks c.done so it can never race Close()'s wg.Wait()
// (golang-pro M2).
func (c *Client) startConsumer(queue string, handler MessageHandler) error {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn == nil {
		return &BrokerError{Op: "Subscribe", Retryable: true, Cause: ErrNotConnected}
	}

	ch, err := conn.Channel()
	if err != nil {
		return mapError(err, "Subscribe/Channel")
	}

	// Per-channel prefetch — lost on reconnect, so it is (re-)applied on
	// every fresh consumer channel before Consume (code-architect Q6).
	if err := ch.Qos(c.cfg.ConsumerPrefetch, 0, false); err != nil {
		_ = ch.Close()
		return mapError(err, "Subscribe/Qos")
	}

	deliveries, err := ch.Consume(queue, "", false, false, false, false, nil)
	if err != nil {
		_ = ch.Close()
		return mapError(err, "Subscribe/Consume "+queue)
	}

	c.mu.Lock()
	select {
	case <-c.done:
		c.mu.Unlock()
		_ = ch.Close()
		return &BrokerError{Op: "Subscribe", Retryable: true, Cause: ErrNotConnected}
	default:
	}
	c.wg.Add(1)
	c.mu.Unlock()

	go c.consumeLoop(ch, deliveries, handler)
	return nil
}

// consumeLoop dispatches deliveries to the handler until the deliveries
// channel closes (broker/connection gone — the reconnect loop re-subscribes)
// or Close() signals done. It does NOT ack/nack: the handler owns the
// delivery lifecycle (Option B).
func (c *Client) consumeLoop(ch AMQPChannelAPI, deliveries <-chan amqp.Delivery, handler MessageHandler) {
	defer c.wg.Done()
	defer func() { _ = ch.Close() }()

	for {
		select {
		case <-c.done:
			return
		case d, ok := <-deliveries:
			if !ok {
				return
			}
			// Handler error is advisory only; the handler is contractually
			// responsible for terminating the delivery.
			_ = handler(c.cancelCtx, &amqpDelivery{d: d})
		}
	}
}
