package dm

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
)

// topicArtifactsRequest is the FROZEN wire topic for the GetArtifactsRequest
// envelope (DocumentManagement/architecture/event-catalog.md §1.4,
// integration-contracts.md §6.1, build-spec D9). Hard-coded — there is NO
// env-var override (build-spec D15 anti-scope): changing the routing key
// would silently de-route every LIC artifact request and is a contract
// break, not a config knob.
const topicArtifactsRequest = "lic.requests.artifacts"

// marshalRequest is the JSON marshaller used to encode the GetArtifactsRequest
// envelope. Pointer-to-stdlib by default; overridable in _test files so the
// otherwise-unreachable reasonMarshalFailure branch (build-spec D6) has
// behavioural coverage. Production callers always use json.Marshal — there is
// no exported seam.
var marshalRequest = json.Marshal

// Compile-time interface assertion (build-spec D11) — the structural
// satisfaction of port.ArtifactRequesterPort (dm.go:33-39) is verified by
// the compiler at build time, NOT at LIC-TASK-047 wiring time. This is the
// universal `var _ Port = (*Impl)(nil)` pattern; egress publishers
// concretely implement domain ports (vs the dmawaiter case where the
// var _ lives in 047 because asserting the router Deliverer port locally
// would force a forbidden import).
var _ port.ArtifactRequesterPort = (*ArtifactRequester)(nil)

// Config is the local startup-time config for ArtifactRequester (build-spec
// D2). Local struct, NO internal/config import (build-spec D13 — the
// pipeline.Config / pendingconfirmation.Config / dmawaiter.ArtifactConfig
// precedent). LIC-TASK-036 / TASK-047 wires
// config.BrokerConfig.Exchanges.LICRequests → Config.Exchange.
type Config struct {
	// Exchange is the topic exchange on which the GetArtifactsRequest is
	// published. MUST be non-empty — an empty Exchange would publish to
	// the AMQP default exchange (direct-routing by queue name), which
	// would silently de-route the message. The exchange is declared by
	// broker.DeclareTopology at startup (topology.go).
	Exchange string
}

// validate fails fast on misconfiguration. errors.Join surfaces all defects
// at once (the dmawaiter.ArtifactConfig.validate / pendingconfirmation.
// Config.validate precedent). NOT a domain error — this is a startup-time
// wiring defect, not an Orchestrator-visible failure.
func (c Config) validate() error {
	var errs []error
	if c.Exchange == "" {
		errs = append(errs, errors.New("dm publisher: Config.Exchange must be non-empty"))
	}
	return errors.Join(errs...)
}

// ArtifactRequester publishes lic.requests.artifacts envelopes
// (port.GetArtifactsRequest) for the pipeline orchestrator
// (high-architecture.md §6.5 step 1, LIC-TASK-042). Immutable and
// stateless after NewArtifactRequester; RequestArtifacts is goroutine-safe
// across distinct correlation_ids (build-spec D12) — the only shared state
// is the broker Publisher seam, which itself serializes publishes
// internally (broker/publish.go pubMu) so this type takes no mutex.
//
// Role satisfied (compile-time-asserted above):
//
//   - port.ArtifactRequesterPort — RequestArtifacts(ctx, correlationID,
//     jobID, documentID, versionID, organizationID, []model.ArtifactType)
//     error
type ArtifactRequester struct {
	cfg       Config
	publisher Publisher
	metrics   Metrics
	clock     Clock
	log       Logger
}

