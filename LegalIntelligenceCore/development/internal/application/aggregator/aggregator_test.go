package aggregator

import (
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"testing"
	"unicode/utf8"

	"contractpro/legal-intelligence-core/internal/domain/model"
)

// defaultConfig mirrors config.ScoringConfig defaults (configuration.md §3 /
// scoring.go loadScoringConfig): 25/10/3 + 15/5, thresholds 0.75 / 0.45.
func defaultConfig() Config {
	return Config{
		WeightHigh:               25,
		WeightMedium:             10,
		WeightLow:                3,
		WeightMissingMandatory:   15,
		WeightAmbiguousMandatory: 5,
		LabelLowThreshold:        0.75,
		LabelMediumThreshold:     0.45,
	}
}

func mustAggregator(t *testing.T, m Metrics) *Aggregator {
	t.Helper()
	a, err := NewAggregator(defaultConfig(), m)
	if err != nil {
		t.Fatalf("NewAggregator: %v", err)
	}
	return a
}

func strptr(s string) *string { return &s }

// spyMetrics records every PromptInjectionDetected(agent) call.
type spyMetrics struct {
	mu    sync.Mutex
	calls []string
}

func (s *spyMetrics) PromptInjectionDetected(agent string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, agent)
}

func (s *spyMetrics) snapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]string(nil), s.calls...)
	return out
}

// ---------------------------------------------------------------------------
// D2 / D14 — Aggregate error & empty-merge determinism
// ---------------------------------------------------------------------------

func TestAggregate_NilRiskAnalysis_Errors(t *testing.T) {
	a := mustAggregator(t, nil)
	_, err := a.Aggregate(Input{Mode: model.PipelineModeInitial})
	if !errors.Is(err, ErrNilRiskAnalysis) {
		t.Fatalf("want ErrNilRiskAnalysis, got %v", err)
	}
}

func TestAggregate_EmptyMerge_DeterministicEmptySliceAndFloors(t *testing.T) {
	a := mustAggregator(t, nil)
	out, err := a.Aggregate(Input{
		Mode:         model.PipelineModeInitial,
		RiskAnalysis: &model.RiskAnalysis{Risks: nil},
	})
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	if out.MergedRiskAnalysis.Risks == nil {
		t.Fatal("merged Risks must be a non-nil empty slice (deterministic JSON [])")
	}
	if len(out.MergedRiskAnalysis.Risks) != 0 {
		t.Fatalf("want 0 merged risks, got %d", len(out.MergedRiskAnalysis.Risks))
	}
	wantProfile := model.RiskProfile{OverallLevel: model.RiskLevelLow}
	if *out.RiskProfile != wantProfile {
		t.Fatalf("profile = %+v, want %+v", *out.RiskProfile, wantProfile)
	}
	if out.AggregateScore.Score != 1.0 || out.AggregateScore.Label != model.AggregateScoreLabelLow {
		t.Fatalf("score = %+v, want {1 low}", *out.AggregateScore)
	}
	if out.Warnings != nil {
		t.Fatalf("want nil warnings, got %+v", out.Warnings)
	}
}

// ---------------------------------------------------------------------------
// D1 ★ — party level is DERIVED from finding.Type; Severity is ignored
// ---------------------------------------------------------------------------

