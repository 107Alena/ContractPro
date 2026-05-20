package orch

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"time"

	"contractpro/legal-intelligence-core/internal/domain/port"
)

// topicClassificationUncertain is the FROZEN wire topic for the
// ClassificationUncertain envelope (LIC event-catalog.md §1.2, high-
// architecture.md §6.13, observability.md §3.9, LIC-TASK-045). Hard-
// coded — there is NO env-var override: changing the routing key would
// silently de-route the type-confirmation prompt and is a contract
// break, not a config knob.
const topicClassificationUncertain = "lic.events.classification-uncertain"

// marshalUncertain is the JSON marshaller used to encode the
// ClassificationUncertain envelope. Pointer-to-stdlib by default;
// overridable in _test files so the otherwise-unreachable
// reasonMarshalFailure branch has behavioural coverage. Production
// callers always use json.Marshal — there is no exported seam.
var marshalUncertain = json.Marshal

// Compile-time interface assertion — the structural satisfaction of
// port.UncertaintyPublisherPort (publisher.go:26-28 in domain/port) is
// verified by the compiler at build time. Same universal
// `var _ Port = (*Impl)(nil)` pattern used by the sibling
// StatusPublisher and the dm publishers.
var _ port.UncertaintyPublisherPort = (*UncertaintyPublisher)(nil)

// UncertaintyPublisherConfig is the local startup-time config for
// UncertaintyPublisher (LIC-TASK-045). Symmetric to
// StatusPublisherConfig — local struct, NO internal/config import.
// LIC-TASK-036 / TASK-047 wires config.BrokerConfig.Exchanges.LICEvents
// → UncertaintyPublisherConfig.Exchange.
type UncertaintyPublisherConfig struct {
	// Exchange is the topic exchange on which the
	// ClassificationUncertain envelope is published. MUST be non-empty.
	// (Same rationale as StatusPublisherConfig.Exchange — empty
	// Exchange would publish to the AMQP default exchange and silently
	// de-route the event.)
	Exchange string
}

// validate fails fast on misconfiguration (errors.Join surfaces all
// defects at once — the StatusPublisherConfig.validate / dm.PublisherConfig.
// validate precedent).
func (c UncertaintyPublisherConfig) validate() error {
	var errs []error
	if c.Exchange == "" {
		errs = append(errs, errors.New("orch publisher: UncertaintyPublisherConfig.Exchange must be non-empty"))
	}
	return errors.Join(errs...)
}

// UncertaintyPublisher publishes lic.events.classification-uncertain
// envelopes (port.ClassificationUncertain) once per version on Agent 1
// low-confidence pause (high-architecture.md §6.13, LIC event-catalog
// §1.2, LIC-TASK-045). Immutable and stateless after
// NewUncertaintyPublisher; PublishClassificationUncertain is
// goroutine-safe across distinct correlation_ids — the only shared
// state is the broker Publisher seam (which itself serializes publishes
// internally on its pubMu).
//
// Role satisfied (compile-time-asserted above):
//
//   - port.UncertaintyPublisherPort —
//     PublishClassificationUncertain(ctx, port.ClassificationUncertain) error
type UncertaintyPublisher struct {
	cfg       UncertaintyPublisherConfig
	publisher Publisher
	metrics   Metrics
	clock     Clock
	log       Logger
}

