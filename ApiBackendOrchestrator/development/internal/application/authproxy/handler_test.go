package authproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"contractpro/api-orchestrator/internal/application/permissions"
	"contractpro/api-orchestrator/internal/egress/uomclient"
	"contractpro/api-orchestrator/internal/infra/observability/logger"
	"contractpro/api-orchestrator/internal/ingress/middleware/auth"
)

// ---------------------------------------------------------------------------
// Mock UOM client
// ---------------------------------------------------------------------------

type mockUOM struct {
	loginFn   func(ctx context.Context, req uomclient.LoginRequest) (*uomclient.LoginResponse, error)
	refreshFn func(ctx context.Context, req uomclient.RefreshRequest) (*uomclient.RefreshResponse, error)
	logoutFn  func(ctx context.Context, req uomclient.LogoutRequest) error
	getMeFn   func(ctx context.Context) (*uomclient.UserProfile, error)
}

func (m *mockUOM) Login(ctx context.Context, req uomclient.LoginRequest) (*uomclient.LoginResponse, error) {
	if m.loginFn != nil {
		return m.loginFn(ctx, req)
	}
	return nil, errors.New("Login not implemented")
}

func (m *mockUOM) Refresh(ctx context.Context, req uomclient.RefreshRequest) (*uomclient.RefreshResponse, error) {
	if m.refreshFn != nil {
		return m.refreshFn(ctx, req)
	}
	return nil, errors.New("Refresh not implemented")
}

func (m *mockUOM) Logout(ctx context.Context, req uomclient.LogoutRequest) error {
	if m.logoutFn != nil {
		return m.logoutFn(ctx, req)
	}
	return errors.New("Logout not implemented")
}

func (m *mockUOM) GetMe(ctx context.Context) (*uomclient.UserProfile, error) {
	if m.getMeFn != nil {
		return m.getMeFn(ctx)
	}
	return nil, errors.New("GetMe not implemented")
}

// Compile-time check.
var _ UOMClient = (*mockUOM)(nil)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func testLogger() *logger.Logger {
	return logger.NewLogger("error")
}

func newTestHandler(uom UOMClient) *Handler {
	return NewHandler(uom, nil, testLogger())
}

func newTestHandlerWithResolver(uom UOMClient, resolver PermissionsResolver) *Handler {
	return NewHandler(uom, resolver, testLogger())
}

func doJSON(t *testing.T, handler http.HandlerFunc, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func doJSONWithContext(t *testing.T, handler http.HandlerFunc, method, path string, body any, ctx context.Context) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf).WithContext(ctx)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func parseErrorResponse(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	return resp
}

// ---------------------------------------------------------------------------
// Tests: HandleLogin
// ---------------------------------------------------------------------------

