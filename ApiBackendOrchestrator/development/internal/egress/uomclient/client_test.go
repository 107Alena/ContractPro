package uomclient

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
		Role:           auth.RoleLawyer,
	})
	return ctx
}

// ---------------------------------------------------------------------------
// Interface compliance
// ---------------------------------------------------------------------------

func TestInterfaceCompliance(t *testing.T) {
	var _ UOMClient = (*Client)(nil)
}

// ---------------------------------------------------------------------------
// Login
// ---------------------------------------------------------------------------

func TestLogin_Success(t *testing.T) {
	c, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/auth/login" {
			t.Errorf("expected /api/v1/auth/login, got %s", r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", r.Header.Get("Content-Type"))
		}

		// Verify request body.
		var req LoginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}
		if req.Email != "user@example.com" {
			t.Errorf("expected email user@example.com, got %s", req.Email)
		}
		if req.Password != "secret123" {
			t.Errorf("expected password secret123, got %s", req.Password)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(LoginResponse{
			AccessToken:  "access-token-123",
			RefreshToken: "refresh-token-456",
			TokenType:    "Bearer",
			ExpiresIn:    900,
			User: &UserProfile{
				UserID:         "user-id-789",
				OrganizationID: "org-id-012",
				Email:          "user@example.com",
				Name:           "Test User",
				Role:           "LAWYER",
			},
		})
	}))

	resp, err := c.Login(testContext(), LoginRequest{
		Email:    "user@example.com",
		Password: "secret123",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.AccessToken != "access-token-123" {
		t.Errorf("expected access_token access-token-123, got %s", resp.AccessToken)
	}
	if resp.RefreshToken != "refresh-token-456" {
		t.Errorf("expected refresh_token refresh-token-456, got %s", resp.RefreshToken)
	}
	if resp.TokenType != "Bearer" {
		t.Errorf("expected token_type Bearer, got %s", resp.TokenType)
	}
	if resp.ExpiresIn != 900 {
		t.Errorf("expected expires_in 900, got %d", resp.ExpiresIn)
	}
	if resp.User == nil {
		t.Fatal("expected user profile, got nil")
	}
	if resp.User.UserID != "user-id-789" {
		t.Errorf("expected user_id user-id-789, got %s", resp.User.UserID)
	}
	if resp.User.Role != "LAWYER" {
		t.Errorf("expected role LAWYER, got %s", resp.User.Role)
	}
}

func TestLogin_InvalidCredentials(t *testing.T) {
	c, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{
			"code":    "INVALID_CREDENTIALS",
			"message": "Invalid email or password",
		})
	}))

	resp, err := c.Login(testContext(), LoginRequest{
		Email:    "user@example.com",
		Password: "wrong",
	})
	if resp != nil {
		t.Errorf("expected nil response, got %+v", resp)
	}
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var ue *UOMError
	if !errors.As(err, &ue) {
		t.Fatalf("expected UOMError, got %T", err)
	}
	if ue.StatusCode != 401 {
		t.Errorf("expected status 401, got %d", ue.StatusCode)
	}
	if ue.Retryable {
		t.Error("expected Retryable=false for 401")
	}
}

