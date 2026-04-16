package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"contractpro/api-orchestrator/internal/config"
	"contractpro/api-orchestrator/internal/infra/observability/logger"
)

// --- CORS Middleware tests ---

func TestCORS_NoOrigin_PassThrough(t *testing.T) {
	mw := CORSMiddleware(config.CORSConfig{
		AllowedOrigins: []string{"https://app.example.com"},
		MaxAge:         3600,
	}, testLogger())

	handler := mw(okHandler())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/contracts", nil)
	// No Origin header.
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("no CORS headers expected without Origin")
	}
	if rec.Header().Get("Vary") != "" {
		t.Fatal("no Vary expected without Origin")
	}
}

func TestCORS_AllowedOrigin_ActualRequest(t *testing.T) {
	mw := CORSMiddleware(config.CORSConfig{
		AllowedOrigins: []string{"https://app.example.com"},
		MaxAge:         3600,
	}, testLogger())

	handler := mw(okHandler())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/contracts", nil)
	req.Header.Set("Origin", "https://app.example.com")
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Fatalf("Allow-Origin: want %q, got %q", "https://app.example.com", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("Allow-Credentials: want %q, got %q", "true", got)
	}
	if got := rec.Header().Get("Access-Control-Expose-Headers"); got == "" {
		t.Fatal("Expose-Headers should be set")
	}
	if !strings.Contains(rec.Header().Get("Vary"), "Origin") {
		t.Fatal("Vary should contain Origin")
	}
}

func TestCORS_DisallowedOrigin_ActualRequest(t *testing.T) {
	mw := CORSMiddleware(config.CORSConfig{
		AllowedOrigins: []string{"https://app.example.com"},
		MaxAge:         3600,
	}, testLogger())

	handler := mw(okHandler())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/contracts", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	handler.ServeHTTP(rec, req)

	// Request passes through (browser enforces CORS).
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("disallowed origin should not get CORS headers")
	}
	// Vary should still be set for caching correctness.
	if !strings.Contains(rec.Header().Get("Vary"), "Origin") {
		t.Fatal("Vary should contain Origin even for disallowed origins")
	}
}

func TestCORS_Preflight_Allowed(t *testing.T) {
	mw := CORSMiddleware(config.CORSConfig{
		AllowedOrigins: []string{"https://app.example.com"},
		MaxAge:         7200,
	}, testLogger())

	handler := mw(failHandler(t))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/contracts", nil)
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight: want 204, got %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Fatalf("Allow-Origin: want %q, got %q", "https://app.example.com", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Fatal("Allow-Methods should be set")
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got == "" {
		t.Fatal("Allow-Headers should be set")
	}
	if got := rec.Header().Get("Access-Control-Max-Age"); got != "7200" {
		t.Fatalf("Max-Age: want %q, got %q", "7200", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("Allow-Credentials: want %q, got %q", "true", got)
	}
	if got := rec.Header().Get("Access-Control-Expose-Headers"); got == "" {
		t.Fatal("Expose-Headers should be set on preflight")
	}
	vary := rec.Header().Get("Vary")
	if !strings.Contains(vary, "Origin") || !strings.Contains(vary, "Access-Control-Request-Method") {
		t.Fatalf("Vary should contain Origin and Access-Control-Request-Method, got %q", vary)
	}
}

func TestCORS_Preflight_Disallowed(t *testing.T) {
	mw := CORSMiddleware(config.CORSConfig{
		AllowedOrigins: []string{"https://app.example.com"},
		MaxAge:         3600,
	}, testLogger())

	handler := mw(failHandler(t))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/contracts", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight: want 204, got %d", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("disallowed preflight should not get CORS headers")
	}
}

func TestCORS_EmptyOrigins_SameOriginOnly(t *testing.T) {
	mw := CORSMiddleware(config.CORSConfig{
		AllowedOrigins: nil,
		MaxAge:         3600,
	}, testLogger())

	handler := mw(okHandler())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/contracts", nil)
	req.Header.Set("Origin", "https://anything.example.com")
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("empty origins list should deny all cross-origin requests")
	}
}