func TestBuildMergedRisks_PartyLevel_DefenseInDepth(t *testing.T) {
	a := mustAggregator(t, nil)
	in := Input{
		Mode:         model.PipelineModeInitial,
		RiskAnalysis: &model.RiskAnalysis{},
		PartyConsistency: &model.PartyConsistencyFindings{Findings: []model.PartyFinding{
			// Agent (possibly injected) under-reports a critical finding.
			{Type: model.PartyFindingAuthorityMissing, Severity: model.RiskLevelLow, Description: "no PoA", ClauseRef: "п.1"},
			// Agent over-reports a non-critical finding.
			{Type: model.PartyFindingNameMismatch, Severity: model.RiskLevelHigh, Description: "name", ClauseRef: "п.2"},
		}},
	}
	out, err := a.Aggregate(in)
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	risks := out.MergedRiskAnalysis.Risks
	if len(risks) != 2 {
		t.Fatalf("want 2 risks, got %d", len(risks))
	}
	if risks[0].ID != "R-P001" || risks[0].Level != model.RiskLevelHigh {
		t.Errorf("R-P001 = {%s %s}, want {R-P001 high} (PARTY_AUTHORITY_MISSING fixed-high, Severity:low ignored)", risks[0].ID, risks[0].Level)
	}
	if risks[1].ID != "R-P002" || risks[1].Level != model.RiskLevelMedium {
		t.Errorf("R-P002 = {%s %s}, want {R-P002 medium} (PARTY_NAME_MISMATCH fixed-medium, Severity:high ignored)", risks[1].ID, risks[1].Level)
	}
	if risks[0].Category != model.RiskCategory(model.PartyFindingAuthorityMissing) {
		t.Errorf("category = %q, want verbatim finding.type", risks[0].Category)
	}
}

// ---------------------------------------------------------------------------
// D11 ★ — Шаг 2: 3 risks + 2 party + 1 missing mandatory → 6, correct ids
// ---------------------------------------------------------------------------

func TestAggregate_MergeOrderAndIDs_Step2(t *testing.T) {
	a := mustAggregator(t, nil)
	in := Input{
		Mode: model.PipelineModeInitial,
		RiskAnalysis: &model.RiskAnalysis{Risks: []model.Risk{
			{ID: "R-001", Level: model.RiskLevelHigh, Category: model.RiskCategoryHiddenFees},
			{ID: "R-002", Level: model.RiskLevelMedium, Category: model.RiskCategoryOther},
			{ID: "R-003", Level: model.RiskLevelLow, Category: model.RiskCategoryOther},
		}},
		PartyConsistency: &model.PartyConsistencyFindings{Findings: []model.PartyFinding{
			{Type: model.PartyFindingNameMismatch, ClauseRef: "п.1"},
			{Type: model.PartyFindingAuthorityMissing, ClauseRef: "п.2"},
		}},
		MandatoryConditions: &model.MandatoryConditionsReport{Conditions: []model.MandatoryCondition{
			{Code: "MC_SUBJECT", Label: "Предмет", Status: model.MandatoryConditionFoundOK},
			{Code: "MC_PRICE", Label: "Цена", Status: model.MandatoryConditionMissing},
		}},
	}
	out, err := a.Aggregate(in)
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	got := make([]string, 0, len(out.MergedRiskAnalysis.Risks))
	for _, r := range out.MergedRiskAnalysis.Risks {
		got = append(got, r.ID)
	}
	want := []string{"R-001", "R-002", "R-003", "R-P001", "R-P002", "R-M001"}
	if len(got) != len(want) {
		t.Fatalf("ids = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ids = %v, want %v", got, want)
		}
	}
	// FOUND_OK is NOT mapped; the only R-M is the MISSING one, high, with
	// the originating MC_* code preserved.
	rm := out.MergedRiskAnalysis.Risks[5]
	if rm.Level != model.RiskLevelHigh || rm.Category != model.RiskCategoryMandatoryConditionMissing {
		t.Errorf("R-M001 = {%s %s}, want {high MANDATORY_CONDITION_MISSING}", rm.Level, rm.Category)
	}
	if rm.MandatoryConditionCode == nil || *rm.MandatoryConditionCode != "MC_PRICE" {
		t.Errorf("R-M001 mandatory_condition_code = %v, want MC_PRICE", rm.MandatoryConditionCode)
	}
}

// ---------------------------------------------------------------------------
// D11 ★ — Шаг 3 score 0.65/medium + layered (no de-duplication) formula
// ---------------------------------------------------------------------------

