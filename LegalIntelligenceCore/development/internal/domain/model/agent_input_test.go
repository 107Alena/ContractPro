package model

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

func TestArtifactType_IsValid(t *testing.T) {
	for _, a := range []ArtifactType{ArtifactSemanticTree, ArtifactExtractedText,
		ArtifactDocumentStructure, ArtifactProcessingWarnings, ArtifactRiskAnalysis} {
		if !a.IsValid() {
			t.Errorf("%q must be valid", a)
		}
	}
	if ArtifactType("OTHER_ARTIFACT").IsValid() {
		t.Error("OTHER_ARTIFACT must NOT be valid")
	}
}

func TestInputArtifactsCompact_Has(t *testing.T) {
	a := InputArtifactsCompact{
		ArtifactExtractedText: json.RawMessage(`"hello"`),
		ArtifactSemanticTree:  json.RawMessage(``), // empty must not count
	}
	if !a.Has(ArtifactExtractedText) {
		t.Error("EXTRACTED_TEXT must be reported present")
	}
	if a.Has(ArtifactSemanticTree) {
		t.Error("empty bytes must NOT count as present")
	}
	if a.Has(ArtifactDocumentStructure) {
		t.Error("missing key must NOT be present")
	}
}

func TestAgentInput_JSONRoundTripPreservesRawArtifacts(t *testing.T) {
	// Use raw JSON to verify defer-decoded artifacts survive untouched
	// (byte-equality is the key property here).
	rawTree := json.RawMessage(`{"id":"root","type":"section","children":[]}`)
	rawText := json.RawMessage(`"Договор поставки..."`)
	in := AgentInput{
		CorrelationID:  "corr-1",
		JobID:          "job-1",
		DocumentID:     "doc-1",
		VersionID:      "ver-1",
		OrganizationID: "org-1",
		Artifacts: InputArtifactsCompact{
			ArtifactSemanticTree:  rawTree,
			ArtifactExtractedText: rawText,
		},
		Classification: &ClassificationResult{
			ContractType:            ContractTypeSupply,
			Confidence:              0.92,
			Alternatives:            []ClassificationAlternative{},
			PromptInjectionDetected: false,
		},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got AgentInput
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(got, in) {
		t.Fatalf("round-trip mismatch:\n got=%+v\nwant=%+v", got, in)
	}
	// Round-trip preserves byte-for-byte content of raw fields (canonical JSON).
	if string(got.Artifacts[ArtifactSemanticTree]) != string(rawTree) {
		t.Fatalf("semantic_tree raw bytes drifted: got %s, want %s", got.Artifacts[ArtifactSemanticTree], rawTree)
	}
}

func TestAgentInput_OmitsUnsetFields(t *testing.T) {
	// Only correlation ids required — everything else must be omitted.
	in := AgentInput{
		CorrelationID:  "c",
		JobID:          "j",
		DocumentID:     "d",
		VersionID:      "v",
		OrganizationID: "o",
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, mustNot := range []string{
		`"artifacts":`, `"classification_result":`, `"key_parameters":`,
		`"party_consistency_findings":`, `"mandatory_conditions_report":`,
		`"risk_analysis":`, `"merged_risk_analysis":`, `"recommendations":`,
		`"parent_risk_analysis":`, `"parent_version_id":`, `"created_by_user_id":`,
	} {
		if bytes.Contains(b, []byte(mustNot)) {
			t.Errorf("unset field %q must be omitted, got %s", mustNot, b)
		}
	}
}
