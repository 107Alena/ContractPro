package consumer

import (
	"context"
	"time"

	"contractpro/legal-intelligence-core/internal/domain/port"
	"contractpro/legal-intelligence-core/internal/infra/broker"
)

// This file holds every Event Consumer SEAM — a consumer-local interface (plus,
// where applicable, a zero-dependency noop default) for collaborators that are
// telemetry / runtime-environment, would force a forbidden import if depended
// on concretely (build-spec D16), or are the inverted dependency edge to
// LIC-TASK-040 (the EventRouter). Everything that crosses to a frozen
// cross-domain RabbitMQ wire (the DLQ publisher) is a domain.port instead
// (a positional NewConsumer param, NOT here — D2).
//
// var _ Seam = noop{} assertions follow each noop pair (the universal
// pendingconfirmation precedent). The
// var _ consumer.BrokerSubscriber = (*broker.Client)(nil) and
// var _ consumer.EventRouter = (*router.Router)(nil) structural-satisfaction
// assertions live in the LIC-TASK-047 WIRING package, NOT here — asserting
// them here would force an internal/ingress/router (the consumer's own
// downstream) import and break hermeticity (build-spec D5/D8/D16, the
// pendingconfirmation PipelineResumer-seam precedent).

// BrokerSubscriber is the consumer-side interface for subscribing to broker
// queues. It is satisfied structurally by *broker.Client
// (broker.Client.Subscribe — subscribe.go:175). Declared consumer-side so the
// broker is injected behind an interface for hermetic unit testing; the
// var _ broker-satisfaction assertion lives in the LIC-TASK-047 wiring package
// (build-spec D5/D8), NOT here. There is NO noop default: a consumer with no
// broker cannot subscribe — NewConsumer fails fast (D2). It is a REQUIRED
// positional NewConsumer param, NOT in Deps.
//
// broker.MessageHandler = func(ctx context.Context, d broker.Delivery) error
// (client.go:72). Importing internal/infra/broker for broker.Delivery +
// broker.MessageHandler ONLY is the single permitted infra import: this is the
// inbound adapter layer and the broker already inverted the amqp091 dependency
// (subscribe.go:31-35) — build-spec D16.
type BrokerSubscriber interface {
	Subscribe(queue string, handler broker.MessageHandler) error
}

// EventRouter is the consumer→router seam. LIC-TASK-040 implements it
// (internal/ingress/router). Declared consumer-side so the consumer is
// hermetic and unit-testable with an in-package fake; the
// var _ EventRouter = (*router.Router)(nil) satisfaction assertion lives in
// the LIC-TASK-047 wiring package, NOT here (build-spec D8/D16, the
// pendingconfirmation PipelineResumer precedent). NO noop default: a consumer
// with no router cannot dispatch — NewConsumer fails fast (D2). It is a
// REQUIRED positional NewConsumer param, NOT in Deps.
//
// One method per event taking the typed struct (build-spec D8): strongly
// typed, fully exercised by the consumer's tests, and structurally matching
// the six existing port.*Handler interfaces (inbound.go:16-51) so a single
// LIC-TASK-040 router type can satisfy both this seam and those ports. The
// Route* names (not Handle*) keep this seam distinct from port.*Handler at
// the type level while remaining structurally trivial for 040 to bridge.
//
// Return contract (mapped by the consumer per build-spec D11/R1):
//
//	nil   ⇒ d.Ack(),       metric outcome = "success"
//	error ⇒ d.Nack(false), metric outcome = "nacked"
//
// The retry-level (x-death) routing of that Nack is 040's concern (R1); a
// plain Nack(false) is the correct in-scope 039 behaviour — the broker main
// queue sets x-dead-letter-exchange with NO static routing key
// (topology.go:84-90, broker CLAUDE.md "§6.4 deviation").
type EventRouter interface {
	RouteVersionArtifactsReady(ctx context.Context, evt port.VersionProcessingArtifactsReady) error
	RouteVersionCreated(ctx context.Context, evt port.VersionCreated) error
	RouteArtifactsProvided(ctx context.Context, evt port.ArtifactsProvided) error
	RoutePersisted(ctx context.Context, evt port.LegalAnalysisArtifactsPersisted) error
	RoutePersistFailed(ctx context.Context, evt port.LegalAnalysisArtifactsPersistFailed) error
	RouteUserConfirmedType(ctx context.Context, evt port.UserConfirmedType) error
}

