package dmconfirmation

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"contractpro/document-processing/internal/domain/port"
)

// --- Interface compliance ---

func TestInterfaceCompliance(t *testing.T) {
	var _ port.DMConfirmationAwaiterPort = (*Awaiter)(nil)
}

// --- Constructor ---

func TestNewAwaiter(t *testing.T) {
	a := NewAwaiter()
	if a == nil {
		t.Fatal("NewAwaiter() returned nil")
	}
	if a.entries == nil {
		t.Fatal("NewAwaiter() did not initialize entries map")
	}
}

// --- Register ---

func TestRegister_Success(t *testing.T) {
	a := NewAwaiter()
	err := a.Register("job-1")
	if err != nil {
		t.Fatalf("Register() unexpected error: %v", err)
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	e, ok := a.entries["job-1"]
	if !ok {
		t.Fatal("entry not created")
	}
	if e.done == nil {
		t.Error("done channel is nil")
	}
	if e.result != nil {
		t.Error("result should be nil after register")
	}
}

func TestRegister_EmptyJobID(t *testing.T) {
	a := NewAwaiter()
	err := a.Register("")
	if err == nil {
		t.Fatal("expected error for empty job ID")
	}
	if port.ErrorCode(err) != port.ErrCodeValidation {
		t.Errorf("expected VALIDATION_ERROR, got %s", port.ErrorCode(err))
	}
}

func TestRegister_DuplicateJobID(t *testing.T) {
	a := NewAwaiter()
	if err := a.Register("job-1"); err != nil {
		t.Fatal(err)
	}
	err := a.Register("job-1")
	if err == nil {
		t.Fatal("expected error for duplicate job ID")
	}
	if port.ErrorCode(err) != port.ErrCodeValidation {
		t.Errorf("expected VALIDATION_ERROR, got %s", port.ErrorCode(err))
	}
}

// --- Confirm then Await ---

func TestConfirmThenAwait(t *testing.T) {
	a := NewAwaiter()
	if err := a.Register("job-1"); err != nil {
		t.Fatal(err)
	}

	// Confirm before Await.
	if err := a.Confirm("job-1"); err != nil {
		t.Fatalf("Confirm() unexpected error: %v", err)
	}

	// Await should return immediately (non-blocking).
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	result, err := a.Await(ctx, "job-1")
	if err != nil {
		t.Fatalf("Await() unexpected error: %v", err)
	}
	if result.JobID != "job-1" {
		t.Errorf("expected job-1, got %s", result.JobID)
	}
	if result.Err != nil {
		t.Errorf("expected nil Err on success, got %v", result.Err)
	}

	// Verify cleanup.
	a.mu.Lock()
	_, exists := a.entries["job-1"]
	a.mu.Unlock()
	if exists {
		t.Error("entry should be cleaned up after Await returns")
	}
}

// --- Await then Confirm ---

func TestAwaitThenConfirm(t *testing.T) {
	a := NewAwaiter()
	if err := a.Register("job-1"); err != nil {
		t.Fatal(err)
	}

	var awaitResult port.DMConfirmationResult
	var awaitErr error
	var wg sync.WaitGroup

	// Await in a goroutine.
	wg.Add(1)
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		awaitResult, awaitErr = a.Await(ctx, "job-1")
	}()

	// Confirm after a short delay.
	time.Sleep(30 * time.Millisecond)
	if err := a.Confirm("job-1"); err != nil {
		t.Fatalf("Confirm() unexpected error: %v", err)
	}

	wg.Wait()

	if awaitErr != nil {
		t.Fatalf("Await() unexpected error: %v", awaitErr)
	}
	if awaitResult.JobID != "job-1" {
		t.Errorf("expected job-1, got %s", awaitResult.JobID)
	}
	if awaitResult.Err != nil {
		t.Errorf("expected nil Err on success, got %v", awaitResult.Err)
	}
}

// --- Reject then Await (retryable) ---

