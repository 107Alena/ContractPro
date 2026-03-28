//go:build integration

package integration

import (
	"testing"

	"contractpro/document-processing/internal/domain/model"
	"contractpro/document-processing/internal/domain/port"
)

// ---------------------------------------------------------------------------
// Test 1: Happy path — full comparison pipeline success
// ---------------------------------------------------------------------------

func TestComparisonPipeline_HappyPath(t *testing.T) {
	h := newComparisonHarness(t)

	cmd := defaultCompareCommand()
	body := mustMarshal(t, cmd)

	err := h.broker.deliverToTopic(testTopicCompareVersions, body)
	if err != nil {
		t.Fatalf("deliverToTopic returned error: %v", err)
	}

	t.Run("status_transitions", func(t *testing.T) {
		if got := len(h.publisher.statusChanged); got != 2 {
			t.Fatalf("expected 2 StatusChangedEvents, got %d", got)
		}

		first := h.publisher.statusChanged[0]
		assertStatus(t, "first", first, model.StatusQueued, model.StatusInProgress)

		second := h.publisher.statusChanged[1]
		assertStatus(t, "second", second, model.StatusInProgress, model.StatusCompleted)
	})

	t.Run("comparison_completed_event", func(t *testing.T) {
		if got := len(h.publisher.comparisonCompleted); got != 1 {
			t.Fatalf("expected 1 ComparisonCompletedEvent, got %d", got)
		}
		evt := h.publisher.comparisonCompleted[0]
		assertEqual(t, "JobID", cmd.JobID, evt.JobID)
		assertEqual(t, "DocumentID", cmd.DocumentID, evt.DocumentID)
		assertEqual(t, "BaseVersionID", cmd.BaseVersionID, evt.BaseVersionID)
		assertEqual(t, "TargetVersionID", cmd.TargetVersionID, evt.TargetVersionID)
		assertEqual(t, "Status", string(model.StatusCompleted), string(evt.Status))
	})

	t.Run("no_comparison_failed_events", func(t *testing.T) {
		assertIntEqual(t, "ComparisonFailedEvents", 0, len(h.publisher.comparisonFailed))
	})

	t.Run("tree_requests_recorded", func(t *testing.T) {
		h.treeReq.mu.Lock()
		reqCount := len(h.treeReq.requests)
		h.treeReq.mu.Unlock()
		assertIntEqual(t, "tree_requests", 2, reqCount)
	})

	t.Run("diff_sent_to_dm", func(t *testing.T) {
		h.dmSender.mu.Lock()
		diffCount := len(h.dmSender.sentDiffs)
		h.dmSender.mu.Unlock()
		if diffCount != 1 {
			t.Fatalf("expected 1 sentDiffs, got %d", diffCount)
		}
		h.dmSender.mu.Lock()
		diff := h.dmSender.sentDiffs[0]
		h.dmSender.mu.Unlock()
		if len(diff.TextDiffs) == 0 {
			t.Error("expected TextDiffs to be non-empty")
		}
		if len(diff.StructuralDiffs) == 0 {
			t.Error("expected StructuralDiffs to be non-empty")
		}
	})

	t.Run("idempotency_completed", func(t *testing.T) {
		assertIdempotencyCompleted(t, h.idempotency, cmd.JobID)
	})
}

// ---------------------------------------------------------------------------
// Test 2: Validation error — same version IDs
// ---------------------------------------------------------------------------

func TestComparisonPipeline_ValidationError_SameVersionIDs(t *testing.T) {
	h := newComparisonHarness(t)

	cmd := defaultCompareCommand()
	cmd.TargetVersionID = cmd.BaseVersionID // same as base
	body := mustMarshal(t, cmd)

	err := h.broker.deliverToTopic(testTopicCompareVersions, body)
	if err != nil {
		t.Fatalf("deliverToTopic returned error: %v", err)
	}

	t.Run("status_transitions", func(t *testing.T) {
		if got := len(h.publisher.statusChanged); got != 2 {
			t.Fatalf("expected 2 StatusChangedEvents, got %d", got)
		}
		assertStatus(t, "first", h.publisher.statusChanged[0], model.StatusQueued, model.StatusInProgress)
		assertStatus(t, "second", h.publisher.statusChanged[1], model.StatusInProgress, model.StatusRejected)
	})

	t.Run("comparison_failed_event", func(t *testing.T) {
		if got := len(h.publisher.comparisonFailed); got != 1 {
			t.Fatalf("expected 1 ComparisonFailedEvent, got %d", got)
		}
		evt := h.publisher.comparisonFailed[0]
		assertEqual(t, "Status", string(model.StatusRejected), string(evt.Status))
		assertEqual(t, "ErrorCode", port.ErrCodeValidation, evt.ErrorCode)
		assertFalse(t, "IsRetryable", evt.IsRetryable)
	})

	t.Run("no_completed_events", func(t *testing.T) {
		assertIntEqual(t, "ComparisonCompletedEvents", 0, len(h.publisher.comparisonCompleted))
	})
}

