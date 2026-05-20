package dm

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"contractpro/legal-intelligence-core/internal/domain/port"
)

// topicAnalysisReady is the FROZEN wire topic for the
// LegalAnalysisArtifactsReady envelope (DocumentManagement/architecture/
// event-catalog.md §1.5, integration-contracts.md §6.1, LIC
// event-catalog.md §2, ADR-LIC-05, build-spec D6). Hard-coded — there is NO
// env-var override (build-spec D15 anti-scope): changing the routing key
// would silently de-route every analysis-ready publication and is a
// contract break, not a config knob.
const topicAnalysisReady = "lic.artifacts.analysis-ready"

// marshalArtifacts is the JSON marshaller used to encode the
// LegalAnalysisArtifactsReady envelope. Pointer-to-stdlib by default;
// overridable in _test files so the otherwise-unreachable
// reasonMarshalFailure branch (build-spec D6) has behavioural coverage.
// Production callers always use json.Marshal — there is no exported seam.
var marshalArtifacts = json.Marshal

// Compile-time interface assertion (build-spec D11) — the structural
// satisfaction of port.AnalysisArtifactsPublisherPort (dm.go:51-53) is
// verified by the compiler at build time, NOT at LIC-TASK-047 wiring time.
// Same universal `var _ Port = (*Impl)(nil)` pattern used for the sibling
// ArtifactRequester in requester.go.
var _ port.AnalysisArtifactsPublisherPort = (*AnalysisArtifactsPublisher)(nil)

// PublisherConfig is the local startup-time config for
// AnalysisArtifactsPublisher (LIC-TASK-043 build-spec D2). Local struct,
// NO internal/config import (build-spec D13 — the pipeline.Config /
// pendingconfirmation.Config / dmawaiter.ArtifactConfig precedent).
// LIC-TASK-036 / TASK-047 wires
// config.BrokerConfig.Exchanges.LICArtifacts → PublisherConfig.Exchange.
//
// Symmetric to RequesterConfig (sibling type for ArtifactRequester) — both
// publishers share the single-field "topic exchange" shape; the topic
// routing key is hardcoded per-publisher (build-spec D9 / D15).
type PublisherConfig struct {
	// Exchange is the topic exchange on which the
	// LegalAnalysisArtifactsReady envelope is published. MUST be
	// non-empty — an empty Exchange would publish to the AMQP default
	// exchange (direct-routing by queue name), which would silently
	// de-route the terminal payload of the pipeline. The exchange is
	// declared by broker.DeclareTopology at startup (topology.go).
	Exchange string
}

// validate fails fast on misconfiguration. errors.Join surfaces all defects
// at once (the dmawaiter.ArtifactConfig.validate / pendingconfirmation.
// Config.validate / RequesterConfig.validate precedent). NOT a domain
// error — this is a startup-time wiring defect, not an Orchestrator-visible
// failure.
func (c PublisherConfig) validate() error {
	var errs []error
	if c.Exchange == "" {
		errs = append(errs, errors.New("dm publisher: PublisherConfig.Exchange must be non-empty"))
	}
	return errors.Join(errs...)
}

// AnalysisArtifactsPublisher publishes lic.artifacts.analysis-ready
// envelopes (port.LegalAnalysisArtifactsReady) for the pipeline orchestrator
// (high-architecture.md §6.5 step 8, LIC-TASK-043). Immutable and stateless
// after NewAnalysisArtifactsPublisher; Publish is goroutine-safe across
// distinct correlation_ids — the only shared state is the broker Publisher
// seam, which itself serializes publishes internally
// (broker/publish.go pubMu) so this type takes no mutex.
//
// Role satisfied (compile-time-asserted above):
//
//   - port.AnalysisArtifactsPublisherPort —
//     Publish(ctx, port.LegalAnalysisArtifactsReady) error
type AnalysisArtifactsPublisher struct {
	cfg       PublisherConfig
	publisher Publisher
	metrics   Metrics
	clock     Clock
	log       Logger
}