// NewUncertaintyPublisher validates the wiring and assembles the
// publisher (NewTypeName per feedback_constructors.md). It fails fast
// (errors.Join surfaces both defects together — the «both defects
// surface together» pin from StatusPublisher's T-CTOR-3):
//
//   - an empty UncertaintyPublisherConfig.Exchange and/or
//   - a nil UncertaintyPublisherDeps.Publisher
//
// are LIC-TASK-036 / TASK-047 wiring defects and MUST be a startup
// error, not a first-call nil-deref.
//
// Publisher has NO noop default and is checked AFTER withDefaults — a
// silent-swallow Publisher would make every
// lic.events.classification-uncertain publish invisible (no broker, no
// log, no metric), blocking the type-confirmation prompt path entirely.
func NewUncertaintyPublisher(cfg UncertaintyPublisherConfig, deps UncertaintyPublisherDeps) (*UncertaintyPublisher, error) {
	var errs []error
	if err := cfg.validate(); err != nil {
		errs = append(errs, err)
	}
	d := deps.withDefaults()
	if d.Publisher == nil {
		errs = append(errs, errors.New("orch publisher: UncertaintyPublisherDeps.Publisher must be non-nil (no noop default — silent swallow on lic.events.classification-uncertain is forbidden)"))
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return &UncertaintyPublisher{
		cfg:       cfg,
		publisher: d.Publisher,
		metrics:   d.Metrics,
		clock:     d.Clock,
		log:       d.Logger,
	}, nil
}

// PublishClassificationUncertain validates, marshals and publishes a
// single port.ClassificationUncertain envelope on
// lic.events.classification-uncertain. Each call produces EXACTLY ONE
// wire message — no retry loop, no DLQ routing (those concerns belong
// to the pendingconfirmation Manager + the pipeline orchestrator). The
// payload is taken by VALUE so the caller-side struct is unmodified by
// the in-publisher Timestamp rewrite.
//
// Validation (pre-publish, errors short-circuit before broker.Publish):
//
// Block A — 5 caller-supplied envelope IDs (correlation_id, job_id,
// document_id, version_id, organization_id). Timestamp is NOT validated
// here — this publisher rewrites it from the Clock seam (see below).
// organization_id is REQUIRED (event-catalog §1.2 required list);
// reasonMissingOrganizationID rejects empty.
//
// Block B — SuggestedType enum: !SuggestedType.IsValid() (rejects both
// empty and any value outside the 12-whitelist of model.ContractType).
//
// Block C — Confidence ∈ [0, 1]: math.IsNaN(c) || c < 0 || c > 1.
// NaN is EXPLICIT — Go float comparisons against NaN always return
// false, so a naïve `c < 0 || c > 1` would silently let NaN through.
//
// Block D — Threshold ∈ [0, 1]: same shape as Block C.
//
// Block E — Alternatives optional. nil or empty slice → publish
// proceeds (range no-op). Non-empty: each alternative MUST satisfy
// alt.ContractType.IsValid() and alt.Confidence ∈ [0, 1] (same
// NaN-explicit pattern). NO cap-validation (FROZEN §1.2 has no
// maxItems), NO uniqueness check (producer policy, not publisher
// concern).
//
// In-method rewrite (after validation passes):
//
//   - Timestamp rewrite: payload.Timestamp = clock.Now().Format(time.
//     RFC3339Nano). The caller-side value is unmodified because
//     PublishClassificationUncertain takes the payload by VALUE — the
//     rewrite acts on the local copy only. UTC matches the
//     codebase-wide convention. The systemClock default returns
//     time.Now().UTC().
//
// Failure modes:
//
//   - Validation failure → (*PublishError){Reason, Cause: nil}; metric
//     PublishOutcomeInvalid; broker.Publish is NOT called.
//   - encoding/json marshal failure → (*PublishError){Reason:
//     reasonMarshalFailure, Cause: err}; metric PublishOutcomeFailure;
//     broker.Publish is NOT called. Defensive — unreachable for
//     compliant inputs.
//   - broker.Publish failure → raw broker error passed through
//     (*broker.BrokerError / sentinel) so the caller's errors.Is /
//     errors.As chain stays intact; metric per classifyOutcome (nacked
//     vs failure).
//   - ctx.Done() inside broker.Publish → raw ctx.Err() passed through
//     (codebase-wide convention; metric PublishOutcomeFailure per
//     classifyOutcome).
//
// The IncPublish metric is ALWAYS incremented exactly once — there is
// no silent exit path.
func (p *UncertaintyPublisher) PublishClassificationUncertain(ctx context.Context, evt port.ClassificationUncertain) error {
	// Block A — 5 required envelope IDs (organization_id REQUIRED per
	// event-catalog §1.2 — no omitempty on the wire DTO).
	if evt.CorrelationID == "" {
		return p.failValidation(reasonMissingCorrelationID)
	}
	if evt.JobID == "" {
		return p.failValidation(reasonMissingJobID)
	}
	if evt.DocumentID == "" {
		return p.failValidation(reasonMissingDocumentID)
	}
	if evt.VersionID == "" {
		return p.failValidation(reasonMissingVersionID)
	}
	if evt.OrganizationID == "" {
		return p.failValidation(reasonMissingOrganizationID)
	}

	// Block B — SuggestedType enum (12-whitelist per
	// model.ContractType.IsValid). Empty and unknown rejected together.
	if !evt.SuggestedType.IsValid() {
		return p.failValidation(reasonInvalidSuggestedType)
	}

	// Block C — Confidence ∈ [0, 1]. NaN-handling EXPLICIT: Go float
	// comparisons against NaN always return false.
	if math.IsNaN(evt.Confidence) || evt.Confidence < 0 || evt.Confidence > 1 {
		return p.failValidation(reasonInvalidConfidence)
	}

	// Block D — Threshold ∈ [0, 1]. Same NaN-explicit pattern.
	if math.IsNaN(evt.Threshold) || evt.Threshold < 0 || evt.Threshold > 1 {
		return p.failValidation(reasonInvalidThreshold)
	}

	// Block E — Alternatives optional. range over nil/empty is a no-op,
	// so both states publish successfully (R6 — nil/empty equivalence).
	for _, alt := range evt.Alternatives {
		if !alt.ContractType.IsValid() {
			return p.failValidation(reasonInvalidAlternativeType)
		}
		if math.IsNaN(alt.Confidence) || alt.Confidence < 0 || alt.Confidence > 1 {
			return p.failValidation(reasonInvalidAlternativeConfidence)
		}
	}

	// Timestamp rewrite. Value-receiver — the caller-side variable is
	// unmodified (the rewrite acts on the local copy only).
	evt.Timestamp = p.clock.Now().Format(time.RFC3339Nano)

	// Marshal. port.ClassificationUncertain's json tags drive the
	// serialised shape; Alternatives carries `omitempty` so a nil/empty
	// slice is omitted from the JSON object entirely (organization_id,
	// suggested_type, confidence, threshold have NO omitempty —
	// verified by Blocks A-D).
	bytes, err := marshalUncertain(evt)
	if err != nil {
		// Defensive: encoding/json on a typed-model struct with float
		// + string + slice fields should not fail in practice. If it
		// does, surface as a PublishError with the original cause for
		// triage and classify as a failure (NOT invalid — invalid is
		// for caller input defects; this is a serialisation defect).
		p.metrics.IncPublish(topicClassificationUncertain, PublishOutcomeFailure)
		return &PublishError{Reason: reasonMarshalFailure, Cause: err}
	}

	// Publish. The broker client serializes publish + waits for the
	// publisher-confirm ack internally; we forward the result verbatim
	// and let classifyOutcome bucket the metric.
	pubErr := p.publisher.Publish(ctx, p.cfg.Exchange, topicClassificationUncertain, bytes)
	p.metrics.IncPublish(topicClassificationUncertain, classifyOutcome(pubErr))
	return pubErr
}

// failValidation centralises the validation-failure exit: emit the
// invalid metric, return a *PublishError with the supplied reason and a
// nil Cause. Kept private — the only caller is
// PublishClassificationUncertain. Symmetric to (*StatusPublisher).
// failValidation.
func (p *UncertaintyPublisher) failValidation(reason string) error {
	p.metrics.IncPublish(topicClassificationUncertain, PublishOutcomeInvalid)
	return &PublishError{Reason: reason}
}