// Metrics is the lic_consumer_messages_total{topic,outcome} seam
// (observability.md §3.9, crosscut.go:43-46). NOT the concrete
// *prometheus.CounterVec and NOT *metrics.Metrics (hermeticity — build-spec
// D16/D17; the pendingconfirmation.Metrics / aggregator.Metrics precedent).
// LIC-TASK-047 wires an adapter over
// *metrics.Metrics.CrossCut.ConsumerMessagesTotal.
type Metrics interface {
	// ConsumerMessage increments lic_consumer_messages_total{topic,outcome}.
	// outcome MUST be one of metrics.PublishOutcome{Success,Invalid,Nacked}
	// string values "success"|"invalid"|"nacked" (labels.go:170-177) — the
	// consumer passes those exact literals via package-local typed constants
	// (build-spec D18) so a typo is a compile error.
	ConsumerMessage(topic, outcome string)
}

// noopMetrics is the zero-dependency default so the hot path never nil-checks.
type noopMetrics struct{}

func (noopMetrics) ConsumerMessage(string, string) {}

var _ Metrics = noopMetrics{}

// Clock is the deterministic-time seam (the pendingconfirmation.Clock
// precedent, a 1-method surface — the consumer needs only Now() for
// LICDLQEnvelope.FailedAt).
type Clock interface {
	Now() time.Time
}

// systemClock is the production default (UTC, the wall clock).
type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

var _ Clock = systemClock{}

// RequestIDs is the consumer-local plain-old-data carrier of the per-delivery
// correlation IDs handed to Logger.WithRequestContext. It is NOT the forbidden
// logger.RequestContext (build-spec D6/R4): the consumer owns the typed event
// and the D11 per-event field map; only the concrete context.WithValue
// mechanics move behind the Logger seam (the LIC-TASK-047 adapter maps this
// to logger.RequestContext). Empty fields are left zero — the 047 logger
// adapter emits only non-empty IDs (context.go:14-15).
type RequestIDs struct {
	CorrelationID   string
	JobID           string
	DocumentID      string
	VersionID       string
	OrganizationID  string
	CreatedByUserID string
}

// Logger is the structured INFO/WARN/ERROR seam plus the ingress-once
// RequestContext attachment. NOT a direct *logger.Logger dependency: the
// package is hermetic (build-spec D16) and may not import
// internal/infra/observability/logger. Info/Warn/Error use ...any (the
// pendingconfirmation seam shape) so the LIC-TASK-047 adapter bridges to
// *logger.Logger's ...slog.Attr API. The consumer needs Error for
// invalid-message + DLQ-publish-failure logging, Warn for router-error, Info
// for accepted dispatch.
type Logger interface {
	Info(ctx context.Context, msg string, kv ...any)
	Warn(ctx context.Context, msg string, kv ...any)
	Error(ctx context.Context, msg string, kv ...any)

	// WithRequestContext returns a child ctx carrying the per-delivery
	// correlation IDs (the logger.WithRequestContext contract — context.go:27,
	// "call once at ingress"). logger.WithRequestContext lives in the
	// forbidden internal/infra/observability/logger package (context.go:30), so
	// the consumer never calls it directly: the LIC-TASK-047 adapter
	// implements this over logger.WithRequestContext + logger.RequestContext;
	// noopLogger returns ctx unchanged. Declared on the Logger seam (not a
	// separate seam) because it is the same observability concern and avoids a
	// 5th seam (YAGNI). The consumer calls it exactly once per delivery,
	// immediately after successful validation, before the router call
	// (build-spec D6/D11/R4).
	WithRequestContext(ctx context.Context, ids RequestIDs) context.Context
}

// noopLogger is the zero-dependency default. WithRequestContext returns ctx
// unchanged (no correlation linkage without the 047 adapter — R4,
// forward-noted).
type noopLogger struct{}

func (noopLogger) Info(context.Context, string, ...any)  {}
func (noopLogger) Warn(context.Context, string, ...any)  {}
func (noopLogger) Error(context.Context, string, ...any) {}
func (noopLogger) WithRequestContext(ctx context.Context, _ RequestIDs) context.Context {
	return ctx
}

var _ Logger = noopLogger{}