func TestHandleLogin_Success(t *testing.T) {
	mock := &mockUOM{
		loginFn: func(_ context.Context, req uomclient.LoginRequest) (*uomclient.LoginResponse, error) {
			if req.Email != "user@example.com" || req.Password != "secret123" {
				t.Errorf("unexpected credentials: email=%q, password=%q", req.Email, req.Password)
			}
			return &uomclient.LoginResponse{
				AccessToken:  "at-123",
				RefreshToken: "rt-456",
				TokenType:    "Bearer",
				ExpiresIn:    900,
				User: &uomclient.UserProfile{
					UserID: "uid-1",
					Email:  "user@example.com",
					Name:   "Test User",
					Role:   "LAWYER",
				},
			}, nil
		},
	}

	h := newTestHandler(mock)
	rec := doJSON(t, h.HandleLogin(), "POST", "/api/v1/auth/login",
		map[string]string{"email": "user@example.com", "password": "secret123"})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp uomclient.LoginResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.AccessToken != "at-123" {
		t.Errorf("access_token = %q, want %q", resp.AccessToken, "at-123")
	}
	if resp.RefreshToken != "rt-456" {
		t.Errorf("refresh_token = %q, want %q", resp.RefreshToken, "rt-456")
	}
	if resp.User == nil || resp.User.Email != "user@example.com" {
		t.Errorf("user profile not returned correctly")
	}

	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

func TestHandleLogin_EmptyEmail(t *testing.T) {
	h := newTestHandler(&mockUOM{})
	rec := doJSON(t, h.HandleLogin(), "POST", "/api/v1/auth/login",
		map[string]string{"email": "", "password": "secret123"})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	resp := parseErrorResponse(t, rec)
	if resp["error_code"] != "VALIDATION_ERROR" {
		t.Errorf("error_code = %q, want VALIDATION_ERROR", resp["error_code"])
	}
}

func TestHandleLogin_WhitespaceOnlyEmail(t *testing.T) {
	h := newTestHandler(&mockUOM{})
	rec := doJSON(t, h.HandleLogin(), "POST", "/api/v1/auth/login",
		map[string]string{"email": "   ", "password": "secret123"})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandleLogin_EmptyPassword(t *testing.T) {
	h := newTestHandler(&mockUOM{})
	rec := doJSON(t, h.HandleLogin(), "POST", "/api/v1/auth/login",
		map[string]string{"email": "user@example.com", "password": ""})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	resp := parseErrorResponse(t, rec)
	if resp["error_code"] != "VALIDATION_ERROR" {
		t.Errorf("error_code = %q, want VALIDATION_ERROR", resp["error_code"])
	}
}

func TestHandleLogin_InvalidJSON(t *testing.T) {
	h := newTestHandler(&mockUOM{})
	req := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader("{invalid"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.HandleLogin().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	resp := parseErrorResponse(t, rec)
	if resp["error_code"] != "VALIDATION_ERROR" {
		t.Errorf("error_code = %q, want VALIDATION_ERROR", resp["error_code"])
	}
}

func TestHandleLogin_InvalidCredentials(t *testing.T) {
	mock := &mockUOM{
		loginFn: func(_ context.Context, _ uomclient.LoginRequest) (*uomclient.LoginResponse, error) {
			return nil, &uomclient.UOMError{
				Operation:  "Login",
				StatusCode: 401,
				Body:       []byte(`{"code":"INVALID_CREDENTIALS"}`),
				Retryable:  false,
			}
		},
	}

	h := newTestHandler(mock)
	rec := doJSON(t, h.HandleLogin(), "POST", "/api/v1/auth/login",
		map[string]string{"email": "user@example.com", "password": "wrong"})

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	resp := parseErrorResponse(t, rec)
	if resp["error_code"] != "INVALID_CREDENTIALS" {
		t.Errorf("error_code = %q, want INVALID_CREDENTIALS", resp["error_code"])
	}
}

func TestHandleLogin_UOMDown(t *testing.T) {
	mock := &mockUOM{
		loginFn: func(_ context.Context, _ uomclient.LoginRequest) (*uomclient.LoginResponse, error) {
			return nil, &uomclient.UOMError{
				Operation:  "Login",
				StatusCode: 503,
				Body:       []byte(`{"error":"service unavailable"}`),
				Retryable:  true,
			}
		},
	}

	h := newTestHandler(mock)
	rec := doJSON(t, h.HandleLogin(), "POST", "/api/v1/auth/login",
		map[string]string{"email": "user@example.com", "password": "secret123"})

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadGateway)
	}
	resp := parseErrorResponse(t, rec)
	if resp["error_code"] != "AUTH_SERVICE_UNAVAILABLE" {
		t.Errorf("error_code = %q, want AUTH_SERVICE_UNAVAILABLE", resp["error_code"])
	}
}

func TestHandleLogin_UOMTransportError(t *testing.T) {
	mock := &mockUOM{
		loginFn: func(_ context.Context, _ uomclient.LoginRequest) (*uomclient.LoginResponse, error) {
			return nil, &uomclient.UOMError{
				Operation: "Login",
				Retryable: true,
				Cause:     errors.New("connection refused"),
			}
		},
	}

	h := newTestHandler(mock)
	rec := doJSON(t, h.HandleLogin(), "POST", "/api/v1/auth/login",
		map[string]string{"email": "user@example.com", "password": "secret123"})

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadGateway)
	}
	resp := parseErrorResponse(t, rec)
	if resp["error_code"] != "AUTH_SERVICE_UNAVAILABLE" {
		t.Errorf("error_code = %q, want AUTH_SERVICE_UNAVAILABLE", resp["error_code"])
	}
}

func TestHandleLogin_ContextCanceled(t *testing.T) {
	mock := &mockUOM{
		loginFn: func(_ context.Context, _ uomclient.LoginRequest) (*uomclient.LoginResponse, error) {
			return nil, context.Canceled
		},
	}

	h := newTestHandler(mock)
	rec := doJSON(t, h.HandleLogin(), "POST", "/api/v1/auth/login",
		map[string]string{"email": "user@example.com", "password": "secret123"})

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadGateway)
	}
}

