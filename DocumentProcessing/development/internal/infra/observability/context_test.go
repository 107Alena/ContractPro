package observability

import (
	"context"
	"testing"
)

func TestWithJobContext_and_JobContextFrom_roundTrip(t *testing.T) {
	t.Parallel()

	jc := JobContext{
		JobID:         "job-123",
		DocumentID:    "doc-456",
		CorrelationID: "corr-789",
		Stage:         "text_extraction",
	}

	ctx := WithJobContext(context.Background(), jc)
	got := JobContextFrom(ctx)

	if got != jc {
		t.Errorf("round-trip mismatch:\n  got  %+v\n  want %+v", got, jc)
	}
}

func TestJobContextFrom_emptyContext_returnsZeroValue(t *testing.T) {
	t.Parallel()

	got := JobContextFrom(context.Background())
	want := JobContext{}

	if got != want {
		t.Errorf("expected zero-value JobContext, got %+v", got)
	}
}

func TestWithStage_updatesOnlyStage(t *testing.T) {
	t.Parallel()

	original := JobContext{
		JobID:         "job-100",
		DocumentID:    "doc-200",
		CorrelationID: "corr-300",
		Stage:         "validation",
	}

	ctx := WithJobContext(context.Background(), original)
	ctx = WithStage(ctx, "ocr")

	got := JobContextFrom(ctx)

	if got.JobID != original.JobID {
		t.Errorf("JobID changed: got %q, want %q", got.JobID, original.JobID)
	}
	if got.DocumentID != original.DocumentID {
		t.Errorf("DocumentID changed: got %q, want %q", got.DocumentID, original.DocumentID)
	}
	if got.CorrelationID != original.CorrelationID {
		t.Errorf("CorrelationID changed: got %q, want %q", got.CorrelationID, original.CorrelationID)
	}
	if got.Stage != "ocr" {
		t.Errorf("Stage = %q, want %q", got.Stage, "ocr")
	}
}

func TestWithStage_onEmptyContext_createsJobContextWithOnlyStage(t *testing.T) {
	t.Parallel()

	ctx := WithStage(context.Background(), "structure_extraction")
	got := JobContextFrom(ctx)

	if got.Stage != "structure_extraction" {
		t.Errorf("Stage = %q, want %q", got.Stage, "structure_extraction")
	}
	if got.JobID != "" {
		t.Errorf("JobID should be empty, got %q", got.JobID)
	}
	if got.DocumentID != "" {
		t.Errorf("DocumentID should be empty, got %q", got.DocumentID)
	}
	if got.CorrelationID != "" {
		t.Errorf("CorrelationID should be empty, got %q", got.CorrelationID)
	}
}

func TestWithJobContext_overwritesPreviousContext(t *testing.T) {
	t.Parallel()

	first := JobContext{
		JobID:         "job-1",
		DocumentID:    "doc-1",
		CorrelationID: "corr-1",
		Stage:         "validation",
	}
	second := JobContext{
		JobID:         "job-2",
		DocumentID:    "doc-2",
		CorrelationID: "corr-2",
		Stage:         "ocr",
	}

	ctx := WithJobContext(context.Background(), first)
	ctx = WithJobContext(ctx, second)

	got := JobContextFrom(ctx)
	if got != second {
		t.Errorf("expected second JobContext, got %+v", got)
	}
}

func TestWithStage_multipleCallsUpdateStageSequentially(t *testing.T) {
	t.Parallel()

	jc := JobContext{
		JobID:      "job-seq",
		DocumentID: "doc-seq",
	}

	ctx := WithJobContext(context.Background(), jc)
	ctx = WithStage(ctx, "stage-1")
	ctx = WithStage(ctx, "stage-2")
	ctx = WithStage(ctx, "stage-3")

	got := JobContextFrom(ctx)
	if got.Stage != "stage-3" {
		t.Errorf("Stage = %q, want %q", got.Stage, "stage-3")
	}
	if got.JobID != "job-seq" {
		t.Errorf("JobID should be preserved, got %q", got.JobID)
	}
}