func TestCORS_WildcardIgnored(t *testing.T) {
	mw := CORSMiddleware(config.CORSConfig{
		AllowedOrigins: []string{"*"},
		MaxAge:         3600,
	}, testLogger())

	handler := mw(okHandler())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/contracts", nil)
	req.Header.Set("Origin", "https://anything.example.com")
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("wildcard should be ignored")
	}
}

func TestCORS_MultipleOrigins(t *testing.T) {
	origins := []string{
		"https://app.example.com",
		"https://staging.example.com",
		"http://localhost:3000",
	}
	mw := CORSMiddleware(config.CORSConfig{
		AllowedOrigins: origins,
		MaxAge:         3600,
	}, testLogger())

	for _, origin := range origins {
		t.Run(origin, func(t *testing.T) {
			handler := mw(okHandler())
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Origin", origin)
			handler.ServeHTTP(rec, req)

			if got := rec.Header().Get("Access-Control-Allow-Origin"); got != origin {
				t.Fatalf("Allow-Origin: want %q, got %q", origin, got)
			}
		})
	}
}

func TestCORS_OriginCaseSensitive(t *testing.T) {
	mw := CORSMiddleware(config.CORSConfig{
		AllowedOrigins: []string{"https://app.example.com"},
		MaxAge:         3600,
	}, testLogger())

	handler := mw(okHandler())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://APP.EXAMPLE.COM")
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("origin matching should be case-sensitive")
	}
}

func TestCORS_OptionsWithoutRequestMethod_NotPreflight(t *testing.T) {
	mw := CORSMiddleware(config.CORSConfig{
		AllowedOrigins: []string{"https://app.example.com"},
		MaxAge:         3600,
	}, testLogger())

	// OPTIONS without Access-Control-Request-Method is NOT a preflight.
	called := false
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "https://app.example.com")
	handler.ServeHTTP(rec, req)

	if !called {
		t.Fatal("non-preflight OPTIONS should pass through to handler")
	}
	// Should still get CORS headers for the actual response.
	if rec.Header().Get("Access-Control-Allow-Origin") != "https://app.example.com" {
		t.Fatal("allowed origin should get CORS headers on non-preflight OPTIONS")
	}
}

func TestCORS_Preflight_DoesNotCallHandler(t *testing.T) {
	mw := CORSMiddleware(config.CORSConfig{
		AllowedOrigins: []string{"https://app.example.com"},
		MaxAge:         3600,
	}, testLogger())

	handler := mw(failHandler(t))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d", rec.Code)
	}
	// failHandler would have failed the test if called.
}

func TestCORS_ExposeHeaders_ContainsRequired(t *testing.T) {
	mw := CORSMiddleware(config.CORSConfig{
		AllowedOrigins: []string{"https://app.example.com"},
		MaxAge:         3600,
	}, testLogger())

	handler := mw(okHandler())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://app.example.com")
	handler.ServeHTTP(rec, req)

	expose := rec.Header().Get("Access-Control-Expose-Headers")
	for _, want := range []string{"X-Request-Id", "Retry-After", "traceparent"} {
		if !strings.Contains(expose, want) {
			t.Errorf("Expose-Headers should contain %q, got %q", want, expose)
		}
	}
}

func TestCORS_AllowHeaders_ContainsRequired(t *testing.T) {
	mw := CORSMiddleware(config.CORSConfig{
		AllowedOrigins: []string{"https://app.example.com"},
		MaxAge:         3600,
	}, testLogger())

	handler := mw(failHandler(t))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	handler.ServeHTTP(rec, req)

	allow := rec.Header().Get("Access-Control-Allow-Headers")
	for _, want := range []string{"Authorization", "Content-Type", "X-Correlation-Id", "traceparent", "tracestate"} {
		if !strings.Contains(allow, want) {
			t.Errorf("Allow-Headers should contain %q, got %q", want, allow)
		}
	}
}

