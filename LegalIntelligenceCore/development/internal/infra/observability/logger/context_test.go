package logger

import (
	"context"
	"testing"
)

func TestRequestContextFrom_Empty(t *testing.T) {
	rc := RequestContextFrom(context.Background())
	if rc != (RequestContext{}) {
		t.Errorf("expected zero-value RequestContext, got %+v", rc)
	}
}

func TestRequestContextFrom_NilContext(t *testing.T) {
	rc := RequestContextFrom(nil) //nolint:staticcheck // intentional nil-ctx safety check
	if rc != (RequestContext{}) {
		t.Errorf("expected zero-value RequestContext for nil ctx, got %+v", rc)
	}
}

func TestWithRequestContext_RoundTrip(t *testing.T) {
	want := RequestContext{
		CorrelationID:   "corr-1",
		JobID:           "job-1",
		DocumentID:      "doc-1",
		VersionID:       "ver-1",
		OrganizationID:  "org-1",
		CreatedByUserID: "user-1",
		MessageID:       "msg-1",
	}
	ctx := WithRequestContext(context.Background(), want)
	got := RequestContextFrom(ctx)
	if got != want {
		t.Errorf("round-trip mismatch:\n got  %+v\n want %+v", got, want)
	}
}

func TestWithRequestContext_OverwritePreservesNothing(t *testing.T) {
	// Sanity: a second WithRequestContext fully replaces the prior one
	// (no merge). This is intentional — ingress sets it once.
	first := RequestContext{JobID: "job-A", CorrelationID: "corr-A"}
	ctx := WithRequestContext(context.Background(), first)
	second := RequestContext{JobID: "job-B"}
	ctx = WithRequestContext(ctx, second)

	got := RequestContextFrom(ctx)
	if got != second {
		t.Errorf("overwrite semantics broken: got %+v want %+v", got, second)
	}
}
