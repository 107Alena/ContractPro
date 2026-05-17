package aggregator

import (
	"sort"

	"contractpro/legal-intelligence-core/internal/domain/model"
)

// Warning user_message strings. The two VERBATIM ones are byte-pinned against
// their high-architecture.md SSOT lines (deviating from a frozen-message SSOT
// is the larger risk — the schemavalidator "BYTE-EXACT SSOT" precedent); the
// three faithful ones (no verbatim text exists in the docs) are package
// constants pinned by tests. All are well under the schema maxLength:500
// (ai-agents-pipeline.md §8 warnings schema).
const (
	// msgPromptInjection is VERBATIM high-architecture.md:837.
	msgPromptInjection = "В тексте договора обнаружены признаки попытки воздействия на инструкции анализа. Результаты могут быть искажены — рекомендуем проверить ключевые риски и параметры вручную."

	// msgReCheckParentMissing is VERBATIM high-architecture.md:1105.
	msgReCheckParentMissing = "Сравнение с предыдущей версией недоступно: данные анализа родительской версии не найдены"

	// msgInputTruncated — faithful RU (no verbatim SSOT); calm, non-blocking,
	// actionable — consistent with the verbatim messages' tone.
	msgInputTruncated = "Договор превысил максимальный размер для анализа и был частично усечён. Результаты охватывают не весь текст — рекомендуем проверить ключевые условия в полной версии документа."

	// msgClassificationParamsMismatch — faithful RU (no verbatim SSOT).
	msgClassificationParamsMismatch = "Обнаружено несоответствие между определённым типом договора и извлечёнными ключевыми параметрами. Рекомендуем проверить тип договора и состав условий вручную."

	// msgRecommendationOrphanRef — faithful RU (no verbatim SSOT). The
	// user_message field is schema-required; orphan_risk_ids is for ops
	// debugging, not shown to the end user (warnings.go model godoc).
	msgRecommendationOrphanRef = "Часть рекомендаций ссылается на риски, отсутствующие в итоговом перечне. Это внутреннее несоответствие данных — затронутые рекомендации могут быть неполными."
)

// promptInjectionSource pairs a flag-carrying agent output with its canonical
// AgentID. Only 5 of the 9 agent result types carry prompt_injection_detected
// in the model SSOT — Recommendations/Summary/DetailedReport/RiskDelta have
// no such field by design (they consume already-structured upstream output,
// not raw contract text; ADR-LIC-07's 5-layer defense only flags agents that
// process untrusted text). §6.11 step 6's "all 9 agent results" is resolved
// against the model SSOT to exactly these 5 (D3 — the schemavalidator
// "model layer is the harder constraint" SSOT-resolution class).
type promptInjectionSource struct {
	agent    model.AgentID
	detected bool
}

// buildWarnings assembles the §6.11 step 6-7 DETAILED_REPORT.warnings
// object-map. Returns nil when no warning fired (Warnings.IsEmpty()) so the
// FROZEN object-map serialises as absent/`{}` rather than a null-field shell.
func (a *Aggregator) buildWarnings(in Input, merged []model.Risk) *model.Warnings {
	w := &model.Warnings{}

	a.applyPromptInjection(in, w)
	applyReCheckParentMissing(in, w)
	applyInputTruncated(in, w)
	applyClassificationParamsMismatch(in, w)
	applyRecommendationOrphanRef(in, merged, w)

	if w.IsEmpty() {
		return nil
	}
	return w
}

// applyPromptInjection is §6.11 step 6 (C-lite, OQ-13). It reads the flags
// from the RAW inputs (BEFORE any stripping — D3 ordering), in canonical
// pipeline order, then sorts detected_by_agents lexicographically for
// deterministic output (§6.11 line 840 / ai-agents-pipeline.md:1343). The
// metric is incremented EXACTLY once per detecting agent.
func (a *Aggregator) applyPromptInjection(in Input, w *model.Warnings) {
	srcs := []promptInjectionSource{
		{model.AgentTypeClassifier, in.Classification != nil && in.Classification.PromptInjectionDetected},
		{model.AgentKeyParams, in.KeyParameters != nil && in.KeyParameters.PromptInjectionDetected},
		{model.AgentPartyConsistency, in.PartyConsistency != nil && in.PartyConsistency.PromptInjectionDetected},
		{model.AgentMandatoryConditions, in.MandatoryConditions != nil && in.MandatoryConditions.PromptInjectionDetected},
		// RAW Agent-5 analysis: the flag lives only on the RiskAnalysis
		// envelope (Risk has no per-element flag); read before stripping.
		{model.AgentRiskDetection, in.RiskAnalysis != nil && in.RiskAnalysis.PromptInjectionDetected},
	}

	detectedBy := make([]string, 0, len(srcs))
	for _, s := range srcs {
		if !s.detected {
			continue
		}
		id := s.agent.String()
		detectedBy = append(detectedBy, id)
		a.metrics.PromptInjectionDetected(id)
	}
	if len(detectedBy) == 0 {
		return
	}
	sort.Strings(detectedBy)

	w.PromptInjectionDetected = &model.PromptInjectionDetectedWarning{
		Detected:         true,
		DetectedByAgents: detectedBy,
		DetectionCount:   len(detectedBy),
		UserMessage:      msgPromptInjection,
	}
}

