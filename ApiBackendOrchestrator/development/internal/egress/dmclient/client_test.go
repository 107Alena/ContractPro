package dmclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sony/gobreaker/v2"

	"contractpro/api-orchestrator/internal/config"
	"contractpro/api-orchestrator/internal/infra/observability/logger"
	"contractpro/api-orchestrator/internal/ingress/middleware/auth"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func testCBConfig(threshold int) config.CircuitBreakerConfig {
	return config.CircuitBreakerConfig{
		FailureThreshold: threshold,
		Timeout:          200 * time.Millisecond,
		MaxRequests:      1,
	}
}

func testClientWithServer(t *testing.T, handler http.Handler) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	c := newClient(
		srv.Client(),
		srv.URL,
		testCBConfig(5),
		2*time.Second,
		2*time.Second,
		3,
		10*time.Millisecond, // Fast backoff for tests.
		logger.NewLogger("debug"),
	)
	// Override CheckRedirect on the test server's client too.
	c.httpClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return c, srv
}

// testContext returns a context with auth and logger values populated.
func testContext() context.Context {
	ctx := context.Background()
	ctx = auth.WithAuthContext(ctx, auth.AuthContext{
		UserID:         "user-123",
		OrganizationID: "org-456",
		Role:           auth.RoleLawyer,
	})
	ctx = logger.WithRequestContext(ctx, logger.RequestContext{
		CorrelationID:  "corr-789",
		OrganizationID: "org-456",
		UserID:         "user-123",
	})
	return ctx
}

// assertHeaders checks that the required DM headers are present on a request.
func assertHeaders(t *testing.T, r *http.Request) {
	t.Helper()
	if got := r.Header.Get("X-Organization-ID"); got != "org-456" {
		t.Errorf("X-Organization-ID = %q, want %q", got, "org-456")
	}
	if got := r.Header.Get("X-User-ID"); got != "user-123" {
		t.Errorf("X-User-ID = %q, want %q", got, "user-123")
	}
	if got := r.Header.Get("X-Correlation-ID"); got != "corr-789" {
		t.Errorf("X-Correlation-ID = %q, want %q", got, "corr-789")
	}
}

// ---------------------------------------------------------------------------
// Header propagation tests
// ---------------------------------------------------------------------------

func TestHeaders_PropagatedOnEveryRequest(t *testing.T) {
	var captured http.Header
	c, _ := testClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(DocumentList{Items: []Document{}, Total: 0, Page: 1, Size: 20})
	}))

	ctx := testContext()
	_, err := c.ListDocuments(ctx, ListDocumentsParams{Page: 1, Size: 20})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := captured.Get("X-Organization-ID"); got != "org-456" {
		t.Errorf("X-Organization-ID = %q, want %q", got, "org-456")
	}
	if got := captured.Get("X-User-ID"); got != "user-123" {
		t.Errorf("X-User-ID = %q, want %q", got, "user-123")
	}
	if got := captured.Get("X-Correlation-ID"); got != "corr-789" {
		t.Errorf("X-Correlation-ID = %q, want %q", got, "corr-789")
	}
}

// ---------------------------------------------------------------------------
// CreateDocument tests
// ---------------------------------------------------------------------------

func TestCreateDocument_Success(t *testing.T) {
	c, _ := testClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertHeaders(t, r)
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/documents" {
			t.Errorf("path = %s, want /documents", r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}

		var reqBody CreateDocumentRequest
		json.NewDecoder(r.Body).Decode(&reqBody)
		if reqBody.Title != "My Contract" {
			t.Errorf("title = %q, want %q", reqBody.Title, "My Contract")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(Document{
			DocumentID:     "doc-001",
			OrganizationID: "org-456",
			Title:          "My Contract",
			Status:         "ACTIVE",
		})
	}))

	doc, err := c.CreateDocument(testContext(), CreateDocumentRequest{Title: "My Contract"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doc.DocumentID != "doc-001" {
		t.Errorf("DocumentID = %q, want %q", doc.DocumentID, "doc-001")
	}
	if doc.Title != "My Contract" {
		t.Errorf("Title = %q, want %q", doc.Title, "My Contract")
	}
}

func TestCreateDocument_400_NoRetry(t *testing.T) {
	var calls atomic.Int32
	c, _ := testClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error_code":"VALIDATION_ERROR","message":"title is required"}`))
	}))

	_, err := c.CreateDocument(testContext(), CreateDocumentRequest{})
	if err == nil {
		t.Fatal("expected error")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("calls = %d, want 1 (400 should not be retried)", got)
	}
	var de *DMError
	if !errors.As(err, &de) {
		t.Fatalf("expected DMError, got %T", err)
	}
	if de.StatusCode != http.StatusBadRequest {
		t.Errorf("StatusCode = %d, want 400", de.StatusCode)
	}
	if de.Retryable {
		t.Error("expected Retryable=false for 400")
	}
}

