package orch

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
)

// topicStatusChanged is the FROZEN wire topic for the LICStatusChangedEvent
// envelope (LIC event-catalog.md §1.1, high-architecture.md §6.13,
// observability.md §3.9, LIC-TASK-044). Hard-coded — there is NO env-var
// override: changing the routing key would silently de-route every status
// transition and is a contract break, not a config knob.
const topicStatusChanged = "lic.events.status-changed"

// marshalStatus is the JSON marshaller used to encode the
// LICStatusChangedEvent envelope. Pointer-to-stdlib by default; overridable
// in _test files so the otherwise-unreachable reasonMarshalFailure branch
// has behavioural coverage. Production callers always use json.Marshal —
// there is no exported seam.
var marshalStatus = json.Marshal

// Compile-time interface assertion — the structural satisfaction of
// port.StatusPublisherPort (publisher.go:15-17 in domain/port) is verified
// by the compiler at build time, NOT at LIC-TASK-047 wiring time. Same
// universal `var _ Port = (*Impl)(nil)` pattern used for the sibling
// dm.ArtifactRequester / dm.AnalysisArtifactsPublisher.
var _ port.StatusPublisherPort = (*StatusPublisher)(nil)

// PublisherConfig is the local startup-time config for StatusPublisher
// (LIC-TASK-044). Local struct, NO internal/config import (the
// pipeline.Config / pendingconfirmation.Config / dm.PublisherConfig
// precedent). LIC-TASK-036 / TASK-047 wires
// config.BrokerConfig.Exchanges.LICEvents → PublisherConfig.Exchange.
type PublisherConfig struct {
	// Exchange is the topic exchange on which the LICStatusChangedEvent
	// envelope is published. MUST be non-empty — an empty Exchange would
	// publish to the AMQP default exchange (direct-routing by queue name),
	// which would silently de-route the status event. The exchange is
	// declared by broker.DeclareTopology at startup (topology.go).
	Exchange string
}

// validate fails fast on misconfiguration. errors.Join surfaces all defects
// at once (the dmawaiter.ArtifactConfig.validate / pendingconfirmation.
// Config.validate / dm.PublisherConfig.validate precedent). NOT a domain
// error — this is a startup-time wiring defect, not an Orchestrator-visible
// failure.
func (c PublisherConfig) validate() error {
	var errs []error
	if c.Exchange == "" {
		errs = append(errs, errors.New("orch publisher: PublisherConfig.Exchange must be non-empty"))
	}
	return errors.Join(errs...)
}

// StatusPublisher publishes lic.events.status-changed envelopes
// (port.LICStatusChangedEvent) for the pipeline orchestrator
// (high-architecture.md §6.13, LIC-TASK-044). Immutable and stateless after
// NewStatusPublisher; PublishStatus is goroutine-safe across distinct
// correlation_ids — the only shared state is the broker Publisher seam,
// which itself serializes publishes internally (broker/publish.go pubMu) so
// this type takes no mutex.
//
// Role satisfied (compile-time-asserted above):
//
//   - port.StatusPublisherPort —
//     PublishStatus(ctx, port.LICStatusChangedEvent) error
type StatusPublisher struct {
	cfg       PublisherConfig
	publisher Publisher
	metrics   Metrics
	clock     Clock
	log       Logger
}

