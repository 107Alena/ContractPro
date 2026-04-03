package tenant

import (
	"context"
	"errors"
	"testing"

	"contractpro/document-management/internal/domain/port"
)

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

type mockDocExistence struct {
	exists bool
	err    error
	calls  int
}

func (m *mockDocExistence) ExistsByID(_ context.Context, _, _ string) (bool, error) {
	m.calls++
	return m.exists, m.err
}

type mockTenantMetrics struct {
	mismatchCount int
}

func (m *mockTenantMetrics) IncTenantMismatch() { m.mismatchCount++ }

type mockTenantLogger struct {
	warnings []string
}

func (m *mockTenantLogger) Warn(msg string, _ ...any) { m.warnings = append(m.warnings, msg) }

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestVerifyTenantOwnership_EmptyOrgID_Skips(t *testing.T) {
	repo := &mockDocExistence{exists: true}
	metrics := &mockTenantMetrics{}
	logger := &mockTenantLogger{}

	err := VerifyTenantOwnership(context.Background(), repo, metrics, logger, "", "doc-1")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if repo.calls != 0 {
		t.Fatalf("expected 0 repo calls, got %d", repo.calls)
	}
	if metrics.mismatchCount != 0 {
		t.Fatalf("expected 0 mismatch metric, got %d", metrics.mismatchCount)
	}
}

func TestVerifyTenantOwnership_DocumentExists_Success(t *testing.T) {
	repo := &mockDocExistence{exists: true}
	metrics := &mockTenantMetrics{}
	logger := &mockTenantLogger{}

	err := VerifyTenantOwnership(context.Background(), repo, metrics, logger, "org-A", "doc-1")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if repo.calls != 1 {
		t.Fatalf("expected 1 repo call, got %d", repo.calls)
	}
	if metrics.mismatchCount != 0 {
		t.Fatalf("expected 0 mismatch metric, got %d", metrics.mismatchCount)
	}
}

func TestVerifyTenantOwnership_DocumentNotExists_TenantMismatch(t *testing.T) {
	repo := &mockDocExistence{exists: false}
	metrics := &mockTenantMetrics{}
	logger := &mockTenantLogger{}

	err := VerifyTenantOwnership(context.Background(), repo, metrics, logger, "org-B", "doc-1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if port.ErrorCode(err) != port.ErrCodeTenantMismatch {
		t.Fatalf("expected TENANT_MISMATCH, got %s", port.ErrorCode(err))
	}
	if port.IsRetryable(err) {
		t.Fatal("expected non-retryable error")
	}
	if metrics.mismatchCount != 1 {
		t.Fatalf("expected 1 mismatch metric, got %d", metrics.mismatchCount)
	}
	if len(logger.warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(logger.warnings))
	}
}

func TestVerifyTenantOwnership_InfraError_Propagated(t *testing.T) {
	infraErr := port.NewDatabaseError("connection lost", errors.New("timeout"))
	repo := &mockDocExistence{err: infraErr}
	metrics := &mockTenantMetrics{}
	logger := &mockTenantLogger{}

	err := VerifyTenantOwnership(context.Background(), repo, metrics, logger, "org-A", "doc-1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if port.ErrorCode(err) != port.ErrCodeDatabaseFailed {
		t.Fatalf("expected DATABASE_FAILED, got %s", port.ErrorCode(err))
	}
	if metrics.mismatchCount != 0 {
		t.Fatalf("expected 0 mismatch metric (infra error, not mismatch), got %d", metrics.mismatchCount)
	}
}

func TestVerifyTenantOwnership_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	repo := &mockDocExistence{err: port.NewTimeoutError("context cancelled", ctx.Err())}
	metrics := &mockTenantMetrics{}
	logger := &mockTenantLogger{}

	err := VerifyTenantOwnership(ctx, repo, metrics, logger, "org-A", "doc-1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
