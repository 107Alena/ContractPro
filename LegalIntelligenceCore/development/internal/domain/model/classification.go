package model

// ClassificationResult is the output of Agent 1 — Contract Type Classifier
// (ai-agents-pipeline.md §1). Wire schema is FROZEN by DM
// LegalAnalysisArtifactsReady.classification_result.
type ClassificationResult struct {
	ContractType            ContractType              `json:"contract_type"`
	Confidence              float64                   `json:"confidence"`
	Alternatives            []ClassificationAlternative `json:"alternatives"`
	Rationale               *string                   `json:"rationale,omitempty"`
	PromptInjectionDetected bool                      `json:"prompt_injection_detected"`
}

// ClassificationAlternative is one of up to three alternative contract types
// with their own confidence values (ai-agents-pipeline.md §1).
type ClassificationAlternative struct {
	ContractType ContractType `json:"contract_type"`
	Confidence   float64      `json:"confidence"`
}