func TestAggregate_Score_Step3(t *testing.T) {
	a := mustAggregator(t, nil)
	out, err := a.Aggregate(Input{
		Mode: model.PipelineModeInitial,
		RiskAnalysis: &model.RiskAnalysis{Risks: []model.Risk{
			{ID: "R-001", Level: model.RiskLevelHigh},
			{ID: "R-002", Level: model.RiskLevelMedium},
		}},
	})
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	// 100 - 25*1 - 10*1 = 65 → 0.65; 0.65 < 0.75, >= 0.45 → medium.
	if out.AggregateScore.Score != 0.65 || out.AggregateScore.Label != model.AggregateScoreLabelMedium {
		t.Fatalf("score = %+v, want {0.65 medium}", *out.AggregateScore)
	}
	want := model.RiskProfile{OverallLevel: model.RiskLevelHigh, HighCount: 1, MediumCount: 1}
	if *out.RiskProfile != want {
		t.Fatalf("profile = %+v, want %+v", *out.RiskProfile, want)
	}
}

func TestAggregate_Score_MandatoryPenaltyLayered(t *testing.T) {
	a := mustAggregator(t, nil)
	// One MISSING mandatory condition: it becomes R-M001 (high) AND adds the
	// separate missing_mandatory penalty. 100 - 25(high) - 15(missing) = 60.
	// If the formula wrongly de-duplicated: 100-25=75 (low) or 100-15=85
	// (low). 0.60 → medium uniquely proves both penalties layer (D11).
	out, err := a.Aggregate(Input{
		Mode:         model.PipelineModeInitial,
		RiskAnalysis: &model.RiskAnalysis{},
		MandatoryConditions: &model.MandatoryConditionsReport{Conditions: []model.MandatoryCondition{
			{Code: "MC_PRICE", Label: "Цена", Status: model.MandatoryConditionMissing},
		}},
	})
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	if out.AggregateScore.Score != 0.60 || out.AggregateScore.Label != model.AggregateScoreLabelMedium {
		t.Fatalf("score = %+v, want {0.6 medium} (layered: high 25 + missing 15)", *out.AggregateScore)
	}
}

func TestAggregate_Score_ClampFloor(t *testing.T) {
	a := mustAggregator(t, nil)
	risks := make([]model.Risk, 0, 10)
	for i := 0; i < 10; i++ {
		risks = append(risks, model.Risk{ID: "R-00" + string(rune('0'+i)), Level: model.RiskLevelHigh})
	}
	out, err := a.Aggregate(Input{Mode: model.PipelineModeInitial, RiskAnalysis: &model.RiskAnalysis{Risks: risks}})
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	// 100 - 25*10 = -150 → clamped to 0 → 0.0 → high.
	if out.AggregateScore.Score != 0.0 || out.AggregateScore.Label != model.AggregateScoreLabelHigh {
		t.Fatalf("score = %+v, want {0 high}", *out.AggregateScore)
	}
}

// ---------------------------------------------------------------------------
// D3 ★ — prompt-injection attribution + metric + ordering vs strip
// ---------------------------------------------------------------------------

func TestApplyPromptInjection_SortedAttribution_AndMetric(t *testing.T) {
	spy := &spyMetrics{}
	a := mustAggregator(t, spy)
	in := Input{
		Mode:           model.PipelineModeInitial,
		RiskAnalysis:   &model.RiskAnalysis{PromptInjectionDetected: true},
		Classification: &model.ClassificationResult{PromptInjectionDetected: true},
		// 4 field-less agents (6/7/8/9) cannot contribute by design.
	}
	out, err := a.Aggregate(in)
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	w := out.Warnings.PromptInjectionDetected
	if w == nil {
		t.Fatal("want PROMPT_INJECTION_DETECTED warning")
	}
	wantBy := []string{model.AgentRiskDetection.String(), model.AgentTypeClassifier.String()}
	sort.Strings(wantBy)
	if w.DetectionCount != 2 || !equalStrings(w.DetectedByAgents, wantBy) {
		t.Fatalf("warning = {%d %v}, want {2 %v} sorted", w.DetectionCount, w.DetectedByAgents, wantBy)
	}
	if !w.Detected || w.UserMessage != msgPromptInjection {
		t.Fatalf("detected=%v msg mismatch", w.Detected)
	}
	gotCalls := spy.snapshot()
	sort.Strings(gotCalls)
	if !equalStrings(gotCalls, wantBy) {
		t.Fatalf("metric calls = %v, want exactly %v (once per detecting agent)", gotCalls, wantBy)
	}
}