// NewStatusPublisher validates the wiring and assembles the publisher. It
// fails fast (NewTypeName per feedback_constructors.md; the dmawaiter /
// pendingconfirmation / pipeline / dm.NewAnalysisArtifactsPublisher
// precedent): an invalid PublisherConfig or a nil Publisher is a
// LIC-TASK-036 / TASK-047 wiring defect and MUST be a startup error, not a
// first-call nil-deref. errors.Join surfaces ALL defects at once. The three
// OPTIONAL seams (Metrics / Clock / Logger) never cause an error;
// PublisherDeps.withDefaults substitutes a noop for each nil one.
//
// Publisher has NO noop default and is checked AFTER withDefaults — a
// silent-swallow Publisher would make every lic.events.status-changed
// publish invisible (no broker, no log, no metric, the Orchestrator never
// sees a status transition). See PublisherDeps.Publisher godoc.
func NewStatusPublisher(cfg PublisherConfig, deps PublisherDeps) (*StatusPublisher, error) {
	var errs []error
	if err := cfg.validate(); err != nil {
		errs = append(errs, err)
	}
	d := deps.withDefaults()
	if d.Publisher == nil {
		errs = append(errs, errors.New("orch publisher: PublisherDeps.Publisher must be non-nil (no noop default — silent swallow on lic.events.status-changed is forbidden)"))
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return &StatusPublisher{
		cfg:       cfg,
		publisher: d.Publisher,
		metrics:   d.Metrics,
		clock:     d.Clock,
		log:       d.Logger,
	}, nil
}

// PublishStatus validates, marshals and publishes a single
// port.LICStatusChangedEvent envelope on lic.events.status-changed. Each
// call produces EXACTLY ONE wire message — no retry loop, no DLQ routing
// (those concerns belong to the pipeline orchestrator at LIC-TASK-036). The
// payload is taken by VALUE so the caller-side struct is unmodified by the
// in-publisher timestamp / ErrorMessage rewrites.
//
// Validation (pre-publish, errors short-circuit before broker.Publish):
//
// Block A — envelope IDs (5 required, NO omitempty on the wire):
// correlation_id, job_id, document_id, version_id, organization_id.
// NOTE: organization_id is REQUIRED here, distinct from the sibling
// dm.GetArtifactsRequest where it carries `omitempty`. The
// port.LICStatusChangedEvent.OrganizationID has NO omitempty tag — every
// Orchestrator-bound status event MUST carry a known organization.
//
// Block B — Status enum: !Status.IsValid() (covers both empty and unknown
// values).
//
// Block C — Stage optional: empty Stage is allowed (omitempty on the wire);
// non-empty Stage MUST satisfy IsValid().
//
// Block D — status-conditional fields:
//
//   - IF Status == FAILED: ErrorCode required (non-empty);
//     ErrorCode.IsPublishableToOrchestrator() must be true (rejects both
//     unknown codes and the documented non-publishable set —
//     INVALID_MESSAGE_SCHEMA, INVALID_ORG_ID_MISMATCH,
//     IDEMPOTENCY_STORE_UNAVAILABLE, plus any future code whose catalog
//     UserMessage is empty); IsRetryable required (non-nil pointer).
//   - IF Status != FAILED (i.e. IN_PROGRESS or COMPLETED): ErrorCode MUST be
//     empty, ErrorMessage MUST be empty, IsRetryable MUST be nil —
//     stale-data-leak guard. Any one of these set is a single combined
//     reasonUnexpectedFailureFields failure.
//
// In-method rewrites (after validation passes):
//
//   - Timestamp rewrite: payload.Timestamp = clock.Now().Format(RFC3339Nano).
//     The caller-side value is unmodified because PublishStatus takes the
//     payload by VALUE — the rewrite acts on the local copy only.
//     RFC3339Nano UTC matches the codebase-wide convention.
//   - ErrorMessage rewrite (FAILED only): payload.ErrorMessage =
//     LookupErrorSpec(ErrorCode).UserMessage. The catalog is the single
//     source of truth for the RU user-facing rendering (NFR-5.2); any
//     caller-supplied ErrorMessage is OVERWRITTEN. A LookupErrorSpec miss
//     here returns the defensive reasonErrorCodeNotInCatalog (theoretically
//     unreachable after Block D step 9).
//
// Failure modes:
//
//   - Validation failure → (*PublishError){Reason, Cause: nil}; metric
//     PublishOutcomeInvalid; broker.Publish is NOT called.
//   - encoding/json marshal failure → (*PublishError){Reason:
//     reasonMarshalFailure, Cause: err}; metric PublishOutcomeFailure;
//     broker.Publish is NOT called. Should be unreachable for compliant
//     inputs.
//   - broker.Publish failure → raw broker error passed through
//     (*broker.BrokerError / sentinel) so the caller's errors.Is /
//     errors.As chain stays intact; metric per classifyOutcome (nacked vs
//     failure).
//   - ctx.Done() inside broker.Publish → raw ctx.Err() passed through
//     (codebase-wide convention; metric PublishOutcomeFailure per
//     classifyOutcome).
//
// The IncPublish metric is ALWAYS incremented exactly once — there is no
// silent exit path.
func (p *StatusPublisher) PublishStatus(ctx context.Context, evt port.LICStatusChangedEvent) error {
	// Block A — 5 required envelope IDs. organization_id is REQUIRED for
	// the orchestrator-bound status event (no omitempty on the wire DTO),
	// in deliberate divergence from the sibling dm publisher where the
	// same field is optional.
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

	// Block B — Status enum. IsValid covers the empty string and any
	// unknown value in a single branch.
	if !evt.Status.IsValid() {
		return p.failValidation(reasonInvalidStatus)
	}

	// Block C — Stage is optional (omitempty on the wire). When supplied,
	// it MUST resolve to a known pipeline stage; an unknown stage would
	// silently corrupt structured logs / labels downstream.
	if evt.Stage != "" && !evt.Stage.IsValid() {
		return p.failValidation(reasonInvalidStage)
	}

	// Block D — status-conditional error fields.
	if evt.Status == model.StatusFailed {
		if evt.ErrorCode == "" {
			return p.failValidation(reasonMissingErrorCode)
		}
		if !evt.ErrorCode.IsPublishableToOrchestrator() {
			// Closes BOTH the documented non-publishable set
			// (INVALID_MESSAGE_SCHEMA, INVALID_ORG_ID_MISMATCH,
			// IDEMPOTENCY_STORE_UNAVAILABLE) AND any code that is not
			// registered in errorCatalog — IsPublishableToOrchestrator
			// returns false for unknown codes too (safe default per the
			// model godoc).
			return p.failValidation(reasonNonPublishableErrorCode)
		}
		if evt.IsRetryable == nil {
			return p.failValidation(reasonMissingRetryable)
		}
	} else {
		// IN_PROGRESS or COMPLETED — none of the FAILED-only fields may
		// be set. A single combined branch keeps the contract tight
		// (stale-data-leak guard: if the aggregator forgot to reset
		// these between transitions, surface ONE clear failure rather
		// than three near-identical ones).
		if evt.ErrorCode != "" || evt.ErrorMessage != "" || evt.IsRetryable != nil {
			return p.failValidation(reasonUnexpectedFailureFields)
		}
	}

	// Timestamp rewrite. The caller-side value is unmodified because
	// PublishStatus takes the payload by VALUE — the rewrite acts on the
	// local copy only. RFC3339Nano UTC matches the codebase-wide
	// convention (the systemClock default returns time.Now().UTC()).
	evt.Timestamp = p.clock.Now().Format(time.RFC3339Nano)

	// ErrorMessage rewrite (FAILED only). The catalog is the single source
	// of truth for the RU user-facing rendering (NFR-5.2); any
	// caller-supplied ErrorMessage is OVERWRITTEN with the spec.UserMessage.
	// The lookup miss leg is defensive (Block D step 9 already rejected
	// non-publishable / unregistered codes), kept for triage if the
	// catalog SSOT ever drifts at runtime.
	if evt.Status == model.StatusFailed {
		spec, ok := model.LookupErrorSpec(evt.ErrorCode)
		if !ok {
			return p.failValidation(reasonErrorCodeNotInCatalog)
		}
		evt.ErrorMessage = spec.UserMessage
	}

	// Marshal. port.LICStatusChangedEvent's json tags drive the serialised
	// shape; Stage / ErrorCode / ErrorMessage / IsRetryable carry
	// `omitempty` so empty / nil values are omitted from the JSON object
	// entirely (organization_id has NO omitempty — verified by Block A).
	bytes, err := marshalStatus(evt)
	if err != nil {
		// Defensive: encoding/json on a typed-model struct should not
		// fail in practice. If it does, surface as a PublishError with
		// the original cause for triage and classify as a failure (NOT
		// invalid — invalid is for caller input defects; this is a
		// serialisation defect). broker.Publish is NOT called.
		p.metrics.IncPublish(topicStatusChanged, PublishOutcomeFailure)
		return &PublishError{Reason: reasonMarshalFailure, Cause: err}
	}

	// Publish. The broker client serializes publish + waits for the
	// publisher-confirm ack internally (broker/publish.go); we forward the
	// result verbatim and let classifyOutcome bucket the metric.
	pubErr := p.publisher.Publish(ctx, p.cfg.Exchange, topicStatusChanged, bytes)
	p.metrics.IncPublish(topicStatusChanged, classifyOutcome(pubErr))
	return pubErr
}

// failValidation centralises the validation-failure exit: emit the invalid
// metric, return a *PublishError with the supplied reason and a nil Cause.
// Kept private — the only caller is PublishStatus. Symmetric to the
// sibling dm publishers' failValidation.
func (p *StatusPublisher) failValidation(reason string) error {
	p.metrics.IncPublish(topicStatusChanged, PublishOutcomeInvalid)
	return &PublishError{Reason: reason}
}