// ---------------------------------------------------------------------------
// GetDocument tests
// ---------------------------------------------------------------------------

func TestGetDocument_Success(t *testing.T) {
	c, _ := testClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertHeaders(t, r)
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/documents/doc-001" {
			t.Errorf("path = %s, want /documents/doc-001", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(DocumentWithCurrentVersion{
			Document: Document{
				DocumentID: "doc-001",
				Title:      "My Contract",
				Status:     "ACTIVE",
			},
		})
	}))

	doc, err := c.GetDocument(testContext(), "doc-001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doc.DocumentID != "doc-001" {
		t.Errorf("DocumentID = %q, want %q", doc.DocumentID, "doc-001")
	}
}

func TestGetDocument_404_NoRetry(t *testing.T) {
	var calls atomic.Int32
	c, _ := testClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error_code":"NOT_FOUND","message":"document not found"}`))
	}))

	_, err := c.GetDocument(testContext(), "nonexistent")
	if err == nil {
		t.Fatal("expected error")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("calls = %d, want 1 (404 should not be retried)", got)
	}
	var de *DMError
	if !errors.As(err, &de) {
		t.Fatalf("expected DMError, got %T", err)
	}
	if de.StatusCode != http.StatusNotFound {
		t.Errorf("StatusCode = %d, want 404", de.StatusCode)
	}
}

func TestGetDocument_409_NoRetry(t *testing.T) {
	var calls atomic.Int32
	c, _ := testClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"error_code":"DOCUMENT_ARCHIVED","message":"archived"}`))
	}))

	_, err := c.GetDocument(testContext(), "doc-archived")
	if err == nil {
		t.Fatal("expected error")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("calls = %d, want 1 (409 should not be retried)", got)
	}
	var de *DMError
	if !errors.As(err, &de) {
		t.Fatalf("expected DMError, got %T", err)
	}
	if de.StatusCode != http.StatusConflict {
		t.Errorf("StatusCode = %d, want 409", de.StatusCode)
	}
	if de.Retryable {
		t.Error("expected Retryable=false for 409")
	}
}

// ---------------------------------------------------------------------------
// Retry on 5xx tests
// ---------------------------------------------------------------------------

func TestRetry_5xxThenSuccess(t *testing.T) {
	var calls atomic.Int32
	c, _ := testClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"error_code":"SERVICE_UNAVAILABLE","message":"try later"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(DocumentList{Items: []Document{}, Total: 0, Page: 1, Size: 20})
	}))

	list, err := c.ListDocuments(testContext(), ListDocumentsParams{Page: 1, Size: 20})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if list.Total != 0 {
		t.Errorf("total = %d, want 0", list.Total)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("calls = %d, want 3 (2 failures + 1 success)", got)
	}
}

func TestRetry_AllRetriesExhausted(t *testing.T) {
	var calls atomic.Int32
	c, _ := testClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error_code":"INTERNAL_ERROR","message":"oops"}`))
	}))

	_, err := c.ListDocuments(testContext(), ListDocumentsParams{Page: 1, Size: 20})
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("calls = %d, want 3 (retryMax=3)", got)
	}
	var de *DMError
	if !errors.As(err, &de) {
		t.Fatalf("expected DMError, got %T", err)
	}
	if de.StatusCode != http.StatusInternalServerError {
		t.Errorf("StatusCode = %d, want 500", de.StatusCode)
	}
	if !de.Retryable {
		t.Error("expected Retryable=true for 500")
	}
}

