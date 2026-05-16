package schemas

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"testing/fstest"

	"contractpro/legal-intelligence-core/internal/domain/model"
)

// TestValidate is the acceptance gate (LIC-TASK-020 criteria 6 & 7): all 9
// schemas present, well-formed JSON, draft-07 pinned, valid top-level shape,
// no orphan files. A clean tree → nil.
func TestValidate(t *testing.T) {
	if err := Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

// TestLoadSchemaAllAgents asserts every pipeline AgentID resolves to
// well-formed JSON bytes.
func TestLoadSchemaAllAgents(t *testing.T) {
	ids := model.AllAgentIDs()
	if len(ids) != 9 {
		t.Fatalf("model.AllAgentIDs() len = %d, want 9", len(ids))
	}
	for _, id := range ids {
		b, err := LoadSchema(id)
		if err != nil {
			t.Errorf("LoadSchema(%s) error: %v", id, err)
			continue
		}
		if !json.Valid(b) {
			t.Errorf("LoadSchema(%s) returned malformed JSON", id)
		}
	}
}

// TestLoadSchemaRiskDetectionDraft07 is test step 3: the risk-detection
// schema is a valid draft-07 JSON Schema (well-formed, $schema pinned,
// expected title).
func TestLoadSchemaRiskDetectionDraft07(t *testing.T) {
	b, err := LoadSchema(model.AgentRiskDetection)
	if err != nil {
		t.Fatalf("LoadSchema(AGENT_RISK_DETECTION) error: %v", err)
	}
	if !json.Valid(b) {
		t.Fatal("risk-detection schema is not well-formed JSON")
	}
	var h schemaHead
	if err := json.Unmarshal(b, &h); err != nil {
		t.Fatalf("decode risk-detection schema head: %v", err)
	}
	if h.Schema != draft07URI {
		t.Errorf("$schema = %q, want %q", h.Schema, draft07URI)
	}
	if h.Title != "RiskAnalysis" {
		t.Errorf("title = %q, want %q", h.Title, "RiskAnalysis")
	}
	if err := validTopLevelType(h.Type); err != nil {
		t.Errorf("top-level type invalid: %v", err)
	}
}

// TestSchemaRootTypes locks the code-architect must-fix #1: Recommendations
// is root "array"; the rest are "object". A validator that demanded
// "object" would wrongly reject Recommendations.
func TestSchemaRootTypes(t *testing.T) {
	want := map[model.AgentID]string{
		model.AgentTypeClassifier:      "object",
		model.AgentKeyParams:           "object",
		model.AgentPartyConsistency:    "object",
		model.AgentMandatoryConditions: "object",
		model.AgentRiskDetection:       "object",
		model.AgentRecommendation:      "array",
		model.AgentSummary:             "object",
		model.AgentDetailedReport:      "object",
		model.AgentRiskDelta:           "object",
	}
	for id, wantType := range want {
		b, err := LoadSchema(id)
		if err != nil {
			t.Errorf("LoadSchema(%s): %v", id, err)
			continue
		}
		var h schemaHead
		if err := json.Unmarshal(b, &h); err != nil {
			t.Errorf("decode %s head: %v", id, err)
			continue
		}
		var got string
		if err := json.Unmarshal(h.Type, &got); err != nil {
			t.Errorf("%s: top-level type is not a single string: %v", id, err)
			continue
		}
		if got != wantType {
			t.Errorf("%s root type = %q, want %q", id, got, wantType)
		}
	}
}

// TestLoadSchemaVerbatimBytes asserts the loader does not canonicalize the
// document: the literal "$schema" token survives in the raw bytes (a
// re-marshal could reorder/strip it). LIC-TASK-023 relies on verbatim bytes.
func TestLoadSchemaVerbatimBytes(t *testing.T) {
	b, err := LoadSchema(model.AgentSummary)
	if err != nil {
		t.Fatalf("LoadSchema(AGENT_SUMMARY): %v", err)
	}
	if !bytes.Contains(b, []byte(`"$schema"`)) {
		t.Error("raw schema bytes missing literal \"$schema\" token (unexpected canonicalization)")
	}
}

// TestLoadSchemaUnknownID: an id outside the 9 errors, does not panic.
func TestLoadSchemaUnknownID(t *testing.T) {
	if _, err := LoadSchema(model.AgentID("AGENT_NOPE")); err == nil {
		t.Fatal("LoadSchema(unknown) = nil error, want non-nil")
	}
}

// TestValidateDeterministicOrdering exercises the failing path via an
// injected FS: missing-agent errors in pipeline order, then orphans sorted.
func TestValidateDeterministicOrdering(t *testing.T) {
	fsys := fstest.MapFS{
		"aaa.json": {Data: []byte("{}")},
		"zzz.json": {Data: []byte("{}")},
	}
	err := validate(fsys, basenames)
	if err == nil {
		t.Fatal("validate(empty fs) = nil, want aggregated error")
	}
	msg := err.Error()
	markers := []string{
		"agent " + model.AgentTypeClassifier.String() + ":",
		"agent " + model.AgentRiskDelta.String() + ":",
		`orphan embedded file "aaa.json"`,
		`orphan embedded file "zzz.json"`,
	}
	prev := -1
	for _, m := range markers {
		i := strings.Index(msg, m)
		if i < 0 {
			t.Fatalf("aggregated error missing marker %q\nfull error:\n%s", m, msg)
		}
		if i <= prev {
			t.Errorf("marker %q out of deterministic order\nfull error:\n%s", m, msg)
		}
		prev = i
	}
}

// TestValidateRejectsMalformedJSON: the !json.Valid branch of loadSchema is
// surfaced as a fatal Validate error (not silently passed to TASK-023).
func TestValidateRejectsMalformedJSON(t *testing.T) {
	fsys := fstest.MapFS{
		"type_classifier.json": {Data: []byte(`{"$schema": not json`)},
	}
	names := map[model.AgentID]string{model.AgentTypeClassifier: "type_classifier"}
	err := validate(fsys, names)
	if err == nil || !strings.Contains(err.Error(), "not well-formed JSON") {
		t.Fatalf("validate(malformed) = %v, want a not-well-formed-JSON error", err)
	}
}

// TestValidateRejectsBadBasename: a path-bearing SSOT table value fails loud
// (SF-1), never resolving a different embedded path.
func TestValidateRejectsBadBasename(t *testing.T) {
	names := map[model.AgentID]string{model.AgentTypeClassifier: "../evil"}
	err := validate(fstest.MapFS{}, names)
	if err == nil || !strings.Contains(err.Error(), `invalid basename "../evil"`) {
		t.Fatalf("validate(bad basename) = %v, want invalid-basename error", err)
	}
}

// TestValidBasename covers the path-segment guard branches.
func TestValidBasename(t *testing.T) {
	for _, ok := range []string{"summary", "risk_delta"} {
		if err := validBasename(ok); err != nil {
			t.Errorf("validBasename(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"", ".x", "a/b", `a\b`, "..", "x..y"} {
		if err := validBasename(bad); err == nil {
			t.Errorf("validBasename(%q) = nil, want error", bad)
		}
	}
}

// TestValidTopLevelType covers the helper's branches directly, including the
// array form draft-07 permits (e.g. ["string","null"]).
func TestValidTopLevelType(t *testing.T) {
	ok := []string{`"object"`, `"array"`, `"string"`, `["string","null"]`, `["integer"]`}
	for _, s := range ok {
		if err := validTopLevelType(json.RawMessage(s)); err != nil {
			t.Errorf("validTopLevelType(%s) = %v, want nil", s, err)
		}
	}
	bad := []string{``, `"widget"`, `[]`, `["string","widget"]`, `42`, `{}`}
	for _, s := range bad {
		if err := validTopLevelType(json.RawMessage(s)); err == nil {
			t.Errorf("validTopLevelType(%s) = nil, want error", s)
		}
	}
}
