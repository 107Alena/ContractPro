package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// OrgRateLimiter unit tests
// ---------------------------------------------------------------------------

func TestNewOrgRateLimiter_PanicOnInvalidReadRPS(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for readRPS <= 0")
		}
	}()
	NewOrgRateLimiter(0, 20, time.Minute, time.Minute)
}

func TestNewOrgRateLimiter_PanicOnInvalidWriteRPS(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for writeRPS <= 0")
		}
	}()
	NewOrgRateLimiter(100, 0, time.Minute, time.Minute)
}

func TestOrgRateLimiter_AllowRead(t *testing.T) {
	t.Parallel()
	limiter := NewOrgRateLimiter(5, 5, time.Hour, time.Hour)
	defer limiter.Close()

	// Should allow up to burst (5 for readRPS=5).
	for i := 0; i < 5; i++ {
		allowed, lt, _ := limiter.Allow("org-1", true)
		if !allowed {
			t.Fatalf("expected read %d to be allowed", i)
		}
		if lt != "read" {
			t.Fatalf("expected limit type 'read', got %q", lt)
		}
	}

	// 6th request should be rejected.
	allowed, _, retryAfter := limiter.Allow("org-1", true)
	if allowed {
		t.Fatal("expected 6th read to be rejected")
	}
	if retryAfter < 1 {
		t.Fatalf("expected retryAfter >= 1, got %d", retryAfter)
	}
}

func TestOrgRateLimiter_AllowWrite(t *testing.T) {
	t.Parallel()
	limiter := NewOrgRateLimiter(100, 3, time.Hour, time.Hour)
	defer limiter.Close()

	// Should allow up to burst (3 for writeRPS=3).
	for i := 0; i < 3; i++ {
		allowed, lt, _ := limiter.Allow("org-1", false)
		if !allowed {
			t.Fatalf("expected write %d to be allowed", i)
		}
		if lt != "write" {
			t.Fatalf("expected limit type 'write', got %q", lt)
		}
	}

	// 4th write should be rejected.
	allowed, _, _ := limiter.Allow("org-1", false)
	if allowed {
		t.Fatal("expected 4th write to be rejected")
	}
}

func TestOrgRateLimiter_SeparateBudgets(t *testing.T) {
	t.Parallel()
	limiter := NewOrgRateLimiter(5, 3, time.Hour, time.Hour)
	defer limiter.Close()

	// Exhaust write budget.
	for i := 0; i < 3; i++ {
		limiter.Allow("org-1", false)
	}
	allowed, _, _ := limiter.Allow("org-1", false)
	if allowed {
		t.Fatal("writes should be exhausted")
	}

	// Read budget should still be available.
	allowed, _, _ = limiter.Allow("org-1", true)
	if !allowed {
		t.Fatal("reads should still be allowed after writes exhausted")
	}
}

func TestOrgRateLimiter_PerOrgIsolation(t *testing.T) {
	t.Parallel()
	limiter := NewOrgRateLimiter(3, 3, time.Hour, time.Hour)
	defer limiter.Close()

	// Exhaust org-A read budget.
	for i := 0; i < 3; i++ {
		limiter.Allow("org-A", true)
	}
	allowed, _, _ := limiter.Allow("org-A", true)
	if allowed {
		t.Fatal("org-A reads should be exhausted")
	}

	// org-B should still have its own budget.
	allowed, _, _ = limiter.Allow("org-B", true)
	if !allowed {
		t.Fatal("org-B should not be affected by org-A's rate limit")
	}
}

func TestOrgRateLimiter_RetryAfterPositive(t *testing.T) {
	t.Parallel()
	limiter := NewOrgRateLimiter(2, 2, time.Hour, time.Hour)
	defer limiter.Close()

	// Exhaust burst.
	limiter.Allow("org-1", true)
	limiter.Allow("org-1", true)

	// Next should return retryAfter >= 1.
	allowed, _, retryAfter := limiter.Allow("org-1", true)
	if allowed {
		t.Fatal("expected rejection")
	}
	if retryAfter < 1 {
		t.Fatalf("expected retryAfter >= 1, got %d", retryAfter)
	}
}