func TestLogin_UOMDown_Retry(t *testing.T) {
	var attempts atomic.Int32
	c, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "internal error"}`))
	}))

	resp, err := c.Login(testContext(), LoginRequest{
		Email:    "user@example.com",
		Password: "secret",
	})
	if resp != nil {
		t.Errorf("expected nil response, got %+v", resp)
	}
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Should have retried: 2 total attempts (initial + 1 retry).
	if got := attempts.Load(); got != 2 {
		t.Errorf("expected 2 attempts, got %d", got)
	}

	var ue *UOMError
	if !errors.As(err, &ue) {
		t.Fatalf("expected UOMError, got %T", err)
	}
	if ue.StatusCode != 500 {
		t.Errorf("expected status 500, got %d", ue.StatusCode)
	}
}

func TestLogin_RetryThenSuccess(t *testing.T) {
	var attempts atomic.Int32
	c, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := attempts.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(LoginResponse{
			AccessToken: "token-after-retry",
			TokenType:   "Bearer",
			ExpiresIn:   900,
		})
	}))

	resp, err := c.Login(testContext(), LoginRequest{
		Email:    "user@example.com",
		Password: "secret",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.AccessToken != "token-after-retry" {
		t.Errorf("expected token-after-retry, got %s", resp.AccessToken)
	}
	if got := attempts.Load(); got != 2 {
		t.Errorf("expected 2 attempts, got %d", got)
	}
}

func TestLogin_NoRetryOn401(t *testing.T) {
	var attempts atomic.Int32
	c, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"code": "INVALID_CREDENTIALS"}`))
	}))

	_, err := c.Login(testContext(), LoginRequest{
		Email:    "user@example.com",
		Password: "wrong",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if got := attempts.Load(); got != 1 {
		t.Errorf("expected 1 attempt (no retry on 401), got %d", got)
	}
}

// ---------------------------------------------------------------------------
// Refresh
// ---------------------------------------------------------------------------

func TestRefresh_Success(t *testing.T) {
	c, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/auth/refresh" {
			t.Errorf("expected /api/v1/auth/refresh, got %s", r.URL.Path)
		}

		var req RefreshRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}
		if req.RefreshToken != "refresh-token-old" {
			t.Errorf("expected refresh_token refresh-token-old, got %s", req.RefreshToken)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(RefreshResponse{
			AccessToken:  "access-token-new",
			RefreshToken: "refresh-token-new",
			TokenType:    "Bearer",
			ExpiresIn:    900,
		})
	}))

	resp, err := c.Refresh(testContext(), RefreshRequest{
		RefreshToken: "refresh-token-old",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.AccessToken != "access-token-new" {
		t.Errorf("expected access-token-new, got %s", resp.AccessToken)
	}
	if resp.RefreshToken != "refresh-token-new" {
		t.Errorf("expected refresh-token-new, got %s", resp.RefreshToken)
	}
}

func TestRefresh_TokenExpired(t *testing.T) {
	c, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{
			"code":    "TOKEN_EXPIRED",
			"message": "Refresh token expired",
		})
	}))

	resp, err := c.Refresh(testContext(), RefreshRequest{
		RefreshToken: "expired-token",
	})
	if resp != nil {
		t.Errorf("expected nil response, got %+v", resp)
	}

	var ue *UOMError
	if !errors.As(err, &ue) {
		t.Fatalf("expected UOMError, got %T", err)
	}
	if ue.StatusCode != 401 {
		t.Errorf("expected status 401, got %d", ue.StatusCode)
	}
	if ue.Retryable {
		t.Error("expected Retryable=false")
	}
}

func TestRefresh_TokenRevoked(t *testing.T) {
	c, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"code": "TOKEN_REVOKED"}`))
	}))

	_, err := c.Refresh(testContext(), RefreshRequest{
		RefreshToken: "revoked-token",
	})

	var ue *UOMError
	if !errors.As(err, &ue) {
		t.Fatalf("expected UOMError, got %T", err)
	}
	if ue.StatusCode != 401 {
		t.Errorf("expected 401, got %d", ue.StatusCode)
	}
}

func TestRefresh_UOMDown(t *testing.T) {
	var attempts atomic.Int32
	c, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))

	_, err := c.Refresh(testContext(), RefreshRequest{
		RefreshToken: "some-token",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if got := attempts.Load(); got != 2 {
		t.Errorf("expected 2 attempts, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// Logout
// ---------------------------------------------------------------------------

func TestLogout_Success(t *testing.T) {
	c, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/auth/logout" {
			t.Errorf("expected /api/v1/auth/logout, got %s", r.URL.Path)
		}

		var req LogoutRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}
		if req.RefreshToken != "refresh-to-revoke" {
			t.Errorf("expected refresh-to-revoke, got %s", req.RefreshToken)
		}

		w.WriteHeader(http.StatusNoContent)
	}))

	err := c.Logout(testContext(), LogoutRequest{
		RefreshToken: "refresh-to-revoke",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLogout_UOMDown(t *testing.T) {
	var attempts atomic.Int32
	c, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))

	err := c.Logout(testContext(), LogoutRequest{
		RefreshToken: "some-token",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if got := attempts.Load(); got != 2 {
		t.Errorf("expected 2 attempts, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// GetMe
// ---------------------------------------------------------------------------

func TestGetMe_Success(t *testing.T) {
	c, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/users/me" {
			t.Errorf("expected /api/v1/users/me, got %s", r.URL.Path)
		}

		// Verify auth headers.
		if r.Header.Get("X-User-ID") != "user-abc-123" {
			t.Errorf("expected X-User-ID user-abc-123, got %s", r.Header.Get("X-User-ID"))
		}

		// GetMe is a GET — no Content-Type header on request.
		if ct := r.Header.Get("Content-Type"); ct != "" {
			t.Errorf("expected no Content-Type for GET, got %s", ct)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(UserProfile{
			UserID:         "user-abc-123",
			OrganizationID: "org-xyz-789",
			Email:          "user@example.com",
			Name:           "Test User",
			Role:           "LAWYER",
			CreatedAt:      "2026-01-01T00:00:00Z",
		})
	}))

	profile, err := c.GetMe(testContextWithAuth())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if profile.UserID != "user-abc-123" {
		t.Errorf("expected user-abc-123, got %s", profile.UserID)
	}
	if profile.OrganizationID != "org-xyz-789" {
		t.Errorf("expected org-xyz-789, got %s", profile.OrganizationID)
	}
	if profile.Email != "user@example.com" {
		t.Errorf("expected user@example.com, got %s", profile.Email)
	}
	if profile.Role != "LAWYER" {
		t.Errorf("expected LAWYER, got %s", profile.Role)
	}
}

func TestGetMe_NotFound(t *testing.T) {
	c, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"code": "USER_NOT_FOUND"}`))
	}))

	profile, err := c.GetMe(testContextWithAuth())
	if profile != nil {
		t.Errorf("expected nil profile, got %+v", profile)
	}

	var ue *UOMError
	if !errors.As(err, &ue) {
		t.Fatalf("expected UOMError, got %T", err)
	}
	if ue.StatusCode != 404 {
		t.Errorf("expected 404, got %d", ue.StatusCode)
	}
}

