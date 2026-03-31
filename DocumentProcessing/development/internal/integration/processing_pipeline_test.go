//go:build integration

package integration

import (
	"encoding/json"
	"testing"

	"contractpro/document-processing/internal/domain/model"
	"contractpro/document-processing/internal/domain/port"
)

// ---------------------------------------------------------------------------
// Test 1: Happy path — text PDF, no warnings
// ---------------------------------------------------------------------------

func TestProcessingPipeline_HappyPath_TextPDF(t *testing.T) {
	h := newTestHarness(t)

	cmd := defaultCommand()
	body := mustMarshal(t, cmd)

	err := h.broker.deliverToTopic(testTopicProcessDocument, body)
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

	t.Run("processing_completed_event", func(t *testing.T) {
		if got := len(h.publisher.processingCompleted); got != 1 {
			t.Fatalf("expected 1 ProcessingCompletedEvent, got %d", got)
		}
		evt := h.publisher.processingCompleted[0]
		assertEqual(t, "JobID", cmd.JobID, evt.JobID)
		assertEqual(t, "DocumentID", cmd.DocumentID, evt.DocumentID)
		assertEqual(t, "Status", string(model.StatusCompleted), string(evt.Status))
		assertFalse(t, "HasWarnings", evt.HasWarnings)
		assertIntEqual(t, "WarningCount", 0, evt.WarningCount)
	})

	t.Run("no_processing_failed_events", func(t *testing.T) {
		assertIntEqual(t, "ProcessingFailedEvents", 0, len(h.publisher.processingFailed))
	})

	t.Run("artifacts_sent_to_dm", func(t *testing.T) {
		if got := len(h.dmSender.sentArtifacts); got != 1 {
			t.Fatalf("expected 1 sentArtifacts, got %d", got)
		}
		art := h.dmSender.sentArtifacts[0]
		assertEqual(t, "JobID", cmd.JobID, art.JobID)
		assertEqual(t, "DocumentID", cmd.DocumentID, art.DocumentID)
		assertEqual(t, "VersionID", cmd.VersionID, art.VersionID)
		assertEqual(t, "OCRRaw.Status", string(model.OCRStatusNotApplicable), string(art.OCRRaw.Status))

		if len(art.Text.Pages) == 0 {
			t.Error("expected Text.Pages to be non-empty")
		}
		if len(art.Structure.Sections) == 0 {
			t.Error("expected Structure.Sections to be non-empty")
		}
		if art.SemanticTree.Root == nil {
			t.Error("expected SemanticTree.Root to be non-nil")
		}
		if len(art.Warnings) != 0 {
			t.Errorf("expected 0 Warnings, got %d", len(art.Warnings))
		}
	})

	t.Run("temp_storage_cleaned_up", func(t *testing.T) {
		assertContains(t, "deletedPrefixes", h.tempStorage.deletedPrefixes, "job-integ-1")
	})

	t.Run("idempotency_completed", func(t *testing.T) {
		assertIdempotencyCompleted(t, h.idempotency, "job-integ-1")
	})
}

// ---------------------------------------------------------------------------
// Test 2: Happy path with warnings
// ---------------------------------------------------------------------------

func TestProcessingPipeline_HappyPath_WithWarnings(t *testing.T) {
	h := newTestHarness(t)
	h.textExtract.warnings = []model.ProcessingWarning{
		{Code: "EMPTY_PAGE", Message: "Page 3 is empty", Stage: model.ProcessingStageTextExtraction},
	}

	cmd := defaultCommand()
	body := mustMarshal(t, cmd)

	err := h.broker.deliverToTopic(testTopicProcessDocument, body)
	if err != nil {
		t.Fatalf("deliverToTopic returned error: %v", err)
	}

	t.Run("status_transitions", func(t *testing.T) {
		if got := len(h.publisher.statusChanged); got != 2 {
			t.Fatalf("expected 2 StatusChangedEvents, got %d", got)
		}
		assertStatus(t, "first", h.publisher.statusChanged[0], model.StatusQueued, model.StatusInProgress)
		assertStatus(t, "second", h.publisher.statusChanged[1], model.StatusInProgress, model.StatusCompletedWithWarnings)
	})

	t.Run("processing_completed_event", func(t *testing.T) {
		if got := len(h.publisher.processingCompleted); got != 1 {
			t.Fatalf("expected 1 ProcessingCompletedEvent, got %d", got)
		}
		evt := h.publisher.processingCompleted[0]
		assertEqual(t, "Status", string(model.StatusCompletedWithWarnings), string(evt.Status))
		assertTrue(t, "HasWarnings", evt.HasWarnings)
		assertIntEqual(t, "WarningCount", 1, evt.WarningCount)
	})

	t.Run("artifacts_contain_warnings", func(t *testing.T) {
		if got := len(h.dmSender.sentArtifacts); got != 1 {
			t.Fatalf("expected 1 sentArtifacts, got %d", got)
		}
		if got := len(h.dmSender.sentArtifacts[0].Warnings); got != 1 {
			t.Errorf("expected 1 warning in artifacts, got %d", got)
		}
	})

	t.Run("temp_storage_cleaned_up", func(t *testing.T) {
		assertContains(t, "deletedPrefixes", h.tempStorage.deletedPrefixes, "job-integ-1")
	})
}