func TestCORS_AllowMethods_ContainsRequired(t *testing.T) {
	mw := CORSMiddleware(config.CORSConfig{
		AllowedOrigins: []string{"https://app.example.com"},
		MaxAge:         3600,
	}, testLogger())

	handler := mw(failHandler(t))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	handler.ServeHTTP(rec, req)

	methods := rec.Header().Get("Access-Control-Allow-Methods")
	for _, want := range []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"} {
		if !strings.Contains(methods, want) {
			t.Errorf("Allow-Methods should contain %q, got %q", want, methods)
		}
	}
}

func TestCORS_EmptyStringOriginFiltered(t *testing.T) {
	mw := CORSMiddleware(config.CORSConfig{
		AllowedOrigins: []string{"", "https://app.example.com"},
		MaxAge:         3600,
	}, testLogger())

	handler := mw(okHandler())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://app.example.com")
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Access-Control-Allow-Origin") != "https://app.example.com" {
		t.Fatal("valid origin should still be allowed after filtering empty strings")
	}
}

// --- Security Headers Middleware tests ---

func TestSecurityHeaders_AllPresent(t *testing.T) {
	mw := SecurityHeadersMiddleware()
	handler := mw(okHandler())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(rec, req)

	checks := map[string]string{
		"X-Content-Type-Options":    "nosniff",
		"X-Frame-Options":          "DENY",
		"Strict-Transport-Security": "max-age=31536000; includeSubDomains",
		"Cache-Control":            "no-store",
	}

	for header, want := range checks {
		if got := rec.Header().Get(header); got != want {
			t.Errorf("%s: want %q, got %q", header, want, got)
		}
	}
}

func TestSecurityHeaders_XRequestID_Generated(t *testing.T) {
	mw := SecurityHeadersMiddleware()
	handler := mw(okHandler())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(rec, req)

	rid := rec.Header().Get("X-Request-Id")
	if rid == "" {
		t.Fatal("X-Request-Id should be generated when not provided")
	}
	// Must also be set on X-Correlation-Id.
	cid := rec.Header().Get("X-Correlation-Id")
	if cid != rid {
		t.Fatalf("X-Correlation-Id (%q) should equal X-Request-Id (%q)", cid, rid)
	}
	// UUID v4 format: 8-4-4-4-12 hex digits.
	if len(rid) != 36 {
		t.Fatalf("X-Request-Id should be UUID, got %q (len=%d)", rid, len(rid))
	}
}

func TestSecurityHeaders_XRequestID_FromClient(t *testing.T) {
	mw := SecurityHeadersMiddleware()
	handler := mw(okHandler())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Correlation-Id", "client-provided-id")
	handler.ServeHTTP(rec, req)

	rid := rec.Header().Get("X-Request-Id")
	if rid != "client-provided-id" {
		t.Fatalf("X-Request-Id should use client-provided correlation ID, got %q", rid)
	}
	cid := rec.Header().Get("X-Correlation-Id")
	if cid != "client-provided-id" {
		t.Fatalf("X-Correlation-Id should preserve client value, got %q", cid)
	}
}

