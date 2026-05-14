package port

import (
	"context"
	"errors"

	"contractpro/legal-intelligence-core/internal/domain/model"
)

// DM-side ports — every cross-domain interaction with Document Management is
// async via RabbitMQ (integration-contracts.md §1, §2; sync REST to DM is
// forbidden, see §9). Two publishers (artifact request, analysis-ready) and
// two awaiters (artifacts response, persist confirmation) cover the four
// touchpoints between LIC and DM.
//
// Publishers issue events with publisher confirms; awaiters wait on the
// corresponding response within a TTL and return a typed error on timeout
// (high-architecture.md §6.12).

// ----------------------------------------------------------------------------
// Outbound — publishers
// ----------------------------------------------------------------------------

// ArtifactRequesterPort publishes lic.requests.artifacts to fetch DM-side
// artifacts for one version (high-architecture.md §6.5 step 1, LIC-TASK-042).
//
// Each call produces one wire message; the implementation generates message_id,
// stamps timestamp, performs publisher-confirm wait-for-broker-ack and
// returns nil on success. correlationID is the unique routing key for the
// awaiter — orchestrator-side it usually carries a per-stage suffix
// (e.g. "<base>:current", "<base>:parent") so parallel base + parent
// requests for RE_CHECK don't collide.
type ArtifactRequesterPort interface {
	RequestArtifacts(
		ctx context.Context,
		correlationID, jobID, documentID, versionID, organizationID string,
		artifactTypes []model.ArtifactType,
	) error
}

// AnalysisArtifactsPublisherPort publishes lic.artifacts.analysis-ready —
// the consolidated payload after the pipeline finishes (high-architecture.md
// §6.5 step 8, LIC-TASK-043).
//
// Name note: the LIC-TASK-013 acceptance criteria calls this
// "ArtifactPersistencePort". The verb on the wire is `publish` (fire-and-
// forget with publisher confirms); persistence is a side effect at DM and
// is confirmed via PersistConfirmationAwaiterPort. The interface name was
// adjusted to match the semantic — see also the godoc on
// PersistConfirmationAwaiterPort which mirrors the relationship.
type AnalysisArtifactsPublisherPort interface {
	Publish(ctx context.Context, payload LegalAnalysisArtifactsReady) error
}

// ----------------------------------------------------------------------------
// Inbound (async response) — awaiters with timeout
// ----------------------------------------------------------------------------

// ErrAwaitTimeout is returned when the awaiter's TTL elapsed before a
// matching response arrived. The orchestrator translates it into a
// DomainError with the appropriate error_code (DM_ARTIFACTS_TIMEOUT or
// DM_PERSIST_TIMEOUT — error-handling.md §3.2).
var ErrAwaitTimeout = errors.New("await timeout")

// ErrDuplicateRegistration is returned by ArtifactsAwaiterPort.Register or
// PersistConfirmationAwaiterPort.Register when the supplied key
// (correlation_id / job_id) already has a slot. Duplicates would silently
// shadow each other; the awaiter rejects them explicitly so the caller can
// log or DLQ the conflict.
var ErrDuplicateRegistration = errors.New("awaiter: duplicate registration")

// ArtifactsAwaiterPort holds an in-process correlation_id → channel registry
// matching ArtifactsProvided responses to the goroutine that issued the
// matching GetArtifactsRequest (high-architecture.md §6.12, LIC-TASK-041).
//
// Lifecycle: Register MUST be called BEFORE the request is published —
// otherwise the response may arrive first and find no registry slot. Each
// call must be paired with Cancel on completion (success or timeout) so
// the registry stays bounded.
type ArtifactsAwaiterPort interface {
	// Register reserves a slot for `correlationID`. Returns a channel
	// that receives exactly one ArtifactsProvided (or is closed on
	// Cancel). Subsequent Register calls with the same correlationID
	// MUST return an error — duplicates would silently shadow each other.
	Register(correlationID string) (<-chan ArtifactsProvided, error)

	// Await blocks until either the response arrives or ctx is
	// cancelled or the awaiter's TTL elapses. Returns ErrAwaitTimeout
	// on timeout. Implementations apply LIC_DM_REQUEST_TIMEOUT (default
	// 30s, configuration.md). Cancellation cleans up the registry slot.
	Await(ctx context.Context, correlationID string) (ArtifactsProvided, error)

	// Cancel releases the registry slot without waiting. Safe to call
	// after Await has already returned; idempotent.
	Cancel(correlationID string)
}

