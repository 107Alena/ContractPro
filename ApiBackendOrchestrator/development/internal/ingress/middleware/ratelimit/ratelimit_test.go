package ratelimit

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"contractpro/api-orchestrator/internal/config"
	"contractpro/api-orchestrator/internal/infra/observability/logger"
	"contractpro/api-orchestrator/internal/ingress/middleware/auth"
)

// --- test doubles ---

// mockStore implements RateLimiterStore for unit tests.
type mockStore struct {
	mu      sync.Mutex
	allowFn func(ctx context.Context, key string, limit int, window time.Duration) (bool, error)
	calls   []storeCall
}

type storeCall struct {
	Key    string
	Limit  int
	Window time.Duration
}

func (m *mockStore) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	m.mu.Lock()
	m.calls = append(m.calls, storeCall{Key: key, Limit: limit, Window: window})
	m.mu.Unlock()
	return m.allowFn(ctx, key, limit, window)
}

var _ RateLimiterStore = (*mockStore)(nil)

// --- helpers ---

func testLogger() *logger.Logger {
	return logger.NewLogger("error")
}

func testConfig(enabled bool) config.RateLimitConfig {
	return config.RateLimitConfig{
		Enabled:  enabled,
		ReadRPS:  200,
		WriteRPS: 50,
	}
}

// newTestRequest creates an HTTP request with AuthContext and
// RequestContext injected, simulating a request that has already
// passed through the auth and security-headers middleware chain.
func newTestRequest(method, path string, orgID string) *http.Request {
	r := httptest.NewRequest(method, path, nil)
	ctx := auth.WithAuthContext(r.Context(), auth.AuthContext{
		OrganizationID: orgID,
		UserID:         "user-1",
		Role:           auth.RoleLawyer,
		TokenID:        "token-1",
	})
	ctx = logger.WithRequestContext(ctx, logger.RequestContext{
		CorrelationID:  "test-corr-id",
		OrganizationID: orgID,
		UserID:         "user-1",
	})
	return r.WithContext(ctx)
}

// handlerCalled returns an http.Handler that sets a flag when called.
func handlerCalled(called *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		*called = true
		w.WriteHeader(http.StatusOK)
	})
}

type errorResponse struct {
	ErrorCode     string `json:"error_code"`
	Message       string `json:"message"`
	CorrelationID string `json:"correlation_id"`
	Suggestion    string `json:"suggestion"`
}

// --- constructor tests ---

func TestNewMiddleware_NilStoreWhenEnabled(t *testing.T) {
	_, err := NewMiddleware(testConfig(true), nil, testLogger())
	if err == nil {
		t.Fatal("expected error for nil store when enabled")
	}
}