// ---------------------------------------------------------------------------
// Test 3: DM tree error — version not found
// ---------------------------------------------------------------------------

func TestComparisonPipeline_DMTreeError_VersionNotFound(t *testing.T) {
	h := newComparisonHarness(t)

	cmd := defaultCompareCommand()

	// Configure the tree requester to deliver an error for the base tree.
	baseCID := baseCorrelationID(cmd)
	h.treeReq.mu.Lock()
	h.treeReq.errors[baseCID] = port.NewDMVersionNotFoundError(cmd.BaseVersionID, nil)
	h.treeReq.mu.Unlock()

	body := mustMarshal(t, cmd)

	err := h.broker.deliverToTopic(testTopicCompareVersions, body)
	if err != nil {
		t.Fatalf("deliverToTopic returned error: %v", err)
	}

	t.Run("status_transitions", func(t *testing.T) {
		// QUEUED -> IN_PROGRESS, IN_PROGRESS -> REJECTED
		if got := len(h.publisher.statusChanged); got != 2 {
			t.Fatalf("expected 2 StatusChangedEvents, got %d", got)
		}
		assertStatus(t, "first", h.publisher.statusChanged[0], model.StatusQueued, model.StatusInProgress)
		assertStatus(t, "second", h.publisher.statusChanged[1], model.StatusInProgress, model.StatusRejected)
	})

	t.Run("comparison_failed_event", func(t *testing.T) {
		if got := len(h.publisher.comparisonFailed); got != 1 {
			t.Fatalf("expected 1 ComparisonFailedEvent, got %d", got)
		}
		evt := h.publisher.comparisonFailed[0]
		assertEqual(t, "Status", string(model.StatusRejected), string(evt.Status))
		assertEqual(t, "ErrorCode", port.ErrCodeDMVersionNotFound, evt.ErrorCode)
		assertFalse(t, "IsRetryable", evt.IsRetryable)
	})

	t.Run("no_completed_events", func(t *testing.T) {
		assertIntEqual(t, "ComparisonCompletedEvents", 0, len(h.publisher.comparisonCompleted))
	})
}

// ---------------------------------------------------------------------------
// Test 4: Duplicate job is skipped
// ---------------------------------------------------------------------------

func TestComparisonPipeline_DuplicateJob_Skipped(t *testing.T) {
	h := newComparisonHarness(t)

	cmd := defaultCompareCommand()
	body := mustMarshal(t, cmd)

	// First delivery.
	err := h.broker.deliverToTopic(testTopicCompareVersions, body)
	if err != nil {
		t.Fatalf("first deliverToTopic returned error: %v", err)
	}

	// Second delivery (duplicate).
	err = h.broker.deliverToTopic(testTopicCompareVersions, body)
	if err != nil {
		t.Fatalf("second deliverToTopic returned error: %v", err)
	}

	t.Run("only_one_set_of_status_events", func(t *testing.T) {
		assertIntEqual(t, "StatusChangedEvents", 2, len(h.publisher.statusChanged))
	})

	t.Run("only_one_completed_event", func(t *testing.T) {
		assertIntEqual(t, "ComparisonCompletedEvents", 1, len(h.publisher.comparisonCompleted))
	})

	t.Run("only_one_diff_sent", func(t *testing.T) {
		h.dmSender.mu.Lock()
		diffCount := len(h.dmSender.sentDiffs)
		h.dmSender.mu.Unlock()
		assertIntEqual(t, "sentDiffs", 1, diffCount)
	})
}

// ---------------------------------------------------------------------------
// Test 5: Invalid JSON is acknowledged (no side effects)
// ---------------------------------------------------------------------------

