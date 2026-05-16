package prompts

import (
	"strings"
	"testing"
	"testing/fstest"

	"contractpro/legal-intelligence-core/internal/domain/model"
)

// TestValidate is the acceptance gate (LIC-TASK-020 criterion 6): all 9
// prompts present and non-empty, no orphan .txt files. A clean tree → nil.
func TestValidate(t *testing.T) {
	if err := Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil (all 9 prompts present, no orphans)", err)
	}
}

// TestLoadPromptAllAgents asserts every pipeline AgentID resolves to a
// non-empty prompt. The basenames map and embedded files must cover exactly
// model.AllAgentIDs().
func TestLoadPromptAllAgents(t *testing.T) {
	ids := model.AllAgentIDs()
	if len(ids) != 9 {
		t.Fatalf("model.AllAgentIDs() len = %d, want 9", len(ids))
	}
	for _, id := range ids {
		p, err := LoadPrompt(id)
		if err != nil {
			t.Errorf("LoadPrompt(%s) error: %v", id, err)
			continue
		}
		if strings.TrimSpace(p) == "" {
			t.Errorf("LoadPrompt(%s) returned blank prompt", id)
		}
	}
}

// TestLoadPromptTypeClassifierNonEmpty is test step 2: LoadPrompt for the
// classifier returns non-empty content, and it is the right prompt (carries
// the SSOT structural markers and the prompt-injection guard).
func TestLoadPromptTypeClassifierNonEmpty(t *testing.T) {
	p, err := LoadPrompt(model.AgentTypeClassifier)
	if err != nil {
		t.Fatalf("LoadPrompt(AGENT_TYPE_CLASSIFIER) error: %v", err)
	}
	if p == "" {
		t.Fatal("LoadPrompt(AGENT_TYPE_CLASSIFIER) returned empty string")
	}
	for _, want := range []string{"ПРИМЕНИМОЕ ПРАВО.", "EMPLOYMENT_CIVIL", "<contract_document>"} {
		if !strings.Contains(p, want) {
			t.Errorf("classifier prompt missing expected marker %q", want)
		}
	}
}

// TestLoadPromptVerbatimNoTrim asserts the loader returns content unchanged:
// the SSOT prompt blocks end with a trailing newline, which must survive
// (legal-domain text is returned byte-for-byte, not trimmed).
func TestLoadPromptVerbatimNoTrim(t *testing.T) {
	p, err := LoadPrompt(model.AgentRiskDetection)
	if err != nil {
		t.Fatalf("LoadPrompt(AGENT_RISK_DETECTION) error: %v", err)
	}
	if !strings.HasSuffix(p, "\n") {
		t.Error("prompt was trimmed: expected trailing newline preserved verbatim")
	}
}

// TestLoadPromptUnknownID: an id outside the 9 errors, does not panic.
func TestLoadPromptUnknownID(t *testing.T) {
	if _, err := LoadPrompt(model.AgentID("AGENT_DEFINITELY_NOT_REAL")); err == nil {
		t.Fatal("LoadPrompt(unknown) = nil error, want non-nil")
	}
}

// TestValidateDeterministicOrdering directly exercises the headline contract
// — the failing path — via an injected FS: missing-agent errors must appear
// in model.AllAgentIDs() pipeline order, then orphan files sorted by name.
// errors.Join concatenates with '\n', so substring index = line order.
func TestValidateDeterministicOrdering(t *testing.T) {
	fsys := fstest.MapFS{
		"aaa.txt": {Data: []byte("orphan")},
		"zzz.txt": {Data: []byte("orphan")},
	}
	err := validate(fsys, basenames) // every mapped file absent + 2 orphans
	if err == nil {
		t.Fatal("validate(empty fs) = nil, want aggregated error")
	}
	msg := err.Error()
	markers := []string{
		"agent " + model.AgentTypeClassifier.String() + ":", // 1st in pipeline order
		"agent " + model.AgentRiskDelta.String() + ":",       // 9th (last)
		`orphan embedded file "aaa.txt"`,                      // orphans sorted: a < z
		`orphan embedded file "zzz.txt"`,
	}
	prev := -1
	for _, m := range markers {
		i := strings.Index(msg, m)
		if i < 0 {
			t.Fatalf("aggregated error missing marker %q\nfull error:\n%s", m, msg)
		}
		if i <= prev {
			t.Errorf("marker %q out of deterministic order (index %d <= %d)\nfull error:\n%s", m, i, prev, msg)
		}
		prev = i
	}
}

// TestValidateRejectsBadBasename: a path-bearing SSOT table value must fail
// loud at Validate, never resolve a different embedded path silently (SF-1).
func TestValidateRejectsBadBasename(t *testing.T) {
	names := map[model.AgentID]string{model.AgentTypeClassifier: "../evil"}
	err := validate(fstest.MapFS{}, names)
	if err == nil {
		t.Fatal("validate(bad basename) = nil, want error")
	}
	if !strings.Contains(err.Error(), `invalid basename "../evil"`) {
		t.Errorf("error does not flag the bad basename: %v", err)
	}
}

// TestValidBasename covers the path-segment guard branches.
func TestValidBasename(t *testing.T) {
	for _, ok := range []string{"type_classifier", "risk_delta", "a"} {
		if err := validBasename(ok); err != nil {
			t.Errorf("validBasename(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"", ".hidden", "sub/x", `sub\x`, "..", "a..b"} {
		if err := validBasename(bad); err == nil {
			t.Errorf("validBasename(%q) = nil, want error", bad)
		}
	}
}
