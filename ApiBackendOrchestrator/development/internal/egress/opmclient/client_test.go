package opmclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"contractpro/api-orchestrator/internal/config"
	"contractpro/api-orchestrator/internal/infra/observability/logger"
	"contractpro/api-orchestrator/internal/ingress/middleware/auth"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func testClient(t *testing.T, handler http.Handler) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	log := logger.NewLogger("debug")
	c := newClient(srv.Client(), srv.URL, 5*time.Second, defaultRetryMax, 10*time.Millisecond, log)
	return c, srv
}

func testContext() context.Context {
	ctx := context.Background()
	ctx = logger.WithRequestContext(ctx, logger.RequestContext{
		CorrelationID: "test-correlation-123",
	})
	return ctx
}

func testContextWithAuth() context.Context {
	ctx := testContext()
	ctx = auth.WithAuthContext(ctx, auth.AuthContext{
		UserID:         "user-abc-123",
		OrganizationID: "org-xyz-789",
		Role:           auth.RoleOrgAdmin,
	})
	return ctx
}

// ---------------------------------------------------------------------------
// Interface compliance
// ---------------------------------------------------------------------------

func TestInterfaceCompliance(t *testing.T) {
	var _ OPMClient = (*Client)(nil)
	var _ OPMClient = (*DisabledClient)(nil)
}

// ---------------------------------------------------------------------------
// Constructor
// ---------------------------------------------------------------------------

func TestNewOPMClient_Enabled(t *testing.T) {
	log := logger.NewLogger("debug")
	cfg := config.OPMClientConfig{
		BaseURL: "http://opm.local",
		Timeout: 5 * time.Second,
	}
	client := NewOPMClient(cfg, log)
	if _, ok := client.(*Client); !ok {
		t.Errorf("expected *Client, got %T", client)
	}
}

func TestNewOPMClient_Disabled(t *testing.T) {
	log := logger.NewLogger("debug")
	cfg := config.OPMClientConfig{
		BaseURL: "",
		Timeout: 5 * time.Second,
	}
	client := NewOPMClient(cfg, log)
	if _, ok := client.(*DisabledClient); !ok {
		t.Errorf("expected *DisabledClient, got %T", client)
	}
}

// ---------------------------------------------------------------------------
// ListPolicies
// ---------------------------------------------------------------------------

func TestListPolicies_Success(t *testing.T) {
	wantBody := `{"policies":[{"policy_id":"p-1","name":"Strict"}]}`
	c, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/policies" {
			t.Errorf("expected /api/v1/policies, got %s", r.URL.Path)
		}
		if orgID := r.URL.Query().Get("organization_id"); orgID != "org-xyz-789" {
			t.Errorf("expected organization_id=org-xyz-789, got %s", orgID)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(wantBody))
	}))

	ctx := testContextWithAuth()
	got, err := c.ListPolicies(ctx, "org-xyz-789")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != wantBody {
		t.Errorf("body = %s, want %s", string(got), wantBody)
	}
}

func TestListPolicies_OPMDown_Retry(t *testing.T) {
	var attempts atomic.Int32
	c, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"internal"}`))
	}))

	ctx := testContextWithAuth()
	_, err := c.ListPolicies(ctx, "org-1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var opmErr *OPMError
	if !errors.As(err, &opmErr) {
		t.Fatalf("expected *OPMError, got %T", err)
	}
	if opmErr.StatusCode != http.StatusInternalServerError {
		t.Errorf("StatusCode = %d, want %d", opmErr.StatusCode, http.StatusInternalServerError)
	}
	if got := attempts.Load(); got != 2 {
		t.Errorf("attempts = %d, want 2", got)
	}
}

func TestListPolicies_RetryThenSuccess(t *testing.T) {
	var attempts atomic.Int32
	wantBody := `{"policies":[]}`
	c, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := attempts.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"transient"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(wantBody))
	}))

	ctx := testContextWithAuth()
	got, err := c.ListPolicies(ctx, "org-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != wantBody {
		t.Errorf("body = %s, want %s", string(got), wantBody)
	}
	if n := attempts.Load(); n != 2 {
		t.Errorf("attempts = %d, want 2", n)
	}
}

func TestListPolicies_NoRetryOn400(t *testing.T) {
	var attempts atomic.Int32
	c, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"bad request"}`))
	}))

	ctx := testContextWithAuth()
	_, err := c.ListPolicies(ctx, "org-1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var opmErr *OPMError
	if !errors.As(err, &opmErr) {
		t.Fatalf("expected *OPMError, got %T", err)
	}
	if opmErr.Retryable {
		t.Error("expected non-retryable error for 400")
	}
	if got := attempts.Load(); got != 1 {
		t.Errorf("attempts = %d, want 1 (no retry for 400)", got)
	}
}