// ---------------------------------------------------------------------------
// Test 3: Validation rejected
// ---------------------------------------------------------------------------

func TestProcessingPipeline_ValidationRejected(t *testing.T) {
	h := newTestHarness(t)
	h.validator.err = port.NewInvalidFormatError("invalid mime type")

	cmd := defaultCommand()
	body := mustMarshal(t, cmd)

	err := h.broker.deliverToTopic(testTopicProcessDocument, body)
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

	t.Run("processing_failed_event", func(t *testing.T) {
		if got := len(h.publisher.processingFailed); got != 1 {
			t.Fatalf("expected 1 ProcessingFailedEvent, got %d", got)
		}
		evt := h.publisher.processingFailed[0]
		assertEqual(t, "Status", string(model.StatusRejected), string(evt.Status))
		assertEqual(t, "ErrorCode", port.ErrCodeInvalidFormat, evt.ErrorCode)
		assertFalse(t, "IsRetryable", evt.IsRetryable)
		assertEqual(t, "FailedAtStage", string(model.ProcessingStageValidatingInput), evt.FailedAtStage)
	})

	t.Run("no_completed_events", func(t *testing.T) {
		assertIntEqual(t, "ProcessingCompletedEvents", 0, len(h.publisher.processingCompleted))
	})

	t.Run("no_artifacts_sent", func(t *testing.T) {
		assertIntEqual(t, "sentArtifacts", 0, len(h.dmSender.sentArtifacts))
	})

	t.Run("temp_storage_cleaned_up", func(t *testing.T) {
		assertContains(t, "deletedPrefixes", h.tempStorage.deletedPrefixes, "job-integ-1")
	})
}

// ---------------------------------------------------------------------------
// Test 4: Fetch error — retryable, exhausts retries → FAILED
// ---------------------------------------------------------------------------

func TestProcessingPipeline_FetchError_Failed(t *testing.T) {
	h := newTestHarness(t, withMaxRetries(2))
	h.fetcher.err = port.NewStorageError("download failed", nil)

	cmd := defaultCommand()
	body := mustMarshal(t, cmd)

	err := h.broker.deliverToTopic(testTopicProcessDocument, body)
	if err != nil {
		t.Fatalf("deliverToTopic returned error: %v", err)
	}

	t.Run("processing_failed_event", func(t *testing.T) {
		if got := len(h.publisher.processingFailed); got != 1 {
			t.Fatalf("expected 1 ProcessingFailedEvent, got %d", got)
		}
		evt := h.publisher.processingFailed[0]
		assertEqual(t, "Status", string(model.StatusFailed), string(evt.Status))
		assertEqual(t, "FailedAtStage", string(model.ProcessingStageFetchingSourceFile), evt.FailedAtStage)
	})

	t.Run("fetcher_retried", func(t *testing.T) {
		h.fetcher.mu.Lock()
		count := h.fetcher.callCount
		h.fetcher.mu.Unlock()
		assertIntEqual(t, "fetcher.callCount", 2, count)
	})

	t.Run("temp_storage_cleaned_up", func(t *testing.T) {
		assertContains(t, "deletedPrefixes", h.tempStorage.deletedPrefixes, "job-integ-1")
	})
}

