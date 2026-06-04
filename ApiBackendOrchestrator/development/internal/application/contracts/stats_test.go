package contracts

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"contractpro/api-orchestrator/internal/egress/dmclient"
)

// ---------------------------------------------------------------------------
// HandleStats — GET /api/v1/contracts/stats (ORCH-TASK-057)
// ---------------------------------------------------------------------------

// fullStats returns a DM stats aggregate exercising every artifact_status that
// maps to a UserProcessingStatus, plus not_started.
func fullStats() *dmclient.DocumentStats {
	by := map[string]int{
		"PENDING_UPLOAD":         1,  // → UPLOADED
		"PENDING_PROCESSING":     2,  // → QUEUED
		"PROCESSING_IN_PROGRESS": 3,  // → PROCESSING
		"ARTIFACTS_READY":        4,  // → ANALYZING
		"ANALYSIS_IN_PROGRESS":   5,  // → ANALYZING (merges)
		"ANALYSIS_READY":         6,  // → GENERATING_REPORTS
		"REPORTS_IN_PROGRESS":    7,  // → GENERATING_REPORTS (merges)
		"FULLY_READY":            8,  // → READY
		"PARTIALLY_AVAILABLE":    9,  // → PARTIALLY_FAILED
		"PROCESSING_FAILED":      10, // → FAILED
		"REJECTED":               11, // → REJECTED
	}
	total := 5 // not_started
	for _, v := range by {
		total += v
	}
	return &dmclient.DocumentStats{ByArtifactStatus: by, NotStarted: 5, Total: total}
}

func statsRequest(query string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/contracts/stats"+query, nil)
	return withAuthContext(r)
}

func TestHandleStats_Success_FullMapping(t *testing.T) {
	dm := &mockDMClient{
		statsFn: func(_ context.Context, _ dmclient.DocumentStatsParams) (*dmclient.DocumentStats, error) {
			return fullStats(), nil
		},
	}
	h := newStatsHandler(dm)

	rr := httptest.NewRecorder()
	h.HandleStats().ServeHTTP(rr, statsRequest(""))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rr.Code, rr.Body.String())
	}

	body := parseJSON(t, rr)
	bps := body["by_processing_status"].(map[string]any)

	want := map[string]float64{
		"uploaded":            1,
		"queued":              2,
		"processing":          3,
		"analyzing":           9,  // 4 + 5
		"awaiting_user_input": 0,  // orchestrator-managed, DM never emits it
		"generating_reports":  13, // 6 + 7
		"ready":               8,
		"partially_failed":    9,
		"failed":              10,
		"rejected":            11,
		"not_started":         5,
	}
	sum := 0.0
	for k, v := range want {
		got, ok := bps[k].(float64)
		if !ok {
			t.Fatalf("by_processing_status[%q] missing", k)
		}
		if got != v {
			t.Errorf("by_processing_status[%q] = %v, want %v", k, got, v)
		}
		sum += v
	}

	// Invariant: sum(by_processing_status) == total.
	total := body["total"].(float64)
	if total != sum {
		t.Errorf("total = %v, want sum %v", total, sum)
	}
	if total != 71 {
		t.Errorf("total = %v, want 71", total)
	}
}

func TestHandleStats_ByRiskLevel_IsNull(t *testing.T) {
	dm := &mockDMClient{
		statsFn: func(_ context.Context, _ dmclient.DocumentStatsParams) (*dmclient.DocumentStats, error) {
			return fullStats(), nil
		},
	}
	h := newStatsHandler(dm)

	rr := httptest.NewRecorder()
	h.HandleStats().ServeHTTP(rr, statsRequest(""))

	// by_risk_level must serialize as JSON null (not {} or omitted).
	if !strings.Contains(rr.Body.String(), `"by_risk_level":null`) {
		t.Errorf("by_risk_level should serialize as null, body: %s", rr.Body.String())
	}
}