// applyReCheckParentMissing is §8.7: the warning fires iff the run is
// RE_CHECK AND the explicit ParentAnalysisMissing signal is set — NEVER
// derived from RiskDelta==nil (D4: a nil delta is ambiguous — could be a
// non-critical Agent-9 skip or provider error; the root cause "parent
// artifact unavailable" is knowable only by the Orchestrator at DM fetch).
func applyReCheckParentMissing(in Input, w *model.Warnings) {
	if in.Mode == model.PipelineModeReCheck && in.ParentAnalysisMissing {
		w.ReCheckParentAnalysisMissing = &model.ReCheckParentAnalysisMissingWarning{
			UserMessage: msgReCheckParentMissing,
		}
	}
}

// applyInputTruncated renders INPUT_TRUNCATED when the Orchestrator passed a
// non-nil Truncation signal (ASSUMPTION-LIC-12). The Aggregator neither
// estimates nor truncates — LIC-TASK-021 is the sole source of the counts.
func applyInputTruncated(in Input, w *model.Warnings) {
	if in.Truncation == nil {
		return
	}
	w.InputTruncated = &model.InputTruncatedWarning{
		TruncatedBytes: in.Truncation.TruncatedBytes,
		TotalBytes:     in.Truncation.TotalBytes,
		UserMessage:    msgInputTruncated,
	}
}

// applyClassificationParamsMismatch is the §6.11 step 7 cross-agent sanity
// check. v1 ships EXACTLY ONE rule (the doc's sole concrete example):
// contract_type == NDA && key_parameters.price != null. Broader rule sets +
// severity tiering are explicitly deferred to v1.1 (ai-agents-pipeline.md:59);
// the single-rule boundary is recorded in CLAUDE.md so a future agent does
// not "helpfully" widen it mid-flight (D9). nil Classification/KeyParameters
// ⇒ no warning, no panic (never treat nil as a mismatch).
func applyClassificationParamsMismatch(in Input, w *model.Warnings) {
	if in.Classification == nil || in.KeyParameters == nil {
		return
	}
	if in.Classification.ContractType == model.ContractTypeNDA && in.KeyParameters.Price != nil {
		w.ClassificationParamsMismatch = &model.ClassificationParamsMismatchWarning{
			UserMessage: msgClassificationParamsMismatch,
		}
	}
}

// applyRecommendationOrphanRef is the §6.11.3 invariant: every
// recommendation.risk_id must reference an element in the MERGED risks[]
// (R-NNN ∪ R-PNNN ∪ R-MNNN — Agent 6 received the merged list). Membership
// is EXISTENCE-only; Agent 6 owns risk_id FORMAT terminally (recommendation
// D4 — the Aggregator does not re-validate format). Orphans are de-duped,
// first-seen order, for deterministic output (D10).
func applyRecommendationOrphanRef(in Input, merged []model.Risk, w *model.Warnings) {
	if len(in.Recommendations) == 0 {
		return
	}
	ids := make(map[string]struct{}, len(merged))
	for _, r := range merged {
		ids[r.ID] = struct{}{}
	}

	var orphans []string
	seen := make(map[string]struct{})
	for _, rec := range in.Recommendations {
		if _, ok := ids[rec.RiskID]; ok {
			continue
		}
		if _, dup := seen[rec.RiskID]; dup {
			continue
		}
		seen[rec.RiskID] = struct{}{}
		orphans = append(orphans, rec.RiskID)
	}
	if len(orphans) == 0 {
		return
	}
	w.RecommendationOrphanRef = &model.RecommendationOrphanRefWarning{
		OrphanRiskIDs: orphans,
		UserMessage:   msgRecommendationOrphanRef,
	}
}
