package pipeline

// Deps bundles the optional-with-noop telemetry / runtime collaborators (the
// build-spec §2.2 "never cause a constructor error" set). It mirrors
// stages.Deps: a nil field degrades to its zero-dependency noop so the common
// production wiring (LIC-TASK-047) passes only the real adapters and tests
// override exactly what they need (the base.Deps.withDefaults /
// stages.Deps.withDefaults precedent). Keeping these off the NewOrchestrator
// positional signature keeps it small and stable across LIC-TASK-037/047.
type Deps struct {
	// JobLimiter — acceptance-#2 job-level semaphore. nil ⇒ noopJobLimiter
	// (always-admit; LIC-TASK-047 injects *concurrency.Semaphore).
	JobLimiter JobLimiter
	// Metrics — the three lic_pipeline_* series. nil ⇒ noopPipelineMetrics.
	Metrics PipelineMetrics
	// Tracer — the root lic.pipeline span. nil ⇒ noopTracer.
	Tracer Tracer
	// Clock — deterministic time. nil ⇒ systemClock (UTC wall clock).
	Clock Clock
	// Logger — structured WARN/ERROR. nil ⇒ noopLogger.
	Logger Logger
	// VersionMetaCache — DEFECT-1 RE_CHECK fallback. nil ⇒ always-miss.
	VersionMetaCache VersionMetaCache
	// PauseController — low-confidence pause. nil ⇒ terminal-fail stub
	// (build-spec D5; LIC-TASK-037 injects the real impl).
	PauseController PauseController
}

// withDefaults substitutes a zero-dependency noop for every nil seam and
// returns the completed Deps so the Orchestrator never nil-checks a seam on
// the hot path (the stages.Deps.withDefaults pattern verbatim).
func (d Deps) withDefaults() Deps {
	if d.JobLimiter == nil {
		d.JobLimiter = noopJobLimiter{}
	}
	if d.Metrics == nil {
		d.Metrics = noopPipelineMetrics{}
	}
	if d.Tracer == nil {
		d.Tracer = noopTracer{}
	}
	if d.Clock == nil {
		d.Clock = systemClock{}
	}
	if d.Logger == nil {
		d.Logger = noopLogger{}
	}
	if d.VersionMetaCache == nil {
		d.VersionMetaCache = noopVersionMetaCache{}
	}
	if d.PauseController == nil {
		d.PauseController = noopPauseController{}
	}
	return d
}