func TestRejectThenAwait_Retryable(t *testing.T) {
	a := NewAwaiter()
	if err := a.Register("job-1"); err != nil {
		t.Fatal(err)
	}

	dmErr := port.NewDMArtifactsPersistFailedError("storage timeout", true, nil)
	if err := a.Reject("job-1", dmErr); err != nil {
		t.Fatalf("Reject() unexpected error: %v", err)
	}

	result, err := a.Await(context.Background(), "job-1")
	if err != nil {
		t.Fatalf("Await() unexpected error: %v", err)
	}
	if result.JobID != "job-1" {
		t.Errorf("expected job-1, got %s", result.JobID)
	}
	if result.Err == nil {
		t.Fatal("expected non-nil Err on rejection")
	}
	if !errors.Is(result.Err, dmErr) {
		t.Errorf("expected DM error, got %v", result.Err)
	}
	if !port.IsRetryable(result.Err) {
		t.Error("expected retryable error")
	}
}

// --- Reject then Await (non-retryable) ---

func TestRejectThenAwait_NonRetryable(t *testing.T) {
	a := NewAwaiter()
	if err := a.Register("job-1"); err != nil {
		t.Fatal(err)
	}

	dmErr := port.NewDMArtifactsPersistFailedError("invalid payload", false, nil)
	if err := a.Reject("job-1", dmErr); err != nil {
		t.Fatalf("Reject() unexpected error: %v", err)
	}

	result, err := a.Await(context.Background(), "job-1")
	if err != nil {
		t.Fatalf("Await() unexpected error: %v", err)
	}
	if result.Err == nil {
		t.Fatal("expected non-nil Err on rejection")
	}
	if port.IsRetryable(result.Err) {
		t.Error("expected non-retryable error")
	}
	if port.ErrorCode(result.Err) != port.ErrCodeDMArtifactsPersistFailed {
		t.Errorf("expected DM_ARTIFACTS_PERSIST_FAILED, got %s", port.ErrorCode(result.Err))
	}
}

// --- Await then Reject ---

func TestAwaitThenReject(t *testing.T) {
	a := NewAwaiter()
	if err := a.Register("job-1"); err != nil {
		t.Fatal(err)
	}

	var awaitResult port.DMConfirmationResult
	var awaitErr error
	var wg sync.WaitGroup

	// Await in a goroutine.
	wg.Add(1)
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		awaitResult, awaitErr = a.Await(ctx, "job-1")
	}()

	// Reject after a short delay.
	time.Sleep(30 * time.Millisecond)
	dmErr := port.NewDMArtifactsPersistFailedError("disk full", true, nil)
	if err := a.Reject("job-1", dmErr); err != nil {
		t.Fatalf("Reject() unexpected error: %v", err)
	}

	wg.Wait()

	if awaitErr != nil {
		t.Fatalf("Await() unexpected error: %v", awaitErr)
	}
	if awaitResult.JobID != "job-1" {
		t.Errorf("expected job-1, got %s", awaitResult.JobID)
	}
	if awaitResult.Err == nil {
		t.Fatal("expected non-nil Err on rejection")
	}
	if !errors.Is(awaitResult.Err, dmErr) {
		t.Errorf("expected DM error, got %v", awaitResult.Err)
	}
}

// --- Await context timeout ---

func TestAwait_ContextTimeout(t *testing.T) {
	a := NewAwaiter()
	if err := a.Register("job-1"); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := a.Await(ctx, "job-1")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected DeadlineExceeded, got %v", err)
	}

	// Verify cleanup after timeout.
	a.mu.Lock()
	_, exists := a.entries["job-1"]
	a.mu.Unlock()
	if exists {
		t.Error("entry should be cleaned up after timeout")
	}
}

// --- Cancel unblocks Await ---

func TestCancel_UnblocksAwait(t *testing.T) {
	a := NewAwaiter()
	if err := a.Register("job-1"); err != nil {
		t.Fatal(err)
	}

	var awaitResult port.DMConfirmationResult
	var awaitErr error
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		awaitResult, awaitErr = a.Await(ctx, "job-1")
	}()

	// Cancel after a short delay.
	time.Sleep(30 * time.Millisecond)
	a.Cancel("job-1")

	wg.Wait()

	if awaitErr != nil {
		t.Fatalf("Await after Cancel should not return error, got: %v", awaitErr)
	}
	// When canceled without a result, Await returns a result with just the JobID.
	if awaitResult.JobID != "job-1" {
		t.Errorf("expected job-1, got %s", awaitResult.JobID)
	}
	if awaitResult.Err != nil {
		t.Errorf("expected nil Err after cancel, got %v", awaitResult.Err)
	}
}

// --- Cancel on unknown job ---