func TestHandleStats_EmptyOrg_Zeroes(t *testing.T) {
	dm := &mockDMClient{
		statsFn: func(_ context.Context, _ dmclient.DocumentStatsParams) (*dmclient.DocumentStats, error) {
			return &dmclient.DocumentStats{ByArtifactStatus: map[string]int{}, NotStarted: 0, Total: 0}, nil
		},
	}
	h := newStatsHandler(dm)

	rr := httptest.NewRecorder()
	h.HandleStats().ServeHTTP(rr, statsRequest(""))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for empty org, got %d", rr.Code)
	}
	body := parseJSON(t, rr)
	if body["total"].(float64) != 0 {
		t.Errorf("total = %v, want 0", body["total"])
	}
	bps := body["by_processing_status"].(map[string]any)
	for k, v := range bps {
		if v.(float64) != 0 {
			t.Errorf("by_processing_status[%q] = %v, want 0", k, v)
		}
	}
}

func TestHandleStats_UnknownStatus_BucketedToProcessing(t *testing.T) {
	dm := &mockDMClient{
		statsFn: func(_ context.Context, _ dmclient.DocumentStatsParams) (*dmclient.DocumentStats, error) {
			return &dmclient.DocumentStats{
				ByArtifactStatus: map[string]int{
					"FULLY_READY":     2,
					"SOME_NEW_STATUS": 3, // unrecognized → processing
				},
				NotStarted: 1,
				Total:      6,
			}, nil
		},
	}
	h := newStatsHandler(dm)

	rr := httptest.NewRecorder()
	h.HandleStats().ServeHTTP(rr, statsRequest(""))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	body := parseJSON(t, rr)
	bps := body["by_processing_status"].(map[string]any)
	if bps["processing"].(float64) != 3 {
		t.Errorf("processing = %v, want 3 (stray bucketed here)", bps["processing"])
	}
	if bps["not_started"].(float64) != 1 {
		t.Errorf("not_started = %v, want 1 (stray must NOT land here)", bps["not_started"])
	}
	if bps["ready"].(float64) != 2 {
		t.Errorf("ready = %v, want 2", bps["ready"])
	}
	// Invariant still holds: 2 + 3 + 1 = 6.
	if body["total"].(float64) != 6 {
		t.Errorf("total = %v, want 6", body["total"])
	}
}

func TestHandleStats_IncludeArchived_Forwarded(t *testing.T) {
	tests := []struct {
		query string
		want  bool
	}{
		{"", false},
		{"?include_archived=false", false},
		{"?include_archived=true", true},
		{"?include_archived=1", true},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			dm := &mockDMClient{
				statsFn: func(_ context.Context, _ dmclient.DocumentStatsParams) (*dmclient.DocumentStats, error) {
					return &dmclient.DocumentStats{ByArtifactStatus: map[string]int{}, Total: 0}, nil
				},
			}
			h := newStatsHandler(dm)

			rr := httptest.NewRecorder()
			h.HandleStats().ServeHTTP(rr, statsRequest(tt.query))

			if rr.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", rr.Code)
			}
			if len(dm.statsCalls) != 1 {
				t.Fatalf("expected 1 DM call, got %d", len(dm.statsCalls))
			}
			if dm.statsCalls[0].IncludeArchived != tt.want {
				t.Errorf("IncludeArchived = %v, want %v", dm.statsCalls[0].IncludeArchived, tt.want)
			}
		})
	}
}

func TestHandleStats_InvalidIncludeArchived(t *testing.T) {
	dm := &mockDMClient{}
	h := newStatsHandler(dm)

	rr := httptest.NewRecorder()
	h.HandleStats().ServeHTTP(rr, statsRequest("?include_archived=maybe"))

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
	if len(dm.statsCalls) != 0 {
		t.Errorf("DM must not be called on invalid param, got %d calls", len(dm.statsCalls))
	}
}

func TestHandleStats_NoN1_SingleDMCall(t *testing.T) {
	dm := &mockDMClient{
		statsFn: func(_ context.Context, _ dmclient.DocumentStatsParams) (*dmclient.DocumentStats, error) {
			return fullStats(), nil
		},
	}
	h := newStatsHandler(dm)

	rr := httptest.NewRecorder()
	h.HandleStats().ServeHTTP(rr, statsRequest(""))

	if len(dm.statsCalls) != 1 {
		t.Errorf("expected exactly 1 DM call (no N+1), got %d", len(dm.statsCalls))
	}
}

func TestHandleStats_CacheControl_Private(t *testing.T) {
	dm := &mockDMClient{
		statsFn: func(_ context.Context, _ dmclient.DocumentStatsParams) (*dmclient.DocumentStats, error) {
			return fullStats(), nil
		},
	}
	h := newStatsHandler(dm)

	rr := httptest.NewRecorder()
	h.HandleStats().ServeHTTP(rr, statsRequest(""))

	cc := rr.Header().Get("Cache-Control")
	if !strings.Contains(cc, "private") {
		t.Errorf("Cache-Control = %q, must be private (org-scoped)", cc)
	}
}

