package dm

import (
	"testing"

	"contractpro/document-processing/internal/domain/model"
	"contractpro/document-processing/internal/domain/port"
)

// --- validateArtifactsPersisted ---

func TestValidateArtifactsPersisted_Valid(t *testing.T) {
	event := model.DocumentProcessingArtifactsPersisted{
		JobID:      "job-1",
		DocumentID: "doc-1",
	}
	if err := validateArtifactsPersisted(event); err != nil {
		t.Errorf("expected nil, got: %v", err)
	}
}

func TestValidateArtifactsPersisted_MissingJobID(t *testing.T) {
	event := model.DocumentProcessingArtifactsPersisted{
		DocumentID: "doc-1",
	}
	err := validateArtifactsPersisted(event)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if port.ErrorCode(err) != port.ErrCodeValidation {
		t.Errorf("ErrorCode = %q, want %q", port.ErrorCode(err), port.ErrCodeValidation)
	}
}

func TestValidateArtifactsPersisted_MissingDocumentID(t *testing.T) {
	event := model.DocumentProcessingArtifactsPersisted{
		JobID: "job-1",
	}
	err := validateArtifactsPersisted(event)
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateArtifactsPersisted_AllMissing(t *testing.T) {
	err := validateArtifactsPersisted(model.DocumentProcessingArtifactsPersisted{})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateArtifactsPersisted_WhitespaceOnly(t *testing.T) {
	event := model.DocumentProcessingArtifactsPersisted{
		JobID:      "  ",
		DocumentID: "doc-1",
	}
	err := validateArtifactsPersisted(event)
	if err == nil {
		t.Fatal("expected validation error for whitespace-only job_id")
	}
}

// --- validateArtifactsPersistFailed ---

func TestValidateArtifactsPersistFailed_Valid(t *testing.T) {
	event := model.DocumentProcessingArtifactsPersistFailed{
		JobID:        "job-1",
		DocumentID:   "doc-1",
		ErrorMessage: "error",
	}
	if err := validateArtifactsPersistFailed(event); err != nil {
		t.Errorf("expected nil, got: %v", err)
	}
}

func TestValidateArtifactsPersistFailed_MissingErrorMessage(t *testing.T) {
	event := model.DocumentProcessingArtifactsPersistFailed{
		JobID:      "job-1",
		DocumentID: "doc-1",
	}
	err := validateArtifactsPersistFailed(event)
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateArtifactsPersistFailed_AllMissing(t *testing.T) {
	err := validateArtifactsPersistFailed(model.DocumentProcessingArtifactsPersistFailed{})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

// --- validateSemanticTreeProvided ---

func TestValidateSemanticTreeProvided_Valid(t *testing.T) {
	event := model.SemanticTreeProvided{
		EventMeta:  model.EventMeta{CorrelationID: "corr-1"},
		JobID:      "job-1",
		DocumentID: "doc-1",
		VersionID:  "v1",
	}
	if err := validateSemanticTreeProvided(event); err != nil {
		t.Errorf("expected nil, got: %v", err)
	}
}

func TestValidateSemanticTreeProvided_MissingCorrelationID(t *testing.T) {
	event := model.SemanticTreeProvided{
		JobID:      "job-1",
		DocumentID: "doc-1",
		VersionID:  "v1",
	}
	err := validateSemanticTreeProvided(event)
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateSemanticTreeProvided_MissingVersionID(t *testing.T) {
	event := model.SemanticTreeProvided{
		EventMeta:  model.EventMeta{CorrelationID: "corr-1"},
		JobID:      "job-1",
		DocumentID: "doc-1",
	}
	err := validateSemanticTreeProvided(event)
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateSemanticTreeProvided_AllMissing(t *testing.T) {
	err := validateSemanticTreeProvided(model.SemanticTreeProvided{})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

// --- validateDiffPersisted ---

func TestValidateDiffPersisted_Valid(t *testing.T) {
	event := model.DocumentVersionDiffPersisted{
		JobID:      "job-1",
		DocumentID: "doc-1",
	}
	if err := validateDiffPersisted(event); err != nil {
		t.Errorf("expected nil, got: %v", err)
	}
}

func TestValidateDiffPersisted_MissingJobID(t *testing.T) {
	event := model.DocumentVersionDiffPersisted{
		DocumentID: "doc-1",
	}
	err := validateDiffPersisted(event)
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateDiffPersisted_AllMissing(t *testing.T) {
	err := validateDiffPersisted(model.DocumentVersionDiffPersisted{})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

// --- validateDiffPersistFailed ---

func TestValidateDiffPersistFailed_Valid(t *testing.T) {
	event := model.DocumentVersionDiffPersistFailed{
		JobID:        "job-1",
		DocumentID:   "doc-1",
		ErrorMessage: "disk full",
	}
	if err := validateDiffPersistFailed(event); err != nil {
		t.Errorf("expected nil, got: %v", err)
	}
}

func TestValidateDiffPersistFailed_MissingErrorMessage(t *testing.T) {
	event := model.DocumentVersionDiffPersistFailed{
		JobID:      "job-1",
		DocumentID: "doc-1",
	}
	err := validateDiffPersistFailed(event)
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateDiffPersistFailed_AllMissing(t *testing.T) {
	err := validateDiffPersistFailed(model.DocumentVersionDiffPersistFailed{})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateDiffPersistFailed_WhitespaceOnly(t *testing.T) {
	event := model.DocumentVersionDiffPersistFailed{
		JobID:        "job-1",
		DocumentID:   "doc-1",
		ErrorMessage: "   ",
	}
	err := validateDiffPersistFailed(event)
	if err == nil {
		t.Fatal("expected validation error for whitespace-only error_message")
	}
}