func TestHandleLogin_ContextDeadlineExceeded(t *testing.T) {
	mock := &mockUOM{
		loginFn: func(_ context.Context, _ uomclient.LoginRequest) (*uomclient.LoginResponse, error) {
			return nil, context.DeadlineExceeded
		},
	}

	h := newTestHandler(mock)
	rec := doJSON(t, h.HandleLogin(), "POST", "/api/v1/auth/login",
		map[string]string{"email": "user@example.com", "password": "secret123"})

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadGateway)
	}
}

func TestHandleLogin_UnknownError(t *testing.T) {
	mock := &mockUOM{
		loginFn: func(_ context.Context, _ uomclient.LoginRequest) (*uomclient.LoginResponse, error) {
			return nil, errors.New("unexpected")
		},
	}

	h := newTestHandler(mock)
	rec := doJSON(t, h.HandleLogin(), "POST", "/api/v1/auth/login",
		map[string]string{"email": "user@example.com", "password": "secret123"})

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	resp := parseErrorResponse(t, rec)
	if resp["error_code"] != "INTERNAL_ERROR" {
		t.Errorf("error_code = %q, want INTERNAL_ERROR", resp["error_code"])
	}
}

func TestHandleLogin_EmailTrimmed(t *testing.T) {
	var capturedEmail string
	mock := &mockUOM{
		loginFn: func(_ context.Context, req uomclient.LoginRequest) (*uomclient.LoginResponse, error) {
			capturedEmail = req.Email
			return &uomclient.LoginResponse{
				AccessToken:  "at",
				RefreshToken: "rt",
				TokenType:    "Bearer",
				ExpiresIn:    900,
			}, nil
		},
	}

	h := newTestHandler(mock)
	rec := doJSON(t, h.HandleLogin(), "POST", "/api/v1/auth/login",
		map[string]string{"email": "  user@example.com  ", "password": "secret123"})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if capturedEmail != "user@example.com" {
		t.Errorf("email = %q, want %q (trimmed)", capturedEmail, "user@example.com")
	}
}

func TestHandleLogin_CorrelationIDGenerated(t *testing.T) {
	mock := &mockUOM{
		loginFn: func(_ context.Context, _ uomclient.LoginRequest) (*uomclient.LoginResponse, error) {
			return nil, &uomclient.UOMError{
				Operation:  "Login",
				StatusCode: 401,
				Body:       []byte(`{"code":"INVALID_CREDENTIALS"}`),
			}
		},
	}

	h := newTestHandler(mock)
	rec := doJSON(t, h.HandleLogin(), "POST", "/api/v1/auth/login",
		map[string]string{"email": "user@example.com", "password": "wrong"})

	resp := parseErrorResponse(t, rec)
	cid, ok := resp["correlation_id"].(string)
	if !ok || cid == "" {
		t.Errorf("correlation_id should be generated for public auth endpoints, got %q", cid)
	}
}

func TestHandleLogin_BodyTooLarge(t *testing.T) {
	h := newTestHandler(&mockUOM{})
	// Generate a body larger than 4KB.
	large := strings.Repeat("x", 5*1024)
	req := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(large))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.HandleLogin().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandleLogin_UnknownFields(t *testing.T) {
	h := newTestHandler(&mockUOM{})
	rec := doJSON(t, h.HandleLogin(), "POST", "/api/v1/auth/login",
		map[string]string{"email": "user@example.com", "password": "secret", "extra": "field"})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; unknown fields should be rejected", rec.Code, http.StatusBadRequest)
	}
}

func TestHandleLogin_UOM400(t *testing.T) {
	mock := &mockUOM{
		loginFn: func(_ context.Context, _ uomclient.LoginRequest) (*uomclient.LoginResponse, error) {
			return nil, &uomclient.UOMError{
				Operation:  "Login",
				StatusCode: 400,
				Body:       []byte(`{"error":"bad request"}`),
				Retryable:  false,
			}
		},
	}

	h := newTestHandler(mock)
	rec := doJSON(t, h.HandleLogin(), "POST", "/api/v1/auth/login",
		map[string]string{"email": "user@example.com", "password": "secret"})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	resp := parseErrorResponse(t, rec)
	if resp["error_code"] != "VALIDATION_ERROR" {
		t.Errorf("error_code = %q, want VALIDATION_ERROR", resp["error_code"])
	}
}

// ---------------------------------------------------------------------------
// Tests: HandleRefresh
// ---------------------------------------------------------------------------