func TestNewMiddleware_NilStoreWhenDisabled(t *testing.T) {
	m, err := NewMiddleware(testConfig(false), nil, testLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m == nil {
		t.Fatal("expected non-nil middleware")
	}
}

func TestNewMiddleware_ZeroReadRPS(t *testing.T) {
	store := &mockStore{allowFn: func(context.Context, string, int, time.Duration) (bool, error) {
		return true, nil
	}}
	cfg := config.RateLimitConfig{Enabled: true, ReadRPS: 0, WriteRPS: 50}
	_, err := NewMiddleware(cfg, store, testLogger())
	if err == nil {
		t.Fatal("expected error for ReadRPS=0 when enabled")
	}
}

func TestNewMiddleware_ZeroWriteRPS(t *testing.T) {
	store := &mockStore{allowFn: func(context.Context, string, int, time.Duration) (bool, error) {
		return true, nil
	}}
	cfg := config.RateLimitConfig{Enabled: true, ReadRPS: 200, WriteRPS: 0}
	_, err := NewMiddleware(cfg, store, testLogger())
	if err == nil {
		t.Fatal("expected error for WriteRPS=0 when enabled")
	}
}

func TestNewMiddleware_NegativeRPS(t *testing.T) {
	store := &mockStore{allowFn: func(context.Context, string, int, time.Duration) (bool, error) {
		return true, nil
	}}
	cfg := config.RateLimitConfig{Enabled: true, ReadRPS: -1, WriteRPS: 50}
	_, err := NewMiddleware(cfg, store, testLogger())
	if err == nil {
		t.Fatal("expected error for negative ReadRPS when enabled")
	}
}

func TestNewMiddleware_Success(t *testing.T) {
	store := &mockStore{allowFn: func(context.Context, string, int, time.Duration) (bool, error) {
		return true, nil
	}}
	m, err := NewMiddleware(testConfig(true), store, testLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m == nil {
		t.Fatal("expected non-nil middleware")
	}
}

// --- disabled middleware ---

func TestHandler_Disabled_PassesThrough(t *testing.T) {
	m, err := NewMiddleware(testConfig(false), nil, testLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var called bool
	handler := m.Handler()(handlerCalled(&called))

	rec := httptest.NewRecorder()
	req := newTestRequest(http.MethodGet, "/api/v1/contracts", "org-1")
	handler.ServeHTTP(rec, req)

	if !called {
		t.Fatal("expected next handler to be called when disabled")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

// --- GET within read limit ---

func TestHandler_GET_WithinLimit(t *testing.T) {
	store := &mockStore{allowFn: func(context.Context, string, int, time.Duration) (bool, error) {
		return true, nil
	}}
	m, _ := NewMiddleware(testConfig(true), store, testLogger())

	var called bool
	handler := m.Handler()(handlerCalled(&called))

	rec := httptest.NewRecorder()
	req := newTestRequest(http.MethodGet, "/api/v1/contracts", "org-abc")
	handler.ServeHTTP(rec, req)

	if !called {
		t.Fatal("expected next handler to be called")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	if len(store.calls) != 1 {
		t.Fatalf("expected 1 store call, got %d", len(store.calls))
	}
	c := store.calls[0]
	if c.Key != "rl:org-abc:read" {
		t.Errorf("expected key 'rl:org-abc:read', got %q", c.Key)
	}
	if c.Limit != 200 {
		t.Errorf("expected limit 200, got %d", c.Limit)
	}
	if c.Window != time.Second {
		t.Errorf("expected 1s window, got %v", c.Window)
	}
}

// --- POST within write limit ---

func TestHandler_POST_WithinLimit(t *testing.T) {
	store := &mockStore{allowFn: func(context.Context, string, int, time.Duration) (bool, error) {
		return true, nil
	}}
	m, _ := NewMiddleware(testConfig(true), store, testLogger())

	var called bool
	handler := m.Handler()(handlerCalled(&called))

	rec := httptest.NewRecorder()
	req := newTestRequest(http.MethodPost, "/api/v1/contracts/upload", "org-xyz")
	handler.ServeHTTP(rec, req)

	if !called {
		t.Fatal("expected next handler to be called")
	}
	c := store.calls[0]
	if c.Key != "rl:org-xyz:write" {
		t.Errorf("expected key 'rl:org-xyz:write', got %q", c.Key)
	}
	if c.Limit != 50 {
		t.Errorf("expected limit 50, got %d", c.Limit)
	}
}

// --- GET exceeds read limit ---

func TestHandler_GET_ExceedsLimit(t *testing.T) {
	store := &mockStore{allowFn: func(context.Context, string, int, time.Duration) (bool, error) {
		return false, nil
	}}
	m, _ := NewMiddleware(testConfig(true), store, testLogger())

	var called bool
	handler := m.Handler()(handlerCalled(&called))

	rec := httptest.NewRecorder()
	req := newTestRequest(http.MethodGet, "/api/v1/contracts", "org-1")
	handler.ServeHTTP(rec, req)

	if called {
		t.Fatal("next handler should NOT be called when rate limited")
	}
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rec.Code)
	}
	if ra := rec.Header().Get("Retry-After"); ra != "1" {
		t.Errorf("expected Retry-After: 1, got %q", ra)
	}

	var resp errorResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
	if resp.ErrorCode != "RATE_LIMIT_EXCEEDED" {
		t.Errorf("expected error_code RATE_LIMIT_EXCEEDED, got %q", resp.ErrorCode)
	}
	if resp.CorrelationID != "test-corr-id" {
		t.Errorf("expected correlation_id 'test-corr-id', got %q", resp.CorrelationID)
	}
}

// --- POST exceeds write limit ---

func TestHandler_POST_ExceedsLimit(t *testing.T) {
	store := &mockStore{allowFn: func(context.Context, string, int, time.Duration) (bool, error) {
		return false, nil
	}}
	m, _ := NewMiddleware(testConfig(true), store, testLogger())

	var called bool
	handler := m.Handler()(handlerCalled(&called))

	rec := httptest.NewRecorder()
	req := newTestRequest(http.MethodPost, "/api/v1/contracts/upload", "org-1")
	handler.ServeHTTP(rec, req)

	if called {
		t.Fatal("next handler should NOT be called when rate limited")
	}
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rec.Code)
	}
	if ra := rec.Header().Get("Retry-After"); ra != "1" {
		t.Errorf("expected Retry-After: 1, got %q", ra)
	}
}

// --- Redis unavailable → allow (degraded mode) ---

func TestHandler_RedisUnavailable_GET(t *testing.T) {
	store := &mockStore{allowFn: func(context.Context, string, int, time.Duration) (bool, error) {
		return false, errors.New("redis: connection refused")
	}}
	m, _ := NewMiddleware(testConfig(true), store, testLogger())

	var called bool
	handler := m.Handler()(handlerCalled(&called))

	rec := httptest.NewRecorder()
	req := newTestRequest(http.MethodGet, "/api/v1/contracts", "org-1")
	handler.ServeHTTP(rec, req)

	if !called {
		t.Fatal("expected next handler to be called in degraded mode")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (degraded), got %d", rec.Code)
	}
}

func TestHandler_RedisUnavailable_POST(t *testing.T) {
	store := &mockStore{allowFn: func(context.Context, string, int, time.Duration) (bool, error) {
		return false, errors.New("redis: connection refused")
	}}
	m, _ := NewMiddleware(testConfig(true), store, testLogger())

	var called bool
	handler := m.Handler()(handlerCalled(&called))

	rec := httptest.NewRecorder()
	req := newTestRequest(http.MethodPost, "/api/v1/contracts/upload", "org-1")
	handler.ServeHTTP(rec, req)

	if !called {
		t.Fatal("expected next handler to be called in degraded mode")
	}
}

// --- Missing AuthContext ---

func TestHandler_MissingAuthContext(t *testing.T) {
	store := &mockStore{allowFn: func(context.Context, string, int, time.Duration) (bool, error) {
		t.Fatal("store should not be called when AuthContext is missing")
		return false, nil
	}}
	m, _ := NewMiddleware(testConfig(true), store, testLogger())

	var called bool
	handler := m.Handler()(handlerCalled(&called))

	rec := httptest.NewRecorder()
	// Request without AuthContext.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/contracts", nil)
	ctx := logger.WithRequestContext(req.Context(), logger.RequestContext{
		CorrelationID: "test-corr-id",
	})
	req = req.WithContext(ctx)

	handler.ServeHTTP(rec, req)

	if called {
		t.Fatal("next handler should NOT be called when AuthContext is missing")
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}

	var resp errorResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
	if resp.ErrorCode != "INTERNAL_ERROR" {
		t.Errorf("expected INTERNAL_ERROR, got %q", resp.ErrorCode)
	}
}

// --- HTTP method classification ---

func TestHandler_HEAD_ClassifiedAsRead(t *testing.T) {
	store := &mockStore{allowFn: func(context.Context, string, int, time.Duration) (bool, error) {
		return true, nil
	}}
	m, _ := NewMiddleware(testConfig(true), store, testLogger())
	handler := m.Handler()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := newTestRequest(http.MethodHead, "/api/v1/contracts", "org-1")
	handler.ServeHTTP(rec, req)

	c := store.calls[0]
	if c.Key != "rl:org-1:read" {
		t.Errorf("HEAD should be classified as read, got key %q", c.Key)
	}
	if c.Limit != 200 {
		t.Errorf("expected read limit 200, got %d", c.Limit)
	}
}

func TestHandler_PUT_ClassifiedAsWrite(t *testing.T) {
	store := &mockStore{allowFn: func(context.Context, string, int, time.Duration) (bool, error) {
		return true, nil
	}}
	m, _ := NewMiddleware(testConfig(true), store, testLogger())
	handler := m.Handler()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := newTestRequest(http.MethodPut, "/api/v1/admin/policies/1", "org-1")
	handler.ServeHTTP(rec, req)

	c := store.calls[0]
	if c.Key != "rl:org-1:write" {
		t.Errorf("PUT should be classified as write, got key %q", c.Key)
	}
	if c.Limit != 50 {
		t.Errorf("expected write limit 50, got %d", c.Limit)
	}
}

func TestHandler_DELETE_ClassifiedAsWrite(t *testing.T) {
	store := &mockStore{allowFn: func(context.Context, string, int, time.Duration) (bool, error) {
		return true, nil
	}}
	m, _ := NewMiddleware(testConfig(true), store, testLogger())
	handler := m.Handler()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := newTestRequest(http.MethodDelete, "/api/v1/contracts/1", "org-1")
	handler.ServeHTTP(rec, req)

	c := store.calls[0]
	if c.Key != "rl:org-1:write" {
		t.Errorf("DELETE should be classified as write, got key %q", c.Key)
	}
}

func TestHandler_PATCH_ClassifiedAsWrite(t *testing.T) {
	store := &mockStore{allowFn: func(context.Context, string, int, time.Duration) (bool, error) {
		return true, nil
	}}
	m, _ := NewMiddleware(testConfig(true), store, testLogger())
	handler := m.Handler()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := newTestRequest(http.MethodPatch, "/api/v1/contracts/1", "org-1")
	handler.ServeHTTP(rec, req)

	c := store.calls[0]
	if c.Key != "rl:org-1:write" {
		t.Errorf("PATCH should be classified as write, got key %q", c.Key)
	}
}

// --- Key format correctness ---

func TestHandler_KeyFormat(t *testing.T) {
	store := &mockStore{allowFn: func(context.Context, string, int, time.Duration) (bool, error) {
		return true, nil
	}}
	m, _ := NewMiddleware(testConfig(true), store, testLogger())
	handler := m.Handler()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	orgID := "550e8400-e29b-41d4-a716-446655440000"
	rec := httptest.NewRecorder()
	req := newTestRequest(http.MethodGet, "/api/v1/contracts", orgID)
	handler.ServeHTTP(rec, req)

	expected := "rl:550e8400-e29b-41d4-a716-446655440000:read"
	if store.calls[0].Key != expected {
		t.Errorf("expected key %q, got %q", expected, store.calls[0].Key)
	}
}

// --- Context cancellation during store call ---

func TestHandler_ContextCancelled(t *testing.T) {
	store := &mockStore{allowFn: func(context.Context, string, int, time.Duration) (bool, error) {
		return false, context.Canceled
	}}
	m, _ := NewMiddleware(testConfig(true), store, testLogger())

	var called bool
	handler := m.Handler()(handlerCalled(&called))

	rec := httptest.NewRecorder()
	req := newTestRequest(http.MethodGet, "/api/v1/contracts", "org-1")
	handler.ServeHTTP(rec, req)

	// Context cancellation is treated as Redis unavailable → allow.
	if !called {
		t.Fatal("expected next handler to be called (degraded mode)")
	}
}

// --- Response format ---

func TestHandler_RateLimited_ResponseContentType(t *testing.T) {
	store := &mockStore{allowFn: func(context.Context, string, int, time.Duration) (bool, error) {
		return false, nil
	}}
	m, _ := NewMiddleware(testConfig(true), store, testLogger())

	handler := m.Handler()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := newTestRequest(http.MethodGet, "/api/v1/contracts", "org-1")
	handler.ServeHTTP(rec, req)

	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}
}

func TestHandler_RateLimited_RussianMessage(t *testing.T) {
	store := &mockStore{allowFn: func(context.Context, string, int, time.Duration) (bool, error) {
		return false, nil
	}}
	m, _ := NewMiddleware(testConfig(true), store, testLogger())

	handler := m.Handler()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := newTestRequest(http.MethodGet, "/api/v1/contracts", "org-1")
	handler.ServeHTTP(rec, req)

	var resp errorResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
	if resp.Message == "" {
		t.Error("expected non-empty Russian message")
	}
	if resp.Suggestion == "" {
		t.Error("expected non-empty Russian suggestion")
	}
}

// --- Per-organization isolation ---

func TestHandler_DifferentOrgs_DifferentKeys(t *testing.T) {
	store := &mockStore{allowFn: func(context.Context, string, int, time.Duration) (bool, error) {
		return true, nil
	}}
	m, _ := NewMiddleware(testConfig(true), store, testLogger())
	handler := m.Handler()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec1 := httptest.NewRecorder()
	req1 := newTestRequest(http.MethodGet, "/api/v1/contracts", "org-A")
	handler.ServeHTTP(rec1, req1)

	rec2 := httptest.NewRecorder()
	req2 := newTestRequest(http.MethodGet, "/api/v1/contracts", "org-B")
	handler.ServeHTTP(rec2, req2)

	if store.calls[0].Key == store.calls[1].Key {
		t.Errorf("different orgs should have different keys, both got %q", store.calls[0].Key)
	}
	if store.calls[0].Key != "rl:org-A:read" {
		t.Errorf("expected 'rl:org-A:read', got %q", store.calls[0].Key)
	}
	if store.calls[1].Key != "rl:org-B:read" {
		t.Errorf("expected 'rl:org-B:read', got %q", store.calls[1].Key)
	}
}

// --- operationClass unit tests ---

func TestOperationClass(t *testing.T) {
	tests := []struct {
		method string
		want   string
	}{
		{http.MethodGet, "read"},
		{http.MethodHead, "read"},
		{http.MethodPost, "write"},
		{http.MethodPut, "write"},
		{http.MethodDelete, "write"},
		{http.MethodPatch, "write"},
		{"UNKNOWN", "write"},
	}
	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			if got := operationClass(tt.method); got != tt.want {
				t.Errorf("operationClass(%q) = %q, want %q", tt.method, got, tt.want)
			}
		})
	}
}

