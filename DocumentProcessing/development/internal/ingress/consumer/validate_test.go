package consumer

import (
	"errors"
	"strings"
	"testing"

	"contractpro/document-processing/internal/domain/model"
	"contractpro/document-processing/internal/domain/port"
)

// --- validateProcessDocumentCommand ---

func TestValidateProcessDocumentCommand_Valid(t *testing.T) {
	cmd := model.ProcessDocumentCommand{
		JobID:      "job-1",
		DocumentID: "doc-1",
		FileURL:    "https://example.com/file.pdf",
	}
	if err := validateProcessDocumentCommand(cmd); err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
}

func TestValidateProcessDocumentCommand_MissingJobID(t *testing.T) {
	cmd := model.ProcessDocumentCommand{
		DocumentID: "doc-1",
		FileURL:    "https://example.com/file.pdf",
	}
	err := validateProcessDocumentCommand(cmd)
	if err == nil {
		t.Fatal("expected error for missing job_id")
	}
	assertValidationError(t, err)
	if !strings.Contains(err.Error(), "job_id") {
		t.Errorf("error should mention job_id: %v", err)
	}
}

func TestValidateProcessDocumentCommand_MissingDocumentID(t *testing.T) {
	cmd := model.ProcessDocumentCommand{
		JobID:   "job-1",
		FileURL: "https://example.com/file.pdf",
	}
	err := validateProcessDocumentCommand(cmd)
	if err == nil {
		t.Fatal("expected error for missing document_id")
	}
	assertValidationError(t, err)
	if !strings.Contains(err.Error(), "document_id") {
		t.Errorf("error should mention document_id: %v", err)
	}
}

func TestValidateProcessDocumentCommand_MissingFileURL(t *testing.T) {
	cmd := model.ProcessDocumentCommand{
		JobID:      "job-1",
		DocumentID: "doc-1",
	}
	err := validateProcessDocumentCommand(cmd)
	if err == nil {
		t.Fatal("expected error for missing file_url")
	}
	assertValidationError(t, err)
	if !strings.Contains(err.Error(), "file_url") {
		t.Errorf("error should mention file_url: %v", err)
	}
}

func TestValidateProcessDocumentCommand_MultipleFieldsMissing(t *testing.T) {
	cmd := model.ProcessDocumentCommand{} // all empty
	err := validateProcessDocumentCommand(cmd)
	if err == nil {
		t.Fatal("expected error for multiple missing fields")
	}
	assertValidationError(t, err)
	msg := err.Error()
	for _, field := range []string{"job_id", "document_id", "file_url"} {
		if !strings.Contains(msg, field) {
			t.Errorf("error should mention %s: %v", field, err)
		}
	}
}

func TestValidateProcessDocumentCommand_WhitespaceOnly(t *testing.T) {
	cmd := model.ProcessDocumentCommand{
		JobID:      "  \t ",
		DocumentID: "  ",
		FileURL:    " \n ",
	}
	err := validateProcessDocumentCommand(cmd)
	if err == nil {
		t.Fatal("expected error for whitespace-only fields")
	}
	assertValidationError(t, err)
	msg := err.Error()
	for _, field := range []string{"job_id", "document_id", "file_url"} {
		if !strings.Contains(msg, field) {
			t.Errorf("error should mention %s: %v", field, err)
		}
	}
}

// --- validateCompareVersionsCommand ---

func TestValidateCompareVersionsCommand_Valid(t *testing.T) {
	cmd := model.CompareVersionsCommand{
		JobID:           "job-1",
		DocumentID:      "doc-1",
		BaseVersionID:   "v1",
		TargetVersionID: "v2",
	}
	if err := validateCompareVersionsCommand(cmd); err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
}

func TestValidateCompareVersionsCommand_MissingJobID(t *testing.T) {
	cmd := model.CompareVersionsCommand{
		DocumentID:      "doc-1",
		BaseVersionID:   "v1",
		TargetVersionID: "v2",
	}
	err := validateCompareVersionsCommand(cmd)
	if err == nil {
		t.Fatal("expected error for missing job_id")
	}
	assertValidationError(t, err)
	if !strings.Contains(err.Error(), "job_id") {
		t.Errorf("error should mention job_id: %v", err)
	}
}

func TestValidateCompareVersionsCommand_MissingBaseVersionID(t *testing.T) {
	cmd := model.CompareVersionsCommand{
		JobID:           "job-1",
		DocumentID:      "doc-1",
		TargetVersionID: "v2",
	}
	err := validateCompareVersionsCommand(cmd)
	if err == nil {
		t.Fatal("expected error for missing base_version_id")
	}
	assertValidationError(t, err)
	if !strings.Contains(err.Error(), "base_version_id") {
		t.Errorf("error should mention base_version_id: %v", err)
	}
}

func TestValidateCompareVersionsCommand_MissingTargetVersionID(t *testing.T) {
	cmd := model.CompareVersionsCommand{
		JobID:         "job-1",
		DocumentID:    "doc-1",
		BaseVersionID: "v1",
	}
	err := validateCompareVersionsCommand(cmd)
	if err == nil {
		t.Fatal("expected error for missing target_version_id")
	}
	assertValidationError(t, err)
	if !strings.Contains(err.Error(), "target_version_id") {
		t.Errorf("error should mention target_version_id: %v", err)
	}
}

func TestValidateCompareVersionsCommand_MultipleFieldsMissing(t *testing.T) {
	cmd := model.CompareVersionsCommand{}
	err := validateCompareVersionsCommand(cmd)
	if err == nil {
		t.Fatal("expected error for multiple missing fields")
	}
	assertValidationError(t, err)
	msg := err.Error()
	for _, field := range []string{"job_id", "document_id", "base_version_id", "target_version_id"} {
		if !strings.Contains(msg, field) {
			t.Errorf("error should mention %s: %v", field, err)
		}
	}
}

func TestValidateCompareVersionsCommand_WhitespaceOnly(t *testing.T) {
	cmd := model.CompareVersionsCommand{
		JobID:           " ",
		DocumentID:      "\t",
		BaseVersionID:   " \n",
		TargetVersionID: "  ",
	}
	err := validateCompareVersionsCommand(cmd)
	if err == nil {
		t.Fatal("expected error for whitespace-only fields")
	}
	assertValidationError(t, err)
	msg := err.Error()
	for _, field := range []string{"job_id", "document_id", "base_version_id", "target_version_id"} {
		if !strings.Contains(msg, field) {
			t.Errorf("error should mention %s: %v", field, err)
		}
	}
}

// --- helper ---

func assertValidationError(t *testing.T, err error) {
	t.Helper()
	var de *port.DomainError
	if !errors.As(err, &de) {
		t.Fatalf("expected *port.DomainError, got %T", err)
	}
	if de.Code != port.ErrCodeValidation {
		t.Errorf("expected code %s, got %s", port.ErrCodeValidation, de.Code)
	}
	if de.Retryable {
		t.Error("validation error should not be retryable")
	}
}
