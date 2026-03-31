package model

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNewIdempotencyRecord(t *testing.T) {
	rec := NewIdempotencyRecord("dp-artifacts:job-123")

	if rec.Key != "dp-artifacts:job-123" {
		t.Errorf("expected key dp-artifacts:job-123, got %s", rec.Key)
	}
	if rec.Status != IdempotencyStatusProcessing {
		t.Errorf("expected PROCESSING, got %s", rec.Status)
	}
	if rec.ResultSnapshot != "" {
		t.Errorf("expected empty result_snapshot, got %s", rec.ResultSnapshot)
	}
	if rec.CreatedAt.IsZero() {
		t.Error("expected non-zero created_at")
	}
}

func TestIdempotencyRecordMarkCompleted(t *testing.T) {
	rec := NewIdempotencyRecord("key-1")
	rec.MarkCompleted(`{"status":"persisted"}`)

	if rec.Status != IdempotencyStatusCompleted {
		t.Errorf("expected COMPLETED, got %s", rec.Status)
	}
	if rec.ResultSnapshot != `{"status":"persisted"}` {
		t.Errorf("expected result_snapshot, got %s", rec.ResultSnapshot)
	}
	if rec.UpdatedAt.Before(rec.CreatedAt) {
		t.Error("expected updated_at >= created_at")
	}
}

func TestIdempotencyRecordIsStuck(t *testing.T) {
	rec := &IdempotencyRecord{
		Key:       "key-1",
		Status:    IdempotencyStatusProcessing,
		CreatedAt: time.Now().UTC().Add(-5 * time.Minute),
		UpdatedAt: time.Now().UTC().Add(-5 * time.Minute),
	}

	if !rec.IsStuck(4 * time.Minute) {
		t.Error("expected IsStuck=true for 5min old PROCESSING record with 4min threshold")
	}

	if rec.IsStuck(10 * time.Minute) {
		t.Error("expected IsStuck=false for 5min old PROCESSING record with 10min threshold")
	}

	rec.MarkCompleted("")
	if rec.IsStuck(1 * time.Minute) {
		t.Error("expected IsStuck=false for COMPLETED record")
	}
}

func TestIdempotencyRecordJSONRoundTrip(t *testing.T) {
	now := time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC)
	rec := &IdempotencyRecord{
		Key:            "dp-artifacts:job-456",
		Status:         IdempotencyStatusCompleted,
		ResultSnapshot: `{"document_id":"doc-1"}`,
		CreatedAt:      now,
		UpdatedAt:      now.Add(time.Second),
	}

	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var restored IdempotencyRecord
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if restored.Key != rec.Key {
		t.Errorf("key mismatch: %s != %s", restored.Key, rec.Key)
	}
	if restored.Status != rec.Status {
		t.Errorf("status mismatch: %s != %s", restored.Status, rec.Status)
	}
	if restored.ResultSnapshot != rec.ResultSnapshot {
		t.Errorf("result_snapshot mismatch: %s != %s", restored.ResultSnapshot, rec.ResultSnapshot)
	}
}

func TestIdempotencyRecordJSONOmitsEmptySnapshot(t *testing.T) {
	rec := NewIdempotencyRecord("key-1")

	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if _, ok := raw["result_snapshot"]; ok {
		t.Error("expected result_snapshot to be omitted for empty value")
	}
}
