package fakes

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"contractpro/legal-intelligence-core/internal/infra/broker"
)

// LIC topology — the six §6.1 queue→routing-key bindings.
// Frozen against topology.go:48-57; mirrored here so a FakeBroker constructed
// via NewFakeBrokerWithLICTopology accepts Inject(routingKey,...) on any of
// the six LIC inbound topics and fans the delivery out to every Subscribe-r
// of the corresponding queue.
//
// The four outbound topics (lic.requests.artifacts,
// lic.artifacts.analysis-ready, lic.events.status-changed,
// lic.events.classification-uncertain, lic.dlq.*) are NOT in this binding
// table — Publish records them, and FakeDM / the verifier read them back.

const (
	QueueVersionArtifactsReady = "lic.q.version-artifacts-ready"
	QueueVersionCreated        = "lic.q.version-created"
	QueueArtifactsProvided     = "lic.q.artifacts-provided"
	QueueLICPersistConfirm     = "lic.q.lic-persist-confirm"
	QueueLICPersistFail        = "lic.q.lic-persist-fail"
	QueueUserConfirmedType     = "lic.q.user-confirmed-type"

	RoutingKeyVersionArtifactsReady = "dm.events.version-artifacts-ready"
	RoutingKeyVersionCreated        = "dm.events.version-created"
	RoutingKeyArtifactsProvided     = "dm.responses.artifacts-provided"
	RoutingKeyPersisted             = "dm.responses.lic-artifacts-persisted"
	RoutingKeyPersistFailed         = "dm.responses.lic-artifacts-persist-failed"
	RoutingKeyUserConfirmedType     = "orch.commands.user-confirmed-type"

	RoutingKeyRequestArtifacts        = "lic.requests.artifacts"
	RoutingKeyAnalysisReady           = "lic.artifacts.analysis-ready"
	RoutingKeyStatusChanged           = "lic.events.status-changed"
	RoutingKeyClassificationUncertain = "lic.events.classification-uncertain"

	// The four §3.2 DLQ routing keys (port.DLQTopic*). LIC-TASK-051 DLQ
	// tests assert against these constants so the literal strings live
	// in exactly one place across the fakes package.
	RoutingKeyDLQInvalidMessage     = "lic.dlq.invalid-message"
	RoutingKeyDLQConsumerFailed     = "lic.dlq.consumer-failed"
	RoutingKeyDLQPublishFailed      = "lic.dlq.publish-failed"
	RoutingKeyDLQAgentOutputInvalid = "lic.dlq.agent-output-invalid"
)

// LICTopologyBindings returns the six §6.1 (queue, routingKey) pairs in
// declaration order so a test that wants only a subset (e.g. just the
// trigger + the response topics) can register them piecewise.
func LICTopologyBindings() [][2]string {
	return [][2]string{
		{QueueVersionArtifactsReady, RoutingKeyVersionArtifactsReady},
		{QueueVersionCreated, RoutingKeyVersionCreated},
		{QueueArtifactsProvided, RoutingKeyArtifactsProvided},
		{QueueLICPersistConfirm, RoutingKeyPersisted},
		{QueueLICPersistFail, RoutingKeyPersistFailed},
		{QueueUserConfirmedType, RoutingKeyUserConfirmedType},
	}
}

// PublishedMessage is one observed broker publish — recorded for verifier
// inspection. Payload is the exact []byte handed to Publish (no decode).
type PublishedMessage struct {
	Exchange   string
	RoutingKey string
	Payload    []byte
	At         time.Time
}

// PublishListener is invoked after a publish is recorded. Listeners run
// synchronously in the publishing goroutine; use a non-blocking listener
// (channel send / closure capture) to avoid stalling the producer. FakeDM
// hangs its DM-response loop off OnPublish.
type PublishListener func(msg PublishedMessage)