func TestApplyPromptInjection_NilPartyConsistency_NoneDetected(t *testing.T) {
	spy := &spyMetrics{}
	a := mustAggregator(t, spy)
	out, err := a.Aggregate(Input{
		Mode:             model.PipelineModeInitial,
		RiskAnalysis:     &model.RiskAnalysis{},
		PartyConsistency: nil, // Agent 3 non-critical: nil must be safe.
	})
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	if out.Warnings != nil {
		t.Fatalf("want no warnings, got %+v", out.Warnings)
	}
	if len(spy.snapshot()) != 0 {
		t.Fatalf("want 0 metric calls, got %v", spy.snapshot())
	}
}

func TestAggregate_InjectionReadFromRaw_BeforeStrip(t *testing.T) {
	a := mustAggregator(t, nil)
	out, err := a.Aggregate(Input{
		Mode:         model.PipelineModeInitial,
		RiskAnalysis: &model.RiskAnalysis{PromptInjectionDetected: true},
	})
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	if out.Warnings == nil || out.Warnings.PromptInjectionDetected == nil {
		t.Fatal("raw RiskAnalysis flag must still drive the warning")
	}
	if out.MergedRiskAnalysis.PromptInjectionDetected {
		t.Fatal("merged RiskAnalysis must NOT carry the internal flag (D6) — proves D3-before-D6 ordering")
	}
}

// ---------------------------------------------------------------------------
// D4 ★ — RE_CHECK_PARENT_ANALYSIS_MISSING is signal-driven (non-tautology)
// ---------------------------------------------------------------------------

func TestApplyReCheckParentMissing_NonTautology(t *testing.T) {
	a := mustAggregator(t, nil)
	base := func() Input {
		return Input{Mode: model.PipelineModeReCheck, RiskAnalysis: &model.RiskAnalysis{}}
	}

	// (a) RE_CHECK + missing, RiskDelta nil → warning.
	in := base()
	in.ParentAnalysisMissing = true
	out, _ := a.Aggregate(in)
	if out.Warnings == nil || out.Warnings.ReCheckParentAnalysisMissing == nil {
		t.Fatal("RE_CHECK + ParentAnalysisMissing (RiskDelta nil) must warn")
	}
	if out.Warnings.ReCheckParentAnalysisMissing.UserMessage != msgReCheckParentMissing {
		t.Fatal("verbatim message mismatch")
	}

	// (b) Same but RiskDelta NON-nil → still warns (independence of delta).
	in = base()
	in.ParentAnalysisMissing = true
	in.RiskDelta = &model.RiskDelta{BaseVersionID: "b", TargetVersionID: "t"}
	out, _ = a.Aggregate(in)
	if out.Warnings == nil || out.Warnings.ReCheckParentAnalysisMissing == nil {
		t.Fatal("warning must NOT depend on RiskDelta being nil (D4 non-tautology)")
	}

	// (c) RE_CHECK, NOT missing, RiskDelta nil → no warning (proves it is
	// NOT derived from RiskDelta==nil).
	in = base()
	in.ParentAnalysisMissing = false
	out, _ = a.Aggregate(in)
	if out.Warnings != nil {
		t.Fatalf("no signal ⇒ no warning even with RiskDelta nil, got %+v", out.Warnings)
	}

	// (d) INITIAL mode → never.
	in = Input{Mode: model.PipelineModeInitial, RiskAnalysis: &model.RiskAnalysis{}, ParentAnalysisMissing: true}
	out, _ = a.Aggregate(in)
	if out.Warnings != nil {
		t.Fatalf("INITIAL must never emit RE_CHECK warning, got %+v", out.Warnings)
	}
}

// ---------------------------------------------------------------------------
// D9 — CLASSIFICATION_PARAMS_MISMATCH single v1 rule + INPUT_TRUNCATED
// ---------------------------------------------------------------------------