func TestComparisonPipeline_InvalidJSON_Acknowledged(t *testing.T) {
	h := newComparisonHarness(t)

	err := h.broker.deliverToTopic(testTopicCompareVersions, []byte("not-valid-json{{{"))
	if err != nil {
		t.Fatalf("deliverToTopic returned error: %v", err)
	}

	t.Run("no_status_events", func(t *testing.T) {
		assertIntEqual(t, "StatusChangedEvents", 0, len(h.publisher.statusChanged))
	})

	t.Run("no_completed_events", func(t *testing.T) {
		assertIntEqual(t, "ComparisonCompletedEvents", 0, len(h.publisher.comparisonCompleted))
	})

	t.Run("no_failed_events", func(t *testing.T) {
		assertIntEqual(t, "ComparisonFailedEvents", 0, len(h.publisher.comparisonFailed))
	})

	t.Run("no_diffs_sent", func(t *testing.T) {
		h.dmSender.mu.Lock()
		diffCount := len(h.dmSender.sentDiffs)
		h.dmSender.mu.Unlock()
		assertIntEqual(t, "sentDiffs", 0, diffCount)
	})
}

// ---------------------------------------------------------------------------
// Test 6: Diff format completeness
// ---------------------------------------------------------------------------

func TestComparisonPipeline_DiffFormat_Complete(t *testing.T) {
	h := newComparisonHarness(t)

	cmd := defaultCompareCommand()
	body := mustMarshal(t, cmd)

	err := h.broker.deliverToTopic(testTopicCompareVersions, body)
	if err != nil {
		t.Fatalf("deliverToTopic returned error: %v", err)
	}

	// Verify diff result content.
	h.dmSender.mu.Lock()
	if got := len(h.dmSender.sentDiffs); got != 1 {
		h.dmSender.mu.Unlock()
		t.Fatalf("expected 1 sentDiffs, got %d", got)
	}
	diff := h.dmSender.sentDiffs[0]
	h.dmSender.mu.Unlock()

	t.Run("text_diffs_present", func(t *testing.T) {
		if len(diff.TextDiffs) == 0 {
			t.Fatal("expected TextDiffs to be non-empty")
		}
		// The default trees have clause-1.1 modified and section-2 added,
		// so we expect at least one text diff entry.
		for i, td := range diff.TextDiffs {
			if td.Type == "" {
				t.Errorf("TextDiffs[%d].Type is empty", i)
			}
		}
	})

	t.Run("structural_diffs_present", func(t *testing.T) {
		if len(diff.StructuralDiffs) == 0 {
			t.Fatal("expected StructuralDiffs to be non-empty")
		}
		for i, sd := range diff.StructuralDiffs {
			if sd.Type == "" {
				t.Errorf("StructuralDiffs[%d].Type is empty", i)
			}
			if sd.NodeID == "" {
				t.Errorf("StructuralDiffs[%d].NodeID is empty", i)
			}
		}
	})

	t.Run("completed_event_counts_match", func(t *testing.T) {
		if got := len(h.publisher.comparisonCompleted); got != 1 {
			t.Fatalf("expected 1 ComparisonCompletedEvent, got %d", got)
		}
		evt := h.publisher.comparisonCompleted[0]
		assertIntEqual(t, "TextDiffCount", len(diff.TextDiffs), evt.TextDiffCount)
		assertIntEqual(t, "StructuralDiffCount", len(diff.StructuralDiffs), evt.StructuralDiffCount)
	})

	t.Run("event_meta", func(t *testing.T) {
		if got := len(h.publisher.comparisonCompleted); got != 1 {
			t.Fatalf("expected 1 ComparisonCompletedEvent, got %d", got)
		}
		evt := h.publisher.comparisonCompleted[0]
		if evt.EventMeta.Timestamp.IsZero() {
			t.Error("expected EventMeta.Timestamp to be non-zero")
		}
		if evt.EventMeta.CorrelationID == "" {
			t.Error("expected EventMeta.CorrelationID to be non-empty")
		}
	})
}

// ---------------------------------------------------------------------------
// Test 7: DM Receiver routing — DiffPersisted → registry → COMPLETED
// ---------------------------------------------------------------------------