func TestHandleStats_FlagOff_FeatureNotAvailable(t *testing.T) {
	dm := &mockDMClient{}
	h := newTestHandler(dm) // statsEnabled=false

	rr := httptest.NewRecorder()
	h.HandleStats().ServeHTTP(rr, statsRequest(""))

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rr.Code)
	}
	body := parseJSON(t, rr)
	if body["error_code"] != "FEATURE_NOT_AVAILABLE" {
		t.Errorf("error_code = %v, want FEATURE_NOT_AVAILABLE", body["error_code"])
	}
	if len(dm.statsCalls) != 0 {
		t.Errorf("DM must not be called when flag off, got %d calls", len(dm.statsCalls))
	}
}

func TestHandleStats_NoAuthContext(t *testing.T) {
	dm := &mockDMClient{}
	h := newStatsHandler(dm)

	rr := httptest.NewRecorder()
	// No auth context attached.
	r := httptest.NewRequest(http.MethodGet, "/api/v1/contracts/stats", nil)
	h.HandleStats().ServeHTTP(rr, r)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
	if len(dm.statsCalls) != 0 {
		t.Errorf("DM must not be called without auth, got %d calls", len(dm.statsCalls))
	}
}

func TestHandleStats_DMError_5xx(t *testing.T) {
	dm := &mockDMClient{
		statsFn: func(_ context.Context, _ dmclient.DocumentStatsParams) (*dmclient.DocumentStats, error) {
			return nil, &dmclient.DMError{Operation: "GetDocumentStats", StatusCode: 503}
		},
	}
	h := newStatsHandler(dm)

	rr := httptest.NewRecorder()
	h.HandleStats().ServeHTTP(rr, statsRequest(""))

	if rr.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rr.Code)
	}
	body := parseJSON(t, rr)
	if body["error_code"] != "DM_UNAVAILABLE" {
		t.Errorf("error_code = %v, want DM_UNAVAILABLE", body["error_code"])
	}
}

func TestHandleStats_DMError_CircuitOpen(t *testing.T) {
	dm := &mockDMClient{
		statsFn: func(_ context.Context, _ dmclient.DocumentStatsParams) (*dmclient.DocumentStats, error) {
			return nil, dmclient.ErrCircuitOpen
		},
	}
	h := newStatsHandler(dm)

	rr := httptest.NewRecorder()
	h.HandleStats().ServeHTTP(rr, statsRequest(""))

	if rr.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rr.Code)
	}
}

func TestHandleStats_UpdatedAt_RFC3339(t *testing.T) {
	dm := &mockDMClient{
		statsFn: func(_ context.Context, _ dmclient.DocumentStatsParams) (*dmclient.DocumentStats, error) {
			return fullStats(), nil
		},
	}
	h := newStatsHandler(dm)

	rr := httptest.NewRecorder()
	h.HandleStats().ServeHTTP(rr, statsRequest(""))

	body := parseJSON(t, rr)
	updatedAt, ok := body["updated_at"].(string)
	if !ok {
		t.Fatal("updated_at should be a string")
	}
	if _, err := time.Parse(time.RFC3339, updatedAt); err != nil {
		t.Errorf("updated_at not RFC3339: %v", updatedAt)
	}
}

// TestMapDocumentStatsToContractStats_TotalMismatch verifies the mapper reports
// a divergence between the recomputed total and the DM-provided total.
func TestMapDocumentStatsToContractStats_TotalMismatch(t *testing.T) {
	stats := dmclient.DocumentStats{
		ByArtifactStatus: map[string]int{"FULLY_READY": 2},
		NotStarted:       1,
		Total:            99, // deliberately wrong
	}
	result, unknown, mismatch := mapDocumentStatsToContractStats(stats, time.Now())
	if result.Total != 3 {
		t.Errorf("computed total = %d, want 3", result.Total)
	}
	if len(unknown) != 0 {
		t.Errorf("unknown = %v, want none", unknown)
	}
	if !mismatch {
		t.Error("expected totalMismatch=true when DM total diverges")
	}
}