func TestApplyClassificationParamsMismatch(t *testing.T) {
	a := mustAggregator(t, nil)
	mk := func(ct model.ContractType, price *string) Input {
		return Input{
			Mode:           model.PipelineModeInitial,
			RiskAnalysis:   &model.RiskAnalysis{},
			Classification: &model.ClassificationResult{ContractType: ct},
			KeyParameters:  &model.KeyParameters{Price: price},
		}
	}
	// NDA + price present → mismatch.
	out, _ := a.Aggregate(mk(model.ContractTypeNDA, strptr("100000 руб.")))
	if out.Warnings == nil || out.Warnings.ClassificationParamsMismatch == nil {
		t.Fatal("NDA + price!=nil must warn")
	}
	if out.Warnings.ClassificationParamsMismatch.UserMessage != msgClassificationParamsMismatch {
		t.Fatal("message mismatch")
	}
	// NDA + price nil → none.
	out, _ = a.Aggregate(mk(model.ContractTypeNDA, nil))
	if out.Warnings != nil {
		t.Fatalf("NDA + price==nil must NOT warn, got %+v", out.Warnings)
	}
	// SUPPLY + price present → none (rule is NDA-specific).
	out, _ = a.Aggregate(mk(model.ContractTypeSupply, strptr("1 руб.")))
	if out.Warnings != nil {
		t.Fatalf("non-NDA must NOT warn, got %+v", out.Warnings)
	}
	// nil Classification/KeyParameters → no panic, no warning.
	out, err := a.Aggregate(Input{Mode: model.PipelineModeInitial, RiskAnalysis: &model.RiskAnalysis{}})
	if err != nil || out.Warnings != nil {
		t.Fatalf("nil cls/kp must be safe, got warnings=%+v err=%v", out.Warnings, err)
	}
}

func TestApplyInputTruncated(t *testing.T) {
	a := mustAggregator(t, nil)
	out, _ := a.Aggregate(Input{
		Mode:         model.PipelineModeInitial,
		RiskAnalysis: &model.RiskAnalysis{},
		Truncation:   &TruncationInfo{TruncatedBytes: 1234, TotalBytes: 9000},
	})
	w := out.Warnings.InputTruncated
	if w == nil || w.TruncatedBytes != 1234 || w.TotalBytes != 9000 || w.UserMessage != msgInputTruncated {
		t.Fatalf("INPUT_TRUNCATED = %+v, want {1234 9000 msg}", w)
	}
	// nil Truncation → none.
	out, _ = a.Aggregate(Input{Mode: model.PipelineModeInitial, RiskAnalysis: &model.RiskAnalysis{}})
	if out.Warnings != nil {
		t.Fatalf("nil Truncation must NOT warn, got %+v", out.Warnings)
	}
}

// ---------------------------------------------------------------------------
// D10 ★ — RECOMMENDATION_ORPHAN_REF existence vs the MERGED set
// ---------------------------------------------------------------------------

func TestApplyRecommendationOrphanRef(t *testing.T) {
	a := mustAggregator(t, nil)
	in := Input{
		Mode: model.PipelineModeInitial,
		RiskAnalysis: &model.RiskAnalysis{Risks: []model.Risk{
			{ID: "R-001", Level: model.RiskLevelHigh},
		}},
		PartyConsistency: &model.PartyConsistencyFindings{Findings: []model.PartyFinding{
			{Type: model.PartyFindingNameMismatch, ClauseRef: "п.1"},
		}},
		MandatoryConditions: &model.MandatoryConditionsReport{Conditions: []model.MandatoryCondition{
			{Code: "MC_X", Label: "X", Status: model.MandatoryConditionMissing},
		}},
		Recommendations: model.Recommendations{
			{RiskID: "R-001"},  // raw — exists.
			{RiskID: "R-P001"}, // synthesized party — exists in MERGED set.
			{RiskID: "R-M001"}, // synthesized mandatory — exists in MERGED set.
			{RiskID: "R-999"},  // orphan.
			{RiskID: "R-999"},  // duplicate orphan — must appear once.
		},
	}
	out, err := a.Aggregate(in)
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	w := out.Warnings.RecommendationOrphanRef
	if w == nil {
		t.Fatal("want RECOMMENDATION_ORPHAN_REF (R-999 absent from merged set)")
	}
	if !equalStrings(w.OrphanRiskIDs, []string{"R-999"}) {
		t.Fatalf("orphans = %v, want exactly [R-999] (synthesized R-P001/R-M001 are NOT orphans; dup once)", w.OrphanRiskIDs)
	}
	if w.UserMessage != msgRecommendationOrphanRef {
		t.Fatal("message mismatch")
	}

	// All references valid → no warning.
	in.Recommendations = model.Recommendations{{RiskID: "R-001"}, {RiskID: "R-P001"}}
	out, _ = a.Aggregate(in)
	if out.Warnings != nil {
		t.Fatalf("all refs valid ⇒ no warning, got %+v", out.Warnings)
	}
}