// NewArtifactRequester validates the wiring and assembles the requester. It
// fails fast (NewTypeName per feedback_constructors.md; the dmawaiter /
// pendingconfirmation / pipeline precedent): an invalid Config or a nil
// Publisher is a LIC-TASK-036 / TASK-047 wiring defect and MUST be a startup
// error, not a first-call nil-deref. errors.Join surfaces ALL defects at
// once. The three OPTIONAL seams (Metrics / Clock / Logger) never cause an
// error; Deps.withDefaults substitutes a noop for each nil one (build-spec
// D2/D9/D10).
//
// Publisher has NO noop default and is checked AFTER withDefaults — a
// silent-swallow Publisher would make every lic.requests.artifacts publish
// invisible (no broker, no log, no metric, the pipeline awaiter blocks
// forever). See Deps.Publisher godoc.
func NewArtifactRequester(cfg Config, deps Deps) (*ArtifactRequester, error) {
	var errs []error
	if err := cfg.validate(); err != nil {
		errs = append(errs, err)
	}
	d := deps.withDefaults()
	if d.Publisher == nil {
		errs = append(errs, errors.New("dm publisher: Deps.Publisher must be non-nil (no noop default — silent swallow on lic.requests.artifacts is forbidden)"))
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return &ArtifactRequester{
		cfg:       cfg,
		publisher: d.Publisher,
		metrics:   d.Metrics,
		clock:     d.Clock,
		log:       d.Logger,
	}, nil
}

// RequestArtifacts builds, validates, marshals and publishes a single
// port.GetArtifactsRequest envelope on lic.requests.artifacts. Each call
// produces EXACTLY ONE wire message — no RE_CHECK fan-out, no retry loop,
// no DLQ routing (build-spec D10/D15 — those concerns belong to the pipeline
// orchestrator at LIC-TASK-036).
//
// Validation (build-spec D4 — pre-publish, errors short-circuit before
// broker.Publish):
//
//   - correlationID, jobID, documentID, versionID: required (non-empty).
//   - organizationID: optional (empty string → omitted on the wire via
//     port.GetArtifactsRequest.OrganizationID's `omitempty` tag).
//   - artifactTypes: non-empty; each entry must satisfy
//     model.ArtifactType.IsValid(). Duplicate types are NOT rejected
//     (the orchestrator may de-duplicate upstream; we forward verbatim).
//
// Failure modes:
//
//   - Validation failure → (*PublishError){Reason, Cause: nil}; metric
//     PublishOutcomeInvalid; broker.Publish is NOT called.
//   - encoding/json marshal failure → (*PublishError){Reason:
//     reasonMarshalFailure, Cause: err}; metric PublishOutcomeFailure;
//     broker.Publish is NOT called. Should be unreachable for compliant
//     inputs (no exotic types on the wire DTO).
//   - broker.Publish failure → raw broker error passed through
//     (*broker.BrokerError / sentinel) so the caller's errors.Is /
//     errors.As chain stays intact (build-spec D7); metric per
//     classifyOutcome (nacked vs failure).
//   - ctx.Done() inside broker.Publish → raw ctx.Err() passed through
//     (build-spec D8 — codebase-wide convention; metric
//     PublishOutcomeFailure per classifyOutcome).
//
// The metric is ALWAYS incremented — there is no silent exit path.
func (r *ArtifactRequester) RequestArtifacts(
	ctx context.Context,
	correlationID, jobID, documentID, versionID, organizationID string,
	artifactTypes []model.ArtifactType,
) error {
	// Pre-publish validation (build-spec D4). Short-circuit BEFORE
	// constructing the wire envelope so an invalid call never reaches
	// broker.Publish. Each branch emits the PublishOutcomeInvalid
	// metric directly (the classifier does NOT cover validation — it is a
	// broker-outcome classifier only, build-spec D7).
	if correlationID == "" {
		return r.failValidation(reasonMissingCorrelationID)
	}
	if jobID == "" {
		return r.failValidation(reasonMissingJobID)
	}
	if documentID == "" {
		return r.failValidation(reasonMissingDocumentID)
	}
	if versionID == "" {
		return r.failValidation(reasonMissingVersionID)
	}
	if len(artifactTypes) == 0 {
		return r.failValidation(reasonMissingArtifactTypes)
	}
	for _, at := range artifactTypes {
		if !at.IsValid() {
			return r.failValidation(reasonInvalidArtifactType)
		}
	}

	// Build the wire envelope (build-spec D6). port.GetArtifactsRequest's
	// json tags (events.go:147-155) drive the serialised shape;
	// OrganizationID has `omitempty` so an empty value is omitted from
	// the JSON object entirely.
	req := port.GetArtifactsRequest{
		CorrelationID:  correlationID,
		Timestamp:      r.clock.Now().Format(time.RFC3339Nano),
		JobID:          jobID,
		DocumentID:     documentID,
		VersionID:      versionID,
		OrganizationID: organizationID,
		ArtifactTypes:  artifactTypes,
	}

	payload, err := marshalRequest(req)
	if err != nil {
		// Defensive: encoding/json on a fixed-shape struct with
		// stdlib-friendly types should not fail. If it does, surface
		// as a PublishError with the original cause for triage and
		// classify as a failure (NOT invalid — invalid is for caller
		// input defects; this is a serialisation defect).
		r.metrics.IncPublish(topicArtifactsRequest, PublishOutcomeFailure)
		return &PublishError{Reason: reasonMarshalFailure, Cause: err}
	}

	// Publish. The broker client serializes publish + waits for the
	// publisher-confirm ack internally (broker/publish.go); we forward
	// the result verbatim and let classifyOutcome bucket the metric.
	pubErr := r.publisher.Publish(ctx, r.cfg.Exchange, topicArtifactsRequest, payload)
	r.metrics.IncPublish(topicArtifactsRequest, classifyOutcome(pubErr))
	return pubErr
}

// failValidation centralises the validation-failure exit: emit the invalid
// metric, return a *PublishError with the supplied reason and a nil Cause
// (build-spec D4). Kept private — the only caller is RequestArtifacts.
func (r *ArtifactRequester) failValidation(reason string) error {
	r.metrics.IncPublish(topicArtifactsRequest, PublishOutcomeInvalid)
	return &PublishError{Reason: reason}
}
