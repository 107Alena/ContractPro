package pipeline

import (
	"context"
	"time"

	"contractpro/legal-intelligence-core/internal/domain/model"
)

// This file holds every Orchestrator SEAM — a local interface plus a
// zero-dependency noop default — for collaborators that are telemetry /
// runtime-environment, would force a forbidden import if depended on
// concretely, or are non-required for the happy path (build-spec §3, the
// stages.Deps / aggregator.Metrics / concurrency.Gauge precedent). Everything
// that crosses to an unimplemented cross-domain RabbitMQ wire is a
// domain.port instead (build-spec §3 table) and lives on the Orchestrator
// struct directly, never here.
//
// var _ Seam = noop{} assertions follow each pair (the universal B-4
// precedent). Noops are value types so the zero value is usable.

// JobLimiter is the acceptance-#2 "Run acquires the job-level semaphore" seam.
// It is structurally satisfied by *concurrency.Semaphore (LIC-TASK-047 passes
// that instance directly, NO adapter; that instance owns the
// lic_pipeline_concurrent_jobs gauge via concurrency.WithGauge — build-spec
// DEFECT-2, so this seam exposes no gauge method). A seam (not a direct
// internal/infra/concurrency import) keeps pipeline hermetic AND lets tests
// assert Acquire/Release ordering without a real channel.
//
// Acquire returns the raw ctx.Err() (context.Canceled /
// context.DeadlineExceeded, unwrapped) on cancel/deadline and consumes no
// slot; a nil return obliges the caller to Release exactly once. Release on a
// successful Acquire is mandatory and idempotent-once (the real semaphore
// PANICS on over/under-release by design — concurrency D5).
type JobLimiter interface {
	Acquire(ctx context.Context) error
	Release()
}

// noopJobLimiter always admits and never blocks — the literal-compliance
// default so the Orchestrator runs before LIC-TASK-047 injects the real
// gauge-wired semaphore (build-spec §2.2 / REFINE note).
type noopJobLimiter struct{}

func (noopJobLimiter) Acquire(context.Context) error { return nil }
func (noopJobLimiter) Release()                      {}

var _ JobLimiter = noopJobLimiter{}

// PipelineMetrics is the observability.md §3.2 telemetry seam for the
// three Orchestrator-owned Prometheus series. It deliberately has NO
// ConcurrentJobs method: lic_pipeline_concurrent_jobs is owned exclusively by
// the JobLimiter semaphore's gauge (build-spec DEFECT-2). A concrete
// metrics import here would break the hermeticity invariant the rest of
// internal/application/* upholds before wiring (the stages.StageMetrics /
// aggregator.Metrics precedent).
//
// Label conventions: mode ∈ {INITIAL,RE_CHECK}; outcome ∈
// {success,failed,timeout}; errorCode is "" for success and timeout, the
// model.ErrorCode string for failed (build-spec §5 codeLabelFor).
type PipelineMetrics interface {
	// PipelineStarted increments lic_pipeline_started_total{mode}. Called
	// exactly once at Run entry.
	PipelineStarted(mode string)
	// PipelineFinished observes lic_pipeline_total_duration_seconds
	// {mode,outcome}. Called exactly once at Run exit.
	PipelineFinished(mode, outcome string, seconds float64)
	// PipelineOutcome increments lic_pipeline_outcome_total
	// {mode,outcome,error_code}. Called exactly once at Run exit.
	PipelineOutcome(mode, outcome, errorCode string)
}

// noopPipelineMetrics is the zero-dependency default (the base.noopMetrics /
// aggregator.noopMetrics precedent) so the hot path never nil-checks.
type noopPipelineMetrics struct{}

func (noopPipelineMetrics) PipelineStarted(string)                   {}
func (noopPipelineMetrics) PipelineFinished(string, string, float64) {}
func (noopPipelineMetrics) PipelineOutcome(string, string, string)   {}

var _ PipelineMetrics = noopPipelineMetrics{}

// Tracer is the observability.md §4.2 OTel seam. The Orchestrator opens ONLY
// the root span lic.pipeline plus the dm/aggregate/publish/persist child
// spans; per-stage spans (lic.stage.*) are opened INSIDE stages.Executor via
// its own Tracer seam, so the Orchestrator MUST pass the span-carrying ctx
// into exec.RunStageN so they nest. A go.opentelemetry.io import here would
// break hermeticity (the stages.Tracer precedent).
type Tracer interface {
	// StartPipeline opens the root lic.pipeline span. The returned ctx
	// carries the span so all child spans (and the per-stage spans opened
	// inside stages.Executor) nest beneath it.
	StartPipeline(ctx context.Context, attrs PipelineSpanAttrs) (context.Context, PipelineSpan)
}

// PipelineSpan is the span handle. Finish is called exactly once (via the
// terminal finalizer) with the terminal error; a non-nil err is recorded as
// span status=Error carrying the real *model.DomainError.
type PipelineSpan interface {
	// StartChild opens a child span (lic.dm.request / lic.dm.await /
	// lic.aggregate / lic.publish / lic.persist.await). The returned ctx
	// carries the child span.
	StartChild(ctx context.Context, name string) (context.Context, PipelineSpan)
	// Finish closes the span. err != nil ⇒ status=Error.
	Finish(err error)
}