func TestGetMe_UOMDown(t *testing.T) {
	var attempts atomic.Int32
	c, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))

	_, err := c.GetMe(testContextWithAuth())
	if err == nil {
		t.Fatal("expected error")
	}
	if got := attempts.Load(); got != 2 {
		t.Errorf("expected 2 attempts, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// Headers
// ---------------------------------------------------------------------------

func TestHeaders_CorrelationID(t *testing.T) {
	c, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Correlation-Id"); got != "test-correlation-123" {
			t.Errorf("expected X-Correlation-Id test-correlation-123, got %s", got)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	c.Logout(testContext(), LogoutRequest{RefreshToken: "x"})
}

func TestHeaders_NoAuthContext(t *testing.T) {
	c, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Without auth context, no X-User-ID header should be sent.
		if got := r.Header.Get("X-User-ID"); got != "" {
			t.Errorf("expected no X-User-ID header, got %s", got)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(LoginResponse{
			AccessToken: "token",
			TokenType:   "Bearer",
			ExpiresIn:   900,
		})
	}))

	_, err := c.Login(testContext(), LoginRequest{
		Email:    "user@example.com",
		Password: "pass",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHeaders_WithAuthContext(t *testing.T) {
	c, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-User-ID"); got != "user-abc-123" {
			t.Errorf("expected X-User-ID user-abc-123, got %s", got)
		}
		if got := r.Header.Get("X-Correlation-Id"); got != "test-correlation-123" {
			t.Errorf("expected X-Correlation-Id test-correlation-123, got %s", got)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(UserProfile{
			UserID: "user-abc-123",
		})
	}))

	_, err := c.GetMe(testContextWithAuth())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Context cancellation
// ---------------------------------------------------------------------------

func TestContextCanceled(t *testing.T) {
	c, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(LoginResponse{AccessToken: "x"})
	}))

	ctx, cancel := context.WithCancel(testContext())
	cancel() // Cancel immediately.

	_, err := c.Login(ctx, LoginRequest{Email: "a", Password: "b"})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestContextCanceledDuringBackoff(t *testing.T) {
	var attempts atomic.Int32
	c, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))

	// Use a longer backoff so we can cancel during it.
	c.retryBackoff = 5 * time.Second

	ctx, cancel := context.WithCancel(testContext())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := c.Login(ctx, LoginRequest{Email: "a", Password: "b"})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	// Should have had 1 attempt (the initial one), then canceled during backoff.
	if got := attempts.Load(); got != 1 {
		t.Errorf("expected 1 attempt before cancel, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// Transport errors
// ---------------------------------------------------------------------------

func TestTransportError_ConnectionRefused(t *testing.T) {
	log := logger.NewLogger("debug")
	c := newClient(&http.Client{}, "http://localhost:1", 1*time.Second, 1, 10*time.Millisecond, log)

	_, err := c.Login(testContext(), LoginRequest{Email: "a", Password: "b"})
	if err == nil {
		t.Fatal("expected error on connection refused")
	}

	var ue *UOMError
	if !errors.As(err, &ue) {
		t.Fatalf("expected UOMError, got %T: %v", err, err)
	}
	if !ue.Retryable {
		t.Error("expected Retryable=true for transport error")
	}
}

// ---------------------------------------------------------------------------
// Error types
// ---------------------------------------------------------------------------

func TestUOMError_Formatting(t *testing.T) {
	t.Run("with_status_code", func(t *testing.T) {
		e := &UOMError{
			Operation:  "Login",
			StatusCode: 401,
			Body:       []byte("unauthorized"),
		}
		msg := e.Error()
		if !strings.Contains(msg, "Login") || !strings.Contains(msg, "401") {
			t.Errorf("expected error message to contain operation and status, got: %s", msg)
		}
	})

	t.Run("with_cause", func(t *testing.T) {
		e := &UOMError{
			Operation: "GetMe",
			Cause:     errors.New("connection refused"),
		}
		msg := e.Error()
		if !strings.Contains(msg, "GetMe") || !strings.Contains(msg, "connection refused") {
			t.Errorf("expected error message to contain operation and cause, got: %s", msg)
		}
	})

	t.Run("unknown", func(t *testing.T) {
		e := &UOMError{Operation: "Refresh"}
		msg := e.Error()
		if !strings.Contains(msg, "unknown error") {
			t.Errorf("expected 'unknown error', got: %s", msg)
		}
	})
}

func TestUOMError_Unwrap(t *testing.T) {
	cause := errors.New("original error")
	e := &UOMError{Operation: "Login", Cause: cause}

	if !errors.Is(e, cause) {
		t.Error("expected Unwrap to expose the cause")
	}
}

// ---------------------------------------------------------------------------
// isRetryable
// ---------------------------------------------------------------------------

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"UOMError retryable", &UOMError{Retryable: true}, true},
		{"UOMError not retryable", &UOMError{Retryable: false}, false},
		{"context.Canceled", context.Canceled, false},
		{"context.DeadlineExceeded", context.DeadlineExceeded, true},
		{"unknown error", errors.New("unknown"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRetryable(tt.err); got != tt.expected {
				t.Errorf("isRetryable(%v) = %v, want %v", tt.err, got, tt.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// mapHTTPError
// ---------------------------------------------------------------------------

func TestMapHTTPError(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		retryable bool
	}{
		{"400 not retryable", 400, false},
		{"401 not retryable", 401, false},
		{"403 not retryable", 403, false},
		{"404 not retryable", 404, false},
		{"500 retryable", 500, true},
		{"502 retryable", 502, true},
		{"503 retryable", 503, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := mapHTTPError("test", tt.status, []byte("body"))
			if e.Retryable != tt.retryable {
				t.Errorf("expected Retryable=%v for status %d", tt.retryable, tt.status)
			}
			if e.StatusCode != tt.status {
				t.Errorf("expected StatusCode=%d, got %d", tt.status, e.StatusCode)
			}
			if e.Operation != "test" {
				t.Errorf("expected Operation=test, got %s", e.Operation)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// mapTransportError
// ---------------------------------------------------------------------------

func TestMapTransportError(t *testing.T) {
	t.Run("context.Canceled passthrough", func(t *testing.T) {
		err := mapTransportError(context.Canceled, "Login")
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	})

	t.Run("context.DeadlineExceeded passthrough", func(t *testing.T) {
		err := mapTransportError(context.DeadlineExceeded, "Login")
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("expected context.DeadlineExceeded, got %v", err)
		}
	})

	t.Run("net.Error retryable", func(t *testing.T) {
		err := mapTransportError(&net.OpError{Op: "dial"}, "Login")
		var ue *UOMError
		if !errors.As(err, &ue) {
			t.Fatalf("expected UOMError, got %T", err)
		}
		if !ue.Retryable {
			t.Error("expected Retryable=true for net.Error")
		}
	})

	t.Run("unknown error retryable", func(t *testing.T) {
		err := mapTransportError(errors.New("something"), "Login")
		var ue *UOMError
		if !errors.As(err, &ue) {
			t.Fatalf("expected UOMError, got %T", err)
		}
		if !ue.Retryable {
			t.Error("expected Retryable=true for unknown transport error")
		}
	})
}

// ---------------------------------------------------------------------------
// Response body limit
// ---------------------------------------------------------------------------

func TestResponseBodyLimit(t *testing.T) {
	// Ensure large response bodies are capped.
	largeBody := strings.Repeat("x", 2*1024*1024) // 2 MB
	c, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, largeBody)
	}))

	_, err := c.Login(testContext(), LoginRequest{Email: "a", Password: "b"})
	// Should fail to decode (truncated JSON) — not panic or OOM.
	if err == nil {
		t.Fatal("expected error on truncated response")
	}
}

// ---------------------------------------------------------------------------
// Empty context (no correlation ID, no auth)
// ---------------------------------------------------------------------------

func TestEmptyContext(t *testing.T) {
	c, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Should not panic with empty context.
		if got := r.Header.Get("X-Correlation-Id"); got != "" {
			t.Errorf("expected no X-Correlation-Id, got %s", got)
		}
		if got := r.Header.Get("X-User-ID"); got != "" {
			t.Errorf("expected no X-User-ID, got %s", got)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(LoginResponse{AccessToken: "x", TokenType: "Bearer"})
	}))

	_, err := c.Login(context.Background(), LoginRequest{Email: "a", Password: "b"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Concurrent safety
// ---------------------------------------------------------------------------

func TestConcurrentRequests(t *testing.T) {
	var count atomic.Int32
	c, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count.Add(1)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(LoginResponse{AccessToken: "x", TokenType: "Bearer"})
	}))

	const n = 20
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			_, err := c.Login(testContext(), LoginRequest{Email: "a", Password: "b"})
			errs <- err
		}()
	}

	for i := 0; i < n; i++ {
		if err := <-errs; err != nil {
			t.Errorf("concurrent request failed: %v", err)
		}
	}

	if got := count.Load(); got != int32(n) {
		t.Errorf("expected %d requests, got %d", n, got)
	}
}