// ---------------------------------------------------------------------------
// UpdatePolicy
// ---------------------------------------------------------------------------

func TestUpdatePolicy_Success(t *testing.T) {
	reqBody := json.RawMessage(`{"name":"Updated Policy","strictness_level":"strict"}`)
	wantResp := `{"policy_id":"p-1","name":"Updated Policy"}`

	c, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/policies/p-1" {
			t.Errorf("expected /api/v1/policies/p-1, got %s", r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", ct)
		}

		body, _ := io.ReadAll(r.Body)
		if string(body) != string(reqBody) {
			t.Errorf("body = %s, want %s", string(body), string(reqBody))
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(wantResp))
	}))

	ctx := testContextWithAuth()
	got, err := c.UpdatePolicy(ctx, "p-1", reqBody)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != wantResp {
		t.Errorf("body = %s, want %s", string(got), wantResp)
	}
}

func TestUpdatePolicy_NotFound(t *testing.T) {
	c, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"not found"}`))
	}))

	ctx := testContextWithAuth()
	_, err := c.UpdatePolicy(ctx, "missing-id", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var opmErr *OPMError
	if !errors.As(err, &opmErr) {
		t.Fatalf("expected *OPMError, got %T", err)
	}
	if opmErr.StatusCode != http.StatusNotFound {
		t.Errorf("StatusCode = %d, want %d", opmErr.StatusCode, http.StatusNotFound)
	}
}

func TestUpdatePolicy_PathEscaping(t *testing.T) {
	c, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// url.PathEscape encodes "/" as "%2F". EscapedPath preserves encoding.
		escaped := r.URL.EscapedPath()
		if !strings.Contains(escaped, "id%2Fwith%2Fslashes") {
			t.Errorf("path not properly escaped: %s", escaped)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	}))

	ctx := testContextWithAuth()
	_, err := c.UpdatePolicy(ctx, "id/with/slashes", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ListChecklists
// ---------------------------------------------------------------------------

func TestListChecklists_Success(t *testing.T) {
	wantBody := `{"checklists":[{"checklist_id":"c-1","name":"Default"}]}`
	c, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/checklists" {
			t.Errorf("expected /api/v1/checklists, got %s", r.URL.Path)
		}
		if orgID := r.URL.Query().Get("organization_id"); orgID != "org-abc" {
			t.Errorf("expected organization_id=org-abc, got %s", orgID)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(wantBody))
	}))

	ctx := testContextWithAuth()
	got, err := c.ListChecklists(ctx, "org-abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != wantBody {
		t.Errorf("body = %s, want %s", string(got), wantBody)
	}
}

// ---------------------------------------------------------------------------
// UpdateChecklist
// ---------------------------------------------------------------------------

func TestUpdateChecklist_Success(t *testing.T) {
	reqBody := json.RawMessage(`{"items":[{"item_id":"i-1","label":"Check GDPR"}]}`)
	wantResp := `{"checklist_id":"c-1","items":[{"item_id":"i-1"}]}`

	c, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/checklists/c-1" {
			t.Errorf("expected /api/v1/checklists/c-1, got %s", r.URL.Path)
		}

		body, _ := io.ReadAll(r.Body)
		if string(body) != string(reqBody) {
			t.Errorf("body = %s, want %s", string(body), string(reqBody))
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(wantResp))
	}))

	ctx := testContextWithAuth()
	got, err := c.UpdateChecklist(ctx, "c-1", reqBody)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != wantResp {
		t.Errorf("body = %s, want %s", string(got), wantResp)
	}
}

// ---------------------------------------------------------------------------
// DisabledClient
// ---------------------------------------------------------------------------

func TestDisabledClient_ListPolicies(t *testing.T) {
	log := logger.NewLogger("debug")
	d := &DisabledClient{log: log.With("component", "opm-client-disabled")}

	got, err := d.ListPolicies(context.Background(), "org-1")
	if got != nil {
		t.Errorf("expected nil response, got %s", string(got))
	}
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var opmErr *OPMError
	if !errors.As(err, &opmErr) {
		t.Fatalf("expected *OPMError, got %T", err)
	}
	if !errors.Is(opmErr.Cause, ErrOPMDisabled) {
		t.Errorf("expected ErrOPMDisabled cause, got %v", opmErr.Cause)
	}
	if opmErr.Operation != "ListPolicies" {
		t.Errorf("Operation = %q, want ListPolicies", opmErr.Operation)
	}
	if opmErr.Retryable {
		t.Error("expected non-retryable for disabled client")
	}
}

func TestDisabledClient_UpdatePolicy(t *testing.T) {
	log := logger.NewLogger("debug")
	d := &DisabledClient{log: log.With("component", "opm-client-disabled")}

	_, err := d.UpdatePolicy(context.Background(), "p-1", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var opmErr *OPMError
	if !errors.As(err, &opmErr) {
		t.Fatalf("expected *OPMError, got %T", err)
	}
	if !errors.Is(opmErr.Cause, ErrOPMDisabled) {
		t.Errorf("expected ErrOPMDisabled cause, got %v", opmErr.Cause)
	}
}

func TestDisabledClient_ListChecklists(t *testing.T) {
	log := logger.NewLogger("debug")
	d := &DisabledClient{log: log.With("component", "opm-client-disabled")}

	_, err := d.ListChecklists(context.Background(), "org-1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var opmErr *OPMError
	if !errors.As(err, &opmErr) {
		t.Fatalf("expected *OPMError, got %T", err)
	}
	if !errors.Is(opmErr.Cause, ErrOPMDisabled) {
		t.Errorf("expected ErrOPMDisabled cause, got %v", opmErr.Cause)
	}
}

func TestDisabledClient_UpdateChecklist(t *testing.T) {
	log := logger.NewLogger("debug")
	d := &DisabledClient{log: log.With("component", "opm-client-disabled")}

	_, err := d.UpdateChecklist(context.Background(), "c-1", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var opmErr *OPMError
	if !errors.As(err, &opmErr) {
		t.Fatalf("expected *OPMError, got %T", err)
	}
	if !errors.Is(opmErr.Cause, ErrOPMDisabled) {
		t.Errorf("expected ErrOPMDisabled cause, got %v", opmErr.Cause)
	}
}

// ---------------------------------------------------------------------------
// Headers
// ---------------------------------------------------------------------------

func TestHeaders_OrganizationAndCorrelation(t *testing.T) {
	c, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Organization-ID"); got != "org-xyz-789" {
			t.Errorf("X-Organization-ID = %q, want org-xyz-789", got)
		}
		if got := r.Header.Get("X-User-ID"); got != "user-abc-123" {
			t.Errorf("X-User-ID = %q, want user-abc-123", got)
		}
		if got := r.Header.Get("X-Correlation-ID"); got != "test-correlation-123" {
			t.Errorf("X-Correlation-ID = %q, want test-correlation-123", got)
		}
		w.Write([]byte(`{}`))
	}))

	ctx := testContextWithAuth()
	_, err := c.ListPolicies(ctx, "org-xyz-789")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHeaders_EmptyContext(t *testing.T) {
	c, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// No panic expected; headers should be empty.
		if got := r.Header.Get("X-Organization-ID"); got != "" {
			t.Errorf("X-Organization-ID = %q, want empty", got)
		}
		if got := r.Header.Get("X-Correlation-ID"); got != "" {
			t.Errorf("X-Correlation-ID = %q, want empty", got)
		}
		w.Write([]byte(`{}`))
	}))

	_, err := c.ListPolicies(context.Background(), "org-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHeaders_PUT_ContentType(t *testing.T) {
	c, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		w.Write([]byte(`{}`))
	}))

	ctx := testContextWithAuth()
	_, err := c.UpdatePolicy(ctx, "p-1", json.RawMessage(`{"name":"test"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHeaders_GET_NoContentType(t *testing.T) {
	c, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); ct != "" {
			t.Errorf("Content-Type = %q, want empty for GET", ct)
		}
		w.Write([]byte(`{}`))
	}))

	ctx := testContextWithAuth()
	_, err := c.ListPolicies(ctx, "org-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Context cancellation
// ---------------------------------------------------------------------------

func TestContextCanceled(t *testing.T) {
	c, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{}`))
	}))

	ctx, cancel := context.WithCancel(testContextWithAuth())
	cancel() // Cancel immediately.

	_, err := c.ListPolicies(ctx, "org-1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestContextCanceledDuringBackoff(t *testing.T) {
	var attempts atomic.Int32
	log := logger.NewLogger("debug")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"fail"}`))
	}))
	t.Cleanup(srv.Close)

	// Use a long backoff so we can cancel during it.
	c := newClient(srv.Client(), srv.URL, 5*time.Second, 2, 5*time.Second, log)

	ctx, cancel := context.WithCancel(testContextWithAuth())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := c.ListPolicies(ctx, "org-1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if got := attempts.Load(); got != 1 {
		t.Errorf("attempts = %d, want 1 (cancelled during backoff)", got)
	}
}

