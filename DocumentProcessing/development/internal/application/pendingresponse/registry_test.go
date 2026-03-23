package pendingresponse

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"contractpro/document-processing/internal/domain/model"
	"contractpro/document-processing/internal/domain/port"
)

// --- Helpers ---

func makeTree(docID string) model.SemanticTree {
	return model.SemanticTree{
		DocumentID: docID,
		Root: &model.SemanticNode{
			ID:       "root",
			Type:     model.NodeTypeRoot,
			Content:  "test",
			Children: nil,
		},
	}
}

// --- Interface compliance ---

func TestInterfaceCompliance(t *testing.T) {
	var _ port.PendingResponseRegistryPort = (*Registry)(nil)
}

// --- Constructor ---

func TestNew(t *testing.T) {
	r := New()
	if r == nil {
		t.Fatal("New() returned nil")
	}
	if r.entries == nil || r.correlationIdx == nil {
		t.Fatal("New() did not initialize maps")
	}
}

// --- Register ---

func TestRegister_Success(t *testing.T) {
	r := New()
	err := r.Register("job-1", []string{"corr-a", "corr-b"})
	if err != nil {
		t.Fatalf("Register() unexpected error: %v", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.entries["job-1"]; !ok {
		t.Error("entry not created")
	}
	if r.correlationIdx["corr-a"] != "job-1" {
		t.Error("reverse index for corr-a not set")
	}
	if r.correlationIdx["corr-b"] != "job-1" {
		t.Error("reverse index for corr-b not set")
	}
}

func TestRegister_EmptyJobID(t *testing.T) {
	r := New()
	err := r.Register("", []string{"corr-a"})
	if err == nil {
		t.Fatal("expected error for empty job ID")
	}
	if port.ErrorCode(err) != port.ErrCodeValidation {
		t.Errorf("expected VALIDATION_ERROR, got %s", port.ErrorCode(err))
	}
}

func TestRegister_EmptyCorrelationIDs(t *testing.T) {
	r := New()
	err := r.Register("job-1", nil)
	if err == nil {
		t.Fatal("expected error for nil correlation IDs")
	}

	err = r.Register("job-2", []string{})
	if err == nil {
		t.Fatal("expected error for empty correlation IDs")
	}
}

func TestRegister_EmptyCorrelationIDInList(t *testing.T) {
	r := New()
	err := r.Register("job-1", []string{"corr-a", ""})
	if err == nil {
		t.Fatal("expected error for empty correlation ID in list")
	}
	if port.ErrorCode(err) != port.ErrCodeValidation {
		t.Errorf("expected VALIDATION_ERROR, got %s", port.ErrorCode(err))
	}
}

func TestRegister_DuplicateCorrelationID(t *testing.T) {
	r := New()
	err := r.Register("job-1", []string{"corr-a", "corr-a"})
	if err == nil {
		t.Fatal("expected error for duplicate correlation ID")
	}
	if port.ErrorCode(err) != port.ErrCodeValidation {
		t.Errorf("expected VALIDATION_ERROR, got %s", port.ErrorCode(err))
	}
}

func TestRegister_CorrelationIDAlreadyInUse(t *testing.T) {
	r := New()
	if err := r.Register("job-1", []string{"corr-a"}); err != nil {
		t.Fatal(err)
	}
	err := r.Register("job-2", []string{"corr-a"})
	if err == nil {
		t.Fatal("expected error for correlation ID already in use")
	}
}

func TestRegister_JobAlreadyRegistered(t *testing.T) {
	r := New()
	if err := r.Register("job-1", []string{"corr-a"}); err != nil {
		t.Fatal(err)
	}
	err := r.Register("job-1", []string{"corr-b"})
	if err == nil {
		t.Fatal("expected error for job already registered")
	}
}

func TestRegister_NoStateMutationOnValidationError(t *testing.T) {
	r := New()
	// Register job-1 with corr-a
	if err := r.Register("job-1", []string{"corr-a"}); err != nil {
		t.Fatal(err)
	}
	// Try to register job-2 with corr-a (already in use) and corr-b.
	// This should fail without adding corr-b to the index.
	err := r.Register("job-2", []string{"corr-b", "corr-a"})
	if err == nil {
		t.Fatal("expected error")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.correlationIdx["corr-b"]; ok {
		t.Error("corr-b should not be in index after failed registration")
	}
	if _, ok := r.entries["job-2"]; ok {
		t.Error("job-2 entry should not exist after failed registration")
	}
}

// --- Receive ---

func TestReceive_Success(t *testing.T) {
	r := New()
	if err := r.Register("job-1", []string{"corr-a"}); err != nil {
		t.Fatal(err)
	}

	tree := makeTree("doc-1")
	err := r.Receive("corr-a", tree)
	if err != nil {
		t.Fatalf("Receive() unexpected error: %v", err)
	}

	r.mu.Lock()
	e := r.entries["job-1"]
	resp := e.responses["corr-a"]
	r.mu.Unlock()

	if resp.Tree == nil {
		t.Fatal("response tree is nil")
	}
	if resp.Tree.DocumentID != "doc-1" {
		t.Errorf("expected doc-1, got %s", resp.Tree.DocumentID)
	}
	if resp.Err != nil {
		t.Errorf("expected nil error, got %v", resp.Err)
	}
}

func TestReceive_UnknownCorrelationID(t *testing.T) {
	r := New()
	err := r.Receive("unknown", makeTree("doc-1"))
	if err == nil {
		t.Fatal("expected error for unknown correlation ID")
	}
	if port.ErrorCode(err) != port.ErrCodeValidation {
		t.Errorf("expected VALIDATION_ERROR, got %s", port.ErrorCode(err))
	}
}

func TestReceive_Idempotent(t *testing.T) {
	r := New()
	if err := r.Register("job-1", []string{"corr-a"}); err != nil {
		t.Fatal(err)
	}

	tree1 := makeTree("doc-1")
	tree2 := makeTree("doc-2")
	if err := r.Receive("corr-a", tree1); err != nil {
		t.Fatal(err)
	}
	// Second receive with different tree — should be ignored.
	if err := r.Receive("corr-a", tree2); err != nil {
		t.Fatal(err)
	}

	r.mu.Lock()
	resp := r.entries["job-1"].responses["corr-a"]
	r.mu.Unlock()

	if resp.Tree.DocumentID != "doc-1" {
		t.Errorf("expected doc-1 (first receive), got %s", resp.Tree.DocumentID)
	}
}

func TestReceive_TreeIsDeepCopied(t *testing.T) {
	r := New()
	if err := r.Register("job-1", []string{"corr-a"}); err != nil {
		t.Fatal(err)
	}

	tree := model.SemanticTree{
		DocumentID: "doc-1",
		Root: &model.SemanticNode{
			ID:       "root",
			Type:     model.NodeTypeRoot,
			Content:  "root content",
			Metadata: map[string]string{"key": "value"},
			Children: []*model.SemanticNode{
				{ID: "child-1", Type: model.NodeTypeSection, Content: "child"},
			},
		},
	}
	if err := r.Receive("corr-a", tree); err != nil {
		t.Fatal(err)
	}

	// Mutate everything on the original — none should affect stored copy.
	tree.DocumentID = "mutated"
	tree.Root.Content = "mutated"
	tree.Root.Metadata["key"] = "mutated"
	tree.Root.Children[0].Content = "mutated"

	r.mu.Lock()
	resp := r.entries["job-1"].responses["corr-a"]
	r.mu.Unlock()

	if resp.Tree.DocumentID != "doc-1" {
		t.Error("DocumentID was mutated")
	}
	if resp.Tree.Root.Content != "root content" {
		t.Error("Root.Content was mutated")
	}
	if resp.Tree.Root.Metadata["key"] != "value" {
		t.Error("Root.Metadata was mutated")
	}
	if resp.Tree.Root.Children[0].Content != "child" {
		t.Error("Root.Children[0].Content was mutated")
	}
}

// --- ReceiveError ---

func TestReceiveError_Success(t *testing.T) {
	r := New()
	if err := r.Register("job-1", []string{"corr-a"}); err != nil {
		t.Fatal(err)
	}

	dmErr := port.NewDMVersionNotFoundError("v1", nil)
	err := r.ReceiveError("corr-a", dmErr)
	if err != nil {
		t.Fatalf("ReceiveError() unexpected error: %v", err)
	}

	r.mu.Lock()
	resp := r.entries["job-1"].responses["corr-a"]
	r.mu.Unlock()

	if resp.Tree != nil {
		t.Error("expected nil tree on error response")
	}
	if !errors.Is(resp.Err, dmErr) {
		t.Errorf("expected DM error, got %v", resp.Err)
	}
}

func TestReceiveError_UnknownCorrelationID(t *testing.T) {
	r := New()
	err := r.ReceiveError("unknown", errors.New("fail"))
	if err == nil {
		t.Fatal("expected error for unknown correlation ID")
	}
	if port.ErrorCode(err) != port.ErrCodeValidation {
		t.Errorf("expected VALIDATION_ERROR, got %s", port.ErrorCode(err))
	}
}

func TestReceiveError_Idempotent(t *testing.T) {
	r := New()
	if err := r.Register("job-1", []string{"corr-a"}); err != nil {
		t.Fatal(err)
	}

	err1 := errors.New("first error")
	err2 := errors.New("second error")
	if err := r.ReceiveError("corr-a", err1); err != nil {
		t.Fatal(err)
	}
	if err := r.ReceiveError("corr-a", err2); err != nil {
		t.Fatal(err)
	}

	r.mu.Lock()
	resp := r.entries["job-1"].responses["corr-a"]
	r.mu.Unlock()

	if resp.Err.Error() != "first error" {
		t.Errorf("expected first error, got %v", resp.Err)
	}
}

func TestReceiveError_NilError(t *testing.T) {
	r := New()
	if err := r.Register("job-1", []string{"corr-a"}); err != nil {
		t.Fatal(err)
	}

	err := r.ReceiveError("corr-a", nil)
	if err == nil {
		t.Fatal("expected error for nil error argument")
	}
	if port.ErrorCode(err) != port.ErrCodeValidation {
		t.Errorf("expected VALIDATION_ERROR, got %s", port.ErrorCode(err))
	}
}

// --- AwaitAll ---

func TestAwaitAll_AllReceived(t *testing.T) {
	r := New()
	if err := r.Register("job-1", []string{"corr-b", "corr-a"}); err != nil {
		t.Fatal(err)
	}

	// Receive both before AwaitAll — should return immediately.
	if err := r.Receive("corr-a", makeTree("doc-a")); err != nil {
		t.Fatal(err)
	}
	if err := r.Receive("corr-b", makeTree("doc-b")); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	responses, err := r.AwaitAll(ctx, "job-1")
	if err != nil {
		t.Fatalf("AwaitAll() unexpected error: %v", err)
	}
	if len(responses) != 2 {
		t.Fatalf("expected 2 responses, got %d", len(responses))
	}

	// Verify sorted by CorrelationID.
	if responses[0].CorrelationID != "corr-a" {
		t.Errorf("expected corr-a first, got %s", responses[0].CorrelationID)
	}
	if responses[1].CorrelationID != "corr-b" {
		t.Errorf("expected corr-b second, got %s", responses[1].CorrelationID)
	}

	// Verify cleanup.
	r.mu.Lock()
	_, entryExists := r.entries["job-1"]
	_, corrAExists := r.correlationIdx["corr-a"]
	_, corrBExists := r.correlationIdx["corr-b"]
	r.mu.Unlock()

	if entryExists {
		t.Error("entry should be cleaned up after AwaitAll")
	}
	if corrAExists || corrBExists {
		t.Error("correlation index should be cleaned up after AwaitAll")
	}
}

func TestAwaitAll_AsyncReceive(t *testing.T) {
	r := New()
	if err := r.Register("job-1", []string{"corr-a", "corr-b"}); err != nil {
		t.Fatal(err)
	}

	// Receive responses asynchronously.
	go func() {
		time.Sleep(20 * time.Millisecond)
		_ = r.Receive("corr-a", makeTree("doc-a"))
		time.Sleep(20 * time.Millisecond)
		_ = r.Receive("corr-b", makeTree("doc-b"))
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	responses, err := r.AwaitAll(ctx, "job-1")
	if err != nil {
		t.Fatalf("AwaitAll() unexpected error: %v", err)
	}
	if len(responses) != 2 {
		t.Fatalf("expected 2 responses, got %d", len(responses))
	}
}

func TestAwaitAll_ContextTimeout(t *testing.T) {
	r := New()
	if err := r.Register("job-1", []string{"corr-a", "corr-b"}); err != nil {
		t.Fatal(err)
	}

	// Receive only one — timeout waiting for the second.
	if err := r.Receive("corr-a", makeTree("doc-a")); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := r.AwaitAll(ctx, "job-1")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected DeadlineExceeded, got %v", err)
	}

	// Verify cleanup after timeout.
	r.mu.Lock()
	_, entryExists := r.entries["job-1"]
	r.mu.Unlock()
	if entryExists {
		t.Error("entry should be cleaned up after timeout")
	}
}

func TestAwaitAll_ContextCanceled(t *testing.T) {
	r := New()
	if err := r.Register("job-1", []string{"corr-a"}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	_, err := r.AwaitAll(ctx, "job-1")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected Canceled, got %v", err)
	}
}

func TestAwaitAll_AlreadyCanceledContext(t *testing.T) {
	r := New()
	if err := r.Register("job-1", []string{"corr-a"}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Already canceled.

	_, err := r.AwaitAll(ctx, "job-1")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected Canceled, got %v", err)
	}
}

func TestAwaitAll_NotRegistered(t *testing.T) {
	r := New()
	_, err := r.AwaitAll(context.Background(), "unknown")
	if err == nil {
		t.Fatal("expected error for unregistered job")
	}
	if port.ErrorCode(err) != port.ErrCodeValidation {
		t.Errorf("expected VALIDATION_ERROR, got %s", port.ErrorCode(err))
	}
}

func TestAwaitAll_WithErrorResponse(t *testing.T) {
	r := New()
	if err := r.Register("job-1", []string{"corr-a", "corr-b"}); err != nil {
		t.Fatal(err)
	}

	if err := r.Receive("corr-a", makeTree("doc-a")); err != nil {
		t.Fatal(err)
	}
	dmErr := port.NewDMVersionNotFoundError("v2", nil)
	if err := r.ReceiveError("corr-b", dmErr); err != nil {
		t.Fatal(err)
	}

	responses, err := r.AwaitAll(context.Background(), "job-1")
	if err != nil {
		t.Fatalf("AwaitAll() unexpected error: %v", err)
	}
	if len(responses) != 2 {
		t.Fatalf("expected 2 responses, got %d", len(responses))
	}

	// corr-a: success
	if responses[0].CorrelationID != "corr-a" {
		t.Errorf("expected corr-a first, got %s", responses[0].CorrelationID)
	}
	if responses[0].Tree == nil || responses[0].Err != nil {
		t.Error("corr-a should be success")
	}

	// corr-b: error
	if responses[1].CorrelationID != "corr-b" {
		t.Errorf("expected corr-b second, got %s", responses[1].CorrelationID)
	}
	if responses[1].Tree != nil {
		t.Error("corr-b should have nil tree")
	}
	if !errors.Is(responses[1].Err, dmErr) {
		t.Errorf("expected DM error, got %v", responses[1].Err)
	}
}

func TestAwaitAll_SingleCorrelationID(t *testing.T) {
	r := New()
	if err := r.Register("job-1", []string{"corr-a"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Receive("corr-a", makeTree("doc-a")); err != nil {
		t.Fatal(err)
	}

	responses, err := r.AwaitAll(context.Background(), "job-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(responses) != 1 {
		t.Fatalf("expected 1 response, got %d", len(responses))
	}
	if responses[0].Tree.DocumentID != "doc-a" {
		t.Errorf("expected doc-a, got %s", responses[0].Tree.DocumentID)
	}
}

// --- Cancel ---

func TestCancel_UnblocksAwaitAll(t *testing.T) {
	r := New()
	if err := r.Register("job-1", []string{"corr-a"}); err != nil {
		t.Fatal(err)
	}

	go func() {
		time.Sleep(30 * time.Millisecond)
		r.Cancel("job-1")
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	responses, err := r.AwaitAll(ctx, "job-1")
	// After Cancel, done channel is closed → AwaitAll unblocks.
	// Cleanup already happened in Cancel, so AwaitAll's cleanup is a no-op.
	if err != nil {
		t.Fatalf("AwaitAll after Cancel should not return error, got: %v", err)
	}
	if responses == nil {
		t.Error("expected non-nil empty slice, got nil")
	}
	if len(responses) != 0 {
		t.Errorf("expected 0 responses after cancel, got %d", len(responses))
	}
}

func TestCancel_Idempotent(t *testing.T) {
	r := New()
	if err := r.Register("job-1", []string{"corr-a"}); err != nil {
		t.Fatal(err)
	}

	// Cancel multiple times — no panic.
	r.Cancel("job-1")
	r.Cancel("job-1")
}

func TestCancel_NonExistentJob(t *testing.T) {
	r := New()
	// No panic on unknown job.
	r.Cancel("non-existent")
}

func TestCancel_CleansUpIndex(t *testing.T) {
	r := New()
	if err := r.Register("job-1", []string{"corr-a", "corr-b"}); err != nil {
		t.Fatal(err)
	}

	r.Cancel("job-1")

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.entries["job-1"]; ok {
		t.Error("entry should be removed after Cancel")
	}
	if _, ok := r.correlationIdx["corr-a"]; ok {
		t.Error("corr-a should be removed from index after Cancel")
	}
	if _, ok := r.correlationIdx["corr-b"]; ok {
		t.Error("corr-b should be removed from index after Cancel")
	}
}

// --- Full lifecycle ---

func TestFullLifecycle(t *testing.T) {
	r := New()

	// Register
	err := r.Register("job-1", []string{"corr-a", "corr-b"})
	if err != nil {
		t.Fatal(err)
	}

	// Receive first response
	err = r.Receive("corr-a", makeTree("doc-a"))
	if err != nil {
		t.Fatal(err)
	}

	// Receive second response
	err = r.Receive("corr-b", makeTree("doc-b"))
	if err != nil {
		t.Fatal(err)
	}

	// AwaitAll
	responses, err := r.AwaitAll(context.Background(), "job-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(responses) != 2 {
		t.Fatalf("expected 2, got %d", len(responses))
	}
	if responses[0].Tree.DocumentID != "doc-a" {
		t.Errorf("expected doc-a, got %s", responses[0].Tree.DocumentID)
	}
	if responses[1].Tree.DocumentID != "doc-b" {
		t.Errorf("expected doc-b, got %s", responses[1].Tree.DocumentID)
	}

	// After AwaitAll, job should be cleaned up.
	// Registering the same job again should work.
	err = r.Register("job-1", []string{"corr-c"})
	if err != nil {
		t.Fatalf("re-register after cleanup should succeed: %v", err)
	}
}

func TestLifecycle_RegisterReceiveErrorAwait(t *testing.T) {
	r := New()
	if err := r.Register("job-1", []string{"corr-a"}); err != nil {
		t.Fatal(err)
	}

	dmErr := port.NewDMVersionNotFoundError("v1", nil)
	if err := r.ReceiveError("corr-a", dmErr); err != nil {
		t.Fatal(err)
	}

	responses, err := r.AwaitAll(context.Background(), "job-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(responses) != 1 {
		t.Fatal("expected 1 response")
	}
	if responses[0].Err == nil {
		t.Error("expected error response")
	}
	if port.ErrorCode(responses[0].Err) != port.ErrCodeDMVersionNotFound {
		t.Errorf("expected DM_VERSION_NOT_FOUND, got %s", port.ErrorCode(responses[0].Err))
	}
}

// --- Concurrency ---

func TestConcurrent_MultipleJobs(t *testing.T) {
	r := New()
	const numJobs = 20
	const corrPerJob = 3

	var wg sync.WaitGroup

	for i := 0; i < numJobs; i++ {
		jobID := fmt.Sprintf("job-%d", i)
		corrIDs := make([]string, corrPerJob)
		for j := 0; j < corrPerJob; j++ {
			corrIDs[j] = fmt.Sprintf("corr-%d-%d", i, j)
		}

		if err := r.Register(jobID, corrIDs); err != nil {
			t.Fatalf("Register(%s) error: %v", jobID, err)
		}

		wg.Add(1)
		go func(jID string, cIDs []string) {
			defer wg.Done()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			responses, err := r.AwaitAll(ctx, jID)
			if err != nil {
				t.Errorf("AwaitAll(%s) error: %v", jID, err)
				return
			}
			if len(responses) != corrPerJob {
				t.Errorf("AwaitAll(%s) expected %d responses, got %d", jID, corrPerJob, len(responses))
			}
		}(jobID, corrIDs)

		// Send responses from another goroutine.
		wg.Add(1)
		go func(cIDs []string) {
			defer wg.Done()
			for _, cid := range cIDs {
				time.Sleep(time.Millisecond)
				_ = r.Receive(cid, makeTree(cid))
			}
		}(corrIDs)
	}

	wg.Wait()
}

func TestConcurrent_ReceiveAndAwait(t *testing.T) {
	r := New()
	if err := r.Register("job-1", []string{"corr-a", "corr-b"}); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup

	// AwaitAll in one goroutine.
	wg.Add(1)
	var awaitResponses []port.PendingResponse
	var awaitErr error
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		awaitResponses, awaitErr = r.AwaitAll(ctx, "job-1")
	}()

	// Receive in separate goroutines.
	wg.Add(2)
	go func() {
		defer wg.Done()
		time.Sleep(10 * time.Millisecond)
		_ = r.Receive("corr-a", makeTree("doc-a"))
	}()
	go func() {
		defer wg.Done()
		time.Sleep(20 * time.Millisecond)
		_ = r.Receive("corr-b", makeTree("doc-b"))
	}()

	wg.Wait()

	if awaitErr != nil {
		t.Fatalf("AwaitAll error: %v", awaitErr)
	}
	if len(awaitResponses) != 2 {
		t.Fatalf("expected 2 responses, got %d", len(awaitResponses))
	}
}

func TestConcurrent_CancelDuringAwait(t *testing.T) {
	r := New()
	if err := r.Register("job-1", []string{"corr-a"}); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(30 * time.Millisecond)
		r.Cancel("job-1")
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// AwaitAll should unblock when Cancel is called.
	_, err := r.AwaitAll(ctx, "job-1")
	if err != nil {
		t.Errorf("expected no error after cancel, got: %v", err)
	}

	wg.Wait()
}

func TestConcurrent_ReceiveAfterCancel(t *testing.T) {
	r := New()
	if err := r.Register("job-1", []string{"corr-a"}); err != nil {
		t.Fatal(err)
	}

	r.Cancel("job-1")

	// Receive after cancel → unknown correlation ID.
	err := r.Receive("corr-a", makeTree("doc-a"))
	if err == nil {
		t.Fatal("expected error for receive after cancel")
	}
	if port.ErrorCode(err) != port.ErrCodeValidation {
		t.Errorf("expected VALIDATION_ERROR, got %s", port.ErrorCode(err))
	}
}

func TestConcurrent_ReceiveAfterTimeout(t *testing.T) {
	r := New()
	if err := r.Register("job-1", []string{"corr-a", "corr-b"}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	// AwaitAll will timeout.
	_, err := r.AwaitAll(ctx, "job-1")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded, got %v", err)
	}

	// After timeout cleanup, correlations are gone.
	err = r.Receive("corr-a", makeTree("doc-a"))
	if err == nil {
		t.Fatal("expected error for receive after timeout cleanup")
	}
}

// --- Edge cases ---

func TestReceive_CloseDoneOnLastResponse(t *testing.T) {
	r := New()
	if err := r.Register("job-1", []string{"corr-a", "corr-b"}); err != nil {
		t.Fatal(err)
	}

	if err := r.Receive("corr-a", makeTree("doc-a")); err != nil {
		t.Fatal(err)
	}

	// Done channel should not be closed yet.
	r.mu.Lock()
	e := r.entries["job-1"]
	r.mu.Unlock()

	select {
	case <-e.done:
		t.Error("done should not be closed after receiving only 1 of 2")
	default:
		// OK
	}

	// Receive last response.
	if err := r.Receive("corr-b", makeTree("doc-b")); err != nil {
		t.Fatal(err)
	}

	// Done channel should be closed now.
	select {
	case <-e.done:
		// OK
	default:
		t.Error("done should be closed after receiving all responses")
	}
}

func TestReceiveError_CloseDoneOnLastResponse(t *testing.T) {
	r := New()
	if err := r.Register("job-1", []string{"corr-a"}); err != nil {
		t.Fatal(err)
	}

	r.mu.Lock()
	e := r.entries["job-1"]
	r.mu.Unlock()

	if err := r.ReceiveError("corr-a", errors.New("fail")); err != nil {
		t.Fatal(err)
	}

	select {
	case <-e.done:
		// OK
	default:
		t.Error("done should be closed after receiving error response")
	}
}

func TestAwaitAll_CancelAfterDoneRace(t *testing.T) {
	// Test the race between done closing and Cancel being called.
	for i := 0; i < 100; i++ {
		r := New()
		if err := r.Register("job-1", []string{"corr-a"}); err != nil {
			t.Fatal(err)
		}

		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			_ = r.Receive("corr-a", makeTree("doc-a"))
		}()

		go func() {
			defer wg.Done()
			r.Cancel("job-1")
		}()

		wg.Wait()
	}
}

func TestMultipleJobs_IndependentLifecycles(t *testing.T) {
	r := New()

	if err := r.Register("job-1", []string{"corr-1a"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register("job-2", []string{"corr-2a"}); err != nil {
		t.Fatal(err)
	}

	// Cancel job-1 should not affect job-2.
	r.Cancel("job-1")

	if err := r.Receive("corr-2a", makeTree("doc-2")); err != nil {
		t.Fatal(err)
	}

	responses, err := r.AwaitAll(context.Background(), "job-2")
	if err != nil {
		t.Fatal(err)
	}
	if len(responses) != 1 || responses[0].Tree.DocumentID != "doc-2" {
		t.Error("job-2 response incorrect after job-1 cancel")
	}
}