// ---------------------------------------------------------------------------
// Test 5: Duplicate job is skipped
// ---------------------------------------------------------------------------

func TestProcessingPipeline_DuplicateJob_Skipped(t *testing.T) {
	h := newTestHarness(t)

	cmd := defaultCommand()
	body := mustMarshal(t, cmd)

	// First delivery.
	err := h.broker.deliverToTopic(testTopicProcessDocument, body)
	if err != nil {
		t.Fatalf("first deliverToTopic returned error: %v", err)
	}

	// Second delivery (duplicate).
	err = h.broker.deliverToTopic(testTopicProcessDocument, body)
	if err != nil {
		t.Fatalf("second deliverToTopic returned error: %v", err)
	}

	t.Run("only_one_set_of_status_events", func(t *testing.T) {
		assertIntEqual(t, "StatusChangedEvents", 2, len(h.publisher.statusChanged))
	})

	t.Run("only_one_completed_event", func(t *testing.T) {
		assertIntEqual(t, "ProcessingCompletedEvents", 1, len(h.publisher.processingCompleted))
	})

	t.Run("only_one_artifact_sent", func(t *testing.T) {
		assertIntEqual(t, "sentArtifacts", 1, len(h.dmSender.sentArtifacts))
	})
}

// ---------------------------------------------------------------------------
// Test 6: Invalid JSON is acknowledged (no side effects)
// ---------------------------------------------------------------------------

func TestProcessingPipeline_InvalidJSON_Acknowledged(t *testing.T) {
	h := newTestHarness(t)

	err := h.broker.deliverToTopic(testTopicProcessDocument, []byte("not-valid-json{{{"))
	if err != nil {
		t.Fatalf("deliverToTopic returned error: %v", err)
	}

	t.Run("no_status_events", func(t *testing.T) {
		assertIntEqual(t, "StatusChangedEvents", 0, len(h.publisher.statusChanged))
	})

	t.Run("no_completed_events", func(t *testing.T) {
		assertIntEqual(t, "ProcessingCompletedEvents", 0, len(h.publisher.processingCompleted))
	})

	t.Run("no_failed_events", func(t *testing.T) {
		assertIntEqual(t, "ProcessingFailedEvents", 0, len(h.publisher.processingFailed))
	})

	t.Run("no_artifacts_sent", func(t *testing.T) {
		assertIntEqual(t, "sentArtifacts", 0, len(h.dmSender.sentArtifacts))
	})
}

// ---------------------------------------------------------------------------
// Test 7: Artifact format completeness
// ---------------------------------------------------------------------------

