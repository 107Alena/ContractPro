package model

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNewDocument(t *testing.T) {
	doc := NewDocument("doc-1", "org-1", "Договор поставки", "user-1")

	if doc.DocumentID != "doc-1" {
		t.Errorf("expected document_id doc-1, got %s", doc.DocumentID)
	}
	if doc.OrganizationID != "org-1" {
		t.Errorf("expected organization_id org-1, got %s", doc.OrganizationID)
	}
	if doc.Title != "Договор поставки" {
		t.Errorf("expected title Договор поставки, got %s", doc.Title)
	}
	if doc.Status != DocumentStatusActive {
		t.Errorf("expected status ACTIVE, got %s", doc.Status)
	}
	if doc.CreatedByUserID != "user-1" {
		t.Errorf("expected created_by_user_id user-1, got %s", doc.CreatedByUserID)
	}
	if doc.CreatedAt.IsZero() {
		t.Error("expected non-zero created_at")
	}
	if doc.DeletedAt != nil {
		t.Error("expected nil deleted_at for new document")
	}
}

func TestDocumentJSONRoundTrip(t *testing.T) {
	now := time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC)
	deletedAt := now.Add(time.Hour)
	doc := &Document{
		DocumentID:       "doc-123",
		OrganizationID:   "org-456",
		Title:            "Договор аренды",
		CurrentVersionID: "ver-789",
		Status:           DocumentStatusArchived,
		CreatedByUserID:  "user-111",
		CreatedAt:        now,
		UpdatedAt:        now,
		DeletedAt:        &deletedAt,
	}

	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var restored Document
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if restored.DocumentID != doc.DocumentID {
		t.Errorf("document_id mismatch: %s != %s", restored.DocumentID, doc.DocumentID)
	}
	if restored.OrganizationID != doc.OrganizationID {
		t.Errorf("organization_id mismatch: %s != %s", restored.OrganizationID, doc.OrganizationID)
	}
	if restored.Title != doc.Title {
		t.Errorf("title mismatch: %s != %s", restored.Title, doc.Title)
	}
	if restored.CurrentVersionID != doc.CurrentVersionID {
		t.Errorf("current_version_id mismatch: %s != %s", restored.CurrentVersionID, doc.CurrentVersionID)
	}
	if restored.Status != doc.Status {
		t.Errorf("status mismatch: %s != %s", restored.Status, doc.Status)
	}
	if restored.CreatedByUserID != doc.CreatedByUserID {
		t.Errorf("created_by_user_id mismatch: %s != %s", restored.CreatedByUserID, doc.CreatedByUserID)
	}
	if restored.DeletedAt == nil {
		t.Error("expected non-nil deleted_at")
	}
}

func TestDocumentJSONOmitsEmptyFields(t *testing.T) {
	doc := NewDocument("doc-1", "org-1", "Test", "user-1")
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if _, ok := raw["current_version_id"]; ok {
		t.Error("expected current_version_id to be omitted for empty string")
	}
	if _, ok := raw["deleted_at"]; ok {
		t.Error("expected deleted_at to be omitted for nil")
	}
}

func TestDocumentStatusTransitions(t *testing.T) {
	tests := []struct {
		from    DocumentStatus
		to      DocumentStatus
		allowed bool
	}{
		{DocumentStatusActive, DocumentStatusArchived, true},
		{DocumentStatusActive, DocumentStatusDeleted, true},
		{DocumentStatusArchived, DocumentStatusDeleted, true},
		{DocumentStatusArchived, DocumentStatusActive, false},
		{DocumentStatusDeleted, DocumentStatusActive, false},
		{DocumentStatusDeleted, DocumentStatusArchived, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.from)+"->"+string(tt.to), func(t *testing.T) {
			result := ValidateDocumentTransition(tt.from, tt.to)
			if result != tt.allowed {
				t.Errorf("expected %v, got %v", tt.allowed, result)
			}
		})
	}
}

func TestDocumentTransitionTo(t *testing.T) {
	doc := NewDocument("doc-1", "org-1", "Test", "user-1")

	if !doc.TransitionTo(DocumentStatusArchived) {
		t.Error("expected successful transition ACTIVE -> ARCHIVED")
	}
	if doc.Status != DocumentStatusArchived {
		t.Errorf("expected ARCHIVED, got %s", doc.Status)
	}

	if doc.TransitionTo(DocumentStatusActive) {
		t.Error("expected failed transition ARCHIVED -> ACTIVE")
	}

	if !doc.TransitionTo(DocumentStatusDeleted) {
		t.Error("expected successful transition ARCHIVED -> DELETED")
	}
	if doc.Status != DocumentStatusDeleted {
		t.Errorf("expected DELETED, got %s", doc.Status)
	}
	if doc.DeletedAt == nil {
		t.Error("expected non-nil deleted_at after deletion")
	}
}

func TestDocumentStatusIsTerminal(t *testing.T) {
	if DocumentStatusActive.IsTerminal() {
		t.Error("ACTIVE should not be terminal")
	}
	if DocumentStatusArchived.IsTerminal() {
		t.Error("ARCHIVED should not be terminal")
	}
	if !DocumentStatusDeleted.IsTerminal() {
		t.Error("DELETED should be terminal")
	}
}