// ---------------------------------------------------------------------------
// Constructor
// ---------------------------------------------------------------------------

func TestNewClient(t *testing.T) {
	log := logger.NewLogger("info")
	c := NewClient(config.UOMClientConfig{
		BaseURL: "http://uom.local",
		Timeout: 3 * time.Second,
	}, log)

	if c.baseURL != "http://uom.local" {
		t.Errorf("expected baseURL http://uom.local, got %s", c.baseURL)
	}
	if c.timeout != 3*time.Second {
		t.Errorf("expected timeout 3s, got %s", c.timeout)
	}
	if c.retryMax != defaultRetryMax {
		t.Errorf("expected retryMax %d, got %d", defaultRetryMax, c.retryMax)
	}
	if c.retryBackoff != defaultRetryBackoff {
		t.Errorf("expected retryBackoff %s, got %s", defaultRetryBackoff, c.retryBackoff)
	}
}

// ---------------------------------------------------------------------------
// Bad JSON response
// ---------------------------------------------------------------------------

func TestLogin_BadJSONResponse(t *testing.T) {
	c, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`not json`))
	}))

	_, err := c.Login(testContext(), LoginRequest{Email: "a", Password: "b"})
	if err == nil {
		t.Fatal("expected error on bad JSON")
	}
	var ue *UOMError
	if !errors.As(err, &ue) {
		t.Fatalf("expected UOMError, got %T", err)
	}
	if ue.Retryable {
		t.Error("expected Retryable=false for JSON decode error")
	}
}

