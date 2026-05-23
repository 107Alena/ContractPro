package fakes

import (
	"encoding/json"
	"testing"

	"github.com/xeipuuv/gojsonschema"

	"contractpro/legal-intelligence-core/internal/agents/schemas"
	"contractpro/legal-intelligence-core/internal/domain/model"
)

func TestFixtureBlobs_ValidJSON(t *testing.T) {
	cases := map[string]string{
		"SemanticTreeRU":       SemanticTreeRU,
		"ExtractedTextRU":      ExtractedTextRU,
		"DocumentStructureRU":  DocumentStructureRU,
		"ProcessingWarningsRU": ProcessingWarningsRU,
		"ParentRiskAnalysisRU": ParentRiskAnalysisRU,
	}
	for name, blob := range cases {
		t.Run(name, func(t *testing.T) {
			var v any
			if err := json.Unmarshal([]byte(blob), &v); err != nil {
				t.Fatalf("%s: invalid JSON: %v", name, err)
			}
		})
	}
}

// TestCannedResponses_SchemaValid_PerAgent loads each agent's REAL
// JSON-schema (internal/agents/schemas) and validates the canned
// response against it via gojsonschema (the same library the production
// Schema Validator uses, LIC-TASK-023). A response that fails here
// would also fail at the production validator boundary — this test is
// the early-warning radar.
func TestCannedResponses_SchemaValid_PerAgent(t *testing.T) {
	for _, a := range model.AllAgentIDs() {
		t.Run(string(a), func(t *testing.T) {
			schemaBytes, err := schemas.LoadSchema(a)
			if err != nil {
				t.Fatalf("LoadSchema(%s): %v", a, err)
			}
			loader := gojsonschema.NewBytesLoader(schemaBytes)
			doc := gojsonschema.NewStringLoader(CannedResponseFor(a))
			res, err := gojsonschema.Validate(loader, doc)
			if err != nil {
				t.Fatalf("validate(%s): %v", a, err)
			}
			if !res.Valid() {
				for _, e := range res.Errors() {
					t.Errorf("agent %s: %s", a, e)
				}
				t.FailNow()
			}
		})
	}
}

func TestCannedResponseFor_UnknownAgentPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for unknown agent")
		}
	}()
	_ = CannedResponseFor(model.AgentID("AGENT_UNKNOWN"))
}

func TestDefaultArtifactsBundle_HasMandatoryTypes(t *testing.T) {
	bundle := DefaultArtifactsBundle()
	want := []model.ArtifactType{
		model.ArtifactSemanticTree,
		model.ArtifactExtractedText,
		model.ArtifactDocumentStructure,
		model.ArtifactProcessingWarnings,
	}
	for _, w := range want {
		if _, ok := bundle[w]; !ok {
			t.Fatalf("default bundle missing %s", w)
		}
	}
	if _, ok := bundle[model.ArtifactRiskAnalysis]; ok {
		t.Fatal("default bundle MUST NOT carry RISK_ANALYSIS (RE_CHECK only)")
	}
}

func TestReCheckArtifactsBundle_HasRiskAnalysis(t *testing.T) {
	bundle := ReCheckArtifactsBundle()
	if _, ok := bundle[model.ArtifactRiskAnalysis]; !ok {
		t.Fatal("RE_CHECK bundle MUST carry RISK_ANALYSIS")
	}
}

func TestBuildAgentInput_CopiesArtifactsMap(t *testing.T) {
	src := DefaultArtifactsBundle()
	input := BuildAgentInput("c", "j", "d", "v", "o", src)
	delete(src, model.ArtifactSemanticTree)
	if _, ok := input.Artifacts[model.ArtifactSemanticTree]; !ok {
		t.Fatal("BuildAgentInput should copy the artifacts map")
	}
}

func TestBuildArtifactsResponse_Wrapping(t *testing.T) {
	r := BuildArtifactsResponse(DefaultArtifactsBundle())
	if r.ErrorCode != "" || r.Drop {
		t.Fatalf("unexpected fields: %+v", r)
	}
	if _, ok := r.Artifacts[model.ArtifactSemanticTree]; !ok {
		t.Fatal("missing artifact")
	}
}
