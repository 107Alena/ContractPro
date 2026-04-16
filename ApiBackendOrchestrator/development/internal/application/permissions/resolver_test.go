package permissions

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"contractpro/api-orchestrator/internal/config"
	"contractpro/api-orchestrator/internal/egress/opmclient"
	"contractpro/api-orchestrator/internal/infra/observability/logger"
	"contractpro/api-orchestrator/internal/ingress/middleware/auth"
)

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

type fakeCache struct {
	mu      sync.Mutex
	entries map[string]UserPermissions
	getErr  error
	setErr  error
	getCnt  int32
	setCnt  int32
}

func newFakeCache() *fakeCache {
	return &fakeCache{entries: map[string]UserPermissions{}}
}

func (f *fakeCache) Get(_ context.Context, orgID string, role auth.Role) (UserPermissions, bool, error) {
	atomic.AddInt32(&f.getCnt, 1)
	if f.getErr != nil {
		return UserPermissions{}, false, f.getErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.entries[CacheKey(orgID, role)]
	return p, ok, nil
}

func (f *fakeCache) Set(_ context.Context, orgID string, role auth.Role, perms UserPermissions) error {
	atomic.AddInt32(&f.setCnt, 1)
	if f.setErr != nil {
		return f.setErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entries[CacheKey(orgID, role)] = perms
	return nil
}

type fakeOPM struct {
	fn    func(ctx context.Context, orgID string) (json.RawMessage, error)
	calls int32
}

func (f *fakeOPM) ListPolicies(ctx context.Context, orgID string) (json.RawMessage, error) {
	atomic.AddInt32(&f.calls, 1)
	if f.fn != nil {
		return f.fn(ctx, orgID)
	}
	return nil, errors.New("not implemented")
}

type fakeMetrics struct {
	mu              sync.Mutex
	cacheHits       []string // flag
	cacheMisses     []string
	opmFallbacks    []string // flag|reason
	durationSamples []float64
}

func (m *fakeMetrics) RecordCacheHit(flag, _ string) {
	m.mu.Lock()
	m.cacheHits = append(m.cacheHits, flag)
	m.mu.Unlock()
}

func (m *fakeMetrics) RecordCacheMiss(flag string) {
	m.mu.Lock()
	m.cacheMisses = append(m.cacheMisses, flag)
	m.mu.Unlock()
}

func (m *fakeMetrics) RecordOPMFallback(flag, reason string) {
	m.mu.Lock()
	m.opmFallbacks = append(m.opmFallbacks, flag+"|"+reason)
	m.mu.Unlock()
}

func (m *fakeMetrics) RecordResolveDuration(s float64) {
	m.mu.Lock()
	m.durationSamples = append(m.durationSamples, s)
	m.mu.Unlock()
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func testLogger() *logger.Logger {
	return logger.NewLogger("error")
}

func newTestResolver(cache CacheStore, opm OPMLookup, metrics Metrics, fallback bool) *Resolver {
	return NewResolver(
		cache, opm,
		config.PermissionsConfig{
			CacheTTL:                      5 * time.Minute,
			OPMFallbackBusinessUserExport: fallback,
			OPMTimeout:                    100 * time.Millisecond,
		},
		config.CircuitBreakerConfig{FailureThreshold: 5, Timeout: 30 * time.Second, MaxRequests: 3},
		metrics,
		testLogger(),
	)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// (1) LAWYER and ORG_ADMIN return export_enabled=true without any OPM call.
func TestResolveForUser_PrivilegedRolesBypassOPM(t *testing.T) {
	cache := newFakeCache()
	opm := &fakeOPM{}
	m := &fakeMetrics{}
	r := newTestResolver(cache, opm, m, false)

	for _, role := range []auth.Role{auth.RoleLawyer, auth.RoleOrgAdmin} {
		t.Run(string(role), func(t *testing.T) {
			got := r.ResolveForUser(context.Background(), role, "org-1")
			if !got.ExportEnabled {
				t.Errorf("ExportEnabled = false, want true")
			}
			if atomic.LoadInt32(&opm.calls) != 0 {
				t.Errorf("unexpected OPM calls = %d, want 0", opm.calls)
			}
			if atomic.LoadInt32(&cache.getCnt) != 0 {
				t.Errorf("unexpected cache get calls = %d, want 0", cache.getCnt)
			}
		})
	}
}

// (2) BUSINESS_USER cache hit — returns cached value without OPM call.
func TestResolveForUser_BusinessUser_CacheHit(t *testing.T) {
	cache := newFakeCache()
	// Pre-populate cache.
	_ = cache.Set(context.Background(), "org-1", auth.RoleBusinessUser,
		UserPermissions{ExportEnabled: true})
	atomic.StoreInt32(&cache.setCnt, 0)

	opm := &fakeOPM{}
	m := &fakeMetrics{}
	r := newTestResolver(cache, opm, m, false)

	got := r.ResolveForUser(context.Background(), auth.RoleBusinessUser, "org-1")
	if !got.ExportEnabled {
		t.Errorf("ExportEnabled = false, want true (from cache)")
	}
	if atomic.LoadInt32(&opm.calls) != 0 {
		t.Errorf("OPM called %d times on cache hit", opm.calls)
	}
	if len(m.cacheHits) != 1 || m.cacheHits[0] != FlagExportEnabled {
		t.Errorf("cache hit metric not recorded: %v", m.cacheHits)
	}
	if len(m.cacheMisses) != 0 {
		t.Errorf("unexpected cache miss records: %v", m.cacheMisses)
	}
}

// (3) BUSINESS_USER cache miss + OPM success — value is returned and cached.
func TestResolveForUser_BusinessUser_CacheMiss_OPMSuccess(t *testing.T) {
	cache := newFakeCache()
	opm := &fakeOPM{
		fn: func(_ context.Context, _ string) (json.RawMessage, error) {
			return json.RawMessage(`[{"name":"business_user_export","enabled":true}]`), nil
		},
	}
	m := &fakeMetrics{}
	r := newTestResolver(cache, opm, m, false)

	got := r.ResolveForUser(context.Background(), auth.RoleBusinessUser, "org-1")
	if !got.ExportEnabled {
		t.Errorf("ExportEnabled = false, want true")
	}
	if atomic.LoadInt32(&opm.calls) != 1 {
		t.Errorf("OPM.ListPolicies called %d times, want 1", opm.calls)
	}
	if atomic.LoadInt32(&cache.setCnt) != 1 {
		t.Errorf("cache.Set called %d times, want 1", cache.setCnt)
	}
	// Verify cached.
	cached, ok, _ := cache.Get(context.Background(), "org-1", auth.RoleBusinessUser)
	if !ok || !cached.ExportEnabled {
		t.Errorf("cached value = (ok=%v, %+v), want (true, ExportEnabled=true)", ok, cached)
	}
	if len(m.cacheMisses) != 1 {
		t.Errorf("cache miss metric records = %d, want 1", len(m.cacheMisses))
	}
	if len(m.opmFallbacks) != 0 {
		t.Errorf("unexpected fallback records: %v", m.opmFallbacks)
	}
}

// (4) BUSINESS_USER cache miss + OPM timeout — fallback, NOT cached, WARN logged.
func TestResolveForUser_BusinessUser_OPMTimeout_Fallback(t *testing.T) {
	cache := newFakeCache()
	opm := &fakeOPM{
		fn: func(ctx context.Context, _ string) (json.RawMessage, error) {
			// Sleep until the per-call context is cancelled by timeout.
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(1 * time.Second):
				return nil, errors.New("unexpectedly no timeout")
			}
		},
	}
	m := &fakeMetrics{}
	// Fallback = true so we can distinguish from the default.
	r := newTestResolver(cache, opm, m, true)

	got := r.ResolveForUser(context.Background(), auth.RoleBusinessUser, "org-1")
	if !got.ExportEnabled {
		t.Errorf("ExportEnabled = false, want true (env fallback)")
	}
	if atomic.LoadInt32(&cache.setCnt) != 0 {
		t.Errorf("cache.Set called %d times on fallback, want 0", cache.setCnt)
	}
	if len(m.opmFallbacks) != 1 {
		t.Fatalf("opmFallbacks = %v, want 1 entry", m.opmFallbacks)
	}
	if m.opmFallbacks[0] != FlagExportEnabled+"|"+FallbackReasonTimeout {
		t.Errorf("opmFallbacks[0] = %q, want %q", m.opmFallbacks[0],
			FlagExportEnabled+"|"+FallbackReasonTimeout)
	}
}

// (5) BUSINESS_USER cache miss + OPM 5xx — fallback path, NOT cached.
func TestResolveForUser_BusinessUser_OPM5xx_Fallback(t *testing.T) {
	cache := newFakeCache()
	opm := &fakeOPM{
		fn: func(_ context.Context, _ string) (json.RawMessage, error) {
			return nil, &opmclient.OPMError{
				Operation:  "ListPolicies",
				StatusCode: 503,
				Retryable:  true,
			}
		},
	}
	m := &fakeMetrics{}
	r := newTestResolver(cache, opm, m, false)

	got := r.ResolveForUser(context.Background(), auth.RoleBusinessUser, "org-1")
	if got.ExportEnabled {
		t.Errorf("ExportEnabled = true, want false (env fallback default)")
	}
	if atomic.LoadInt32(&cache.setCnt) != 0 {
		t.Errorf("cache.Set called on fallback")
	}
	if len(m.opmFallbacks) != 1 {
		t.Fatalf("opmFallbacks = %v, want 1 entry", m.opmFallbacks)
	}
	if m.opmFallbacks[0] != FlagExportEnabled+"|"+FallbackReasonOPMUnavailable {
		t.Errorf("opmFallbacks[0] = %q, want %q", m.opmFallbacks[0],
			FlagExportEnabled+"|"+FallbackReasonOPMUnavailable)
	}
}

// OPM returns empty array → policy not found → env fallback, reason=no_policy.
func TestResolveForUser_BusinessUser_PolicyNotFound(t *testing.T) {
	cache := newFakeCache()
	opm := &fakeOPM{
		fn: func(_ context.Context, _ string) (json.RawMessage, error) {
			return json.RawMessage(`[{"name":"other_policy","enabled":true}]`), nil
		},
	}
	m := &fakeMetrics{}
	r := newTestResolver(cache, opm, m, false)

	got := r.ResolveForUser(context.Background(), auth.RoleBusinessUser, "org-1")
	if got.ExportEnabled {
		t.Errorf("ExportEnabled = true, want false (no_policy fallback)")
	}
	if atomic.LoadInt32(&cache.setCnt) != 0 {
		t.Errorf("cache.Set called when policy not found")
	}
	if len(m.opmFallbacks) != 1 || m.opmFallbacks[0] != FlagExportEnabled+"|"+FallbackReasonNoPolicy {
		t.Errorf("opmFallbacks = %v, want [export_enabled|no_policy]", m.opmFallbacks)
	}
}

// OPM returns malformed JSON → fallback with reason=malformed_response.
func TestResolveForUser_BusinessUser_MalformedOPM(t *testing.T) {
	cache := newFakeCache()
	opm := &fakeOPM{
		fn: func(_ context.Context, _ string) (json.RawMessage, error) {
			return json.RawMessage(`{"not_an_array": true}`), nil
		},
	}
	m := &fakeMetrics{}
	r := newTestResolver(cache, opm, m, true)

	got := r.ResolveForUser(context.Background(), auth.RoleBusinessUser, "org-1")
	if !got.ExportEnabled {
		t.Errorf("ExportEnabled = false, want true (env fallback)")
	}
	if atomic.LoadInt32(&cache.setCnt) != 0 {
		t.Errorf("cache.Set called on malformed response")
	}
	if len(m.opmFallbacks) != 1 || m.opmFallbacks[0] != FlagExportEnabled+"|"+FallbackReasonMalformedResponse {
		t.Errorf("opmFallbacks = %v, want [export_enabled|malformed_response]", m.opmFallbacks)
	}
}

// Cache backend error is treated as miss without fatal error.
func TestResolveForUser_BusinessUser_CacheError_TreatedAsMiss(t *testing.T) {
	cache := newFakeCache()
	cache.getErr = errors.New("redis unavailable")

	opm := &fakeOPM{
		fn: func(_ context.Context, _ string) (json.RawMessage, error) {
			return json.RawMessage(`[{"name":"business_user_export","enabled":true}]`), nil
		},
	}
	m := &fakeMetrics{}
	r := newTestResolver(cache, opm, m, false)

	got := r.ResolveForUser(context.Background(), auth.RoleBusinessUser, "org-1")
	if !got.ExportEnabled {
		t.Errorf("ExportEnabled = false, want true")
	}
	if len(m.cacheMisses) != 1 {
		t.Errorf("cache miss should be recorded on backend error")
	}
	if len(m.cacheHits) != 0 {
		t.Errorf("unexpected cache hits on backend error")
	}
}

// Duration histogram is observed on every resolve.
func TestResolveForUser_DurationAlwaysRecorded(t *testing.T) {
	cache := newFakeCache()
	opm := &fakeOPM{}
	m := &fakeMetrics{}
	r := newTestResolver(cache, opm, m, false)

	_ = r.ResolveForUser(context.Background(), auth.RoleLawyer, "org-1")
	_ = r.ResolveForUser(context.Background(), auth.RoleOrgAdmin, "org-1")
	if len(m.durationSamples) != 2 {
		t.Errorf("durationSamples = %d, want 2", len(m.durationSamples))
	}
}

// Unknown role returns env fallback.
func TestResolveForUser_UnknownRole_Fallback(t *testing.T) {
	cache := newFakeCache()
	opm := &fakeOPM{}
	m := &fakeMetrics{}
	r := newTestResolver(cache, opm, m, true)

	got := r.ResolveForUser(context.Background(), auth.Role("UNKNOWN"), "org-1")
	if !got.ExportEnabled {
		t.Errorf("ExportEnabled = false, want true (fallback default)")
	}
	if atomic.LoadInt32(&opm.calls) != 0 {
		t.Errorf("OPM called for unknown role")
	}
}

// --- opmlookup parser ---

func TestParseBusinessUserExportFlag(t *testing.T) {
	tests := []struct {
		name        string
		raw         string
		wantEnabled bool
		wantFound   bool
		wantErr     bool
	}{
		{"empty", ``, false, false, false},
		{"empty array", `[]`, false, false, false},
		{"found true", `[{"name":"business_user_export","enabled":true}]`, true, true, false},
		{"found false", `[{"name":"business_user_export","enabled":false}]`, false, true, false},
		{
			"found among others",
			`[{"name":"foo","enabled":true},{"name":"business_user_export","enabled":true}]`,
			true, true, false,
		},
		{"extra fields ignored", `[{"name":"business_user_export","enabled":true,"description":"x"}]`, true, true, false},
		{"not array", `{"policies":[]}`, false, false, true},
		{"garbage", `not json`, false, false, true},
		{"case sensitive", `[{"name":"Business_User_Export","enabled":true}]`, false, false, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			enabled, found, err := parseBusinessUserExportFlag([]byte(tc.raw))
			if (err != nil) != tc.wantErr {
				t.Errorf("err = %v, wantErr = %v", err, tc.wantErr)
			}
			if enabled != tc.wantEnabled {
				t.Errorf("enabled = %v, want %v", enabled, tc.wantEnabled)
			}
			if found != tc.wantFound {
				t.Errorf("found = %v, want %v", found, tc.wantFound)
			}
		})
	}
}

// --- hashOrgID ---

func TestHashOrgID_Stable(t *testing.T) {
	h1 := hashOrgID("org-1")
	h2 := hashOrgID("org-1")
	if h1 != h2 {
		t.Errorf("hashOrgID not stable: %q vs %q", h1, h2)
	}
	if len(h1) != 8 {
		t.Errorf("hashOrgID length = %d, want 8", len(h1))
	}
	if hashOrgID("org-1") == hashOrgID("org-2") {
		t.Error("hashOrgID collisions on distinct inputs (unexpected)")
	}
}

// --- CacheKey / InvalidateChannel ---

func TestCacheKey(t *testing.T) {
	if got := CacheKey("org-1", auth.RoleBusinessUser); got != "permissions:org-1:BUSINESS_USER" {
		t.Errorf("CacheKey = %q, want permissions:org-1:BUSINESS_USER", got)
	}
}

func TestInvalidateChannel(t *testing.T) {
	if got := InvalidateChannel("org-1"); got != "permissions:invalidate:org-1" {
		t.Errorf("InvalidateChannel = %q, want permissions:invalidate:org-1", got)
	}
}