// ---------------------------------------------------------------------------
// D6 — stripping
// ---------------------------------------------------------------------------

func TestAggregate_Stripping(t *testing.T) {
	a := mustAggregator(t, nil)
	in := Input{
		Mode: model.PipelineModeInitial,
		RiskAnalysis: &model.RiskAnalysis{Risks: []model.Risk{
			{ID: "R-001", Level: model.RiskLevelLow, Rationale: strptr("internal LLM reasoning")},
		}},
		KeyParameters: &model.KeyParameters{
			Subject:                 "поставка",
			InternalExtras:          &model.KeyParametersInternalExtras{Termination: strptr("§5")},
			PromptInjectionDetected: true,
		},
	}
	out, err := a.Aggregate(in)
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	if out.MergedRiskAnalysis.Risks[0].Rationale != nil {
		t.Error("merged risk rationale must be stripped to nil")
	}
	if out.StrippedKeyParameters.InternalExtras != nil || out.StrippedKeyParameters.PromptInjectionDetected {
		t.Error("key_parameters internal_extras/prompt_injection_detected must be stripped")
	}
	if out.StrippedKeyParameters.Subject != "поставка" {
		t.Error("non-internal key_parameters fields must survive")
	}
	// nil KeyParameters ⇒ nil StrippedKeyParameters.
	out, _ = a.Aggregate(Input{Mode: model.PipelineModeInitial, RiskAnalysis: &model.RiskAnalysis{}})
	if out.StrippedKeyParameters != nil {
		t.Error("nil KeyParameters must yield nil StrippedKeyParameters")
	}
}

// ---------------------------------------------------------------------------
// D5 ★ — immutability of Input + distinct allocations
// ---------------------------------------------------------------------------

func TestAggregate_DoesNotMutateInput(t *testing.T) {
	a := mustAggregator(t, nil)
	in := Input{
		Mode: model.PipelineModeInitial,
		RiskAnalysis: &model.RiskAnalysis{
			Risks:                   []model.Risk{{ID: "R-001", Level: model.RiskLevelHigh, Rationale: strptr("keep me")}},
			PromptInjectionDetected: true,
		},
		KeyParameters: &model.KeyParameters{
			Subject:                 "x",
			InternalExtras:          &model.KeyParametersInternalExtras{},
			PromptInjectionDetected: true,
		},
	}
	beforeRA, _ := json.Marshal(in.RiskAnalysis)
	beforeKP, _ := json.Marshal(in.KeyParameters)

	out, err := a.Aggregate(in)
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}

	afterRA, _ := json.Marshal(in.RiskAnalysis)
	afterKP, _ := json.Marshal(in.KeyParameters)
	if string(beforeRA) != string(afterRA) {
		t.Fatalf("in.RiskAnalysis mutated:\n before=%s\n after =%s", beforeRA, afterRA)
	}
	if string(beforeKP) != string(afterKP) {
		t.Fatalf("in.KeyParameters mutated:\n before=%s\n after =%s", beforeKP, afterKP)
	}
	// Distinct allocations.
	if out.MergedRiskAnalysis == in.RiskAnalysis {
		t.Fatal("MergedRiskAnalysis must be a distinct allocation")
	}
	if &out.MergedRiskAnalysis.Risks[0] == &in.RiskAnalysis.Risks[0] {
		t.Fatal("merged Risks must use a distinct backing array")
	}
	if out.StrippedKeyParameters == in.KeyParameters {
		t.Fatal("StrippedKeyParameters must be a distinct allocation")
	}
	// Mutating the output must not reach back into the input.
	out.MergedRiskAnalysis.Risks[0].Level = model.RiskLevelLow
	if in.RiskAnalysis.Risks[0].Level != model.RiskLevelHigh {
		t.Fatal("mutating Output reached back into Input")
	}
	if in.RiskAnalysis.Risks[0].Rationale == nil {
		t.Fatal("input rationale must be preserved (strip only on the copy)")
	}
}