func TestHandleRefresh_Success(t *testing.T) {
	mock := &mockUOM{
		refreshFn: func(_ context.Context, req uomclient.RefreshRequest) (*uomclient.RefreshResponse, error) {
			if req.RefreshToken != "rt-old" {
				t.Errorf("refresh_token = %q, want %q", req.RefreshToken, "rt-old")
			}
			return &uomclient.RefreshResponse{
				AccessToken:  "at-new",
				RefreshToken: "rt-new",
				TokenType:    "Bearer",
				ExpiresIn:    900,
			}, nil
		},
	}

	h := newTestHandler(mock)
	rec := doJSON(t, h.HandleRefresh(), "POST", "/api/v1/auth/refresh",
		map[string]string{"refresh_token": "rt-old"})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp uomclient.RefreshResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.AccessToken != "at-new" {
		t.Errorf("access_token = %q, want %q", resp.AccessToken, "at-new")
	}
	if resp.RefreshToken != "rt-new" {
		t.Errorf("refresh_token = %q, want %q", resp.RefreshToken, "rt-new")
	}
}

func TestHandleRefresh_EmptyToken(t *testing.T) {
	h := newTestHandler(&mockUOM{})
	rec := doJSON(t, h.HandleRefresh(), "POST", "/api/v1/auth/refresh",
		map[string]string{"refresh_token": ""})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandleRefresh_WhitespaceOnlyToken(t *testing.T) {
	h := newTestHandler(&mockUOM{})
	rec := doJSON(t, h.HandleRefresh(), "POST", "/api/v1/auth/refresh",
		map[string]string{"refresh_token": "   "})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandleRefresh_TokenExpired(t *testing.T) {
	mock := &mockUOM{
		refreshFn: func(_ context.Context, _ uomclient.RefreshRequest) (*uomclient.RefreshResponse, error) {
			return nil, &uomclient.UOMError{
				Operation:  "Refresh",
				StatusCode: 401,
				Body:       []byte(`{"code":"TOKEN_EXPIRED"}`),
				Retryable:  false,
			}
		},
	}

	h := newTestHandler(mock)
	rec := doJSON(t, h.HandleRefresh(), "POST", "/api/v1/auth/refresh",
		map[string]string{"refresh_token": "rt-expired"})

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	resp := parseErrorResponse(t, rec)
	if resp["error_code"] != "REFRESH_TOKEN_EXPIRED" {
		t.Errorf("error_code = %q, want REFRESH_TOKEN_EXPIRED", resp["error_code"])
	}
}

func TestHandleRefresh_TokenRevoked(t *testing.T) {
	mock := &mockUOM{
		refreshFn: func(_ context.Context, _ uomclient.RefreshRequest) (*uomclient.RefreshResponse, error) {
			return nil, &uomclient.UOMError{
				Operation:  "Refresh",
				StatusCode: 401,
				Body:       []byte(`{"code":"TOKEN_REVOKED"}`),
				Retryable:  false,
			}
		},
	}

	h := newTestHandler(mock)
	rec := doJSON(t, h.HandleRefresh(), "POST", "/api/v1/auth/refresh",
		map[string]string{"refresh_token": "rt-revoked"})

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	resp := parseErrorResponse(t, rec)
	if resp["error_code"] != "TOKEN_REVOKED" {
		t.Errorf("error_code = %q, want TOKEN_REVOKED", resp["error_code"])
	}
}

func TestHandleRefresh_UOMDown(t *testing.T) {
	mock := &mockUOM{
		refreshFn: func(_ context.Context, _ uomclient.RefreshRequest) (*uomclient.RefreshResponse, error) {
			return nil, &uomclient.UOMError{
				Operation:  "Refresh",
				StatusCode: 503,
				Body:       []byte(`{}`),
				Retryable:  true,
			}
		},
	}

	h := newTestHandler(mock)
	rec := doJSON(t, h.HandleRefresh(), "POST", "/api/v1/auth/refresh",
		map[string]string{"refresh_token": "rt"})

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadGateway)
	}
}

