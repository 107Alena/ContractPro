package dlq

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"contractpro/legal-intelligence-core/internal/domain/port"
)

// marshalEnvelope is the JSON marshaller used to encode the LICDLQEnvelope.
// Pointer-to-stdlib by default; overridable in _test files so the otherwise-
// unreachable reasonMarshalFailure branch has behavioural coverage.
// Production callers always use json.Marshal — there is no exported seam.
var marshalEnvelope = json.Marshal

// Compile-time interface assertion — the structural satisfaction of
// port.DLQPublisherPort (publisher.go in domain/port) is verified by the
// compiler at build time, NOT at LIC-TASK-047 wiring time. Same universal
// `var _ Port = (*Impl)(nil)` pattern used for the sibling
// dm.AnalysisArtifactsPublisher, orch.StatusPublisher, etc.
var _ port.DLQPublisherPort = (*DLQPublisher)(nil)

// Config is the local startup-time config for DLQPublisher. Local struct,
// NO internal/config import (the pipeline.Config / pendingconfirmation.
// Config / dm.PublisherConfig / orch.StatusPublisherConfig precedent).
// LIC-TASK-047 wires config.BrokerConfig.ExchangeDLX → Config.Exchange.
type Config struct {
	// Exchange is the DLX topic exchange on which the LICDLQEnvelope is
	// published. MUST be non-empty — an empty Exchange would publish to
	// the AMQP default exchange (direct-routing by queue name), which
	// would silently de-route every DLQ envelope. The exchange is
	// declared by broker.DeclareTopology at startup (topology.go) — the
	// same one main queues dead-letter into via x-dead-letter-exchange.
	Exchange string
}

// validate fails fast on misconfiguration. errors.Join surfaces all defects
// at once (the dm.PublisherConfig.validate / orch.StatusPublisherConfig.
// validate precedent). NOT a domain error — this is a startup-time wiring
// defect, not an Orchestrator-visible failure.
func (c Config) validate() error {
	var errs []error
	if c.Exchange == "" {
		errs = append(errs, errors.New("dlq publisher: Config.Exchange must be non-empty"))
	}
	return errors.Join(errs...)
}

// DLQPublisher publishes LICDLQEnvelope envelopes to the four PII-safe LIC
// DLQ topics (LIC event-catalog §3.2, integration-contracts.md §10,
// observability.md §3.8, LIC-TASK-046). Immutable and stateless after
// NewDLQPublisher; PublishDLQ is goroutine-safe across distinct envelopes
// — the only shared state is the broker Publisher seam, which itself
// serializes publishes internally (broker/publish.go pubMu) so this type
// takes no mutex.
//
// Role satisfied (compile-time-asserted above):
//
//   - port.DLQPublisherPort —
//     PublishDLQ(ctx, port.DLQTopic, port.LICDLQEnvelope) error
type DLQPublisher struct {
	cfg       Config
	publisher Publisher
	metrics   Metrics
	clock     Clock
	log       Logger
}