func TestAggregate_ConcurrentRaceClean(t *testing.T) {
	a := mustAggregator(t, &spyMetrics{})
	in := Input{
		Mode: model.PipelineModeReCheck,
		RiskAnalysis: &model.RiskAnalysis{
			Risks:                   []model.Risk{{ID: "R-001", Level: model.RiskLevelHigh, Rationale: strptr("r")}},
			PromptInjectionDetected: true,
		},
		PartyConsistency:      &model.PartyConsistencyFindings{Findings: []model.PartyFinding{{Type: model.PartyFindingAuthorityMissing}}},
		MandatoryConditions:   &model.MandatoryConditionsReport{Conditions: []model.MandatoryCondition{{Code: "MC_A", Status: model.MandatoryConditionFoundAmbiguous}}},
		KeyParameters:         &model.KeyParameters{PromptInjectionDetected: true},
		Recommendations:       model.Recommendations{{RiskID: "R-404"}},
		Truncation:            &TruncationInfo{TruncatedBytes: 1, TotalBytes: 2},
		ParentAnalysisMissing: true,
	}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := a.Aggregate(in); err != nil {
				t.Errorf("Aggregate: %v", err)
			}
		}()
	}
	wg.Wait()
}

// ---------------------------------------------------------------------------
// D7 — constructor fail-fast / nil metrics
// ---------------------------------------------------------------------------

func TestNewAggregator_FailFast(t *testing.T) {
	bad := []Config{
		func() Config { c := defaultConfig(); c.WeightHigh = -1; return c }(),
		func() Config { c := defaultConfig(); c.LabelLowThreshold = 1.5; return c }(),
		func() Config { c := defaultConfig(); c.LabelMediumThreshold = 0.9; return c }(), // >= low
	}
	for i, c := range bad {
		if _, err := NewAggregator(c, nil); err == nil {
			t.Errorf("case %d: want error for invalid config %+v", i, c)
		}
	}
	if _, err := NewAggregator(defaultConfig(), nil); err != nil {
		t.Fatalf("nil metrics must be accepted (noop): %v", err)
	}
}

// ---------------------------------------------------------------------------
// D8 — user_message constants byte-exact + length bound
// ---------------------------------------------------------------------------

func TestUserMessageConstants(t *testing.T) {
	// VERBATIM high-architecture.md:837.
	const wantPI = "В тексте договора обнаружены признаки попытки воздействия на инструкции анализа. Результаты могут быть искажены — рекомендуем проверить ключевые риски и параметры вручную."
	if msgPromptInjection != wantPI {
		t.Errorf("msgPromptInjection drifted from high-architecture.md:837")
	}
	// VERBATIM high-architecture.md:1105.
	const wantRC = "Сравнение с предыдущей версией недоступно: данные анализа родительской версии не найдены"
	if msgReCheckParentMissing != wantRC {
		t.Errorf("msgReCheckParentMissing drifted from high-architecture.md:1105")
	}
	for name, s := range map[string]string{
		"msgPromptInjection":              msgPromptInjection,
		"msgReCheckParentMissing":         msgReCheckParentMissing,
		"msgInputTruncated":               msgInputTruncated,
		"msgClassificationParamsMismatch": msgClassificationParamsMismatch,
		"msgRecommendationOrphanRef":      msgRecommendationOrphanRef,
	} {
		if s == "" {
			t.Errorf("%s is empty", name)
		}
		if len(s) > 500 {
			t.Errorf("%s len=%d exceeds schema maxLength:500", name, len(s))
		}
		if !utf8.ValidString(s) {
			t.Errorf("%s is not valid UTF-8", name)
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