// ---------------------------------------------------------------------------
// DeleteDocument tests
// ---------------------------------------------------------------------------

func TestDeleteDocument_Success(t *testing.T) {
	c, _ := testClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertHeaders(t, r)
		if r.Method != http.MethodDelete {
			t.Errorf("method = %s, want DELETE", r.Method)
		}
		if r.URL.Path != "/documents/doc-001" {
			t.Errorf("path = %s, want /documents/doc-001", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Document{DocumentID: "doc-001", Status: "DELETED"})
	}))

	doc, err := c.DeleteDocument(testContext(), "doc-001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doc.Status != "DELETED" {
		t.Errorf("Status = %q, want DELETED", doc.Status)
	}
}

// ---------------------------------------------------------------------------
// ArchiveDocument tests
// ---------------------------------------------------------------------------

func TestArchiveDocument_Success(t *testing.T) {
	c, _ := testClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertHeaders(t, r)
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/documents/doc-001/archive" {
			t.Errorf("path = %s, want /documents/doc-001/archive", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Document{DocumentID: "doc-001", Status: "ARCHIVED"})
	}))

	doc, err := c.ArchiveDocument(testContext(), "doc-001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doc.Status != "ARCHIVED" {
		t.Errorf("Status = %q, want ARCHIVED", doc.Status)
	}
}

// ---------------------------------------------------------------------------
// CreateVersion tests
// ---------------------------------------------------------------------------

func TestCreateVersion_Success(t *testing.T) {
	c, _ := testClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertHeaders(t, r)
		if r.URL.Path != "/documents/doc-001/versions" {
			t.Errorf("path = %s, want /documents/doc-001/versions", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}

		var reqBody CreateVersionRequest
		json.NewDecoder(r.Body).Decode(&reqBody)
		if reqBody.SourceFileKey != "uploads/org/doc/file.pdf" {
			t.Errorf("SourceFileKey = %q", reqBody.SourceFileKey)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(DocumentVersion{VersionID: "ver-001", DocumentID: "doc-001"})
	}))

	ver, err := c.CreateVersion(testContext(), "doc-001", CreateVersionRequest{
		SourceFileKey:      "uploads/org/doc/file.pdf",
		SourceFileName:     "contract.pdf",
		SourceFileSize:     1024,
		SourceFileChecksum: "abc123",
		OriginType:         "UPLOAD",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ver.VersionID != "ver-001" {
		t.Errorf("VersionID = %q, want %q", ver.VersionID, "ver-001")
	}
}

// ---------------------------------------------------------------------------
// ListVersions tests
// ---------------------------------------------------------------------------

func TestListVersions_Success(t *testing.T) {
	c, _ := testClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertHeaders(t, r)
		if r.URL.Path != "/documents/doc-001/versions" {
			t.Errorf("path = %s, want /documents/doc-001/versions", r.URL.Path)
		}
		if got := r.URL.Query().Get("page"); got != "2" {
			t.Errorf("page = %q, want 2", got)
		}
		if got := r.URL.Query().Get("size"); got != "10" {
			t.Errorf("size = %q, want 10", got)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(VersionList{Items: []DocumentVersion{}, Total: 0, Page: 2, Size: 10})
	}))

	list, err := c.ListVersions(testContext(), "doc-001", ListVersionsParams{Page: 2, Size: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if list.Page != 2 {
		t.Errorf("Page = %d, want 2", list.Page)
	}
}

// ---------------------------------------------------------------------------
// GetVersion tests
// ---------------------------------------------------------------------------

func TestGetVersion_Success(t *testing.T) {
	c, _ := testClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertHeaders(t, r)
		if r.URL.Path != "/documents/doc-001/versions/ver-001" {
			t.Errorf("path = %s, want /documents/doc-001/versions/ver-001", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(DocumentVersionWithArtifacts{
			DocumentVersion: DocumentVersion{VersionID: "ver-001"},
			Artifacts:       []ArtifactDescriptor{},
		})
	}))

	ver, err := c.GetVersion(testContext(), "doc-001", "ver-001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ver.VersionID != "ver-001" {
		t.Errorf("VersionID = %q, want %q", ver.VersionID, "ver-001")
	}
}