// FakeBroker is the in-memory RabbitMQ double. Goroutine-safe.
//
// Wiring it into LIC components:
//   - consumer.NewConsumer(brokerSubscriber=fb, ...) — Subscribe satisfies
//     consumer.BrokerSubscriber (Subscribe(queue, broker.MessageHandler)).
//   - dm/orch/dlq publishers' Publisher seam — Publish matches their
//     1-method interface byte-for-byte.
//
// Inject(routingKey, headers, body) routes the delivery to every queue
// bound on routingKey (the broker.amqp091 fan-out semantics) and blocks
// until every handler returns and terminates its delivery. Test code can
// therefore assert ACK/NACK outcomes deterministically without time.Sleep.
type FakeBroker struct {
	mu       sync.Mutex
	subs     map[string][]broker.MessageHandler
	bindings map[string]map[string]struct{} // routingKey -> queue set

	published []PublishedMessage
	listeners []listenerEntry

	pubErrs []error // FIFO: next Publish call consumes & returns the head

	closed bool
}

type listenerEntry struct {
	routingKey string // "" matches every routing key
	fn         PublishListener
}

// NewFakeBroker returns an empty broker (no LIC topology bound, no
// publish listeners). Use NewFakeBrokerWithLICTopology when integration
// tests need the six §6.1 bindings preset.
func NewFakeBroker() *FakeBroker {
	return &FakeBroker{
		subs:     make(map[string][]broker.MessageHandler),
		bindings: make(map[string]map[string]struct{}),
	}
}

// NewFakeBrokerWithLICTopology returns a broker pre-bound with the six §6.1
// (queue, routing key) pairs (LICTopologyBindings).
func NewFakeBrokerWithLICTopology() *FakeBroker {
	fb := NewFakeBroker()
	for _, b := range LICTopologyBindings() {
		fb.Bind(b[0], b[1])
	}
	return fb
}

// Bind records a (queue, routingKey) binding. Calling Bind for an already-
// bound pair is a no-op; a queue may have multiple routing keys and a
// routing key may fan out to multiple queues.
func (f *FakeBroker) Bind(queue, routingKey string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	bs, ok := f.bindings[routingKey]
	if !ok {
		bs = make(map[string]struct{})
		f.bindings[routingKey] = bs
	}
	bs[queue] = struct{}{}
}

// Subscribe registers handler on queue. It is consumer.BrokerSubscriber.
// auto-ack semantics off: the handler MUST call Ack/Nack/Reject on every
// delivery; FakeDelivery enforces "exactly one terminate" and surfaces a
// lifecycle error if the handler forgets or duplicates.
func (f *FakeBroker) Subscribe(queue string, handler broker.MessageHandler) error {
	if handler == nil {
		return errors.New("fakes: Subscribe handler is nil")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return errors.New("fakes: Subscribe on closed broker")
	}
	f.subs[queue] = append(f.subs[queue], handler)
	return nil
}

// Publish satisfies the dm/orch/dlq Publisher seam. The payload is recorded
// verbatim, any registered listener whose routing key matches (or is "")
// fires, and the next pre-loaded error is consumed and returned if any.
//
// Publish honours ctx: a ctx already done is surfaced raw (matches the
// codebase-wide convention — broker/publish.go:54-56).
func (f *FakeBroker) Publish(ctx context.Context, exchange, routingKey string, payload []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(payload) == 0 {
		return errors.New("fakes: Publish empty payload")
	}
	cp := append([]byte(nil), payload...)
	msg := PublishedMessage{
		Exchange:   exchange,
		RoutingKey: routingKey,
		Payload:    cp,
		At:         time.Now(),
	}

	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return errors.New("fakes: Publish on closed broker")
	}
	// Consume pre-loaded errors before recording / fan-out so a programmed
	// failure does not perturb the visible state.
	if len(f.pubErrs) > 0 {
		err := f.pubErrs[0]
		f.pubErrs = f.pubErrs[1:]
		f.mu.Unlock()
		return err
	}
	f.published = append(f.published, msg)
	// Snapshot listeners so we can fire them outside the lock.
	listeners := append([]listenerEntry(nil), f.listeners...)
	f.mu.Unlock()

	for _, l := range listeners {
		if l.routingKey == "" || l.routingKey == routingKey {
			l.fn(msg)
		}
	}
	return nil
}