// PersistConfirmationAwaiterPort awaits LegalAnalysisArtifactsPersisted or
// ...PersistFailed keyed by job_id (high-architecture.md §6.5 step 9,
// §6.12, LIC-TASK-041).
//
// Split from ArtifactsAwaiterPort per ISP: the keys differ (job_id vs
// correlation_id), TTLs differ (30s persist vs 5s artifacts in spec),
// and only the persist awaiter has the failure mode where the response
// itself carries IsRetryable=false (DM_PERSIST_FAILED — see error-handling.md).
type PersistConfirmationAwaiterPort interface {
	// Register reserves a slot for `jobID`. The returned channel
	// receives a PersistConfirmation envelope; the awaiter never tries
	// to discriminate Persisted vs PersistFailed for the caller — both
	// arrive as a single typed struct.
	Register(jobID string) (<-chan PersistConfirmation, error)

	// Await blocks until either Persisted or PersistFailed arrives or
	// ctx is cancelled or the awaiter's TTL elapses
	// (LIC_DM_PERSIST_CONFIRM_TIMEOUT, default 30s).
	Await(ctx context.Context, jobID string) (PersistConfirmation, error)

	// Cancel releases the registry slot without waiting.
	Cancel(jobID string)
}

// PersistConfirmation is the discriminated-union envelope produced by the
// consumer when either LegalAnalysisArtifactsPersisted or
// ...PersistFailed arrives for a registered job_id. Exactly one of
// `Success` / `Failure` is non-nil.
//
// Construct via NewPersistConfirmationSuccess / NewPersistConfirmationFailure
// — the constructors guarantee the invariant so call sites cannot accidentally
// produce a both-set value. IsSuccess / IsFailure return false on a both-set
// value (caller misuse) and on a zero value; tests assert this.
type PersistConfirmation struct {
	Success *LegalAnalysisArtifactsPersisted
	Failure *LegalAnalysisArtifactsPersistFailed
}

// NewPersistConfirmationSuccess wraps a Persisted envelope. Panics on nil —
// adapters must never enqueue a half-constructed confirmation.
func NewPersistConfirmationSuccess(p *LegalAnalysisArtifactsPersisted) PersistConfirmation {
	if p == nil {
		panic("port: NewPersistConfirmationSuccess called with nil envelope")
	}
	return PersistConfirmation{Success: p}
}

// NewPersistConfirmationFailure wraps a PersistFailed envelope. Panics on nil
// — see NewPersistConfirmationSuccess.
func NewPersistConfirmationFailure(p *LegalAnalysisArtifactsPersistFailed) PersistConfirmation {
	if p == nil {
		panic("port: NewPersistConfirmationFailure called with nil envelope")
	}
	return PersistConfirmation{Failure: p}
}

// IsSuccess reports whether the confirmation carries a successful persist.
// Returns false on both-set ambiguity to avoid a silent assumption — see
// type-level godoc.
func (c PersistConfirmation) IsSuccess() bool {
	return c.Success != nil && c.Failure == nil
}

// IsFailure reports whether the confirmation carries a persist failure.
// Returns false on both-set ambiguity — see IsSuccess.
func (c PersistConfirmation) IsFailure() bool {
	return c.Failure != nil && c.Success == nil
}

// IsValid reports whether exactly one of Success / Failure is set — an
// XOR check that callers may use to reject malformed envelopes before
// branching on IsSuccess / IsFailure. Constructors guarantee validity
// for their products; this helper protects against literal-form misuse.
func (c PersistConfirmation) IsValid() bool {
	return (c.Success != nil) != (c.Failure != nil)
}