// ---------------------------------------------------------------------------
// ListArtifacts tests
// ---------------------------------------------------------------------------

func TestListArtifacts_WithFilters(t *testing.T) {
	c, _ := testClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertHeaders(t, r)
		if r.URL.Path != "/documents/doc-001/versions/ver-001/artifacts" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("artifact_type"); got != "SEMANTIC_TREE" {
			t.Errorf("artifact_type = %q, want SEMANTIC_TREE", got)
		}
		if got := r.URL.Query().Get("producer_domain"); got != "DP" {
			t.Errorf("producer_domain = %q, want DP", got)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ArtifactDescriptorList{Items: []ArtifactDescriptor{}})
	}))

	list, err := c.ListArtifacts(testContext(), "doc-001", "ver-001", ListArtifactsParams{
		ArtifactType:   "SEMANTIC_TREE",
		ProducerDomain: "DP",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if list.Items == nil {
		t.Error("Items should not be nil")
	}
}

// ---------------------------------------------------------------------------
// GetArtifact tests — JSON (200) and redirect (302)
// ---------------------------------------------------------------------------

func TestGetArtifact_JSON200(t *testing.T) {
	c, _ := testClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertHeaders(t, r)
		if r.URL.Path != "/documents/doc-001/versions/ver-001/artifacts/SEMANTIC_TREE" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"nodes": [{"id": "root"}]}`))
	}))

	resp, err := c.GetArtifact(testContext(), "doc-001", "ver-001", "SEMANTIC_TREE")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content == nil {
		t.Fatal("Content should not be nil for 200")
	}
	if resp.RedirectURL != "" {
		t.Errorf("RedirectURL should be empty for 200, got %q", resp.RedirectURL)
	}

	var parsed map[string]any
	if err := json.Unmarshal(resp.Content, &parsed); err != nil {
		t.Fatalf("failed to parse Content: %v", err)
	}
}

func TestGetArtifact_Redirect302(t *testing.T) {
	c, _ := testClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertHeaders(t, r)
		if r.URL.Path != "/documents/doc-001/versions/ver-001/artifacts/EXPORT_PDF" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Header().Set("Location", "https://storage.example.com/presigned-url")
		w.WriteHeader(http.StatusFound)
	}))

	resp, err := c.GetArtifact(testContext(), "doc-001", "ver-001", "EXPORT_PDF")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.RedirectURL != "https://storage.example.com/presigned-url" {
		t.Errorf("RedirectURL = %q, want presigned URL", resp.RedirectURL)
	}
	if resp.Content != nil {
		t.Errorf("Content should be nil for 302")
	}
}

func TestGetArtifact_302MissingLocation(t *testing.T) {
	c, _ := testClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// 302 without Location header.
		w.WriteHeader(http.StatusFound)
	}))

	_, err := c.GetArtifact(testContext(), "doc-001", "ver-001", "EXPORT_PDF")
	if err == nil {
		t.Fatal("expected error for 302 without Location")
	}
	var de *DMError
	if !errors.As(err, &de) {
		t.Fatalf("expected DMError, got %T", err)
	}
	if de.Retryable {
		t.Error("expected Retryable=false for missing Location")
	}
}

func TestGetArtifact_404_NoRetry(t *testing.T) {
	var calls atomic.Int32
	c, _ := testClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error_code":"NOT_FOUND","message":"artifact not found"}`))
	}))

	_, err := c.GetArtifact(testContext(), "doc-001", "ver-001", "SEMANTIC_TREE")
	if err == nil {
		t.Fatal("expected error")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("calls = %d, want 1", got)
	}
}

// ---------------------------------------------------------------------------
// GetDiff tests
// ---------------------------------------------------------------------------

func TestGetDiff_Success(t *testing.T) {
	c, _ := testClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertHeaders(t, r)
		if r.URL.Path != "/documents/doc-001/diffs/base-ver/target-ver" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(VersionDiff{
			DiffID:          "diff-001",
			DocumentID:      "doc-001",
			BaseVersionID:   "base-ver",
			TargetVersionID: "target-ver",
			TextDiffCount:   3,
		})
	}))

	diff, err := c.GetDiff(testContext(), "doc-001", "base-ver", "target-ver")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if diff.DiffID != "diff-001" {
		t.Errorf("DiffID = %q, want %q", diff.DiffID, "diff-001")
	}
	if diff.TextDiffCount != 3 {
		t.Errorf("TextDiffCount = %d, want 3", diff.TextDiffCount)
	}
}