func TestCancel_UnknownJob(t *testing.T) {
	a := NewAwaiter()
	// Should not panic on non-existent job.
	a.Cancel("non-existent")
}

// --- Confirm unknown job ---

func TestConfirm_UnknownJob(t *testing.T) {
	a := NewAwaiter()
	err := a.Confirm("non-existent")
	if err == nil {
		t.Fatal("expected error for unknown job")
	}
	if port.ErrorCode(err) != port.ErrCodeValidation {
		t.Errorf("expected VALIDATION_ERROR, got %s", port.ErrorCode(err))
	}
}

// --- Reject unknown job ---

func TestReject_UnknownJob(t *testing.T) {
	a := NewAwaiter()
	err := a.Reject("non-existent", errors.New("some error"))
	if err == nil {
		t.Fatal("expected error for unknown job")
	}
	if port.ErrorCode(err) != port.ErrCodeValidation {
		t.Errorf("expected VALIDATION_ERROR, got %s", port.ErrorCode(err))
	}
}

// --- Reject with nil error ---

func TestReject_NilError(t *testing.T) {
	a := NewAwaiter()
	if err := a.Register("job-1"); err != nil {
		t.Fatal(err)
	}

	err := a.Reject("job-1", nil)
	if err == nil {
		t.Fatal("expected error for nil error argument")
	}
	if port.ErrorCode(err) != port.ErrCodeValidation {
		t.Errorf("expected VALIDATION_ERROR, got %s", port.ErrorCode(err))
	}
}

// --- Confirm idempotent ---

func TestConfirm_Idempotent(t *testing.T) {
	a := NewAwaiter()
	if err := a.Register("job-1"); err != nil {
		t.Fatal(err)
	}

	// First confirm.
	if err := a.Confirm("job-1"); err != nil {
		t.Fatalf("first Confirm() unexpected error: %v", err)
	}

	// Second confirm — should be silently ignored (no error).
	if err := a.Confirm("job-1"); err != nil {
		t.Fatalf("second Confirm() should be no-op, got: %v", err)
	}

	// Verify the result is still the success confirmation.
	a.mu.Lock()
	e := a.entries["job-1"]
	a.mu.Unlock()

	if e.result == nil {
		t.Fatal("result should not be nil after Confirm")
	}
	if e.result.Err != nil {
		t.Errorf("expected nil Err, got %v", e.result.Err)
	}
}

// --- Reject idempotent ---

func TestReject_Idempotent(t *testing.T) {
	a := NewAwaiter()
	if err := a.Register("job-1"); err != nil {
		t.Fatal(err)
	}

	firstErr := port.NewDMArtifactsPersistFailedError("first failure", true, nil)
	secondErr := port.NewDMArtifactsPersistFailedError("second failure", false, nil)

	// First reject.
	if err := a.Reject("job-1", firstErr); err != nil {
		t.Fatalf("first Reject() unexpected error: %v", err)
	}

	// Second reject — should be silently ignored (first result wins).
	if err := a.Reject("job-1", secondErr); err != nil {
		t.Fatalf("second Reject() should be no-op, got: %v", err)
	}

	// Verify the result is the first rejection error.
	a.mu.Lock()
	e := a.entries["job-1"]
	a.mu.Unlock()

	if e.result == nil {
		t.Fatal("result should not be nil after Reject")
	}
	if !errors.Is(e.result.Err, firstErr) {
		t.Errorf("expected first error, got %v", e.result.Err)
	}
}

// --- Concurrent multiple jobs ---

func TestConcurrent_MultipleJobs(t *testing.T) {
	a := NewAwaiter()
	const numJobs = 50

	var wg sync.WaitGroup

	for i := 0; i < numJobs; i++ {
		jobID := fmt.Sprintf("job-%d", i)

		if err := a.Register(jobID); err != nil {
			t.Fatalf("Register(%s) error: %v", jobID, err)
		}

		// Await in one goroutine.
		wg.Add(1)
		go func(jID string) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			result, err := a.Await(ctx, jID)
			if err != nil {
				t.Errorf("Await(%s) error: %v", jID, err)
				return
			}
			if result.JobID != jID {
				t.Errorf("Await(%s) expected jobID %s, got %s", jID, jID, result.JobID)
			}
			if result.Err != nil {
				t.Errorf("Await(%s) expected nil Err, got %v", jID, result.Err)
			}
		}(jobID)

		// Confirm from another goroutine.
		wg.Add(1)
		go func(jID string) {
			defer wg.Done()
			time.Sleep(time.Millisecond)
			if err := a.Confirm(jID); err != nil {
				t.Errorf("Confirm(%s) error: %v", jID, err)
			}
		}(jobID)
	}

	wg.Wait()
}