// NewDLQPublisher validates the wiring and assembles the publisher. It
// fails fast (NewTypeName per feedback_constructors.md): an invalid Config
// or a nil Publisher is a LIC-TASK-047 wiring defect and MUST be a startup
// error, not a first-call nil-deref. errors.Join surfaces ALL defects at
// once. The three OPTIONAL seams (Metrics / Clock / Logger) never cause
// an error; Deps.withDefaults substitutes a noop for each nil one.
//
// Publisher has NO noop default and is checked AFTER withDefaults — a
// silent-swallow Publisher would erase every DLQ envelope, defeat the
// §11 LICDLQGrowth alert, and break the §9.3 post-mortem channel. See
// Deps.Publisher godoc.
func NewDLQPublisher(cfg Config, deps Deps) (*DLQPublisher, error) {
	var errs []error
	if err := cfg.validate(); err != nil {
		errs = append(errs, err)
	}
	d := deps.withDefaults()
	if d.Publisher == nil {
		errs = append(errs, errors.New("dlq publisher: Deps.Publisher must be non-nil (no noop default — silent swallow on lic.dlq.* is forbidden)"))
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return &DLQPublisher{
		cfg:       cfg,
		publisher: d.Publisher,
		metrics:   d.Metrics,
		clock:     d.Clock,
		log:       d.Logger,
	}, nil
}

// PublishDLQ validates, marshals and publishes a single LICDLQEnvelope on
// the supplied DLQTopic. Each call produces EXACTLY ONE wire message — no
// retry loop, no fan-out, no per-publish payload archival (those concerns
// are upstream of this publisher: §10.2 publish-failed object-storage
// retention is the caller's option in v1).
//
// Validation (pre-publish, errors short-circuit before broker.Publish):
//
//   - topic MUST be one of the four DLQTopic constants (DLQTopic.IsValid).
//     An invalid topic would silently land on an unbound routing key.
//   - envelope.OriginalTopic MUST be non-empty (the failure context — what
//     wire was the failed message on).
//   - envelope.OriginalMessageHash MUST be non-empty. The caller is
//     expected to compute this via HashPayload (FULL-payload HMAC-SHA-256
//     keyed by LIC_DLQ_HASH_KEY). An empty hash defeats the §6.5 PII
//     contract because forensics would have no way to dedupe / lookup.
//   - envelope.ErrorCode MUST be non-empty. NOT validated against the
//     ErrorCatalog or IsPublishableToOrchestrator — DLQ catches ALL
//     terminal errors, INCLUDING the non-publishable ones (INVALID_
//     MESSAGE_SCHEMA, INVALID_ORG_ID_MISMATCH, IDEMPOTENCY_STORE_
//     UNAVAILABLE). Any non-empty string is accepted at the publisher
//     boundary; the catalog is the user-facing-rendering contract,
//     orthogonal to DLQ forensics.
//   - envelope.ErrorMessage MUST be non-empty (the failure description —
//     dev-readable EN text, in contrast to ErrorCode which is the
//     machine identifier).
//   - envelope.RetryCount MUST be >= 0 (Go int default is 0, naturally
//     satisfied; defensive against int underflow).
//   - envelope.OriginalMessageSizeBytes MUST be >= 0 (same defensive
//     check; -1 sentinel would silently slip into log aggregators).
//
// DELIBERATELY NOT validated (architect Q3 — best-effort per spec):
//
//   - correlation_id, job_id, document_id, version_id, organization_id —
//     all best-effort per integration-contracts §10.1; when an
//     invalid-message envelope fails JSON parsing entirely none of these
//     may be recoverable.
//   - agent_id, stage, raw_llm_response_hash — defensive optional set
//     per event-catalog §3.1; the publisher trusts the caller-supplied
//     model.
//   - payload_storage_key — set only for publish-failed when v1 object-
//     storage retention is enabled (§10.2); empty is the v1 default.
//
// In-method rewrites (after validation passes):
//
//   - FailedAt stamp: ONLY when envelope.FailedAt is empty. If the
//     caller set FailedAt (e.g. preserving the time the failure was
//     detected upstream of the DLQ publish path), it is left alone —
//     deliberate asymmetry vs the dm/orch publishers' always-overwrite
//     Timestamp pattern. See seams.go.Clock godoc.
//
// Failure modes:
//
//   - Validation failure → (*PublishError){Reason, Cause: nil}; metric
//     IncPublish(topic, PublishOutcomeInvalid); broker.Publish is NOT
//     called; IncDLQPublished is NOT called.
//   - encoding/json marshal failure → (*PublishError){Reason:
//     reasonMarshalFailure, Cause: err}; metric IncPublish(topic,
//     PublishOutcomeFailure); broker.Publish is NOT called;
//     IncDLQPublished is NOT called. Should be unreachable for compliant
//     inputs.
//   - broker.Publish failure → raw broker error passed through
//     (*broker.BrokerError / sentinel) so the caller's errors.Is /
//     errors.As chain stays intact; metric IncPublish(topic,
//     classifyOutcome(err)); IncDLQPublished is NOT called.
//   - ctx.Done() inside broker.Publish → raw ctx.Err() passed through
//     (codebase-wide convention); metric IncPublish(topic,
//     PublishOutcomeFailure); IncDLQPublished is NOT called.
//   - Success → nil; metric IncPublish(topic, PublishOutcomeSuccess) AND
//     IncDLQPublished(topic, reason). BOTH counters bump.
//
// IncPublish is ALWAYS incremented exactly once per call — there is no
// silent exit path. IncDLQPublished is incremented at most once and only
// on broker-ack success.
//
// PublishDLQ takes envelope by VALUE so the in-method FailedAt rewrite
// (when caller-empty) acts on the local copy only; the caller-side
// variable is byte-for-byte unchanged.
func (p *DLQPublisher) PublishDLQ(ctx context.Context, topic port.DLQTopic, envelope port.LICDLQEnvelope) error {
	topicStr := string(topic)

	// Block A — topic must be one of the four declared values.
	if !topic.IsValid() {
		return p.failValidation(topicStr, reasonInvalidTopic)
	}

	// Block B — minimal required envelope fields (architect Q3).
	if envelope.OriginalTopic == "" {
		return p.failValidation(topicStr, reasonMissingOriginalTopic)
	}
	if envelope.OriginalMessageHash == "" {
		return p.failValidation(topicStr, reasonMissingOriginalHash)
	}
	if envelope.ErrorCode == "" {
		return p.failValidation(topicStr, reasonMissingErrorCode)
	}
	if envelope.ErrorMessage == "" {
		return p.failValidation(topicStr, reasonMissingErrorMessage)
	}
	if envelope.RetryCount < 0 {
		return p.failValidation(topicStr, reasonNegativeRetryCount)
	}
	if envelope.OriginalMessageSizeBytes < 0 {
		return p.failValidation(topicStr, reasonNegativeMessageSize)
	}

	// FailedAt stamp ONLY when empty (architect Q6 — semantic difference
	// vs dm/orch publishers). The caller-side variable is unmodified
	// because PublishDLQ takes the envelope by VALUE — the rewrite
	// acts on the local copy only.
	if envelope.FailedAt == "" {
		envelope.FailedAt = p.clock.Now().Format(time.RFC3339Nano)
	}

	// Marshal. LICDLQEnvelope's json tags drive the serialised shape;
	// correlation IDs / agent_id / stage / raw_llm_response_hash /
	// payload_storage_key carry omitempty so empty values are omitted
	// from the JSON object entirely. OriginalTopic / OriginalMessageHash
	// / OriginalMessageSizeBytes / ErrorCode / ErrorMessage / RetryCount
	// / FailedAt have NO omitempty — verified by Block A/B.
	bytes, err := marshalEnvelope(envelope)
	if err != nil {
		// Defensive: encoding/json on a typed-model struct should not
		// fail in practice. If it does, surface as a PublishError with
		// the original cause for triage and classify as a failure
		// (NOT invalid — invalid is for caller input defects; this is
		// a serialisation defect). broker.Publish is NOT called.
		p.metrics.IncPublish(topicStr, PublishOutcomeFailure)
		return &PublishError{Reason: reasonMarshalFailure, Cause: err}
	}

	// Publish. The broker client serializes publish + waits for the
	// publisher-confirm ack internally (broker/publish.go); we forward
	// the result verbatim and let classifyOutcome bucket the metric.
	// The routing key is the literal DLQ topic string (e.g.
	// "lic.dlq.invalid-message") — the DLX exchange topology binds
	// the four DLQ queues to these keys directly.
	pubErr := p.publisher.Publish(ctx, p.cfg.Exchange, topicStr, bytes)
	outcome := classifyOutcome(pubErr)
	p.metrics.IncPublish(topicStr, outcome)

	// On broker-ack success ONLY — increment the DLQ-specific counter
	// (observability.md §3.8). On any failure, the §11 LICDLQGrowth
	// alert reads this as "envelopes that reached the DLQ", so failed
	// publishes are deliberately excluded.
	if outcome == PublishOutcomeSuccess {
		p.metrics.IncDLQPublished(topicStr, topicToReason(topic))
	}

	return pubErr
}

// failValidation centralises the validation-failure exit: emit the invalid
// metric, return a *PublishError with the supplied reason and a nil Cause.
// Kept private — the only caller is PublishDLQ. Symmetric to the sibling
// dm / orch publishers' failValidation.
func (p *DLQPublisher) failValidation(topicStr, reason string) error {
	p.metrics.IncPublish(topicStr, PublishOutcomeInvalid)
	return &PublishError{Reason: reason}
}

// topicToReason maps a valid DLQTopic to its lic_dlq_published_total{reason}
// label value (architect Q2 — 1:1 mapping, 4 reasons → 16 series at 4×4).
// Called only with topic.IsValid() inputs (Block A short-circuits earlier
// on invalid topics) so the default-arm is defensive only.
func topicToReason(topic port.DLQTopic) string {
	switch topic {
	case port.DLQTopicInvalidMessage:
		return reasonInvalidMessage
	case port.DLQTopicConsumerFailed:
		return reasonConsumerFailed
	case port.DLQTopicPublishFailed:
		return reasonPublishFailed
	case port.DLQTopicAgentOutputInvalid:
		return reasonAgentOutputInvalid
	default:
		return "unknown"
	}
}