// ---------------------------------------------------------------------------
// ListAuditRecords tests
// ---------------------------------------------------------------------------

func TestListAuditRecords_Success(t *testing.T) {
	c, _ := testClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertHeaders(t, r)
		if r.URL.Path != "/audit" {
			t.Errorf("path = %s, want /audit", r.URL.Path)
		}
		q := r.URL.Query()
		if got := q.Get("document_id"); got != "doc-001" {
			t.Errorf("document_id = %q, want doc-001", got)
		}
		if got := q.Get("action"); got != "DOCUMENT_CREATED" {
			t.Errorf("action = %q, want DOCUMENT_CREATED", got)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(AuditRecordList{Items: []AuditRecord{}, Total: 0, Page: 1, Size: 20})
	}))

	list, err := c.ListAuditRecords(testContext(), ListAuditParams{
		DocumentID: "doc-001",
		Action:     "DOCUMENT_CREATED",
		Page:       1,
		Size:       20,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if list.Total != 0 {
		t.Errorf("Total = %d, want 0", list.Total)
	}
}

// ---------------------------------------------------------------------------
// Context cancellation tests
// ---------------------------------------------------------------------------

func TestParentContextCanceled_ReturnsImmediately(t *testing.T) {
	var calls atomic.Int32
	c, _ := testClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Document{})
	}))

	ctx, cancel := context.WithCancel(testContext())
	cancel()

	_, err := c.GetDocument(ctx, "doc-001")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if got := calls.Load(); got != 0 {
		t.Errorf("calls = %d, want 0 (should bail before HTTP call)", got)
	}
}

func TestContextCancelledDuringBackoff(t *testing.T) {
	var calls atomic.Int32
	c, _ := testClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error_code":"INTERNAL_ERROR"}`))
	}))
	// Use longer backoff to ensure cancellation happens during wait.
	c.retryBackoff = 5 * time.Second

	ctx, cancel := context.WithCancel(testContext())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	_, err := c.ListDocuments(ctx, ListDocumentsParams{Page: 1, Size: 20})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("calls = %d, want 1 (cancelled during backoff after 1st attempt)", got)
	}
}

// ---------------------------------------------------------------------------
// Circuit breaker tests
// ---------------------------------------------------------------------------

func TestCircuitBreaker_OpensAfterThreshold(t *testing.T) {
	var calls atomic.Int32
	c, _ := testClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error_code":"INTERNAL_ERROR"}`))
	}))
	// Low CB threshold, no retries so each call is 1 attempt.
	c.retryMax = 1
	c.cb = buildTestCB(2)

	ctx := testContext()

	// Trip the CB: 2 failures.
	_, _ = c.GetDocument(ctx, "doc-001")
	_, _ = c.GetDocument(ctx, "doc-001")
	if got := calls.Load(); got != 2 {
		t.Fatalf("pre-trip calls = %d, want 2", got)
	}

	// Third call should be rejected by CB without reaching DM.
	_, err := c.GetDocument(ctx, "doc-001")
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("expected ErrCircuitOpen, got %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("post-trip calls = %d, want 2 (CB should block)", got)
	}
}

func TestCircuitBreaker_4xxDoesNotTrip(t *testing.T) {
	var calls atomic.Int32
	c, _ := testClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error_code":"NOT_FOUND"}`))
	}))
	c.retryMax = 1
	c.cb = buildTestCB(2)

	ctx := testContext()

	// Make 5 calls with 404 — CB should stay closed.
	for i := 0; i < 5; i++ {
		_, _ = c.GetDocument(ctx, "doc-001")
	}
	if got := calls.Load(); got != 5 {
		t.Errorf("calls = %d, want 5 (4xx should not trip CB)", got)
	}
}

