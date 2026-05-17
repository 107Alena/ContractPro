package stages

// Stage is the closed 6-value pipeline-stage identifier used as BOTH the
// lic_pipeline_stage_duration_seconds{stage} Prometheus label value AND the
// OTel span-name suffix ("lic.stage."+Stage). Its String() values are the
// observability.md §4.2 span-tree suffixes VERBATIM.
//
// SSOT precedence (code-architect D5). observability.md:105 is the explicit
// normative clause: "Этот документ (observability.md) — авторитетный источник
// enum-значений для всех Prometheus-меток LIC … при расхождении приоритет за
// observability.md." The metrics/pipeline.go code comment ("Stage label
// values come from domain/model/status.go (STAGE_*)") is a non-normative
// hint that is UNWORKABLE here — model.Stage STAGE_AGENT_* values are
// per-agent and there is no group-level value for the parallel 2-agent
// Stages 1/3/5 — and loses to observability.md §4.2 on conflict. This is the
// same doc-conflict reconciliation class as the schemavalidator PriorTurns /
// aggregator SSOT-gap resolutions; recorded authoritatively in CLAUDE.md.
//
// DISTINCT from model.Stage. model.Stage (status.go) is the SSOT for
// lic.events.status-changed and *model.DomainError.Stage (per-agent
// STAGE_AGENT_*); this Stage is the SSOT for the stage metric/span only. The
// two are never interchanged — DomainError construction uses canonicalStage
// (model.AgentID→model.Stage), NEVER this type (code-architect D4).
type Stage string

const (
	// Stage1 — Type Classifier ‖ Key Parameters Extractor (errgroup).
	Stage1 Stage = "s1.parallel"
	// Stage2 — Party Data Consistency (sequential, non-critical).
	Stage2 Stage = "s2.party_consistency"
	// Stage3 — Mandatory Conditions ‖ Risk Detection (errgroup).
	Stage3 Stage = "s3.parallel"
	// Stage4 — Recommendation (sequential, after Aggregator merge by 036).
	Stage4 Stage = "s4.recommendation"
	// Stage5 — Business Summary ‖ Detailed Report (errgroup).
	Stage5 Stage = "s5.parallel"
	// Stage6 — Risk Delta (sequential, RE_CHECK only).
	Stage6 Stage = "s6.risk_delta"
)

// String returns the wire representation (metric label value / span suffix).
func (s Stage) String() string { return string(s) }

// IsValid reports whether s is one of the six declared pipeline stages.
func (s Stage) IsValid() bool {
	switch s {
	case Stage1, Stage2, Stage3, Stage4, Stage5, Stage6:
		return true
	default:
		return false
	}
}

// AllStages returns a fresh slice with every stage in pipeline order.
// Callers may mutate the returned slice (house style, cf. model.AllStages).
func AllStages() []Stage {
	return []Stage{Stage1, Stage2, Stage3, Stage4, Stage5, Stage6}
}