// InjectPublishError pushes err onto the FIFO error queue; the next Publish
// call returns it instead of recording the message. Used to drive the DLQ /
// status-publish-failure paths from tests.
func (f *FakeBroker) InjectPublishError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pubErrs = append(f.pubErrs, err)
}

// OnPublish installs a listener fired AFTER each matching Publish call.
// routingKey "" matches every publish; any other value matches that
// routing key exactly. FakeDM uses this to react to LIC's outbound DM
// wires; the verifier uses the "" wildcard to maintain a live tail.
func (f *FakeBroker) OnPublish(routingKey string, fn PublishListener) {
	if fn == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listeners = append(f.listeners, listenerEntry{routingKey: routingKey, fn: fn})
}

// Inject delivers a message addressed to routingKey to EVERY queue bound on
// that key. Each handler call runs synchronously; Inject returns once every
// handler has returned AND every delivery has been terminated (Ack/Nack/
// Reject), so callers can immediately inspect the resulting publishes.
//
// headers may be nil; an empty Headers() map is then exposed (the real
// broker.amqpDelivery.Headers() returns nil in that case, but the fake
// returns an empty map to keep test assertions consistent).
//
// XDeath: a header value of []XDeathEntry under the "x-death" key — or, for
// header authenticity, the typed XDeathHeader convenience — is exposed
// through FakeDelivery.XDeath().
func (f *FakeBroker) Inject(ctx context.Context, routingKey string, headers map[string]any, body []byte) (InjectResult, error) {
	if err := ctx.Err(); err != nil {
		return InjectResult{}, err
	}
	bodyCopy := append([]byte(nil), body...)

	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return InjectResult{}, errors.New("fakes: Inject on closed broker")
	}
	// Record the injection so verifier helpers (WaitForPublish,
	// PublishedOn) work for inbound deliveries too. Inject simulates
	// a message that arrived FROM the broker — the same one-way recorded
	// observation as an outbound Publish from the LIC side, and tests
	// asserting "DM responded" are simpler when one log covers both.
	f.published = append(f.published, PublishedMessage{
		Exchange:   "",
		RoutingKey: routingKey,
		Payload:    bodyCopy,
		At:         time.Now(),
	})
	queues := make([]string, 0)
	if qs, ok := f.bindings[routingKey]; ok {
		for q := range qs {
			queues = append(queues, q)
		}
	}
	var handlers []broker.MessageHandler
	for _, q := range queues {
		handlers = append(handlers, f.subs[q]...)
	}
	f.mu.Unlock()

	var hdrCopy map[string]any
	if headers != nil {
		hdrCopy = make(map[string]any, len(headers))
		for k, v := range headers {
			hdrCopy[k] = v
		}
	}

	res := InjectResult{Queues: queues, Handlers: len(handlers)}
	for _, h := range handlers {
		fd := newFakeDelivery(bodyCopy, hdrCopy)
		// Handler error is advisory only (matches real consumeLoop
		// broker/subscribe.go:266-271). The delivery itself is the ACK
		// authority.
		handlerErr := h(ctx, fd)
		_ = handlerErr
		switch fd.outcome() {
		case outcomeAck:
			res.Acked++
		case outcomeNack:
			res.Nacked++
		case outcomeReject:
			res.Rejected++
		default:
			res.Unterminated++
		}
	}
	return res, nil
}

// InjectResult summarises one Inject fan-out so tests can assert outcomes
// without polling for state.
type InjectResult struct {
	Queues        []string // queues bound to the injected routing key
	Handlers      int      // total handlers that received the delivery
	Acked         int
	Nacked        int
	Rejected      int
	Unterminated  int // handlers that returned without Ack/Nack/Reject
}

