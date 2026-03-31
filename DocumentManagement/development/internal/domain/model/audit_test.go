package model

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNewAuditRecord(t *testing.T) {
	ar := NewAuditRecord("audit-1", "org-1", AuditActionDocumentCreated, ActorTypeUser, "user-1")

	if ar.AuditID != "audit-1" {
		t.Errorf("expected audit_id audit-1, got %s", ar.AuditID)
	}
	if ar.OrganizationID != "org-1" {
		t.Errorf("expected organization_id org-1, got %s", ar.OrganizationID)
	}
	if ar.Action != AuditActionDocumentCreated {
		t.Errorf("expected DOCUMENT_CREATED, got %s", ar.Action)
	}
	if ar.ActorType != ActorTypeUser {
		t.Errorf("expected USER, got %s", ar.ActorType)
	}
	if ar.ActorID != "user-1" {
		t.Errorf("expected actor_id user-1, got %s", ar.ActorID)
	}
	if ar.CreatedAt.IsZero() {
		t.Error("expected non-zero created_at")
	}
}

func TestAuditRecordBuilderChain(t *testing.T) {
	details := json.RawMessage(`{"old_status":"ACTIVE","new_status":"ARCHIVED"}`)
	ar := NewAuditRecord("audit-1", "org-1", AuditActionDocumentArchived, ActorTypeUser, "user-1").
		WithDocument("doc-1").
		WithVersion("ver-1").
		WithJob("job-1", "corr-1").
		WithDetails(details)

	if ar.DocumentID != "doc-1" {
		t.Errorf("expected document_id doc-1, got %s", ar.DocumentID)
	}
	if ar.VersionID != "ver-1" {
		t.Errorf("expected version_id ver-1, got %s", ar.VersionID)
	}
	if ar.JobID != "job-1" {
		t.Errorf("expected job_id job-1, got %s", ar.JobID)
	}
	if ar.CorrelationID != "corr-1" {
		t.Errorf("expected correlation_id corr-1, got %s", ar.CorrelationID)
	}
	if ar.Details == nil {
		t.Error("expected non-nil details")
	}
}

func TestAuditRecordJSONRoundTrip(t *testing.T) {
	now := time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC)
	details := json.RawMessage(`{"artifact_type":"SEMANTIC_TREE","producer":"DP"}`)
	ar := &AuditRecord{
		AuditID:        "audit-123",
		OrganizationID: "org-456",
		DocumentID:     "doc-789",
		VersionID:      "ver-111",
		Action:         AuditActionArtifactSaved,
		ActorType:      ActorTypeDomain,
		ActorID:        "DP",
		JobID:          "job-222",
		CorrelationID:  "corr-333",
		Details:        details,
		CreatedAt:      now,
	}

	data, err := json.Marshal(ar)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var restored AuditRecord
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if restored.AuditID != ar.AuditID {
		t.Errorf("audit_id mismatch: %s != %s", restored.AuditID, ar.AuditID)
	}
	if restored.Action != ar.Action {
		t.Errorf("action mismatch: %s != %s", restored.Action, ar.Action)
	}
	if restored.ActorType != ar.ActorType {
		t.Errorf("actor_type mismatch: %s != %s", restored.ActorType, ar.ActorType)
	}
	if restored.ActorID != ar.ActorID {
		t.Errorf("actor_id mismatch: %s != %s", restored.ActorID, ar.ActorID)
	}
	if restored.JobID != ar.JobID {
		t.Errorf("job_id mismatch: %s != %s", restored.JobID, ar.JobID)
	}
	if string(restored.Details) != string(ar.Details) {
		t.Errorf("details mismatch: %s != %s", restored.Details, ar.Details)
	}
}

func TestAuditRecordJSONOmitsEmptyFields(t *testing.T) {
	ar := NewAuditRecord("audit-1", "org-1", AuditActionDocumentCreated, ActorTypeUser, "user-1")

	data, err := json.Marshal(ar)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	for _, field := range []string{"document_id", "version_id", "job_id", "correlation_id", "details"} {
		if _, ok := raw[field]; ok {
			t.Errorf("expected %s to be omitted for empty value", field)
		}
	}
}

func TestAllAuditActionsCount(t *testing.T) {
	expected := 9
	if len(AllAuditActions) != expected {
		t.Errorf("expected %d audit actions, got %d", expected, len(AllAuditActions))
	}
}

func TestAllActorTypesCount(t *testing.T) {
	expected := 3
	if len(AllActorTypes) != expected {
		t.Errorf("expected %d actor types, got %d", expected, len(AllActorTypes))
	}
}