// ---------------------------------------------------------------------------
// Transport errors
// ---------------------------------------------------------------------------

func TestTransportError_ConnectionRefused(t *testing.T) {
	log := logger.NewLogger("debug")
	// Use a port that nothing listens on.
	c := newClient(&http.Client{}, "http://127.0.0.1:1", 1*time.Second, 1, 10*time.Millisecond, log)

	_, err := c.ListPolicies(testContextWithAuth(), "org-1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var opmErr *OPMError
	if !errors.As(err, &opmErr) {
		t.Fatalf("expected *OPMError, got %T", err)
	}
	if !opmErr.Retryable {
		t.Error("expected retryable error for connection refused")
	}
}

// ---------------------------------------------------------------------------
// Response body limit
// ---------------------------------------------------------------------------

func TestResponseBodyLimit(t *testing.T) {
	// Create a response larger than 1MB.
	bigBody := strings.Repeat("x", 2*1024*1024)
	c, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(bigBody))
	}))

	ctx := testContextWithAuth()
	got, err := c.ListPolicies(ctx, "org-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != maxResponseBodySize {
		t.Errorf("response length = %d, want exactly %d", len(got), maxResponseBodySize)
	}
}

// ---------------------------------------------------------------------------
// Empty response body
// ---------------------------------------------------------------------------