func TestProcessingPipeline_ArtifactFormat_Complete(t *testing.T) {
	h := newTestHarness(t)

	// Override with rich structure data.
	h.structExtract.structure = &model.DocumentStructure{
		DocumentID: "doc-integ-1",
		Sections: []model.Section{
			{
				Number: "1",
				Title:  "Предмет договора",
				Clauses: []model.Clause{
					{Number: "1.1", Content: "Поставщик обязуется передать товар."},
				},
			},
			{
				Number: "2",
				Title:  "Цена и порядок расчётов",
				Clauses: []model.Clause{
					{Number: "2.1", Content: "Общая стоимость составляет 100 000 руб."},
				},
			},
		},
		Appendices: []model.Appendix{
			{Number: "1", Title: "Спецификация", Content: "Перечень товаров."},
		},
		PartyDetails: []model.PartyDetails{
			{
				Name:           "ООО Рога и Копыта",
				INN:            "7701234567",
				Address:        "г. Москва, ул. Примерная, д. 1",
				Representative: "Иванов И.И.",
			},
		},
	}

	// Override semantic tree with richer structure.
	h.treeBuilder.tree = &model.SemanticTree{
		DocumentID: "doc-integ-1",
		Root: &model.SemanticNode{
			ID:   "root",
			Type: model.NodeTypeRoot,
			Children: []*model.SemanticNode{
				{
					ID:      "section-1",
					Type:    model.NodeTypeSection,
					Content: "Предмет договора",
				},
				{
					ID:      "section-2",
					Type:    model.NodeTypeSection,
					Content: "Цена и порядок расчётов",
				},
			},
		},
	}

	cmd := defaultCommand()
	body := mustMarshal(t, cmd)

	err := h.broker.deliverToTopic(testTopicProcessDocument, body)
	if err != nil {
		t.Fatalf("deliverToTopic returned error: %v", err)
	}

	if got := len(h.dmSender.sentArtifacts); got != 1 {
		t.Fatalf("expected 1 sentArtifacts, got %d", got)
	}
	art := h.dmSender.sentArtifacts[0]

	t.Run("event_meta", func(t *testing.T) {
		assertEqual(t, "CorrelationID", cmd.JobID, art.EventMeta.CorrelationID)
		if art.EventMeta.Timestamp.IsZero() {
			t.Error("expected EventMeta.Timestamp to be non-zero")
		}
	})

	t.Run("ocr_raw", func(t *testing.T) {
		assertEqual(t, "OCRRaw.Status", string(model.OCRStatusNotApplicable), string(art.OCRRaw.Status))
	})

	t.Run("text", func(t *testing.T) {
		assertIntEqual(t, "Text.Pages", 2, len(art.Text.Pages))
		for i, p := range art.Text.Pages {
			if p.Text == "" {
				t.Errorf("expected Page[%d].Text to be non-empty", i)
			}
		}
	})

	t.Run("structure_sections", func(t *testing.T) {
		assertIntEqual(t, "Structure.Sections", 2, len(art.Structure.Sections))
	})

	t.Run("structure_appendices", func(t *testing.T) {
		assertIntEqual(t, "Structure.Appendices", 1, len(art.Structure.Appendices))
	})

	t.Run("structure_party_details", func(t *testing.T) {
		if art.Structure.PartyDetails == nil {
			t.Fatal("expected Structure.PartyDetails to be non-nil")
		}
		if len(art.Structure.PartyDetails) == 0 {
			t.Error("expected Structure.PartyDetails to be non-empty")
		}
	})

	t.Run("semantic_tree", func(t *testing.T) {
		if art.SemanticTree.Root == nil {
			t.Fatal("expected SemanticTree.Root to be non-nil")
		}
		assertEqual(t, "Root.Type", string(model.NodeTypeRoot), string(art.SemanticTree.Root.Type))
		if len(art.SemanticTree.Root.Children) == 0 {
			t.Error("expected SemanticTree.Root to have children")
		}
	})

	t.Run("warnings_empty", func(t *testing.T) {
		if len(art.Warnings) != 0 {
			t.Errorf("expected 0 Warnings, got %d", len(art.Warnings))
		}
	})
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	return data
}

func assertEqual(t *testing.T, field, expected, actual string) {
	t.Helper()
	if expected != actual {
		t.Errorf("%s: expected %q, got %q", field, expected, actual)
	}
}

func assertIntEqual(t *testing.T, field string, expected, actual int) {
	t.Helper()
	if expected != actual {
		t.Errorf("%s: expected %d, got %d", field, expected, actual)
	}
}

func assertTrue(t *testing.T, field string, value bool) {
	t.Helper()
	if !value {
		t.Errorf("%s: expected true, got false", field)
	}
}

func assertFalse(t *testing.T, field string, value bool) {
	t.Helper()
	if value {
		t.Errorf("%s: expected false, got true", field)
	}
}

func assertStatus(t *testing.T, label string, evt model.StatusChangedEvent, expectedOld, expectedNew model.JobStatus) {
	t.Helper()
	if evt.OldStatus != expectedOld {
		t.Errorf("%s StatusChanged: OldStatus expected %q, got %q", label, expectedOld, evt.OldStatus)
	}
	if evt.NewStatus != expectedNew {
		t.Errorf("%s StatusChanged: NewStatus expected %q, got %q", label, expectedNew, evt.NewStatus)
	}
}

func assertContains(t *testing.T, field string, slice []string, value string) {
	t.Helper()
	for _, s := range slice {
		if s == value {
			return
		}
	}
	t.Errorf("%s: expected to contain %q, got %v", field, value, slice)
}

func assertIdempotencyCompleted(t *testing.T, store *memoryIdempotencyStore, jobID string) {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	status, ok := store.store[jobID]
	if !ok {
		t.Errorf("idempotency: expected job %q to exist", jobID)
		return
	}
	if status != port.IdempotencyStatusCompleted {
		t.Errorf("idempotency: expected job %q status %q, got %q", jobID, port.IdempotencyStatusCompleted, status)
	}
}
