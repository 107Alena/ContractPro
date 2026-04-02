package model

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDLQRecordJSONRoundTrip(t *testing.T) {
	failedAt := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	record := DLQRecord{
		OriginalTopic:   "dp.artifacts.processing-ready",
		OriginalMessage: json.RawMessage(`{"job_id":"job-1","document_id":"doc-1"}`),
		ErrorCode:       "DOCUMENT_NOT_FOUND",
		ErrorMessage:    "document doc-1 not found",
		RetryCount:      3,
		CorrelationID:   "corr-1",
		JobID:           "job-1",
		FailedAt:        failedAt,
		Category:        DLQCategoryIngestion,
	}

	data, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var restored DLQRecord
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if restored.OriginalTopic != record.OriginalTopic {
		t.Errorf("original_topic mismatch: %s != %s", restored.OriginalTopic, record.OriginalTopic)
	}
	if string(restored.OriginalMessage) != string(record.OriginalMessage) {
		t.Errorf("original_message content mismatch")
	}
	if restored.ErrorCode != record.ErrorCode {
		t.Errorf("error_code mismatch: %s != %s", restored.ErrorCode, record.ErrorCode)
	}
	if restored.ErrorMessage != record.ErrorMessage {
		t.Errorf("error_message mismatch")
	}
	if restored.RetryCount != record.RetryCount {
		t.Errorf("retry_count mismatch: %d != %d", restored.RetryCount, record.RetryCount)
	}
	if restored.CorrelationID != record.CorrelationID {
		t.Errorf("correlation_id mismatch: %s != %s", restored.CorrelationID, record.CorrelationID)
	}
	if restored.JobID != record.JobID {
		t.Errorf("job_id mismatch: %s != %s", restored.JobID, record.JobID)
	}
	if !restored.FailedAt.Equal(record.FailedAt) {
		t.Errorf("failed_at mismatch: %v != %v", restored.FailedAt, record.FailedAt)
	}
	if restored.Category != record.Category {
		t.Errorf("category mismatch: %s != %s", restored.Category, record.Category)
	}
}

func TestDLQRecordOriginalMessagePreservation(t *testing.T) {
	// Verify original_message survives round-trip as raw JSON.
	originalMsg := `{"correlation_id":"corr-1","timestamp":"2026-04-01T12:00:00Z","job_id":"job-1","document_id":"doc-1","version_id":"ver-1","ocr_raw":{"pages":[]},"text":{"content":"test"}}`
	record := DLQRecord{
		OriginalTopic:   "dp.artifacts.processing-ready",
		OriginalMessage: json.RawMessage(originalMsg),
		ErrorCode:       "INTERNAL_ERROR",
		ErrorMessage:    "unexpected error",
		RetryCount:      1,
		CorrelationID:   "corr-1",
		JobID:           "job-1",
		FailedAt:        time.Now().UTC(),
		Category:        DLQCategoryIngestion,
	}

	data, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var restored DLQRecord
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if string(restored.OriginalMessage) != originalMsg {
		t.Errorf("original_message was modified during round-trip:\ngot:  %s\nwant: %s",
			string(restored.OriginalMessage), originalMsg)
	}
}

func TestDLQRecordJSONFieldNames(t *testing.T) {
	record := DLQRecord{
		OriginalTopic:   "test.topic",
		OriginalMessage: json.RawMessage(`{}`),
		ErrorCode:       "ERR",
		ErrorMessage:    "msg",
		RetryCount:      0,
		CorrelationID:   "corr-1",
		JobID:           "job-1",
		FailedAt:        time.Now().UTC(),
		Category:        DLQCategoryInvalid,
	}

	data, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	expectedFields := []string{
		"original_topic", "original_message", "error_code", "error_message",
		"retry_count", "correlation_id", "job_id", "failed_at", "category",
	}
	for _, key := range expectedFields {
		if _, ok := raw[key]; !ok {
			t.Errorf("expected JSON key %q to be present", key)
		}
	}
}