func TestCircuitBreaker_HalfOpenRecovery(t *testing.T) {
	var shouldFail atomic.Bool
	shouldFail.Store(true)
	var calls atomic.Int32

	c, _ := testClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		if shouldFail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error_code":"INTERNAL_ERROR"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(DocumentList{Items: []Document{}, Total: 0, Page: 1, Size: 20})
	}))
	c.retryMax = 1
	cbCfg := config.CircuitBreakerConfig{
		FailureThreshold: 2,
		Timeout:          100 * time.Millisecond, // Fast transition to half-open.
		MaxRequests:      1,
	}
	c.cb = buildCB(cbCfg)

	ctx := testContext()

	// Trip to OPEN.
	_, _ = c.ListDocuments(ctx, ListDocumentsParams{Page: 1, Size: 20})
	_, _ = c.ListDocuments(ctx, ListDocumentsParams{Page: 1, Size: 20})

	// Wait for half-open.
	time.Sleep(150 * time.Millisecond)

	// Fix DM and call — should succeed and close CB.
	shouldFail.Store(false)
	list, err := c.ListDocuments(ctx, ListDocumentsParams{Page: 1, Size: 20})
	if err != nil {
		t.Fatalf("expected success in half-open, got %v", err)
	}
	if list == nil {
		t.Fatal("expected non-nil result")
	}

	// CB should be closed now — more calls succeed.
	_, err = c.ListDocuments(ctx, ListDocumentsParams{Page: 1, Size: 20})
	if err != nil {
		t.Fatalf("expected success after CB closed, got %v", err)
	}
}

func TestCircuitBreaker_ErrCircuitOpenNotRetried(t *testing.T) {
	var calls atomic.Int32
	c, _ := testClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error_code":"INTERNAL_ERROR"}`))
	}))
	c.retryMax = 1
	c.cb = buildTestCB(2)

	ctx := testContext()

	// Trip CB.
	_, _ = c.DeleteDocument(ctx, "doc-001")
	_, _ = c.DeleteDocument(ctx, "doc-001")

	calls.Store(0)
	// Now with retries enabled — should still return immediately (no retry on CB open).
	c.retryMax = 3
	_, err := c.DeleteDocument(ctx, "doc-001")
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("expected ErrCircuitOpen, got %v", err)
	}
	if got := calls.Load(); got != 0 {
		t.Errorf("calls = %d, want 0 (CB open, no retry)", got)
	}
}

func TestCircuitBreaker_TripsDuringRetry(t *testing.T) {
	var calls atomic.Int32
	c, _ := testClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error_code":"INTERNAL_ERROR"}`))
	}))
	// CB threshold = 2, high retryMax. CB should trip before retries exhaust.
	c.retryMax = 10
	cbCfg := config.CircuitBreakerConfig{
		FailureThreshold: 2,
		Timeout:          60 * time.Second,
		MaxRequests:      1,
	}
	c.cb = buildCB(cbCfg)

	_, err := c.ListDocuments(testContext(), ListDocumentsParams{Page: 1, Size: 20})
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("expected ErrCircuitOpen after CB trips mid-retry, got %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("calls = %d, want 2 (CB trips after threshold)", got)
	}
}

// ---------------------------------------------------------------------------
// Backoff delay tests
// ---------------------------------------------------------------------------

func TestBackoffDelay_Values(t *testing.T) {
	c := &Client{retryBackoff: 200 * time.Millisecond}

	for i := 0; i < 20; i++ {
		d1 := c.backoffDelay(1)
		if d1 < 200*time.Millisecond || d1 > 250*time.Millisecond {
			t.Errorf("attempt 1: delay = %v, want [200ms, 250ms]", d1)
		}
		d2 := c.backoffDelay(2)
		if d2 < 400*time.Millisecond || d2 > 500*time.Millisecond {
			t.Errorf("attempt 2: delay = %v, want [400ms, 500ms]", d2)
		}
		d3 := c.backoffDelay(3)
		if d3 < 800*time.Millisecond || d3 > 1000*time.Millisecond {
			t.Errorf("attempt 3: delay = %v, want [800ms, 1000ms]", d3)
		}
	}
}

// ---------------------------------------------------------------------------
// DMError tests
// ---------------------------------------------------------------------------