// ---------------------------------------------------------------------------
// 400 Bad Request
// ---------------------------------------------------------------------------

func TestLogin_BadRequest(t *testing.T) {
	c, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"code": "VALIDATION_ERROR", "message": "email is required"}`))
	}))

	_, err := c.Login(testContext(), LoginRequest{})
	if err == nil {
		t.Fatal("expected error on 400")
	}
	var ue *UOMError
	if !errors.As(err, &ue) {
		t.Fatalf("expected UOMError, got %T", err)
	}
	if ue.StatusCode != 400 {
		t.Errorf("expected 400, got %d", ue.StatusCode)
	}
	if ue.Retryable {
		t.Error("expected Retryable=false for 400")
	}
}

// ---------------------------------------------------------------------------
// GetMe without AuthContext (edge case)
// ---------------------------------------------------------------------------

func TestGetMe_WithoutAuthContext(t *testing.T) {
	c, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Without auth context, no X-User-ID header.
		if got := r.Header.Get("X-User-ID"); got != "" {
			t.Errorf("expected no X-User-ID, got %s", got)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(UserProfile{
			UserID: "some-user",
		})
	}))

	profile, err := c.GetMe(testContext())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if profile.UserID != "some-user" {
		t.Errorf("expected some-user, got %s", profile.UserID)
	}
}

// ---------------------------------------------------------------------------
// 200 with empty body
// ---------------------------------------------------------------------------

func TestLogin_EmptyBody200(t *testing.T) {
	c, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		// No body written.
	}))

	resp, err := c.Login(testContext(), LoginRequest{Email: "a", Password: "b"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Zero-value struct expected.
	if resp.AccessToken != "" {
		t.Errorf("expected empty access_token, got %s", resp.AccessToken)
	}
}