// Published returns a snapshot of every recorded publish, oldest first.
func (f *FakeBroker) Published() []PublishedMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]PublishedMessage, len(f.published))
	copy(out, f.published)
	return out
}

// PublishedOn returns a snapshot filtered to the given routing key.
func (f *FakeBroker) PublishedOn(routingKey string) []PublishedMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []PublishedMessage
	for _, m := range f.published {
		if m.RoutingKey == routingKey {
			out = append(out, m)
		}
	}
	return out
}

// ResetPublished clears the publish log (bindings, subscriptions and
// pre-loaded errors are unchanged). Useful between phases of a multi-step
// test.
func (f *FakeBroker) ResetPublished() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.published = nil
}

// Close marks the broker closed: subsequent Subscribe/Publish/Inject fail.
// Idempotent. Tests that exercise graceful-shutdown paths use this to drive
// broker.ErrNotConnected-class behaviour without instantiating the real
// client; non-shutdown tests need not call it.
func (f *FakeBroker) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

// Closed reports whether Close has been called. Safe to call concurrently.
func (f *FakeBroker) Closed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

// ----------------------------------------------------------------------------
// FakeDelivery: a broker.Delivery that records terminate decisions.
// ----------------------------------------------------------------------------

type deliveryOutcome int

const (
	outcomeOpen deliveryOutcome = iota
	outcomeAck
	outcomeNack
	outcomeReject
)

// FakeDelivery is broker.Delivery for the in-memory broker. It records
// exactly one Ack / Nack / Reject; the second call returns ErrAlreadyTerminated
// so a handler bug surfaces in tests.
type FakeDelivery struct {
	mu       sync.Mutex
	body     []byte
	headers  map[string]any
	state    deliveryOutcome
	requeue  *bool // set on Nack/Reject; nil if not Nacked/Rejected
}

// ErrAlreadyTerminated is returned by Ack/Nack/Reject when the delivery has
// already been terminated by a prior call.
var ErrAlreadyTerminated = errors.New("fakes: delivery already terminated")

func newFakeDelivery(body []byte, headers map[string]any) *FakeDelivery {
	return &FakeDelivery{body: body, headers: headers}
}

// Body returns the raw payload bytes.
func (d *FakeDelivery) Body() []byte { return d.body }

// Header returns a single header value and whether it was present.
func (d *FakeDelivery) Header(key string) (any, bool) {
	if d.headers == nil {
		return nil, false
	}
	v, ok := d.headers[key]
	return v, ok
}

// Headers returns a shallow copy of the header table (nil-safe).
func (d *FakeDelivery) Headers() map[string]any {
	if d.headers == nil {
		return map[string]any{}
	}
	cp := make(map[string]any, len(d.headers))
	for k, v := range d.headers {
		cp[k] = v
	}
	return cp
}

// XDeath decodes the "x-death" header. The fake accepts either the typed
// shape []broker.XDeathEntry (the most ergonomic for hand-written tests) or
// the wire-faithful []any of amqp091-compatible Table values. nil / absent
// header returns nil.
func (d *FakeDelivery) XDeath() []broker.XDeathEntry {
	v, ok := d.Header("x-death")
	if !ok {
		return nil
	}
	if entries, ok := v.([]broker.XDeathEntry); ok {
		out := make([]broker.XDeathEntry, len(entries))
		copy(out, entries)
		return out
	}
	// Wire-faithful path: []any of map[string]any items.
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]broker.XDeathEntry, 0, len(list))
	for _, item := range list {
		tbl, ok := item.(map[string]any)
		if !ok {
			continue
		}
		e := broker.XDeathEntry{}
		if s, ok := tbl["queue"].(string); ok {
			e.Queue = s
		}
		if s, ok := tbl["reason"].(string); ok {
			e.Reason = s
		}
		if s, ok := tbl["exchange"].(string); ok {
			e.Exchange = s
		}
		switch n := tbl["count"].(type) {
		case int64:
			e.Count = n
		case int:
			e.Count = int64(n)
		case int32:
			e.Count = int64(n)
		}
		if rks, ok := tbl["routing-keys"].([]any); ok {
			for _, rk := range rks {
				if s, ok := rk.(string); ok {
					e.RoutingKeys = append(e.RoutingKeys, s)
				}
			}
		}
		out = append(out, e)
	}
	return out
}