func TestDMError_ErrorString_WithStatusCode(t *testing.T) {
	de := &DMError{
		Operation:  "GetDocument",
		StatusCode: 500,
		Body:       []byte(`{"error_code":"INTERNAL_ERROR"}`),
		Retryable:  true,
	}
	want := `dmclient: GetDocument: HTTP 500: {"error_code":"INTERNAL_ERROR"}`
	if de.Error() != want {
		t.Errorf("Error() = %q, want %q", de.Error(), want)
	}
}

func TestDMError_ErrorString_WithCause(t *testing.T) {
	de := &DMError{
		Operation: "CreateDocument",
		Retryable: true,
		Cause:     fmt.Errorf("connection refused"),
	}
	want := "dmclient: CreateDocument: connection refused"
	if de.Error() != want {
		t.Errorf("Error() = %q, want %q", de.Error(), want)
	}
}

func TestDMError_Unwrap(t *testing.T) {
	cause := errors.New("root cause")
	de := &DMError{Cause: cause}
	if !errors.Is(de, cause) {
		t.Error("Unwrap should expose the cause")
	}
}

// ---------------------------------------------------------------------------
// isRetryable tests
// ---------------------------------------------------------------------------

func TestIsRetryable_RetryableDMError(t *testing.T) {
	if !isRetryable(&DMError{Retryable: true}) {
		t.Error("expected true")
	}
}

func TestIsRetryable_NonRetryableDMError(t *testing.T) {
	if isRetryable(&DMError{Retryable: false}) {
		t.Error("expected false")
	}
}

func TestIsRetryable_ErrCircuitOpen(t *testing.T) {
	if isRetryable(fmt.Errorf("wrap: %w", ErrCircuitOpen)) {
		t.Error("expected false for ErrCircuitOpen")
	}
}

func TestIsRetryable_ContextCanceled(t *testing.T) {
	if isRetryable(context.Canceled) {
		t.Error("expected false for context.Canceled")
	}
}

func TestIsRetryable_ContextDeadlineExceeded(t *testing.T) {
	if !isRetryable(context.DeadlineExceeded) {
		t.Error("expected true for per-attempt DeadlineExceeded")
	}
}

// ---------------------------------------------------------------------------
// isCBFailure tests
// ---------------------------------------------------------------------------

func TestIsCBFailure_RetryableDMError(t *testing.T) {
	if !isCBFailure(&DMError{Retryable: true}) {
		t.Error("expected true")
	}
}

func TestIsCBFailure_NonRetryableDMError(t *testing.T) {
	if isCBFailure(&DMError{Retryable: false}) {
		t.Error("expected false for non-retryable")
	}
}

func TestIsCBFailure_ContextCanceled(t *testing.T) {
	if isCBFailure(context.Canceled) {
		t.Error("expected false for context.Canceled")
	}
}

func TestIsCBFailure_ContextDeadlineExceeded(t *testing.T) {
	if !isCBFailure(context.DeadlineExceeded) {
		t.Error("expected true for DeadlineExceeded")
	}
}

// ---------------------------------------------------------------------------
// mapHTTPError tests
// ---------------------------------------------------------------------------

func TestMapHTTPError_5xx_Retryable(t *testing.T) {
	de := mapHTTPError("GetDocument", 503, []byte("unavailable"))
	if !de.Retryable {
		t.Error("expected Retryable=true for 503")
	}
	if de.StatusCode != 503 {
		t.Errorf("StatusCode = %d, want 503", de.StatusCode)
	}
}

func TestMapHTTPError_4xx_NonRetryable(t *testing.T) {
	de := mapHTTPError("GetDocument", 404, []byte("not found"))
	if de.Retryable {
		t.Error("expected Retryable=false for 404")
	}
}

func TestMapHTTPError_400_NonRetryable(t *testing.T) {
	de := mapHTTPError("CreateDocument", 400, []byte("bad request"))
	if de.Retryable {
		t.Error("expected Retryable=false for 400")
	}
}

// ---------------------------------------------------------------------------
// mapTransportError tests
// ---------------------------------------------------------------------------

