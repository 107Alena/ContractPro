// Package consumer implements the LIC Event Consumer (LIC-TASK-039,
// high-architecture.md §6.2/§6.12, integration-contracts.md §6.1/§10,
// security.md §10/§11.2, observability.md §3.9). It is the inbound RabbitMQ
// adapter: it subscribes to the six frozen LIC queues, deserialises and
// envelope-validates each typed event, routes valid events to LIC-TASK-040
// (via the EventRouter seam) and dead-letters invalid ones to
// lic.dlq.invalid-message with a PII-safe HMAC envelope — owning the
// exactly-one-ack and exactly-one-metric-outcome invariants per delivery.
//
//   - NewConsumer(...)  — fail-fast constructor (errors.Join of per-arg
//     errors): required BrokerSubscriber / EventRouter / port.DLQPublisherPort
//     / non-empty dlqHashKey are positional; Metrics / Clock / Logger are the
//     optional-with-noop Deps bundle (build-spec D2/D19).
//   - Start()           — sync.Once, subscribes the six (queue, handler) pairs
//     in the frozen D7 order; first Subscribe error is wrapped and
//     short-circuits; repeated calls return the stored first result (D3).
//   - handleX(ctx, d)   — the six thin per-topic handler methods, each
//     delegating to the generic handle core (D11) with its frozen
//     subscription row (D7) and a typed decode/validate + route closure
//     (D9/D10/D12). Every path performs exactly one of d.Ack()/d.Nack(false)
//     and exactly one c.metrics.ConsumerMessage(topic,outcome) before
//     returning the advisory nil; d.Reject is never used.
//
// Hermetic adapter: stdlib + github.com/google/uuid + internal/domain/{model,
// port} + internal/infra/broker (types only — broker.Delivery /
// broker.MessageHandler; the broker already inverted the amqp091 dependency,
// build-spec D16). It does NOT import amqp091, internal/config (dlqHashKey is
// a ctor param), the concrete logger/metrics/tracer (seamed — D6/D17/R4),
// internal/application/*, or internal/ingress/router (its own downstream — the
// dependency is INVERTED via the EventRouter seam, D8). The
// var _ consumer.BrokerSubscriber/EventRouter satisfaction assertions are the
// LIC-TASK-047 wiring package's, NOT here (D5/D8/D16).
//
// Design adjudicated by the authoritative build-spec
// (BUILD_SPEC_LIC_039.md — decisions D1..D20, reconciliations R1..R6);
// implemented by subagent golang-pro. The authoritative reconciliations are
// recorded in this package's CLAUDE.md.
package consumer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
	"contractpro/legal-intelligence-core/internal/infra/broker"
)

// Outcome string constants (build-spec D18). labels.go is in the forbidden
// metrics package (D16); these are value-identical to
// metrics.PublishOutcome{Success,Invalid,Nacked} (labels.go:173-176, whose
// doc explicitly sanctions a package-local mirror as "the de-facto
// contract"). Typed as untyped string consts so a typo is a compile error at
// the c.metrics.ConsumerMessage call sites.
const (
	outcomeSuccess = "success" // == metrics.PublishOutcomeSuccess (labels.go:173)
	outcomeInvalid = "invalid" // == metrics.PublishOutcomeInvalid (labels.go:176)
	outcomeNacked  = "nacked"  // == metrics.PublishOutcomeNacked  (labels.go:175)
)

// subscription is one frozen queue→topic mapping. topic = routing key = the
// metric `topic` label = LICDLQEnvelope.OriginalTopic (build-spec D7/D13).
type subscription struct {
	queue string
	topic string
}

// subscriptionTable is the local FROZEN queue→topic table (build-spec D7,
// option (a) — zero broker-package change). The order EXACTLY mirrors
// broker.(*Client).subscriptionSpecs() (topology.go:49-56) so subscription
// order is deterministic and TestSubscriptionTableMatchesContract can pin it;
// a divergence is also caught by the broker's own topology test. The queue
// names and routing keys are FROZEN contract identifiers
// (topology.go:38-39, integration-contracts.md:170-175).
var subscriptionTable = [6]subscription{
	{"lic.q.version-artifacts-ready", "dm.events.version-artifacts-ready"},
	{"lic.q.version-created", "dm.events.version-created"},
	{"lic.q.artifacts-provided", "dm.responses.artifacts-provided"},
	{"lic.q.lic-persist-confirm", "dm.responses.lic-artifacts-persisted"},
	{"lic.q.lic-persist-fail", "dm.responses.lic-artifacts-persist-failed"},
	{"lic.q.user-confirmed-type", "orch.commands.user-confirmed-type"},
}

// Subscription-table indices (build-spec D7 order). Used by the six thin
// handlers to fetch their frozen row without a lookup.
const (
	idxVersionArtifactsReady = iota
	idxVersionCreated
	idxArtifactsProvided
	idxPersisted
	idxPersistFailed
	idxUserConfirmedType
)