func TestOrgRateLimiter_GCEvictsIdleOrgs(t *testing.T) {
	t.Parallel()

	limiter := NewOrgRateLimiter(100, 20, time.Hour, time.Hour)
	defer limiter.Close()

	// Use a controllable clock.
	now := time.Now()
	limiter.mu.Lock()
	limiter.nowFunc = func() time.Time { return now }
	limiter.idleTTL = 5 * time.Minute
	limiter.mu.Unlock()

	// Create an org entry.
	limiter.Allow("org-evict", true)
	if limiter.OrgCount() != 1 {
		t.Fatal("expected 1 org after Allow")
	}

	// Advance clock past idle TTL.
	now = now.Add(6 * time.Minute)

	// Manually trigger eviction.
	limiter.evictIdle()

	if limiter.OrgCount() != 0 {
		t.Fatal("expected 0 orgs after eviction")
	}
}

func TestOrgRateLimiter_GCKeepsActiveOrgs(t *testing.T) {
	t.Parallel()

	limiter := NewOrgRateLimiter(100, 20, time.Hour, time.Hour)
	defer limiter.Close()

	now := time.Now()
	limiter.mu.Lock()
	limiter.nowFunc = func() time.Time { return now }
	limiter.idleTTL = 5 * time.Minute
	limiter.mu.Unlock()

	limiter.Allow("org-active", true)

	// Advance clock within idle TTL.
	now = now.Add(3 * time.Minute)
	limiter.evictIdle()

	if limiter.OrgCount() != 1 {
		t.Fatal("expected org-active to survive eviction")
	}
}

func TestOrgRateLimiter_CloseIdempotent(t *testing.T) {
	t.Parallel()
	limiter := NewOrgRateLimiter(100, 20, time.Minute, time.Minute)
	limiter.Close()
	limiter.Close() // Should not panic.
}

func TestOrgRateLimiter_ConcurrentAccess(t *testing.T) {
	t.Parallel()
	limiter := NewOrgRateLimiter(1000, 1000, time.Hour, time.Hour)
	defer limiter.Close()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				limiter.Allow("org-concurrent", true)
				limiter.Allow("org-concurrent", false)
			}
		}()
	}
	wg.Wait()
	// No race detector errors is the success condition.
}

// ---------------------------------------------------------------------------
// rateLimitMiddleware tests
// ---------------------------------------------------------------------------

type recordingMetrics struct {
	mu     sync.Mutex
	counts map[string]int
}

func (r *recordingMetrics) IncRateLimited(limitType string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counts[limitType]++
}

func (r *recordingMetrics) getCount(limitType string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.counts[limitType]
}