// --- Additional edge cases ---

func TestAwait_NotRegistered(t *testing.T) {
	a := NewAwaiter()
	_, err := a.Await(context.Background(), "unknown")
	if err == nil {
		t.Fatal("expected error for unregistered job")
	}
	if port.ErrorCode(err) != port.ErrCodeValidation {
		t.Errorf("expected VALIDATION_ERROR, got %s", port.ErrorCode(err))
	}
}

func TestAwait_AlreadyCanceledContext(t *testing.T) {
	a := NewAwaiter()
	if err := a.Register("job-1"); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Already canceled.

	_, err := a.Await(ctx, "job-1")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected Canceled, got %v", err)
	}
}

func TestFullLifecycle_ReRegisterAfterCleanup(t *testing.T) {
	a := NewAwaiter()

	// First lifecycle.
	if err := a.Register("job-1"); err != nil {
		t.Fatal(err)
	}
	if err := a.Confirm("job-1"); err != nil {
		t.Fatal(err)
	}
	result, err := a.Await(context.Background(), "job-1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Err != nil {
		t.Fatalf("expected success, got %v", result.Err)
	}

	// After Await returns, entry is cleaned up. Re-register should succeed.
	if err := a.Register("job-1"); err != nil {
		t.Fatalf("re-register after cleanup should succeed: %v", err)
	}
}

func TestConfirmThenReject_FirstResultWins(t *testing.T) {
	a := NewAwaiter()
	if err := a.Register("job-1"); err != nil {
		t.Fatal(err)
	}

	// Confirm first.
	if err := a.Confirm("job-1"); err != nil {
		t.Fatal(err)
	}

	// Reject after confirm — should be silently ignored.
	dmErr := port.NewDMArtifactsPersistFailedError("too late", true, nil)
	if err := a.Reject("job-1", dmErr); err != nil {
		t.Fatalf("Reject after Confirm should be no-op, got: %v", err)
	}

	result, err := a.Await(context.Background(), "job-1")
	if err != nil {
		t.Fatal(err)
	}
	// The success confirmation should win.
	if result.Err != nil {
		t.Errorf("expected nil Err (confirm wins), got %v", result.Err)
	}
}

func TestRejectThenConfirm_FirstResultWins(t *testing.T) {
	a := NewAwaiter()
	if err := a.Register("job-1"); err != nil {
		t.Fatal(err)
	}

	dmErr := port.NewDMArtifactsPersistFailedError("fail", true, nil)
	if err := a.Reject("job-1", dmErr); err != nil {
		t.Fatal(err)
	}

	// Confirm after reject — should be silently ignored.
	if err := a.Confirm("job-1"); err != nil {
		t.Fatalf("Confirm after Reject should be no-op, got: %v", err)
	}

	result, err := a.Await(context.Background(), "job-1")
	if err != nil {
		t.Fatal(err)
	}
	// The rejection error should win.
	if result.Err == nil {
		t.Fatal("expected non-nil Err (reject wins)")
	}
	if !errors.Is(result.Err, dmErr) {
		t.Errorf("expected DM error, got %v", result.Err)
	}
}

func TestConcurrent_ConfirmAndCancelRace(t *testing.T) {
	// Stress test for the race between Confirm and Cancel.
	for i := 0; i < 100; i++ {
		a := NewAwaiter()
		if err := a.Register("job-1"); err != nil {
			t.Fatal(err)
		}

		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			_ = a.Confirm("job-1")
		}()

		go func() {
			defer wg.Done()
			a.Cancel("job-1")
		}()

		wg.Wait()
	}
}

func TestConcurrent_RejectAndCancelRace(t *testing.T) {
	// Stress test for the race between Reject and Cancel.
	for i := 0; i < 100; i++ {
		a := NewAwaiter()
		if err := a.Register("job-1"); err != nil {
			t.Fatal(err)
		}

		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			_ = a.Reject("job-1", errors.New("fail"))
		}()

		go func() {
			defer wg.Done()
			a.Cancel("job-1")
		}()

		wg.Wait()
	}
}