func TestSecurityHeaders_PropagatesOnRequestHeader(t *testing.T) {
	mw := SecurityHeadersMiddleware()

	var propagated string
	handler := mw(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		propagated = r.Header.Get("X-Correlation-Id")
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	// No client header — middleware should generate and propagate.
	handler.ServeHTTP(rec, req)

	if propagated == "" {
		t.Fatal("correlation ID should be propagated on request header")
	}
	if propagated != rec.Header().Get("X-Request-Id") {
		t.Fatalf("propagated request header (%q) should match response X-Request-Id (%q)",
			propagated, rec.Header().Get("X-Request-Id"))
	}
}

func TestSecurityHeaders_InjectsRequestContext(t *testing.T) {
	mw := SecurityHeadersMiddleware()

	var ctxCID string
	handler := mw(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		rc := logger.RequestContextFrom(r.Context())
		ctxCID = rc.CorrelationID
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Correlation-Id", "test-cid-123")
	handler.ServeHTTP(rec, req)

	if ctxCID != "test-cid-123" {
		t.Fatalf("RequestContext.CorrelationID: want %q, got %q", "test-cid-123", ctxCID)
	}
}

func TestSecurityHeaders_CacheControlOverridable(t *testing.T) {
	mw := SecurityHeadersMiddleware()

	// Simulate a handler that overrides Cache-Control (e.g., health probe).
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=60")
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=60" {
		t.Fatalf("Cache-Control should be overridable, got %q", got)
	}
}

func TestSecurityHeaders_UniqueIDs_PerRequest(t *testing.T) {
	mw := SecurityHeadersMiddleware()
	handler := mw(okHandler())

	ids := make(map[string]struct{}, 100)
	for i := 0; i < 100; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		handler.ServeHTTP(rec, req)
		id := rec.Header().Get("X-Request-Id")
		if _, dup := ids[id]; dup {
			t.Fatalf("duplicate X-Request-Id: %q", id)
		}
		ids[id] = struct{}{}
	}
}

// --- CORS + SecurityHeaders combo tests ---

func TestCORSAndSecurityHeaders_Integration(t *testing.T) {
	corsMW := CORSMiddleware(config.CORSConfig{
		AllowedOrigins: []string{"https://app.example.com"},
		MaxAge:         3600,
	}, testLogger())
	secMW := SecurityHeadersMiddleware()

	// Chain: CORS → SecurityHeaders → handler (same as production order).
	handler := corsMW(secMW(okHandler()))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/contracts", nil)
	req.Header.Set("Origin", "https://app.example.com")
	handler.ServeHTTP(rec, req)

	// CORS headers present.
	if rec.Header().Get("Access-Control-Allow-Origin") != "https://app.example.com" {
		t.Fatal("CORS Allow-Origin should be set")
	}
	// Security headers present.
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("X-Content-Type-Options should be set")
	}
	if rec.Header().Get("X-Request-Id") == "" {
		t.Fatal("X-Request-Id should be set")
	}
}

func TestCORS_Preflight_ShortCircuitsBeforeDownstream(t *testing.T) {
	// Preflight should return 204 from CORS middleware without
	// reaching downstream middleware or handler.
	corsMW := CORSMiddleware(config.CORSConfig{
		AllowedOrigins: []string{"https://app.example.com"},
		MaxAge:         3600,
	}, testLogger())
	secMW := SecurityHeadersMiddleware()

	handler := corsMW(secMW(failHandler(t)))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d", rec.Code)
	}
	// CORS headers should be set on the allowed preflight.
	if rec.Header().Get("Access-Control-Allow-Origin") == "" {
		t.Fatal("CORS headers should be set on preflight")
	}
}

// --- Server-level integration tests ---

func TestServer_SecurityHeaders_OnEveryResponse(t *testing.T) {
	s := newTestServer()

	endpoints := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/healthz"},
		{http.MethodGet, "/readyz"},
		{http.MethodPost, "/api/v1/auth/login"},
		{http.MethodGet, "/api/v1/contracts"},
	}

	for _, ep := range endpoints {
		t.Run(ep.method+" "+ep.path, func(t *testing.T) {
			rec := doRequest(t, s, ep.method, ep.path)
			if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Errorf("%s %s: X-Content-Type-Options should be nosniff", ep.method, ep.path)
			}
			if rec.Header().Get("X-Frame-Options") != "DENY" {
				t.Errorf("%s %s: X-Frame-Options should be DENY", ep.method, ep.path)
			}
			if rec.Header().Get("X-Request-Id") == "" {
				t.Errorf("%s %s: X-Request-Id should be set", ep.method, ep.path)
			}
		})
	}
}