// --- rateLimitKey unit tests ---

func TestRateLimitKey(t *testing.T) {
	tests := []struct {
		orgID string
		class string
		want  string
	}{
		{"org-1", "read", "rl:org-1:read"},
		{"org-1", "write", "rl:org-1:write"},
		{"550e8400-e29b-41d4-a716-446655440000", "read", "rl:550e8400-e29b-41d4-a716-446655440000:read"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := rateLimitKey(tt.orgID, tt.class); got != tt.want {
				t.Errorf("rateLimitKey(%q, %q) = %q, want %q", tt.orgID, tt.class, got, tt.want)
			}
		})
	}
}

// --- Disabled middleware does not call store ---

func TestHandler_Disabled_NoStoreCall(t *testing.T) {
	store := &mockStore{allowFn: func(context.Context, string, int, time.Duration) (bool, error) {
		t.Fatal("store should not be called when disabled")
		return false, nil
	}}
	m, _ := NewMiddleware(config.RateLimitConfig{
		Enabled:  false,
		ReadRPS:  200,
		WriteRPS: 50,
	}, store, testLogger())

	handler := m.Handler()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := newTestRequest(http.MethodGet, "/api/v1/contracts", "org-1")
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

// --- Concurrent safety ---

func TestHandler_ConcurrentRequests(t *testing.T) {
	var mu sync.Mutex
	callCount := 0
	store := &mockStore{allowFn: func(context.Context, string, int, time.Duration) (bool, error) {
		mu.Lock()
		callCount++
		mu.Unlock()
		return true, nil
	}}
	m, _ := NewMiddleware(testConfig(true), store, testLogger())
	handler := m.Handler()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			req := newTestRequest(http.MethodGet, "/api/v1/contracts", "org-1")
			handler.ServeHTTP(rec, req)
		}()
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if callCount != n {
		t.Errorf("expected %d store calls, got %d", n, callCount)
	}
}
