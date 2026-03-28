package app

import (
	"context"
	"errors"
	"testing"

	"contractpro/document-processing/internal/domain/model"
	"contractpro/document-processing/internal/domain/port"
	"contractpro/document-processing/internal/infra/observability"
)

// ---------------------------------------------------------------------------
// Mock: mockDMAwaiter
// ---------------------------------------------------------------------------

type mockDMAwaiter struct {
	confirmed []string
	rejected  map[string]error
}

func (m *mockDMAwaiter) Register(jobID string) error { return nil }
func (m *mockDMAwaiter) Await(ctx context.Context, jobID string) (port.DMConfirmationResult, error) {
	return port.DMConfirmationResult{JobID: jobID}, nil
}
func (m *mockDMAwaiter) Confirm(jobID string) error {
	m.confirmed = append(m.confirmed, jobID)
	return nil
}
func (m *mockDMAwaiter) Reject(jobID string, err error) error {
	if m.rejected == nil {
		m.rejected = make(map[string]error)
	}
	m.rejected[jobID] = err
	return nil
}
func (m *mockDMAwaiter) Cancel(jobID string) {}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestHandleArtifactsPersisted_CallsConfirm(t *testing.T) {
	mock := &mockDMAwaiter{}
	logger := observability.NewLogger("error")
	h := newDMResponseHandler(mock, logger)

	event := model.DocumentProcessingArtifactsPersisted{
		JobID:      "job-1",
		DocumentID: "doc-1",
	}

	err := h.HandleArtifactsPersisted(context.Background(), event)
	if err != nil {
		t.Fatalf("HandleArtifactsPersisted returned error: %v", err)
	}

	if len(mock.confirmed) != 1 || mock.confirmed[0] != "job-1" {
		t.Fatalf("expected confirmed=[\"job-1\"], got %v", mock.confirmed)
	}
}

func TestHandleArtifactsPersistFailed_CallsReject_Retryable(t *testing.T) {
	mock := &mockDMAwaiter{}
	logger := observability.NewLogger("error")
	h := newDMResponseHandler(mock, logger)

	event := model.DocumentProcessingArtifactsPersistFailed{
		JobID:        "job-1",
		IsRetryable:  true,
		ErrorMessage: "storage full",
	}

	err := h.HandleArtifactsPersistFailed(context.Background(), event)
	if err != nil {
		t.Fatalf("HandleArtifactsPersistFailed returned error: %v", err)
	}

	rejErr, ok := mock.rejected["job-1"]
	if !ok {
		t.Fatal("expected job-1 to be rejected, but it was not")
	}

	var domErr *port.DomainError
	if !errors.As(rejErr, &domErr) {
		t.Fatalf("expected *DomainError, got %T: %v", rejErr, rejErr)
	}
	if domErr.Code != port.ErrCodeDMArtifactsPersistFailed {
		t.Fatalf("expected code %s, got %s", port.ErrCodeDMArtifactsPersistFailed, domErr.Code)
	}
	if !domErr.Retryable {
		t.Fatal("expected Retryable=true, got false")
	}
}

func TestHandleArtifactsPersistFailed_CallsReject_NonRetryable(t *testing.T) {
	mock := &mockDMAwaiter{}
	logger := observability.NewLogger("error")
	h := newDMResponseHandler(mock, logger)

	event := model.DocumentProcessingArtifactsPersistFailed{
		JobID:        "job-2",
		IsRetryable:  false,
		ErrorMessage: "invalid data",
	}

	err := h.HandleArtifactsPersistFailed(context.Background(), event)
	if err != nil {
		t.Fatalf("HandleArtifactsPersistFailed returned error: %v", err)
	}

	rejErr, ok := mock.rejected["job-2"]
	if !ok {
		t.Fatal("expected job-2 to be rejected, but it was not")
	}

	var domErr *port.DomainError
	if !errors.As(rejErr, &domErr) {
		t.Fatalf("expected *DomainError, got %T: %v", rejErr, rejErr)
	}
	if domErr.Code != port.ErrCodeDMArtifactsPersistFailed {
		t.Fatalf("expected code %s, got %s", port.ErrCodeDMArtifactsPersistFailed, domErr.Code)
	}
	if domErr.Retryable {
		t.Fatal("expected Retryable=false, got true")
	}
}

func TestHandleDiffPersisted_NoError(t *testing.T) {
	mock := &mockDMAwaiter{}
	logger := observability.NewLogger("error")
	h := newDMResponseHandler(mock, logger)

	event := model.DocumentVersionDiffPersisted{
		JobID:      "job-3",
		DocumentID: "doc-3",
	}

	err := h.HandleDiffPersisted(context.Background(), event)
	if err != nil {
		t.Fatalf("HandleDiffPersisted returned error: %v", err)
	}
}

func TestHandleDiffPersistFailed_NoError(t *testing.T) {
	mock := &mockDMAwaiter{}
	logger := observability.NewLogger("error")
	h := newDMResponseHandler(mock, logger)

	event := model.DocumentVersionDiffPersistFailed{
		JobID:        "job-4",
		DocumentID:   "doc-4",
		ErrorMessage: "some failure",
		IsRetryable:  true,
	}

	err := h.HandleDiffPersistFailed(context.Background(), event)
	if err != nil {
		t.Fatalf("HandleDiffPersistFailed returned error: %v", err)
	}
}