func TestHandleRefresh_InvalidJSON(t *testing.T) {
	h := newTestHandler(&mockUOM{})
	req := httptest.NewRequest("POST", "/api/v1/auth/refresh", strings.NewReader("not json"))
	rec := httptest.NewRecorder()
	h.HandleRefresh().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

// ---------------------------------------------------------------------------
// Tests: HandleLogout
// ---------------------------------------------------------------------------

func TestHandleLogout_Success(t *testing.T) {
	var capturedToken string
	mock := &mockUOM{
		logoutFn: func(_ context.Context, req uomclient.LogoutRequest) error {
			capturedToken = req.RefreshToken
			return nil
		},
	}

	h := newTestHandler(mock)
	rec := doJSON(t, h.HandleLogout(), "POST", "/api/v1/auth/logout",
		map[string]string{"refresh_token": "rt-to-revoke"})

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if capturedToken != "rt-to-revoke" {
		t.Errorf("refresh_token = %q, want %q", capturedToken, "rt-to-revoke")
	}
	// 204 should have no body.
	if rec.Body.Len() != 0 {
		t.Errorf("body should be empty for 204, got %d bytes", rec.Body.Len())
	}
}

func TestHandleLogout_EmptyToken(t *testing.T) {
	h := newTestHandler(&mockUOM{})
	rec := doJSON(t, h.HandleLogout(), "POST", "/api/v1/auth/logout",
		map[string]string{"refresh_token": ""})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandleLogout_UOMDown(t *testing.T) {
	mock := &mockUOM{
		logoutFn: func(_ context.Context, _ uomclient.LogoutRequest) error {
			return &uomclient.UOMError{
				Operation:  "Logout",
				StatusCode: 502,
				Body:       []byte(`{}`),
				Retryable:  true,
			}
		},
	}

	h := newTestHandler(mock)
	rec := doJSON(t, h.HandleLogout(), "POST", "/api/v1/auth/logout",
		map[string]string{"refresh_token": "rt"})

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadGateway)
	}
}

func TestHandleLogout_InvalidJSON(t *testing.T) {
	h := newTestHandler(&mockUOM{})
	req := httptest.NewRequest("POST", "/api/v1/auth/logout", strings.NewReader("{bad"))
	rec := httptest.NewRecorder()
	h.HandleLogout().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

// ---------------------------------------------------------------------------
// Tests: HandleGetMe
// ---------------------------------------------------------------------------

func TestHandleGetMe_Success(t *testing.T) {
	mock := &mockUOM{
		getMeFn: func(_ context.Context) (*uomclient.UserProfile, error) {
			return &uomclient.UserProfile{
				UserID:         "uid-1",
				OrganizationID: "org-1",
				Email:          "user@example.com",
				Name:           "Test User",
				Role:           "LAWYER",
				CreatedAt:      "2024-01-01T00:00:00Z",
			}, nil
		},
	}

	h := newTestHandler(mock)
	ctx := auth.WithAuthContext(context.Background(), auth.AuthContext{
		UserID:         "uid-1",
		OrganizationID: "org-1",
		Role:           auth.RoleLawyer,
		TokenID:        "tid-1",
	})
	// Also set RequestContext for correlation_id.
	ctx = logger.WithRequestContext(ctx, logger.RequestContext{
		CorrelationID:  "corr-123",
		OrganizationID: "org-1",
		UserID:         "uid-1",
	})

	rec := doJSONWithContext(t, h.HandleGetMe(), "GET", "/api/v1/users/me", nil, ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var profile uomclient.UserProfile
	if err := json.NewDecoder(rec.Body).Decode(&profile); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if profile.UserID != "uid-1" {
		t.Errorf("user_id = %q, want %q", profile.UserID, "uid-1")
	}
	if profile.Email != "user@example.com" {
		t.Errorf("email = %q, want %q", profile.Email, "user@example.com")
	}
	if profile.Role != "LAWYER" {
		t.Errorf("role = %q, want %q", profile.Role, "LAWYER")
	}
}

func TestHandleGetMe_NoAuth(t *testing.T) {
	h := newTestHandler(&mockUOM{})
	rec := doJSON(t, h.HandleGetMe(), "GET", "/api/v1/users/me", nil)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	resp := parseErrorResponse(t, rec)
	if resp["error_code"] != "AUTH_TOKEN_MISSING" {
		t.Errorf("error_code = %q, want AUTH_TOKEN_MISSING", resp["error_code"])
	}
}

func TestHandleGetMe_UOMDown(t *testing.T) {
	mock := &mockUOM{
		getMeFn: func(_ context.Context) (*uomclient.UserProfile, error) {
			return nil, &uomclient.UOMError{
				Operation:  "GetMe",
				StatusCode: 503,
				Body:       []byte(`{}`),
				Retryable:  true,
			}
		},
	}

	h := newTestHandler(mock)
	ctx := auth.WithAuthContext(context.Background(), auth.AuthContext{
		UserID:         "uid-1",
		OrganizationID: "org-1",
		Role:           auth.RoleLawyer,
		TokenID:        "tid-1",
	})
	rec := doJSONWithContext(t, h.HandleGetMe(), "GET", "/api/v1/users/me", nil, ctx)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadGateway)
	}
}