// PipelineSpanAttrs are the observability.md §4.2 root-span attributes.
type PipelineSpanAttrs struct {
	JobID      string
	VersionID  string
	Mode       string
	OriginType string
}

// noopTracer / noopSpan are the zero-dependency defaults. StartPipeline /
// StartChild return ctx unchanged so W3C propagation still works when tracing
// is off (the stages.noopTracer precedent).
type noopTracer struct{}

func (noopTracer) StartPipeline(ctx context.Context, _ PipelineSpanAttrs) (context.Context, PipelineSpan) {
	return ctx, noopSpan{}
}

type noopSpan struct{}

func (noopSpan) StartChild(ctx context.Context, _ string) (context.Context, PipelineSpan) {
	return ctx, noopSpan{}
}
func (noopSpan) Finish(error) {}

var (
	_ Tracer       = noopTracer{}
	_ PipelineSpan = noopSpan{}
)

// Clock is the deterministic-time seam (the stages.Deps.now precedent,
// promoted to a 2-method interface because the Orchestrator needs Now() for
// event timestamps and Since() for the duration metric).
type Clock interface {
	// Now is the event Timestamp source and the duration start/stop clock.
	Now() time.Time
	// Since returns the elapsed duration since t.
	Since(t time.Time) time.Duration
}

// systemClock is the production default (UTC, the wall clock).
type systemClock struct{}

func (systemClock) Now() time.Time                  { return time.Now().UTC() }
func (systemClock) Since(t time.Time) time.Duration { return time.Since(t) }

var _ Clock = systemClock{}

// Logger is the structured WARN/ERROR seam. The Orchestrator owns the
// RE_CHECK parent-degradation WARN and the non-publishable-terminal-code
// DLQ-log fallback (stages/CLAUDE.md FN-1: "034 is hermetic — no logger"). A
// concrete logger import here would break hermeticity.
type Logger interface {
	Warn(ctx context.Context, msg string, kv ...any)
	Error(ctx context.Context, msg string, kv ...any)
}

// noopLogger is the zero-dependency default.
type noopLogger struct{}

func (noopLogger) Warn(context.Context, string, ...any)  {}
func (noopLogger) Error(context.Context, string, ...any) {}

var _ Logger = noopLogger{}

// VersionMetaCache is the build-spec DEFECT-1 fallback-only RE_CHECK source.
// It is consulted IFF trigger.ParentVersionID == nil (the §8.3 race where the
// trigger lacks the pointer but the VersionCreated handler, LIC-TASK-040,
// already populated lic-version-meta:{version_id}). A miss is (nil,nil) and
// is NOT an error — it degrades to INITIAL flow (high-arch:1069-1070), never
// FAILED. A Redis import here would break hermeticity.
type VersionMetaCache interface {
	// GetParentVersionID returns the cached parent_version_id for versionID,
	// or (nil,nil) on miss. A non-nil error is treated by the Orchestrator
	// exactly like a miss (degrade to INITIAL; never fail).
	GetParentVersionID(ctx context.Context, versionID string) (*string, error)
}

// noopVersionMetaCache always misses (the DEFECT-1 non-required-fallback
// default).
type noopVersionMetaCache struct{}

func (noopVersionMetaCache) GetParentVersionID(context.Context, string) (*string, error) {
	return nil, nil
}

var _ VersionMetaCache = noopVersionMetaCache{}

// PauseController is the low-confidence pause seam. LIC-TASK-037 owns the real
// impl (pending-state Redis + classification-uncertain publish + paused
// sentinel). The Orchestrator is happy-path-only, so the default MUST
// terminally fail (build-spec D5) — a paused pipeline must never silently
// proceed.
type PauseController interface {
	// Pause is invoked ONLY when Agent 1 confidence < threshold. The real
	// impl returns a paused sentinel that LIC-TASK-040 maps to "ACK, no
	// COMPLETED"; the noop returns a terminal non-retryable error.
	Pause(ctx context.Context, st *model.PipelineState) error
}

// noopPauseController terminally fails. ErrCodeInternal's catalog default is
// retryable=true (error_codes.go:221-225) so the build-spec DEFECT-3 fix
// chains .WithRetryable(false) — the pause-stub MUST be non-retryable so a
// low-confidence run is not endlessly redelivered by LIC-TASK-040.
type noopPauseController struct{}

func (noopPauseController) Pause(_ context.Context, st *model.PipelineState) error {
	return model.NewDomainError(model.ErrCodeInternal, model.StageAwaitingUserConfirmation).
		WithRetryable(false).
		WithDevMessage("pause requested but PauseController not wired (LIC-TASK-037); happy-path-only orchestrator").
		WithAttribute("version_id", st.VersionID)
}

var _ PauseController = noopPauseController{}
