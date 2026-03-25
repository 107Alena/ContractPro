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

// --- sanitizeString ---

func TestSanitizeString(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"normal_text", "hello world", "hello world"},
		{"empty_string", "", ""},
		{"null_byte", "job\x00id", "jobid"},
		{"percent_encoded_null", "job%00id", "jobid"},
		{"path_traversal_forward", "../../etc/passwd", "etc/passwd"},
		{"path_traversal_backslash", "..\\..\\windows\\system32", "windows\\system32"},
		{"mixed_path_traversal", "../config/../secret", "config/secret"},
		{"nested_path_traversal", "....//etc/passwd", "etc/passwd"},
		{"double_nested_backslash", "....\\\\windows\\system32", "windows\\system32"},
		{"c0_control_chars", "abc\x01\x02\x03def", "abcdef"},
		{"preserves_tab_newline", "line1\tline2\nline3\r", "line1\tline2\nline3"},
		{"trims_whitespace", "  hello  ", "hello"},
		{"null_and_whitespace", "  \x00  ", ""},
		{"cyrillic_preserved", "документ-123", "документ-123"},
		{"special_chars_preserved", "file@name#1$2", "file@name#1$2"},
		{"url_preserved", "https://example.com/file.pdf?token=abc", "https://example.com/file.pdf?token=abc"},
		{"uuid_preserved", "550e8400-e29b-41d4-a716-446655440000", "550e8400-e29b-41d4-a716-446655440000"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeString(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeString(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSanitizeProcessDocumentCommand(t *testing.T) {
	cmd := model.ProcessDocumentCommand{
		JobID:      "job\x00-1",
		DocumentID: "../doc-1",
		FileURL:    "https://example.com/file.pdf",
		OrgID:      "org\x01-1",
		UserID:     "user%00-1",
		FileName:   "../../etc/passwd",
		MimeType:   "application/pdf",
		Checksum:   "abc\x00def",
	}

	sanitizeProcessDocumentCommand(&cmd)

	if cmd.JobID != "job-1" {
		t.Errorf("JobID = %q, want %q", cmd.JobID, "job-1")
	}
	if cmd.DocumentID != "doc-1" {
		t.Errorf("DocumentID = %q, want %q", cmd.DocumentID, "doc-1")
	}
	if cmd.FileURL != "https://example.com/file.pdf" {
		t.Errorf("FileURL = %q, want unchanged", cmd.FileURL)
	}
	if cmd.OrgID != "org-1" {
		t.Errorf("OrgID = %q, want %q", cmd.OrgID, "org-1")
	}
	if cmd.UserID != "user-1" {
		t.Errorf("UserID = %q, want %q", cmd.UserID, "user-1")
	}
	if cmd.FileName != "etc/passwd" {
		t.Errorf("FileName = %q, want %q", cmd.FileName, "etc/passwd")
	}
	if cmd.MimeType != "application/pdf" {
		t.Errorf("MimeType = %q, want unchanged", cmd.MimeType)
	}
	if cmd.Checksum != "abcdef" {
		t.Errorf("Checksum = %q, want %q", cmd.Checksum, "abcdef")
	}
}

func TestSanitizeCompareVersionsCommand(t *testing.T) {
	cmd := model.CompareVersionsCommand{
		JobID:           "job\x00-1",
		DocumentID:      "../doc-1",
		BaseVersionID:   "v1%00",
		TargetVersionID: "v2\x01\x02",
		OrgID:           "  org  ",
		UserID:          "user\x00",
	}

	sanitizeCompareVersionsCommand(&cmd)

	if cmd.JobID != "job-1" {
		t.Errorf("JobID = %q, want %q", cmd.JobID, "job-1")
	}
	if cmd.DocumentID != "doc-1" {
		t.Errorf("DocumentID = %q, want %q", cmd.DocumentID, "doc-1")
	}
	if cmd.BaseVersionID != "v1" {
		t.Errorf("BaseVersionID = %q, want %q", cmd.BaseVersionID, "v1")
	}
	if cmd.TargetVersionID != "v2" {
		t.Errorf("TargetVersionID = %q, want %q", cmd.TargetVersionID, "v2")
	}
	if cmd.OrgID != "org" {
		t.Errorf("OrgID = %q, want %q", cmd.OrgID, "org")
	}
	if cmd.UserID != "user" {
		t.Errorf("UserID = %q, want %q", cmd.UserID, "user")
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