func TestMapTransportError_ContextCanceled_PassThrough(t *testing.T) {
	err := mapTransportError(context.Canceled, "GetDocument")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestMapTransportError_NetError_Retryable(t *testing.T) {
	netErr := &net.OpError{Op: "dial", Err: errors.New("connection refused")}
	err := mapTransportError(netErr, "GetDocument")
	var de *DMError
	if !errors.As(err, &de) {
		t.Fatalf("expected DMError, got %T", err)
	}
	if !de.Retryable {
		t.Error("expected Retryable=true for network error")
	}
}

func TestMapTransportError_DeadlineExceeded_PassThrough(t *testing.T) {
	err := mapTransportError(context.DeadlineExceeded, "GetDocument")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded pass-through, got %v", err)
	}
	// Should NOT be wrapped in DMError.
	var de *DMError
	if errors.As(err, &de) {
		t.Error("DeadlineExceeded should be passed through raw, not wrapped in DMError")
	}
}

func TestMapTransportError_UnknownError_Retryable(t *testing.T) {
	err := mapTransportError(errors.New("something unexpected"), "GetDocument")
	var de *DMError
	if !errors.As(err, &de) {
		t.Fatalf("expected DMError, got %T", err)
	}
	if !de.Retryable {
		t.Error("expected Retryable=true for unknown error")
	}
}

// ---------------------------------------------------------------------------
// Empty auth context tests (S-4)
// ---------------------------------------------------------------------------

func TestEmptyContext_NoPanic_NoEmptyHeaders(t *testing.T) {
	var capturedHeaders http.Header
	c, _ := testClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(DocumentList{Items: []Document{}, Total: 0, Page: 1, Size: 20})
	}))

	// Use bare context.Background() — no auth, no logger context.
	_, err := c.ListDocuments(context.Background(), ListDocumentsParams{Page: 1, Size: 20})
	if err != nil {
		t.Fatalf("unexpected error with empty context: %v", err)
	}

	// Headers should be absent, not set to empty strings.
	if got := capturedHeaders.Get("X-Organization-ID"); got != "" {
		t.Errorf("X-Organization-ID = %q, want empty (not set)", got)
	}
	if got := capturedHeaders.Get("X-User-ID"); got != "" {
		t.Errorf("X-User-ID = %q, want empty (not set)", got)
	}
	if got := capturedHeaders.Get("X-Correlation-ID"); got != "" {
		t.Errorf("X-Correlation-ID = %q, want empty (not set)", got)
	}
}

// ---------------------------------------------------------------------------
// ListDocuments query parameter tests
// ---------------------------------------------------------------------------

func TestListDocuments_QueryParams(t *testing.T) {
	c, _ := testClientWithServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if got := q.Get("page"); got != "3" {
			t.Errorf("page = %q, want 3", got)
		}
		if got := q.Get("size"); got != "50" {
			t.Errorf("size = %q, want 50", got)
		}
		if got := q.Get("status"); got != "ARCHIVED" {
			t.Errorf("status = %q, want ARCHIVED", got)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(DocumentList{Items: []Document{}, Total: 0, Page: 3, Size: 50})
	}))

	_, err := c.ListDocuments(testContext(), ListDocumentsParams{Page: 3, Size: 50, Status: "ARCHIVED"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// DMClient interface compile-time check
// ---------------------------------------------------------------------------

func TestClientImplementsDMClient(t *testing.T) {
	// This test exists to verify the compile-time interface check is in place.
	var _ DMClient = (*Client)(nil)
}

// ---------------------------------------------------------------------------
// CB helpers for tests
// ---------------------------------------------------------------------------

func buildTestCB(threshold int) *gobreaker.CircuitBreaker[struct{}] {
	return buildCB(testCBConfig(threshold))
}

func buildCB(cfg config.CircuitBreakerConfig) *gobreaker.CircuitBreaker[struct{}] {
	return gobreaker.NewCircuitBreaker[struct{}](gobreaker.Settings{
		Name:        "dm-client-test",
		MaxRequests: uint32(cfg.MaxRequests),
		Timeout:     cfg.Timeout,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= uint32(cfg.FailureThreshold)
		},
		IsSuccessful: func(err error) bool {
			if err == nil {
				return true
			}
			return !isCBFailure(err)
		},
	})
}