// Consumer subscribes to the six inbound LIC queues and routes deserialised,
// envelope-validated events to LIC-TASK-040. Immutable after construction; the
// six handler methods are goroutine-safe for distinct deliveries (the broker
// may invoke them concurrently across the six consumer channels —
// subscribe.go:249) because the Consumer holds no mutable per-delivery state
// (startOnce/startErr guard only Start()).
type Consumer struct {
	sub        BrokerSubscriber
	router     EventRouter
	dlq        port.DLQPublisherPort
	dlqHashKey string
	metrics    Metrics
	clock      Clock
	log        Logger

	startOnce sync.Once
	startErr  error
}

// NewConsumer creates a Consumer. sub / router / dlq / dlqHashKey are REQUIRED
// (a consumer without any of them cannot perform its contract); deps is
// optional (nil fields → noop). Fail-fast via errors.Join of per-arg errors
// (the pendingconfirmation.NewManager precedent — build-spec D2): each of
// sub==nil, router==nil, dlq==nil, strings.TrimSpace(dlqHashKey)=="" adds a
// distinct errors.New(...); on any failure returns (nil, joinedErr).
func NewConsumer(
	sub BrokerSubscriber,
	router EventRouter,
	dlq port.DLQPublisherPort,
	dlqHashKey string,
	deps Deps,
) (*Consumer, error) {
	var errs []error
	if sub == nil {
		errs = append(errs, errors.New("consumer: sub (BrokerSubscriber) must not be nil"))
	}
	if router == nil {
		errs = append(errs, errors.New("consumer: router (EventRouter) must not be nil"))
	}
	if dlq == nil {
		errs = append(errs, errors.New("consumer: dlq (port.DLQPublisherPort) must not be nil"))
	}
	if strings.TrimSpace(dlqHashKey) == "" {
		errs = append(errs, errors.New("consumer: dlqHashKey (LIC_DLQ_HASH_KEY) must not be empty"))
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	d := deps.withDefaults()
	return &Consumer{
		sub:        sub,
		router:     router,
		dlq:        dlq,
		dlqHashKey: dlqHashKey,
		metrics:    d.Metrics,
		clock:      d.Clock,
		log:        d.Logger,
	}, nil
}

// Start subscribes the six (queue, handler) pairs in the deterministic frozen
// D7 order. It is idempotent (sync.Once — repeated calls return the stored
// first-attempt result). On the first Subscribe error it wraps with
// fmt.Errorf("consumer: subscribe to %s: %w", queue, err), stores it, and
// stops (does NOT attempt the remaining subscriptions — partial-subscription
// cleanup is the caller's broker-shutdown responsibility, the DP precedent).
// Start() does NOT block; the broker's consumeLoop runs the handlers
// (subscribe.go:249-274). Reconnect re-subscribes automatically
// (subscribe.go:167-195).
func (c *Consumer) Start() error {
	c.startOnce.Do(func() {
		handlers := [6]broker.MessageHandler{
			c.handleVersionArtifactsReady,
			c.handleVersionCreated,
			c.handleArtifactsProvided,
			c.handlePersisted,
			c.handlePersistFailed,
			c.handleUserConfirmedType,
		}
		for i := range subscriptionTable {
			queue := subscriptionTable[i].queue
			if err := c.sub.Subscribe(queue, handlers[i]); err != nil {
				c.startErr = fmt.Errorf("consumer: subscribe to %s: %w", queue, err)
				return
			}
		}
	})
	return c.startErr
}

// ----------------------------------------------------------------------------
// The six thin per-topic handler methods. Each delegates to the generic
// handle core (build-spec D11) with its frozen subscription row (D7) and a
// typed decode/validate (D9/D10/D12) + route (D8) closure. Signature matches
// broker.MessageHandler (client.go:72).
// ----------------------------------------------------------------------------

func (c *Consumer) handleVersionArtifactsReady(ctx context.Context, d broker.Delivery) error {
	return handle(c, ctx, d, subscriptionTable[idxVersionArtifactsReady],
		decodeVersionArtifactsReady, c.router.RouteVersionArtifactsReady)
}

func (c *Consumer) handleVersionCreated(ctx context.Context, d broker.Delivery) error {
	return handle(c, ctx, d, subscriptionTable[idxVersionCreated],
		decodeVersionCreated, c.router.RouteVersionCreated)
}

func (c *Consumer) handleArtifactsProvided(ctx context.Context, d broker.Delivery) error {
	return handle(c, ctx, d, subscriptionTable[idxArtifactsProvided],
		decodeArtifactsProvided, c.router.RouteArtifactsProvided)
}

func (c *Consumer) handlePersisted(ctx context.Context, d broker.Delivery) error {
	return handle(c, ctx, d, subscriptionTable[idxPersisted],
		decodePersisted, c.router.RoutePersisted)
}

func (c *Consumer) handlePersistFailed(ctx context.Context, d broker.Delivery) error {
	return handle(c, ctx, d, subscriptionTable[idxPersistFailed],
		decodePersistFailed, c.router.RoutePersistFailed)
}

func (c *Consumer) handleUserConfirmedType(ctx context.Context, d broker.Delivery) error {
	return handle(c, ctx, d, subscriptionTable[idxUserConfirmedType],
		decodeUserConfirmedType, c.router.RouteUserConfirmedType)
}

// handle is the generic per-delivery core (build-spec D11, exact control
// flow). decode is the typed event's decodeAndValidate (validate.go,
// D9/D10/D12); route is the matching EventRouter seam method (D8). The
// exactly-one-of-Ack/Nack/Reject and exactly-one-metric-outcome invariants
// are structural: every path calls exactly one c.metrics.ConsumerMessage and
// exactly one d.Ack()/d.Nack(false) before returning the advisory nil; no
// path falls through; d.Reject is never used (Nack(false) is the §6.4 DLX
// path — subscribe.go:51-52, topology.go:84-90). An Ack/Nack transport error
// is logged but does NOT change the already-decided outcome and is NOT
// re-acked (the broker channel is gone if ack failed; re-acking would
// error/panic). The metric classifies the message, not the transport, so it
// is incremented for the decided outcome regardless of ack transport success.
func handle[T any](
	c *Consumer,
	ctx context.Context,
	d broker.Delivery,
	sub subscription,
	decode func([]byte) (T, RequestIDs, *model.DomainError),
	route func(context.Context, T) error,
) error {
	body := d.Body()

	evt, ids, verr := decode(body)

	if verr != nil {
		// INVALID PATH (build-spec D13). Best-effort forensic IDs come from
		// the tolerant idProbe (D12), NOT the zero RequestIDs.
		env := buildInvalidEnvelope(sub.topic, body, verr, probeIDs(body), c.clock, c.dlqHashKey)
		// Sanitized reason; NO raw body (PII — security.md §10,
		// integration-contracts.md:319).
		c.log.Error(ctx, "invalid inbound message",
			"topic", sub.topic, "reason", verr.Error(), "raw_size", len(body))
		if perr := c.dlq.PublishDLQ(ctx, port.DLQTopicInvalidMessage, env); perr != nil {
			// DLQ publish itself failed — DO NOT silently drop (acceptance:
			// "не отбрасываем permanently без логирования"; build-spec R2):
			// Error log + nacked metric + Nack(false) so the message returns
			// via the DLX-loop and is not lost.
			c.log.Error(ctx, "DLQ publish failed for invalid message",
				"topic", sub.topic, "error", perr)
			c.metrics.ConsumerMessage(sub.topic, outcomeNacked)
			c.nack(ctx, d, sub.topic)
			return nil
		}
		c.metrics.ConsumerMessage(sub.topic, outcomeInvalid)
		c.ack(ctx, d, sub.topic)
		return nil
	}

	// VALID PATH. Attach the per-delivery correlation IDs ONCE at ingress
	// (build-spec D6/R4 — the logger.WithRequestContext "call once at ingress"
	// contract satisfied via the seam, before the router call).
	ctx = c.log.WithRequestContext(ctx, ids)
	c.log.Info(ctx, "inbound message accepted", "topic", sub.topic)

	if rerr := route(ctx, evt); rerr != nil {
		// R1 — a plain Nack(false) is the correct in-scope 039 behaviour; the
		// retry-level (x-death) routing of that Nack is 040's concern.
		c.log.Warn(ctx, "router returned error; nacking",
			"topic", sub.topic, "error", rerr)
		c.metrics.ConsumerMessage(sub.topic, outcomeNacked)
		c.nack(ctx, d, sub.topic)
		return nil
	}
	c.metrics.ConsumerMessage(sub.topic, outcomeSuccess)
	c.ack(ctx, d, sub.topic)
	return nil
}

// ack performs the positive acknowledgement. A transport error is logged but
// does NOT change the decided outcome and is NOT retried (build-spec D11
// invariant: the broker channel is gone if ack failed; re-acking would
// error/panic).
func (c *Consumer) ack(ctx context.Context, d broker.Delivery, topic string) {
	if err := d.Ack(); err != nil {
		c.log.Error(ctx, "ack failed", "topic", topic, "error", err)
	}
}

// nack performs Nack(requeue=false) — the §6.4 DLX dead-letter path
// (subscribe.go:51-52, topology.go:84-90). d.Reject is never used (build-spec
// D11). Transport error handled like ack.
func (c *Consumer) nack(ctx context.Context, d broker.Delivery, topic string) {
	if err := d.Nack(false); err != nil {
		c.log.Error(ctx, "nack failed", "topic", topic, "error", err)
	}
}
