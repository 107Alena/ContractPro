package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"contractpro/document-management/internal/domain/model"
	"contractpro/document-management/internal/domain/port"
)

func TestDocumentStats_Happy(t *testing.T) {
	d := newTestDeps()
	var capturedOrg string
	var capturedArchived bool
	d.lifecycle.statsFn = func(_ context.Context, orgID string, includeArchived bool) (*port.DocumentStats, error) {
		capturedOrg = orgID
		capturedArchived = includeArchived
		return &port.DocumentStats{
			ByArtifactStatus: map[model.ArtifactStatus]int{
				model.ArtifactStatusPending:    2,
				model.ArtifactStatusFullyReady: 5,
			},
			NotStarted: 3,
			Total:      10,
		}, nil
	}

	rr := doRequest(d.handler, http.MethodGet, "/api/v1/documents/stats", nil)
	require.Equal(t, http.StatusOK, rr.Code)

	// Scope from X-Organization-ID; default include_archived=false.
	assert.Equal(t, "org-1", capturedOrg)
	assert.False(t, capturedArchived)

	var resp documentStatsResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))

	// Every enum status present (0 when absent).
	require.Len(t, resp.ByArtifactStatus, len(model.AllArtifactStatuses))
	for _, st := range model.AllArtifactStatuses {
		_, ok := resp.ByArtifactStatus[string(st)]
		assert.True(t, ok, "missing status key %s", st)
	}
	assert.Equal(t, 2, resp.ByArtifactStatus["PENDING"])
	assert.Equal(t, 5, resp.ByArtifactStatus["FULLY_READY"])
	assert.Equal(t, 0, resp.ByArtifactStatus["REPORTS_READY"])
	assert.Equal(t, 3, resp.NotStarted)
	assert.Equal(t, 10, resp.Total)
}

func TestDocumentStats_IncludeArchived(t *testing.T) {
	d := newTestDeps()
	var capturedArchived bool
	d.lifecycle.statsFn = func(_ context.Context, _ string, includeArchived bool) (*port.DocumentStats, error) {
		capturedArchived = includeArchived
		return &port.DocumentStats{ByArtifactStatus: map[model.ArtifactStatus]int{}}, nil
	}

	rr := doRequest(d.handler, http.MethodGet, "/api/v1/documents/stats?include_archived=true", nil)
	require.Equal(t, http.StatusOK, rr.Code)
	assert.True(t, capturedArchived)
}

func TestDocumentStats_IncludeArchivedNonTrueIsFalse(t *testing.T) {
	d := newTestDeps()
	var capturedArchived bool
	d.lifecycle.statsFn = func(_ context.Context, _ string, includeArchived bool) (*port.DocumentStats, error) {
		capturedArchived = includeArchived
		return &port.DocumentStats{ByArtifactStatus: map[model.ArtifactStatus]int{}}, nil
	}

	rr := doRequest(d.handler, http.MethodGet, "/api/v1/documents/stats?include_archived=1", nil)
	require.Equal(t, http.StatusOK, rr.Code)
	assert.False(t, capturedArchived, "only literal 'true' enables archived")
}

func TestDocumentStats_OrgScoping(t *testing.T) {
	d := newTestDeps()
	var capturedOrg string
	d.lifecycle.statsFn = func(_ context.Context, orgID string, _ bool) (*port.DocumentStats, error) {
		capturedOrg = orgID
		return &port.DocumentStats{ByArtifactStatus: map[model.ArtifactStatus]int{}}, nil
	}

	rr := doRequestWithHeaders(d.handler, http.MethodGet, "/api/v1/documents/stats", nil, map[string]string{
		"X-Organization-ID": "org-XYZ",
		"X-User-ID":         "user-1",
		"X-User-Role":       "admin",
	})
	require.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "org-XYZ", capturedOrg)
}

func TestDocumentStats_ServiceError(t *testing.T) {
	d := newTestDeps()
	d.lifecycle.statsFn = func(_ context.Context, _ string, _ bool) (*port.DocumentStats, error) {
		return nil, port.NewDatabaseError("boom", assert.AnError)
	}

	rr := doRequest(d.handler, http.MethodGet, "/api/v1/documents/stats", nil)
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
}

// TestDocumentStats_RouteNotShadowed proves GET /documents/stats is not captured
// by the GET /documents/{document_id} wildcard route.
func TestDocumentStats_RouteNotShadowed(t *testing.T) {
	d := newTestDeps()
	statsCalled := false
	getDocCalled := false
	d.lifecycle.statsFn = func(_ context.Context, _ string, _ bool) (*port.DocumentStats, error) {
		statsCalled = true
		return &port.DocumentStats{ByArtifactStatus: map[model.ArtifactStatus]int{}}, nil
	}
	d.lifecycle.getDoc = func(_ context.Context, _, _ string) (*model.Document, error) {
		getDocCalled = true
		return model.NewDocument("stats", "org-1", "t", "u"), nil
	}

	rr := doRequest(d.handler, http.MethodGet, "/api/v1/documents/stats", nil)
	require.Equal(t, http.StatusOK, rr.Code)
	assert.True(t, statsCalled, "stats handler must be invoked")
	assert.False(t, getDocCalled, "getDocument must NOT be invoked for /documents/stats")
}