func TestEmptyResponseBody(t *testing.T) {
	c, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		// No body written.
	}))

	ctx := testContextWithAuth()
	got, err := c.ListPolicies(ctx, "org-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for empty body, got %s", string(got))
	}
}

// ---------------------------------------------------------------------------
// OPMError
// ---------------------------------------------------------------------------

func TestOPMError_Formatting_WithStatusCode(t *testing.T) {
	err := &OPMError{
		Operation:  "ListPolicies",
		StatusCode: 500,
		Body:       []byte(`{"error":"internal"}`),
	}
	want := `opmclient: ListPolicies: HTTP 500: {"error":"internal"}`
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestOPMError_Formatting_WithCause(t *testing.T) {
	err := &OPMError{
		Operation: "UpdatePolicy",
		Cause:     errors.New("connection refused"),
	}
	want := "opmclient: UpdatePolicy: connection refused"
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestOPMError_Formatting_Unknown(t *testing.T) {
	err := &OPMError{
		Operation: "ListChecklists",
	}
	want := "opmclient: ListChecklists: unknown error"
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestOPMError_Unwrap(t *testing.T) {
	cause := errors.New("root cause")
	err := &OPMError{Operation: "test", Cause: cause}
	if !errors.Is(err, cause) {
		t.Error("expected errors.Is to find cause through Unwrap")
	}
}

// ---------------------------------------------------------------------------
// isRetryable
// ---------------------------------------------------------------------------

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "OPMError retryable",
			err:  &OPMError{Operation: "test", Retryable: true},
			want: true,
		},
		{
			name: "OPMError non-retryable",
			err:  &OPMError{Operation: "test", Retryable: false},
			want: false,
		},
		{
			name: "context.Canceled",
			err:  context.Canceled,
			want: false,
		},
		{
			name: "context.DeadlineExceeded",
			err:  context.DeadlineExceeded,
			want: true,
		},
		{
			name: "unknown error",
			err:  errors.New("unknown"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isRetryable(tt.err)
			if got != tt.want {
				t.Errorf("isRetryable(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// mapHTTPError
// ---------------------------------------------------------------------------

func TestMapHTTPError(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		retryable  bool
	}{
		{"400 non-retryable", 400, false},
		{"404 non-retryable", 404, false},
		{"429 non-retryable", 429, false},
		{"500 retryable", 500, true},
		{"502 retryable", 502, true},
		{"503 retryable", 503, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := mapHTTPError("test", tt.statusCode, []byte("body"))
			if err.Retryable != tt.retryable {
				t.Errorf("Retryable = %v, want %v", err.Retryable, tt.retryable)
			}
			if err.StatusCode != tt.statusCode {
				t.Errorf("StatusCode = %d, want %d", err.StatusCode, tt.statusCode)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// mapTransportError
// ---------------------------------------------------------------------------

func TestMapTransportError(t *testing.T) {
	t.Run("context.Canceled passes through", func(t *testing.T) {
		err := mapTransportError(context.Canceled, "test")
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled pass-through, got %T", err)
		}
	})

	t.Run("context.DeadlineExceeded passes through", func(t *testing.T) {
		err := mapTransportError(context.DeadlineExceeded, "test")
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("expected context.DeadlineExceeded pass-through, got %T", err)
		}
	})

	t.Run("net.Error wraps as retryable", func(t *testing.T) {
		netErr := &net.DNSError{Err: "lookup failed", Name: "opm.local"}
		err := mapTransportError(netErr, "test")
		var opmErr *OPMError
		if !errors.As(err, &opmErr) {
			t.Fatalf("expected *OPMError, got %T", err)
		}
		if !opmErr.Retryable {
			t.Error("expected retryable for net.Error")
		}
	})

	t.Run("unknown error wraps as retryable", func(t *testing.T) {
		err := mapTransportError(errors.New("unknown"), "test")
		var opmErr *OPMError
		if !errors.As(err, &opmErr) {
			t.Fatalf("expected *OPMError, got %T", err)
		}
		if !opmErr.Retryable {
			t.Error("expected retryable for unknown error")
		}
	})
}

// ---------------------------------------------------------------------------
// Concurrent safety
// ---------------------------------------------------------------------------

func TestConcurrentRequests(t *testing.T) {
	c, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"ok":true}`))
	}))

	ctx := testContextWithAuth()
	errs := make(chan error, 20)

	for i := 0; i < 20; i++ {
		go func() {
			_, err := c.ListPolicies(ctx, "org-1")
			errs <- err
		}()
	}

	for i := 0; i < 20; i++ {
		if err := <-errs; err != nil {
			t.Errorf("concurrent request failed: %v", err)
		}
	}
}

// ---------------------------------------------------------------------------
// buildURL
// ---------------------------------------------------------------------------

func TestBuildURL(t *testing.T) {
	log := logger.NewLogger("debug")
	c := newClient(&http.Client{}, "http://opm.local", 5*time.Second, 2, 200*time.Millisecond, log)

	t.Run("without query params", func(t *testing.T) {
		got := c.buildURL("/api/v1/policies/p-1", nil)
		want := "http://opm.local/api/v1/policies/p-1"
		if got != want {
			t.Errorf("buildURL = %q, want %q", got, want)
		}
	})

	t.Run("with query params", func(t *testing.T) {
		q := map[string][]string{"organization_id": {"org-1"}}
		got := c.buildURL("/api/v1/policies", q)
		want := "http://opm.local/api/v1/policies?organization_id=org-1"
		if got != want {
			t.Errorf("buildURL = %q, want %q", got, want)
		}
	})
}

// ---------------------------------------------------------------------------
// ErrOPMDisabled sentinel
// ---------------------------------------------------------------------------

func TestErrOPMDisabled_IsDetectable(t *testing.T) {
	err := &OPMError{Operation: "test", Cause: ErrOPMDisabled}
	if !errors.Is(err, ErrOPMDisabled) {
		t.Error("expected errors.Is(err, ErrOPMDisabled) to be true")
	}
}