func TestRateLimitMiddleware_NilLimiter_Passthrough(t *testing.T) {
	t.Parallel()

	var called atomic.Bool
	handler := rateLimitMiddleware(nil, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called.Store(true)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req = req.WithContext(context.WithValue(req.Context(), authContextKey{}, &AuthContext{
		OrganizationID: "org-1",
		UserID:         "user-1",
	}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !called.Load() {
		t.Fatal("handler should have been called with nil limiter")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestRateLimitMiddleware_NoAuthContext_Passthrough(t *testing.T) {
	t.Parallel()

	limiter := NewOrgRateLimiter(1, 1, time.Hour, time.Hour)
	defer limiter.Close()

	var called atomic.Bool
	handler := rateLimitMiddleware(limiter, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called.Store(true)
		w.WriteHeader(http.StatusOK)
	}))

	// Request without auth context.
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !called.Load() {
		t.Fatal("handler should have been called without auth context")
	}
}

func TestRateLimitMiddleware_429Response(t *testing.T) {
	t.Parallel()

	limiter := NewOrgRateLimiter(1, 1, time.Hour, time.Hour)
	defer limiter.Close()

	metrics := &recordingMetrics{counts: make(map[string]int)}
	handler := rateLimitMiddleware(limiter, metrics)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	sendReq := func(method string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/test", nil)
		req = req.WithContext(context.WithValue(req.Context(), authContextKey{}, &AuthContext{
			OrganizationID: "org-429",
			UserID:         "user-1",
		}))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}

	// First GET should pass.
	w := sendReq(http.MethodGet)
	if w.Code != http.StatusOK {
		t.Fatalf("first GET should be 200, got %d", w.Code)
	}

	// Second GET should be rate limited.
	w = sendReq(http.MethodGet)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("second GET should be 429, got %d", w.Code)
	}

	// Verify Retry-After header.
	retryAfter := w.Header().Get("Retry-After")
	if retryAfter == "" {
		t.Fatal("expected Retry-After header")
	}

	// Verify JSON body.
	var errResp ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if errResp.ErrorCode != "RATE_LIMITED" {
		t.Fatalf("expected error code RATE_LIMITED, got %q", errResp.ErrorCode)
	}

	// Verify metrics.
	if metrics.getCount("read") != 1 {
		t.Fatalf("expected 1 read rate limit increment, got %d", metrics.getCount("read"))
	}
}

func TestRateLimitMiddleware_ReadVsWrite(t *testing.T) {
	t.Parallel()

	// 2 reads/s, 1 write/s.
	limiter := NewOrgRateLimiter(2, 1, time.Hour, time.Hour)
	defer limiter.Close()

	metrics := &recordingMetrics{counts: make(map[string]int)}
	handler := rateLimitMiddleware(limiter, metrics)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	makeReq := func(method string) int {
		req := httptest.NewRequest(method, "/test", nil)
		req = req.WithContext(context.WithValue(req.Context(), authContextKey{}, &AuthContext{
			OrganizationID: "org-rw",
			UserID:         "user-1",
		}))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w.Code
	}

	// 1 write should pass.
	if code := makeReq(http.MethodPost); code != http.StatusOK {
		t.Fatalf("first POST should be 200, got %d", code)
	}

	// 2nd write should be rate limited.
	if code := makeReq(http.MethodPost); code != http.StatusTooManyRequests {
		t.Fatalf("second POST should be 429, got %d", code)
	}

	// But reads should still work (separate budget).
	if code := makeReq(http.MethodGet); code != http.StatusOK {
		t.Fatalf("GET should still be 200 after writes exhausted, got %d", code)
	}
}

func TestRateLimitMiddleware_HeadIsTreatedAsRead(t *testing.T) {
	t.Parallel()

	limiter := NewOrgRateLimiter(2, 1, time.Hour, time.Hour)
	defer limiter.Close()

	handler := rateLimitMiddleware(limiter, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// HEAD should use read budget.
	req := httptest.NewRequest(http.MethodHead, "/test", nil)
	req = req.WithContext(context.WithValue(req.Context(), authContextKey{}, &AuthContext{
		OrganizationID: "org-head",
		UserID:         "user-1",
	}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("HEAD should be allowed, got %d", w.Code)
	}
}

func TestRateLimitMiddleware_DeleteIsTreatedAsWrite(t *testing.T) {
	t.Parallel()

	limiter := NewOrgRateLimiter(100, 1, time.Hour, time.Hour)
	defer limiter.Close()

	metrics := &recordingMetrics{counts: make(map[string]int)}
	handler := rateLimitMiddleware(limiter, metrics)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	makeReq := func(method string) int {
		req := httptest.NewRequest(method, "/test", nil)
		req = req.WithContext(context.WithValue(req.Context(), authContextKey{}, &AuthContext{
			OrganizationID: "org-del",
			UserID:         "user-1",
		}))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w.Code
	}

	// First DELETE passes.
	if code := makeReq(http.MethodDelete); code != http.StatusOK {
		t.Fatalf("first DELETE should be 200, got %d", code)
	}
	// Second DELETE should be rate limited.
	if code := makeReq(http.MethodDelete); code != http.StatusTooManyRequests {
		t.Fatalf("second DELETE should be 429, got %d", code)
	}
	if metrics.getCount("write") != 1 {
		t.Fatalf("expected 1 write rate limit, got %d", metrics.getCount("write"))
	}
}

func TestRateLimitMiddleware_NilMetrics_NoError(t *testing.T) {
	t.Parallel()

	limiter := NewOrgRateLimiter(1, 1, time.Hour, time.Hour)
	defer limiter.Close()

	// nil metrics should not panic.
	handler := rateLimitMiddleware(limiter, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req = req.WithContext(context.WithValue(req.Context(), authContextKey{}, &AuthContext{
		OrganizationID: "org-nilm",
		UserID:         "user-1",
	}))
	w := httptest.NewRecorder()

	// First request passes.
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// Second request triggers rate limit without metrics — should not panic.
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/test", nil)
	req = req.WithContext(context.WithValue(req.Context(), authContextKey{}, &AuthContext{
		OrganizationID: "org-nilm",
		UserID:         "user-1",
	}))
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", w.Code)
	}
}

func TestRateLimitMiddleware_ContentType(t *testing.T) {
	t.Parallel()

	limiter := NewOrgRateLimiter(1, 1, time.Hour, time.Hour)
	defer limiter.Close()

	handler := rateLimitMiddleware(limiter, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Exhaust budget.
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req = req.WithContext(context.WithValue(req.Context(), authContextKey{}, &AuthContext{
		OrganizationID: "org-ct",
		UserID:         "user-1",
	}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Trigger 429.
	req = httptest.NewRequest(http.MethodGet, "/test", nil)
	req = req.WithContext(context.WithValue(req.Context(), authContextKey{}, &AuthContext{
		OrganizationID: "org-ct",
		UserID:         "user-1",
	}))
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %q", ct)
	}
	if nosniff := w.Header().Get("X-Content-Type-Options"); nosniff != "nosniff" {
		t.Fatalf("expected X-Content-Type-Options nosniff, got %q", nosniff)
	}
}

func TestOrgRateLimiter_OrgCount(t *testing.T) {
	t.Parallel()

	limiter := NewOrgRateLimiter(100, 20, time.Hour, time.Hour)
	defer limiter.Close()

	if limiter.OrgCount() != 0 {
		t.Fatal("expected 0 orgs initially")
	}

	limiter.Allow("org-1", true)
	limiter.Allow("org-2", true)
	limiter.Allow("org-3", false)

	if limiter.OrgCount() != 3 {
		t.Fatalf("expected 3 orgs, got %d", limiter.OrgCount())
	}
}

func TestOrgRateLimiter_AllowAfterClose(t *testing.T) {
	t.Parallel()
	// After Close(), Allow() still works (GC is stopped but limiters are intact).
	// This is correct because requests may still be in flight during graceful shutdown.
	limiter := NewOrgRateLimiter(100, 20, time.Minute, time.Minute)
	limiter.Close()

	allowed, _, _ := limiter.Allow("org-post-close", true)
	if !allowed {
		t.Fatal("Allow() should work after Close()")
	}
}

func TestOrgRateLimiter_ConcurrentClose(t *testing.T) {
	t.Parallel()
	// Verify sync.Once protects against concurrent Close() calls.
	limiter := NewOrgRateLimiter(100, 20, time.Minute, time.Minute)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			limiter.Close()
		}()
	}
	wg.Wait()
}

func TestOrgRateLimiter_DefaultCleanupInterval(t *testing.T) {
	t.Parallel()
	// Passing zero should not panic (defaults applied).
	limiter := NewOrgRateLimiter(100, 20, 0, 0)
	defer limiter.Close()

	limiter.Allow("org-1", true)
	if limiter.OrgCount() != 1 {
		t.Fatal("expected 1 org")
	}
}