// Ack marks the delivery acknowledged. Returns ErrAlreadyTerminated on the
// second call.
func (d *FakeDelivery) Ack() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.state != outcomeOpen {
		return ErrAlreadyTerminated
	}
	d.state = outcomeAck
	return nil
}

// Nack marks the delivery negatively acknowledged. requeue is recorded so
// tests can assert the §6.4 deviation (Nack(false) for DLX-loop semantics).
func (d *FakeDelivery) Nack(requeue bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.state != outcomeOpen {
		return ErrAlreadyTerminated
	}
	d.state = outcomeNack
	rq := requeue
	d.requeue = &rq
	return nil
}

// Reject marks the delivery rejected. requeue is recorded the same way as
// Nack.requeue.
func (d *FakeDelivery) Reject(requeue bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.state != outcomeOpen {
		return ErrAlreadyTerminated
	}
	d.state = outcomeReject
	rq := requeue
	d.requeue = &rq
	return nil
}

// Acked reports whether Ack was the terminate call. Helper for tests.
func (d *FakeDelivery) Acked() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.state == outcomeAck
}

// Nacked reports whether Nack was the terminate call and, if so, with what
// requeue flag.
func (d *FakeDelivery) Nacked() (bool, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.state != outcomeNack || d.requeue == nil {
		return false, false
	}
	return true, *d.requeue
}

// Rejected reports whether Reject was the terminate call and, if so, with
// what requeue flag.
func (d *FakeDelivery) Rejected() (bool, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.state != outcomeReject || d.requeue == nil {
		return false, false
	}
	return true, *d.requeue
}

// Terminated reports whether any of Ack/Nack/Reject fired.
func (d *FakeDelivery) Terminated() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.state != outcomeOpen
}

// outcome is the unlocked snapshot used by Inject to summarise the
// fan-out. Caller must hold no lock (atomicity is the caller-side guarantee
// — Inject reads after the handler returns, no contention).
func (d *FakeDelivery) outcome() deliveryOutcome {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.state
}

// XDeathHeader builds the headers map carrying an x-death entry chain at
// the given retry counts (level 1 = retry.1, etc.). The exact shape the
// real broker decodes (broker/subscribe.go:91-136) — equivalent to amqp091's
// []amqp.Table.
//
// Mirrors integration-contracts.md §6.4 (the consumer adapter (LIC-TASK-043)
// reads Count to pick the retry level: 0→retry.1, 1→retry.2, 2→retry.3,
// ≥MaxRedeliveries→lic.dlq.consumer-failed).
func XDeathHeader(queue, originalRoutingKey string, count int64) map[string]any {
	return map[string]any{
		"x-death": []broker.XDeathEntry{
			{
				Queue:       queue,
				Reason:      "rejected",
				Exchange:    "lic.x.events",
				Count:       count,
				RoutingKeys: []string{originalRoutingKey},
			},
		},
	}
}

// ensure compile-time the types implement what they should.
var (
	_ broker.Delivery = (*FakeDelivery)(nil)
)

// String returns a short summary suitable for test log output.
func (m PublishedMessage) String() string {
	return fmt.Sprintf("PublishedMessage{exchange=%q routingKey=%q bytes=%d at=%s}",
		m.Exchange, m.RoutingKey, len(m.Payload), m.At.Format(time.RFC3339Nano))
}