func TestServer_CORSHeaders_OnCrossOriginRequest(t *testing.T) {
	s := NewServer(Deps{
		Config: testConfig(),
		CORSConfig: config.CORSConfig{
			AllowedOrigins: []string{"https://app.contractpro.ru"},
			MaxAge:         3600,
		},
		Health: testHealth(),
		Logger: testLogger(),
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/contracts", nil)
	req.Header.Set("Origin", "https://app.contractpro.ru")
	s.Router().ServeHTTP(rec, req)

	if rec.Header().Get("Access-Control-Allow-Origin") != "https://app.contractpro.ru" {
		t.Fatal("CORS Allow-Origin should be set for configured origin")
	}
}

func TestServer_CORSHeaders_NotSetForDisallowedOrigin(t *testing.T) {
	s := NewServer(Deps{
		Config: testConfig(),
		CORSConfig: config.CORSConfig{
			AllowedOrigins: []string{"https://app.contractpro.ru"},
			MaxAge:         3600,
		},
		Health: testHealth(),
		Logger: testLogger(),
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/contracts", nil)
	req.Header.Set("Origin", "https://evil.com")
	s.Router().ServeHTTP(rec, req)

	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("disallowed origin should not get CORS headers")
	}
}

func TestServer_Preflight_ReturnsNoContent(t *testing.T) {
	s := NewServer(Deps{
		Config: testConfig(),
		CORSConfig: config.CORSConfig{
			AllowedOrigins: []string{"https://app.contractpro.ru"},
			MaxAge:         3600,
		},
		Health: testHealth(),
		Logger: testLogger(),
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/contracts", nil)
	req.Header.Set("Origin", "https://app.contractpro.ru")
	req.Header.Set("Access-Control-Request-Method", "POST")
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight: want 204, got %d", rec.Code)
	}
}

func TestServer_DefaultCORSConfig_SameOriginOnly(t *testing.T) {
	// Default Deps (zero CORSConfig) → no CORS headers.
	s := newTestServer()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/contracts", nil)
	req.Header.Set("Origin", "https://any.example.com")
	s.Router().ServeHTTP(rec, req)

	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("default config should be same-origin only (no CORS headers)")
	}
}

func TestServer_XRequestID_PreservedFromClient(t *testing.T) {
	s := newTestServer()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("X-Correlation-Id", "my-custom-id")
	s.Router().ServeHTTP(rec, req)

	if rec.Header().Get("X-Request-Id") != "my-custom-id" {
		t.Fatalf("X-Request-Id should preserve client-provided ID, got %q",
			rec.Header().Get("X-Request-Id"))
	}
}

// --- Correlation ID validation tests ---

func TestSecurityHeaders_InvalidCID_TooLong(t *testing.T) {
	mw := SecurityHeadersMiddleware()
	handler := mw(okHandler())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	// 200 chars exceeds maxCorrelationIDLen (128).
	req.Header.Set("X-Correlation-Id", strings.Repeat("a", 200))
	handler.ServeHTTP(rec, req)

	rid := rec.Header().Get("X-Request-Id")
	if len(rid) != 36 {
		t.Fatalf("invalid CID should be replaced with UUID, got %q (len=%d)", rid, len(rid))
	}
}

func TestSecurityHeaders_InvalidCID_ControlChars(t *testing.T) {
	mw := SecurityHeadersMiddleware()
	handler := mw(okHandler())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Correlation-Id", "id-with-\x00-null")
	handler.ServeHTTP(rec, req)

	rid := rec.Header().Get("X-Request-Id")
	if len(rid) != 36 {
		t.Fatalf("CID with control chars should be replaced with UUID, got %q", rid)
	}
}

func TestSecurityHeaders_ValidCID_MaxLength(t *testing.T) {
	mw := SecurityHeadersMiddleware()
	handler := mw(okHandler())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	cid := strings.Repeat("a", 128) // exactly at maxCorrelationIDLen
	req.Header.Set("X-Correlation-Id", cid)
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("X-Request-Id") != cid {
		t.Fatalf("valid CID at max length should be preserved, got %q", rec.Header().Get("X-Request-Id"))
	}
}

func TestIsValidCorrelationID(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"uuid", "550e8400-e29b-41d4-a716-446655440000", true},
		{"short", "abc", true},
		{"at_max_len", strings.Repeat("x", 128), true},
		{"over_max_len", strings.Repeat("x", 129), false},
		{"empty", "", false},
		{"null_byte", "id\x00x", false},
		{"newline", "id\nx", false},
		{"tab", "id\tx", false},
		{"del", "id\x7Fx", false},
		{"space_ok", "id with spaces", true},
		{"tilde_ok", "id~value", true},
		{"printable_boundary_low", string(rune(0x20)), true},
		{"printable_boundary_high", string(rune(0x7E)), true},
		{"below_printable", string(rune(0x1F)), false},
		{"above_printable", string(rune(0x7F)), false},
		{"unicode", "id-\u00e9", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isValidCorrelationID(tt.input); got != tt.want {
				t.Errorf("isValidCorrelationID(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// --- ORCH-TASK-051: CORS updates (W3C Trace Context + no-op default) ---

// When ORCH_CORS_ALLOWED_ORIGINS is empty, the middleware must pass every
// request (including OPTIONS) straight to the downstream handler with no
// CORS headers — the same-origin deployment topology (ADR-6).
func TestCORS_EmptyOrigins_OptionsReachesHandler(t *testing.T) {
	mw := CORSMiddleware(config.CORSConfig{
		AllowedOrigins: nil,
		MaxAge:         3600,
	}, testLogger())

	called := false
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/contracts", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	handler.ServeHTTP(rec, req)

	if !called {
		t.Fatal("with empty AllowedOrigins, OPTIONS preflight must reach the downstream handler")
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("no CORS headers expected when AllowedOrigins is empty")
	}
	if rec.Header().Get("Vary") != "" {
		t.Fatal("no Vary expected when AllowedOrigins is empty")
	}
}

// Preflight request advertising the W3C traceparent header must succeed —
// OpenTelemetry frontend instrumentation injects it on every request.
func TestCORS_Preflight_AllowsTraceparentHeader(t *testing.T) {
	mw := CORSMiddleware(config.CORSConfig{
		AllowedOrigins: []string{"https://app.example.com"},
		MaxAge:         3600,
	}, testLogger())

	handler := mw(failHandler(t))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/contracts", nil)
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "traceparent, tracestate, x-correlation-id")
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight: want 204, got %d", rec.Code)
	}
	allow := rec.Header().Get("Access-Control-Allow-Headers")
	for _, want := range []string{"traceparent", "tracestate"} {
		if !strings.Contains(allow, want) {
			t.Errorf("Allow-Headers must contain %q for W3C Trace Context, got %q", want, allow)
		}
	}
}

// Response Expose-Headers must advertise traceparent so that the frontend can
// read the backend-assigned trace ID via fetch/XHR.
func TestCORS_ExposeHeaders_IncludesTraceparent(t *testing.T) {
	mw := CORSMiddleware(config.CORSConfig{
		AllowedOrigins: []string{"https://app.example.com"},
		MaxAge:         3600,
	}, testLogger())

	handler := mw(okHandler())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://app.example.com")
	handler.ServeHTTP(rec, req)

	expose := rec.Header().Get("Access-Control-Expose-Headers")
	if !strings.Contains(expose, "traceparent") {
		t.Fatalf("Expose-Headers must include 'traceparent', got %q", expose)
	}
}

// --- helpers ---

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func failHandler(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("handler should not be called")
	})
}