func TestHandleGetMe_UOM404(t *testing.T) {
	mock := &mockUOM{
		getMeFn: func(_ context.Context) (*uomclient.UserProfile, error) {
			return nil, &uomclient.UOMError{
				Operation:  "GetMe",
				StatusCode: 404,
				Body:       []byte(`{"error":"user not found"}`),
				Retryable:  false,
			}
		},
	}

	h := newTestHandler(mock)
	ctx := auth.WithAuthContext(context.Background(), auth.AuthContext{
		UserID:         "uid-1",
		OrganizationID: "org-1",
		Role:           auth.RoleLawyer,
		TokenID:        "tid-1",
	})
	rec := doJSONWithContext(t, h.HandleGetMe(), "GET", "/api/v1/users/me", nil, ctx)

	// 404 from UOM maps to VALIDATION_ERROR (4xx) via MapUOMError.
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandleGetMe_AllRoles(t *testing.T) {
	mock := &mockUOM{
		getMeFn: func(_ context.Context) (*uomclient.UserProfile, error) {
			return &uomclient.UserProfile{
				UserID: "uid-1",
				Email:  "u@e.com",
				Role:   "LAWYER",
			}, nil
		},
	}

	roles := []auth.Role{auth.RoleLawyer, auth.RoleBusinessUser, auth.RoleOrgAdmin}
	for _, role := range roles {
		t.Run(string(role), func(t *testing.T) {
			h := newTestHandler(mock)
			ctx := auth.WithAuthContext(context.Background(), auth.AuthContext{
				UserID:         "uid-1",
				OrganizationID: "org-1",
				Role:           role,
				TokenID:        "tid-1",
			})
			rec := doJSONWithContext(t, h.HandleGetMe(), "GET", "/api/v1/users/me", nil, ctx)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d for role %s", rec.Code, http.StatusOK, role)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Tests: HandleGetMe with PermissionsResolver (ORCH-TASK-050)
// ---------------------------------------------------------------------------

type stubResolver struct {
	fn       func(ctx context.Context, role auth.Role, orgID string) permissions.UserPermissions
	lastRole auth.Role
	lastOrg  string
	calls    int
}

func (s *stubResolver) ResolveForUser(ctx context.Context, role auth.Role, orgID string) permissions.UserPermissions {
	s.calls++
	s.lastRole = role
	s.lastOrg = orgID
	if s.fn != nil {
		return s.fn(ctx, role, orgID)
	}
	return permissions.UserPermissions{}
}

func TestHandleGetMe_PermissionsEnriched(t *testing.T) {
	mock := &mockUOM{
		getMeFn: func(_ context.Context) (*uomclient.UserProfile, error) {
			return &uomclient.UserProfile{
				UserID:         "uid-1",
				OrganizationID: "org-1",
				Email:          "u@example.com",
				Name:           "Ivan",
				Role:           "BUSINESS_USER",
				CreatedAt:      "2024-01-01T00:00:00Z",
			}, nil
		},
	}
	resolver := &stubResolver{
		fn: func(_ context.Context, _ auth.Role, _ string) permissions.UserPermissions {
			return permissions.UserPermissions{ExportEnabled: true}
		},
	}

	h := newTestHandlerWithResolver(mock, resolver)
	ctx := auth.WithAuthContext(context.Background(), auth.AuthContext{
		UserID:         "uid-1",
		OrganizationID: "org-1",
		Role:           auth.RoleBusinessUser,
		TokenID:        "tid-1",
	})
	rec := doJSONWithContext(t, h.HandleGetMe(), "GET", "/api/v1/users/me", nil, ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	// UOM fields remain top-level.
	if body["user_id"] != "uid-1" {
		t.Errorf("user_id = %v, want uid-1", body["user_id"])
	}
	if body["email"] != "u@example.com" {
		t.Errorf("email = %v, want u@example.com", body["email"])
	}

	// permissions field present and structured.
	permsRaw, ok := body["permissions"].(map[string]any)
	if !ok {
		t.Fatalf("permissions missing or wrong type in response: %v", body["permissions"])
	}
	if permsRaw["export_enabled"] != true {
		t.Errorf("export_enabled = %v, want true", permsRaw["export_enabled"])
	}

	// Resolver received the AuthContext values, not UOM response values.
	if resolver.calls != 1 {
		t.Errorf("resolver.calls = %d, want 1", resolver.calls)
	}
	if resolver.lastRole != auth.RoleBusinessUser {
		t.Errorf("resolver.lastRole = %q, want BUSINESS_USER", resolver.lastRole)
	}
	if resolver.lastOrg != "org-1" {
		t.Errorf("resolver.lastOrg = %q, want org-1", resolver.lastOrg)
	}
}

func TestHandleGetMe_NilResolver_NoPermissionsField(t *testing.T) {
	mock := &mockUOM{
		getMeFn: func(_ context.Context) (*uomclient.UserProfile, error) {
			return &uomclient.UserProfile{UserID: "uid-1", Email: "u@e.com", Role: "LAWYER"}, nil
		},
	}

	h := newTestHandler(mock) // resolver = nil
	ctx := auth.WithAuthContext(context.Background(), auth.AuthContext{
		UserID: "uid-1", OrganizationID: "org-1", Role: auth.RoleLawyer, TokenID: "tid-1",
	})
	rec := doJSONWithContext(t, h.HandleGetMe(), "GET", "/api/v1/users/me", nil, ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if _, ok := body["permissions"]; ok {
		t.Errorf("permissions field present when resolver is nil; body=%v", body)
	}
}

func TestHandleGetMe_PermissionsForAllRoles(t *testing.T) {
	profileByRole := map[auth.Role]string{
		auth.RoleLawyer:       "LAWYER",
		auth.RoleBusinessUser: "BUSINESS_USER",
		auth.RoleOrgAdmin:     "ORG_ADMIN",
	}
	for role, roleStr := range profileByRole {
		t.Run(string(role), func(t *testing.T) {
			mock := &mockUOM{
				getMeFn: func(_ context.Context) (*uomclient.UserProfile, error) {
					return &uomclient.UserProfile{
						UserID: "uid-1", OrganizationID: "org-1", Role: roleStr,
					}, nil
				},
			}
			expectedExport := role == auth.RoleLawyer || role == auth.RoleOrgAdmin
			resolver := &stubResolver{
				fn: func(_ context.Context, r auth.Role, _ string) permissions.UserPermissions {
					// Reflect realistic resolver behavior.
					return permissions.UserPermissions{
						ExportEnabled: r == auth.RoleLawyer || r == auth.RoleOrgAdmin,
					}
				},
			}
			h := newTestHandlerWithResolver(mock, resolver)
			ctx := auth.WithAuthContext(context.Background(), auth.AuthContext{
				UserID: "uid-1", OrganizationID: "org-1", Role: role, TokenID: "tid-1",
			})
			rec := doJSONWithContext(t, h.HandleGetMe(), "GET", "/api/v1/users/me", nil, ctx)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
			}
			var body struct {
				Permissions permissions.UserPermissions `json:"permissions"`
			}
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if body.Permissions.ExportEnabled != expectedExport {
				t.Errorf("role=%s export_enabled=%v, want %v", role,
					body.Permissions.ExportEnabled, expectedExport)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Tests: HandleLogin — additional edge cases (code review)
// ---------------------------------------------------------------------------

func TestHandleLogin_EmptyJSONObject(t *testing.T) {
	h := newTestHandler(&mockUOM{})
	rec := doJSON(t, h.HandleLogin(), "POST", "/api/v1/auth/login",
		map[string]string{})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	resp := parseErrorResponse(t, rec)
	if resp["error_code"] != "VALIDATION_ERROR" {
		t.Errorf("error_code = %q, want VALIDATION_ERROR", resp["error_code"])
	}
}

func TestHandleLogin_DeadlineExceededWrappedInUOMError(t *testing.T) {
	mock := &mockUOM{
		loginFn: func(_ context.Context, _ uomclient.LoginRequest) (*uomclient.LoginResponse, error) {
			return nil, &uomclient.UOMError{
				Operation: "Login",
				Retryable: true,
				Cause:     context.DeadlineExceeded,
			}
		},
	}

	h := newTestHandler(mock)
	rec := doJSON(t, h.HandleLogin(), "POST", "/api/v1/auth/login",
		map[string]string{"email": "user@example.com", "password": "secret"})

	// errors.Is unwraps: UOMError.Cause = context.DeadlineExceeded
	// handleUOMError checks context.DeadlineExceeded first → 502
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadGateway)
	}
	resp := parseErrorResponse(t, rec)
	if resp["error_code"] != "AUTH_SERVICE_UNAVAILABLE" {
		t.Errorf("error_code = %q, want AUTH_SERVICE_UNAVAILABLE", resp["error_code"])
	}
}

// ---------------------------------------------------------------------------
// Tests: HandleLogout — additional edge cases (code review)
// ---------------------------------------------------------------------------

func TestHandleLogout_ContextCanceled(t *testing.T) {
	mock := &mockUOM{
		logoutFn: func(_ context.Context, _ uomclient.LogoutRequest) error {
			return context.Canceled
		},
	}

	h := newTestHandler(mock)
	rec := doJSON(t, h.HandleLogout(), "POST", "/api/v1/auth/logout",
		map[string]string{"refresh_token": "rt"})

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadGateway)
	}
}

// ---------------------------------------------------------------------------
// Tests: HandleRefresh — additional edge cases (code review)
// ---------------------------------------------------------------------------

func TestHandleRefresh_ContextCanceled(t *testing.T) {
	mock := &mockUOM{
		refreshFn: func(_ context.Context, _ uomclient.RefreshRequest) (*uomclient.RefreshResponse, error) {
			return nil, context.Canceled
		},
	}

	h := newTestHandler(mock)
	rec := doJSON(t, h.HandleRefresh(), "POST", "/api/v1/auth/refresh",
		map[string]string{"refresh_token": "rt"})

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadGateway)
	}
}

// ---------------------------------------------------------------------------
// Tests: handleUOMError — edge cases
// ---------------------------------------------------------------------------

func TestHandleUOMError_UOM500(t *testing.T) {
	mock := &mockUOM{
		loginFn: func(_ context.Context, _ uomclient.LoginRequest) (*uomclient.LoginResponse, error) {
			return nil, &uomclient.UOMError{
				Operation:  "Login",
				StatusCode: 500,
				Body:       []byte(`{"error":"internal"}`),
				Retryable:  true,
			}
		},
	}

	h := newTestHandler(mock)
	rec := doJSON(t, h.HandleLogin(), "POST", "/api/v1/auth/login",
		map[string]string{"email": "user@example.com", "password": "secret"})

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadGateway)
	}
	resp := parseErrorResponse(t, rec)
	if resp["error_code"] != "AUTH_SERVICE_UNAVAILABLE" {
		t.Errorf("error_code = %q, want AUTH_SERVICE_UNAVAILABLE", resp["error_code"])
	}
}

// ---------------------------------------------------------------------------
// Tests: ensureCorrelationID
// ---------------------------------------------------------------------------

func TestEnsureCorrelationID_Preserves(t *testing.T) {
	h := newTestHandler(&mockUOM{})
	rc := logger.RequestContext{CorrelationID: "existing-id"}
	ctx := logger.WithRequestContext(context.Background(), rc)
	result := h.ensureCorrelationID(httptest.NewRequest("POST", "/", nil).WithContext(ctx))
	got := logger.RequestContextFrom(result)
	if got.CorrelationID != "existing-id" {
		t.Errorf("correlation_id = %q, want %q", got.CorrelationID, "existing-id")
	}
}

func TestEnsureCorrelationID_Generates(t *testing.T) {
	h := newTestHandler(&mockUOM{})
	result := h.ensureCorrelationID(httptest.NewRequest("POST", "/", nil))
	got := logger.RequestContextFrom(result)
	if got.CorrelationID == "" {
		t.Error("correlation_id should be generated when missing")
	}
}

// ---------------------------------------------------------------------------
// Tests: decodeBody — edge cases
// ---------------------------------------------------------------------------

func TestDecodeBody_EmptyBody(t *testing.T) {
	h := newTestHandler(&mockUOM{})
	req := httptest.NewRequest("POST", "/", nil)
	rec := httptest.NewRecorder()

	var dest loginRequest
	ok := h.decodeBody(rec, req, &dest)

	if ok {
		t.Error("decodeBody should return false for empty body")
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

// ---------------------------------------------------------------------------
// Tests: Interface compliance
// ---------------------------------------------------------------------------

func TestInterfaceCompliance(t *testing.T) {
	var _ UOMClient = (*mockUOM)(nil)
	var _ UOMClient = (*uomclient.Client)(nil)
}

// ---------------------------------------------------------------------------
// Tests: Constructor
// ---------------------------------------------------------------------------

func TestNewHandler(t *testing.T) {
	h := NewHandler(&mockUOM{}, nil, testLogger())
	if h == nil {
		t.Fatal("NewHandler returned nil")
	}
	if h.uom == nil {
		t.Error("uom client should not be nil")
	}
	if h.log == nil {
		t.Error("logger should not be nil")
	}
	if h.resolver != nil {
		t.Error("resolver should be nil (not provided)")
	}
}