func TestComparisonPipeline_DiffPersisted_ViaReceiver_Completed(t *testing.T) {
	h := newComparisonHarnessWithReceiver(t, nil) // nil = success (DiffPersisted)

	cmd := defaultCompareCommand()
	body := mustMarshal(t, cmd)

	err := h.broker.deliverToTopic(testTopicCompareVersions, body)
	if err != nil {
		t.Fatalf("deliverToTopic returned error: %v", err)
	}

	t.Run("status_transitions", func(t *testing.T) {
		if got := len(h.publisher.statusChanged); got != 2 {
			t.Fatalf("expected 2 StatusChangedEvents, got %d", got)
		}
		assertStatus(t, "first", h.publisher.statusChanged[0], model.StatusQueued, model.StatusInProgress)
		assertStatus(t, "second", h.publisher.statusChanged[1], model.StatusInProgress, model.StatusCompleted)
	})

	t.Run("comparison_completed_event", func(t *testing.T) {
		if got := len(h.publisher.comparisonCompleted); got != 1 {
			t.Fatalf("expected 1 ComparisonCompletedEvent, got %d", got)
		}
		evt := h.publisher.comparisonCompleted[0]
		assertEqual(t, "Status", string(model.StatusCompleted), string(evt.Status))
	})

	t.Run("no_failed_events", func(t *testing.T) {
		assertIntEqual(t, "ComparisonFailedEvents", 0, len(h.publisher.comparisonFailed))
	})

	t.Run("diff_sent_to_dm", func(t *testing.T) {
		h.dmSender.mu.Lock()
		diffCount := len(h.dmSender.sentDiffs)
		h.dmSender.mu.Unlock()
		assertIntEqual(t, "sentDiffs", 1, diffCount)
	})
}

// ---------------------------------------------------------------------------
// Test 8: DM Receiver routing — DiffPersistFailed (non-retryable) → registry → FAILED
// ---------------------------------------------------------------------------

func TestComparisonPipeline_DiffPersistFailed_ViaReceiver_Failed(t *testing.T) {
	h := newComparisonHarnessWithReceiver(t, &diffConfirmError{
		ErrorMessage: "permanent DM storage failure",
		IsRetryable:  false,
	})

	cmd := defaultCompareCommand()
	body := mustMarshal(t, cmd)

	err := h.broker.deliverToTopic(testTopicCompareVersions, body)
	if err != nil {
		t.Fatalf("deliverToTopic returned error: %v", err)
	}

	t.Run("status_transitions", func(t *testing.T) {
		if got := len(h.publisher.statusChanged); got != 2 {
			t.Fatalf("expected 2 StatusChangedEvents, got %d", got)
		}
		assertStatus(t, "first", h.publisher.statusChanged[0], model.StatusQueued, model.StatusInProgress)
		assertStatus(t, "second", h.publisher.statusChanged[1], model.StatusInProgress, model.StatusFailed)
	})

	t.Run("comparison_failed_event", func(t *testing.T) {
		if got := len(h.publisher.comparisonFailed); got != 1 {
			t.Fatalf("expected 1 ComparisonFailedEvent, got %d", got)
		}
		evt := h.publisher.comparisonFailed[0]
		assertEqual(t, "Status", string(model.StatusFailed), string(evt.Status))
		assertEqual(t, "ErrorCode", port.ErrCodeDMDiffPersistFailed, evt.ErrorCode)
		assertFalse(t, "IsRetryable", evt.IsRetryable)
	})

	t.Run("no_completed_events", func(t *testing.T) {
		assertIntEqual(t, "ComparisonCompletedEvents", 0, len(h.publisher.comparisonCompleted))
	})
}

// ---------------------------------------------------------------------------
// Test 9: DM Receiver routing — DiffPersistFailed (retryable) → is_retryable passthrough
// ---------------------------------------------------------------------------

func TestComparisonPipeline_DiffPersistFailed_ViaReceiver_Retryable(t *testing.T) {
	h := newComparisonHarnessWithReceiver(t, &diffConfirmError{
		ErrorMessage: "temporary DM storage failure",
		IsRetryable:  true,
	})

	cmd := defaultCompareCommand()
	body := mustMarshal(t, cmd)

	err := h.broker.deliverToTopic(testTopicCompareVersions, body)
	if err != nil {
		t.Fatalf("deliverToTopic returned error: %v", err)
	}

	t.Run("comparison_failed_event_retryable", func(t *testing.T) {
		if got := len(h.publisher.comparisonFailed); got != 1 {
			t.Fatalf("expected 1 ComparisonFailedEvent, got %d", got)
		}
		evt := h.publisher.comparisonFailed[0]
		assertEqual(t, "Status", string(model.StatusFailed), string(evt.Status))
		assertEqual(t, "ErrorCode", port.ErrCodeDMDiffPersistFailed, evt.ErrorCode)
		assertTrue(t, "IsRetryable", evt.IsRetryable)
	})
}