// NewAnalysisArtifactsPublisher validates the wiring and assembles the
// publisher. It fails fast (NewTypeName per feedback_constructors.md; the
// dmawaiter / pendingconfirmation / pipeline / ArtifactRequester
// precedent): an invalid PublisherConfig or a nil Publisher is a
// LIC-TASK-036 / TASK-047 wiring defect and MUST be a startup error, not a
// first-call nil-deref. errors.Join surfaces ALL defects at once. The
// three OPTIONAL seams (Metrics / Clock / Logger) never cause an error;
// PublisherDeps.withDefaults substitutes a noop for each nil one
// (build-spec D2/D9/D10).
//
// Publisher has NO noop default and is checked AFTER withDefaults — a
// silent-swallow Publisher would lose the terminal analysis-ready payload
// of every pipeline run and break the §6.5 step 9 persist-confirmation
// contract. See PublisherDeps.Publisher godoc.
func NewAnalysisArtifactsPublisher(cfg PublisherConfig, deps PublisherDeps) (*AnalysisArtifactsPublisher, error) {
	var errs []error
	if err := cfg.validate(); err != nil {
		errs = append(errs, err)
	}
	d := deps.withDefaults()
	if d.Publisher == nil {
		errs = append(errs, errors.New("dm publisher: PublisherDeps.Publisher must be non-nil (no noop default — silent swallow on lic.artifacts.analysis-ready is forbidden)"))
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return &AnalysisArtifactsPublisher{
		cfg:       cfg,
		publisher: d.Publisher,
		metrics:   d.Metrics,
		clock:     d.Clock,
		log:       d.Logger,
	}, nil
}

// Publish validates, marshals and publishes a single
// port.LegalAnalysisArtifactsReady envelope on lic.artifacts.analysis-ready.
// Each call produces EXACTLY ONE wire message — no retry loop, no DLQ
// routing (build-spec D10/D15 — those concerns belong to the pipeline
// orchestrator at LIC-TASK-036). The payload is taken by VALUE so the
// caller-side struct is unmodified by the in-publisher timestamp rewrite
// (build-spec D5).
//
// Validation (build-spec D4 — pre-publish, errors short-circuit before
// broker.Publish):
//
//   - correlationID, jobID, documentID, versionID: required (non-empty).
//   - organizationID: optional (empty string → omitted on the wire via
//     port.LegalAnalysisArtifactsReady.OrganizationID's `omitempty` tag).
//   - ClassificationResult, KeyParameters, RiskAnalysis, RiskProfile,
//     Summary, DetailedReport, AggregateScore: required (non-nil
//     pointer to the typed model — Aggregator produces these).
//   - Recommendations: required NON-NIL slice. An empty slice (len==0)
//     is VALID (no recommendations for this contract); a nil slice is
//     invalid (Aggregator must initialise to []Recommendation{} when
//     none).
//   - RiskDelta: optional pointer (`omitempty` on the wire). Present
//     only when parent_version_id != null AND parent RISK_ANALYSIS was
//     available — orchestrator concern.
//
// NEITHER enum nor regex validation runs here — those are Aggregator
// concerns (LIC-TASK-035). The publisher trusts the Aggregator-produced
// model and only enforces the envelope-level required-field contract.
//
// Failure modes:
//
//   - Validation failure → (*PublishError){Reason, Cause: nil}; metric
//     PublishOutcomeInvalid; ObservePublishedSize NOT called;
//     broker.Publish is NOT called.
//   - encoding/json marshal failure → (*PublishError){Reason:
//     reasonMarshalFailure, Cause: err}; metric PublishOutcomeFailure;
//     ObservePublishedSize NOT called; broker.Publish is NOT called.
//     Should be unreachable for compliant inputs.
//   - broker.Publish failure → raw broker error passed through
//     (*broker.BrokerError / sentinel) so the caller's errors.Is /
//     errors.As chain stays intact (build-spec D7); ObservePublishedSize
//     IS called (the payload reached the wire boundary); metric per
//     classifyOutcome (nacked vs failure).
//   - ctx.Done() inside broker.Publish → raw ctx.Err() passed through
//     (build-spec D8 — codebase-wide convention; metric
//     PublishOutcomeFailure per classifyOutcome).
//
// The IncPublish metric is ALWAYS incremented — there is no silent exit
// path. ObservePublishedSize is incremented on every successful marshal
// (success + nacked + failure-after-marshal) so the histogram captures
// the size distribution of every payload that reached the wire boundary.
func (p *AnalysisArtifactsPublisher) Publish(ctx context.Context, payload port.LegalAnalysisArtifactsReady) error {
	// Pre-publish validation (build-spec D4). Short-circuit BEFORE
	// marshalling so an invalid payload never reaches the wire. Each
	// branch emits the PublishOutcomeInvalid metric directly (the
	// classifier does NOT cover validation — it is a broker-outcome
	// classifier only, build-spec D7).
	if payload.CorrelationID == "" {
		return p.failValidation(reasonMissingCorrelationID)
	}
	if payload.JobID == "" {
		return p.failValidation(reasonMissingJobID)
	}
	if payload.DocumentID == "" {
		return p.failValidation(reasonMissingDocumentID)
	}
	if payload.VersionID == "" {
		return p.failValidation(reasonMissingVersionID)
	}
	if payload.ClassificationResult == nil {
		return p.failValidation(reasonMissingClassificationResult)
	}
	if payload.KeyParameters == nil {
		return p.failValidation(reasonMissingKeyParameters)
	}
	if payload.RiskAnalysis == nil {
		return p.failValidation(reasonMissingRiskAnalysis)
	}
	if payload.RiskProfile == nil {
		return p.failValidation(reasonMissingRiskProfile)
	}
	if payload.Recommendations == nil {
		return p.failValidation(reasonMissingRecommendations)
	}
	if payload.Summary == nil {
		return p.failValidation(reasonMissingSummary)
	}
	if payload.DetailedReport == nil {
		return p.failValidation(reasonMissingDetailedReport)
	}
	if payload.AggregateScore == nil {
		return p.failValidation(reasonMissingAggregateScore)
	}

	// Timestamp rewrite (build-spec D5). The caller-side value is
	// unmodified because Publish takes the payload by VALUE — the
	// rewrite acts on the local copy only. RFC3339Nano UTC matches
	// the codebase-wide convention (the systemClock default returns
	// time.Now().UTC()).
	payload.Timestamp = p.clock.Now().Format(time.RFC3339Nano)

	// Marshal (build-spec D6). port.LegalAnalysisArtifactsReady's json
	// tags drive the serialised shape; OrganizationID + RiskDelta carry
	// `omitempty` so empty / nil values are omitted from the JSON
	// object entirely.
	bytes, err := marshalArtifacts(payload)
	if err != nil {
		// Defensive: encoding/json on a typed-model struct should not
		// fail in practice. If it does, surface as a PublishError with
		// the original cause for triage and classify as a failure
		// (NOT invalid — invalid is for caller input defects; this is
		// a serialisation defect). broker.Publish is NOT called.
		p.metrics.IncPublish(topicAnalysisReady, PublishOutcomeFailure)
		return &PublishError{Reason: reasonMarshalFailure, Cause: err}
	}

	// Observe wire size (observability.md §3.5). Called UNCONDITIONALLY
	// on every successful marshal — including paths that subsequently
	// fail at broker.Publish — so the histogram captures the size
	// distribution of every payload that reached the wire boundary.
	p.metrics.ObservePublishedSize(len(bytes))

	// Publish. The broker client serializes publish + waits for the
	// publisher-confirm ack internally (broker/publish.go); we forward
	// the result verbatim and let classifyOutcome bucket the metric.
	pubErr := p.publisher.Publish(ctx, p.cfg.Exchange, topicAnalysisReady, bytes)
	p.metrics.IncPublish(topicAnalysisReady, classifyOutcome(pubErr))
	return pubErr
}

// failValidation centralises the validation-failure exit: emit the invalid
// metric, return a *PublishError with the supplied reason and a nil Cause
// (build-spec D4). Kept private — the only caller is Publish. Symmetric to
// ArtifactRequester.failValidation.
func (p *AnalysisArtifactsPublisher) failValidation(reason string) error {
	p.metrics.IncPublish(topicAnalysisReady, PublishOutcomeInvalid)
	return &PublishError{Reason: reason}
}
