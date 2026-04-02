package observability

import (
	"context"
	"testing"
)

func TestWithEventContext_RoundTrip(t *testing.T) {
	ec := EventContext{
		CorrelationID:  "corr-123",
		JobID:          "job-456",
		DocumentID:     "doc-789",
		VersionID:      "ver-012",
		OrganizationID: "org-345",
		Stage:          "ingestion",
	}

	ctx := WithEventContext(context.Background(), ec)
	got := EventContextFrom(ctx)

	if got != ec {
		t.Fatalf("EventContext round-trip failed: got %+v, want %+v", got, ec)
	}
}

func TestEventContextFrom_Empty(t *testing.T) {
	got := EventContextFrom(context.Background())
	want := EventContext{}
	if got != want {
		t.Fatalf("expected zero EventContext, got %+v", got)
	}
}

func TestWithStage_UpdatesStage(t *testing.T) {
	ec := EventContext{
		CorrelationID: "corr-1",
		JobID:         "job-2",
		Stage:         "old-stage",
	}

	ctx := WithEventContext(context.Background(), ec)
	ctx = WithStage(ctx, "new-stage")

	got := EventContextFrom(ctx)
	if got.Stage != "new-stage" {
		t.Fatalf("Stage not updated: got %q, want %q", got.Stage, "new-stage")
	}
	if got.CorrelationID != "corr-1" {
		t.Fatal("CorrelationID lost after WithStage")
	}
	if got.JobID != "job-2" {
		t.Fatal("JobID lost after WithStage")
	}
}

func TestWithStage_NoExistingContext(t *testing.T) {
	ctx := WithStage(context.Background(), "some-stage")
	got := EventContextFrom(ctx)

	if got.Stage != "some-stage" {
		t.Fatalf("Stage not set: got %q", got.Stage)
	}
	if got.CorrelationID != "" || got.JobID != "" {
		t.Fatal("expected empty fields for new context")
	}
}

func TestEventContext_AllFieldsPreserved(t *testing.T) {
	ec := EventContext{
		CorrelationID:  "c",
		JobID:          "j",
		DocumentID:     "d",
		VersionID:      "v",
		OrganizationID: "o",
		Stage:          "s",
	}

	ctx := WithEventContext(context.Background(), ec)

	// Overwrite with new context to verify isolation.
	ec2 := EventContext{CorrelationID: "c2"}
	ctx2 := WithEventContext(ctx, ec2)

	// Original context unchanged.
	got1 := EventContextFrom(ctx)
	if got1.CorrelationID != "c" {
		t.Fatal("original context was mutated")
	}

	// New context has new value.
	got2 := EventContextFrom(ctx2)
	if got2.CorrelationID != "c2" {
		t.Fatal("new context not set")
	}
}
