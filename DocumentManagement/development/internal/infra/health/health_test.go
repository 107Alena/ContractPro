package health

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func doGet(t *testing.T, h *Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.Mux().ServeHTTP(rec, req)
	return rec
}

func decodeStatus(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
	v, ok := body["status"]
	if !ok {
		t.Fatal("response body missing 'status' key")
	}
	s, ok := v.(string)
	if !ok {
		t.Fatalf("'status' is not a string: %T", v)
	}
	return s
}

func decodeReadiness(t *testing.T, rec *httptest.ResponseRecorder) ReadinessResponse {
	t.Helper()
	var resp ReadinessResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode ReadinessResponse: %v", err)
	}
	return resp
}

func healthyChecker() ComponentChecker {
	return func(_ context.Context) error { return nil }
}

func unhealthyChecker(msg string) ComponentChecker {
	return func(_ context.Context) error { return errors.New(msg) }
}

func slowChecker(d time.Duration) ComponentChecker {
	return func(ctx context.Context) error {
		select {
		case <-time.After(d):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func panicChecker(msg string) ComponentChecker {
	return func(_ context.Context) error { panic(msg) }
}

// ---------------------------------------------------------------------------
// /healthz — Liveness
// ---------------------------------------------------------------------------

func TestLiveness_AlwaysReturns200(t *testing.T) {
	h := NewHandler(nil, nil)
	rec := doGet(t, h, "/healthz")

	if rec.Code != http.StatusOK {
		t.Fatalf("/healthz: want 200, got %d", rec.Code)
	}
	if got := decodeStatus(t, rec); got != "ok" {
		t.Fatalf("/healthz: want status 'ok', got %q", got)
	}
}

func TestLiveness_ContentTypeJSON(t *testing.T) {
	h := NewHandler(nil, nil)
	rec := doGet(t, h, "/healthz")

	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Fatalf("/healthz: want Content-Type 'application/json', got %q", ct)
	}
}

func TestLiveness_PostReturns405(t *testing.T) {
	h := NewHandler(nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/healthz", nil)
	rec := httptest.NewRecorder()
	h.Mux().ServeHTTP(rec, req)

	if rec.Code == http.StatusOK {
		t.Fatalf("/healthz POST: should not return 200, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// /readyz — Readiness: not ready by default
// ---------------------------------------------------------------------------

func TestReadiness_NotReadyByDefault(t *testing.T) {
	h := NewHandler(nil, nil)
	rec := doGet(t, h, "/readyz")

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("/readyz (default): want 503, got %d", rec.Code)
	}
	if got := decodeStatus(t, rec); got != "not_ready" {
		t.Fatalf("/readyz (default): want 'not_ready', got %q", got)
	}
}

func TestReadiness_NotReadyByDefault_EmptyComponents(t *testing.T) {
	h := NewHandler(nil, nil)
	rec := doGet(t, h, "/readyz")

	resp := decodeReadiness(t, rec)
	if len(resp.Components) != 0 {
		t.Fatalf("want 0 components when not ready (flag=false), got %d", len(resp.Components))
	}
}

// ---------------------------------------------------------------------------
// /readyz — Readiness: SetReady toggle
// ---------------------------------------------------------------------------

func TestReadiness_ReadyAfterSetReadyTrue(t *testing.T) {
	h := NewHandler(nil, nil)
	h.SetReady(true)

	rec := doGet(t, h, "/readyz")

	if rec.Code != http.StatusOK {
		t.Fatalf("/readyz (ready, no checkers): want 200, got %d", rec.Code)
	}
	if got := decodeStatus(t, rec); got != "ready" {
		t.Fatalf("/readyz (ready): want 'ready', got %q", got)
	}
}

func TestReadiness_NotReadyAfterToggleBack(t *testing.T) {
	h := NewHandler(nil, nil)
	h.SetReady(true)
	h.SetReady(false)

	rec := doGet(t, h, "/readyz")

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("/readyz (toggled back): want 503, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// /readyz — Readiness: all core healthy
// ---------------------------------------------------------------------------

func TestReadiness_AllCoreHealthy(t *testing.T) {
	core := map[string]ComponentChecker{
		"postgres": healthyChecker(),
		"redis":    healthyChecker(),
		"rabbitmq": healthyChecker(),
	}
	h := NewHandler(core, nil)
	h.SetReady(true)

	rec := doGet(t, h, "/readyz")

	if rec.Code != http.StatusOK {
		t.Fatalf("/readyz: want 200, got %d", rec.Code)
	}

	resp := decodeReadiness(t, rec)
	for name, cs := range resp.Components {
		if cs.Status != "up" {
			t.Errorf("component %s: want 'up', got %q", name, cs.Status)
		}
		if cs.Error != "" {
			t.Errorf("component %s: want no error, got %q", name, cs.Error)
		}
	}
}

// ---------------------------------------------------------------------------
// /readyz — Readiness: core component down → 503
// ---------------------------------------------------------------------------

func TestReadiness_CoreDown_Returns503(t *testing.T) {
	core := map[string]ComponentChecker{
		"postgres": healthyChecker(),
		"redis":    unhealthyChecker("connection refused"),
		"rabbitmq": healthyChecker(),
	}
	h := NewHandler(core, nil)
	h.SetReady(true)

	rec := doGet(t, h, "/readyz")

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("/readyz (redis down): want 503, got %d", rec.Code)
	}

	resp := decodeReadiness(t, rec)
	if resp.Status != "not_ready" {
		t.Fatalf("want status 'not_ready', got %q", resp.Status)
	}
	if resp.Components["redis"].Status != "down" {
		t.Fatalf("redis: want 'down', got %q", resp.Components["redis"].Status)
	}
	if resp.Components["redis"].Error != "connection refused" {
		t.Fatalf("redis error: want 'connection refused', got %q", resp.Components["redis"].Error)
	}
	if resp.Components["postgres"].Status != "up" {
		t.Fatalf("postgres: want 'up', got %q", resp.Components["postgres"].Status)
	}
}

func TestReadiness_MultipleCoreDown(t *testing.T) {
	core := map[string]ComponentChecker{
		"postgres": unhealthyChecker("pg: timeout"),
		"redis":    unhealthyChecker("redis: refused"),
		"rabbitmq": healthyChecker(),
	}
	h := NewHandler(core, nil)
	h.SetReady(true)

	rec := doGet(t, h, "/readyz")

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("/readyz: want 503, got %d", rec.Code)
	}
	resp := decodeReadiness(t, rec)
	if resp.Components["postgres"].Status != "down" {
		t.Fatalf("postgres: want 'down', got %q", resp.Components["postgres"].Status)
	}
	if resp.Components["redis"].Status != "down" {
		t.Fatalf("redis: want 'down', got %q", resp.Components["redis"].Status)
	}
}

// ---------------------------------------------------------------------------
// /readyz — REV-024: non-core down does NOT block readiness
// ---------------------------------------------------------------------------

func TestReadiness_NonCoreDown_StillReady(t *testing.T) {
	core := map[string]ComponentChecker{
		"postgres": healthyChecker(),
		"redis":    healthyChecker(),
		"rabbitmq": healthyChecker(),
	}
	nonCore := map[string]ComponentChecker{
		"object_storage": unhealthyChecker("S3: timeout"),
	}
	h := NewHandler(core, nonCore)
	h.SetReady(true)

	rec := doGet(t, h, "/readyz")

	if rec.Code != http.StatusOK {
		t.Fatalf("/readyz (REV-024): want 200 even with S3 down, got %d", rec.Code)
	}
	resp := decodeReadiness(t, rec)
	if resp.Status != "ready" {
		t.Fatalf("want 'ready', got %q", resp.Status)
	}

	osStatus := resp.Components["object_storage"]
	if osStatus.Status != "down" {
		t.Fatalf("object_storage: want 'down', got %q", osStatus.Status)
	}
	if osStatus.Error != "S3: timeout" {
		t.Fatalf("object_storage error: want 'S3: timeout', got %q", osStatus.Error)
	}
}

func TestReadiness_NonCoreHealthy_Reported(t *testing.T) {
	core := map[string]ComponentChecker{
		"postgres": healthyChecker(),
	}
	nonCore := map[string]ComponentChecker{
		"object_storage": healthyChecker(),
	}
	h := NewHandler(core, nonCore)
	h.SetReady(true)

	rec := doGet(t, h, "/readyz")
	resp := decodeReadiness(t, rec)

	if resp.Components["object_storage"].Status != "up" {
		t.Fatalf("object_storage: want 'up', got %q", resp.Components["object_storage"].Status)
	}
}

// ---------------------------------------------------------------------------
// /readyz — Component breakdown: all components listed
// ---------------------------------------------------------------------------

func TestReadiness_ComponentBreakdown_AllListed(t *testing.T) {
	core := map[string]ComponentChecker{
		"postgres": healthyChecker(),
		"redis":    healthyChecker(),
		"rabbitmq": healthyChecker(),
	}
	nonCore := map[string]ComponentChecker{
		"object_storage": healthyChecker(),
	}
	h := NewHandler(core, nonCore)
	h.SetReady(true)

	rec := doGet(t, h, "/readyz")
	resp := decodeReadiness(t, rec)

	expected := []string{"postgres", "redis", "rabbitmq", "object_storage"}
	for _, name := range expected {
		if _, ok := resp.Components[name]; !ok {
			t.Errorf("missing component %q in response", name)
		}
	}
	if len(resp.Components) != len(expected) {
		t.Errorf("want %d components, got %d", len(expected), len(resp.Components))
	}
}

// ---------------------------------------------------------------------------
// /readyz — Check timeout
// ---------------------------------------------------------------------------

func TestReadiness_CheckTimeout(t *testing.T) {
	core := map[string]ComponentChecker{
		"slow_db": slowChecker(10 * time.Second),
	}
	h := NewHandler(core, nil, WithCheckTimeout(50*time.Millisecond))
	h.SetReady(true)

	start := time.Now()
	rec := doGet(t, h, "/readyz")
	elapsed := time.Since(start)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("/readyz (timeout): want 503, got %d", rec.Code)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("timeout did not work: elapsed %v", elapsed)
	}
	resp := decodeReadiness(t, rec)
	if resp.Components["slow_db"].Status != "down" {
		t.Fatalf("slow_db: want 'down', got %q", resp.Components["slow_db"].Status)
	}
}

// ---------------------------------------------------------------------------
// /readyz — Content-Type
// ---------------------------------------------------------------------------

func TestReadiness_ContentTypeJSON(t *testing.T) {
	tests := []struct {
		name  string
		ready bool
	}{
		{"not_ready", false},
		{"ready_no_checkers", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewHandler(nil, nil)
			if tt.ready {
				h.SetReady(true)
			}
			rec := doGet(t, h, "/readyz")
			ct := rec.Header().Get("Content-Type")
			if ct != "application/json" {
				t.Fatalf("want Content-Type 'application/json', got %q", ct)
			}
		})
	}
}

func TestReadiness_ContentTypeJSON_WithComponents(t *testing.T) {
	core := map[string]ComponentChecker{
		"postgres": healthyChecker(),
	}
	h := NewHandler(core, nil)
	h.SetReady(true)

	rec := doGet(t, h, "/readyz")
	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Fatalf("want Content-Type 'application/json', got %q", ct)
	}
}

// ---------------------------------------------------------------------------
// Constructor: WithCheckTimeout
// ---------------------------------------------------------------------------

func TestWithCheckTimeout_Positive(t *testing.T) {
	h := NewHandler(nil, nil, WithCheckTimeout(10*time.Second))
	if h.timeout != 10*time.Second {
		t.Fatalf("want timeout 10s, got %v", h.timeout)
	}
}

func TestWithCheckTimeout_ZeroIgnored(t *testing.T) {
	h := NewHandler(nil, nil, WithCheckTimeout(0))
	if h.timeout != defaultCheckTimeout {
		t.Fatalf("want default timeout %v, got %v", defaultCheckTimeout, h.timeout)
	}
}

func TestWithCheckTimeout_NegativeIgnored(t *testing.T) {
	h := NewHandler(nil, nil, WithCheckTimeout(-1*time.Second))
	if h.timeout != defaultCheckTimeout {
		t.Fatalf("want default timeout %v, got %v", defaultCheckTimeout, h.timeout)
	}
}

// ---------------------------------------------------------------------------
// Constructor: nil maps
// ---------------------------------------------------------------------------

func TestNewHandler_NilMaps(t *testing.T) {
	h := NewHandler(nil, nil)
	h.SetReady(true)

	rec := doGet(t, h, "/readyz")
	if rec.Code != http.StatusOK {
		t.Fatalf("nil maps: want 200, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// Constructor: name collision panics
// ---------------------------------------------------------------------------

func TestNewHandler_NameCollision_Panics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on name collision, got none")
		}
	}()

	core := map[string]ComponentChecker{"db": healthyChecker()}
	nonCore := map[string]ComponentChecker{"db": healthyChecker()}
	NewHandler(core, nonCore)
}

// ---------------------------------------------------------------------------
// Mux returns non-nil
// ---------------------------------------------------------------------------

func TestMux_NotNil(t *testing.T) {
	h := NewHandler(nil, nil)
	if h.Mux() == nil {
		t.Fatal("Mux() returned nil")
	}
}

// ---------------------------------------------------------------------------
// /readyz — Core down + non-core down → 503 with full breakdown
// ---------------------------------------------------------------------------

func TestReadiness_CoreAndNonCoreDown(t *testing.T) {
	core := map[string]ComponentChecker{
		"postgres": unhealthyChecker("pg: down"),
	}
	nonCore := map[string]ComponentChecker{
		"object_storage": unhealthyChecker("s3: down"),
	}
	h := NewHandler(core, nonCore)
	h.SetReady(true)

	rec := doGet(t, h, "/readyz")

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", rec.Code)
	}
	resp := decodeReadiness(t, rec)
	if resp.Components["postgres"].Status != "down" {
		t.Fatalf("postgres: want 'down', got %q", resp.Components["postgres"].Status)
	}
	if resp.Components["object_storage"].Status != "down" {
		t.Fatalf("object_storage: want 'down', got %q", resp.Components["object_storage"].Status)
	}
}

// ---------------------------------------------------------------------------
// /readyz — Concurrent checker execution
// ---------------------------------------------------------------------------

func TestReadiness_CheckersRunConcurrently(t *testing.T) {
	// Each checker takes 100ms. If sequential, total ≥ 300ms.
	// Concurrently, total should be ~100ms.
	checker := slowChecker(100 * time.Millisecond)
	core := map[string]ComponentChecker{
		"a": checker,
		"b": checker,
		"c": checker,
	}
	h := NewHandler(core, nil, WithCheckTimeout(5*time.Second))
	h.SetReady(true)

	start := time.Now()
	rec := doGet(t, h, "/readyz")
	elapsed := time.Since(start)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if elapsed > 250*time.Millisecond {
		t.Fatalf("checkers appear sequential: elapsed %v (want < 250ms)", elapsed)
	}
}

// ---------------------------------------------------------------------------
// Checker panic recovery
// ---------------------------------------------------------------------------

func TestReadiness_CheckerPanic_RecoveredAsDown(t *testing.T) {
	core := map[string]ComponentChecker{
		"postgres":  healthyChecker(),
		"panicking": panicChecker("unexpected nil pointer"),
	}
	h := NewHandler(core, nil)
	h.SetReady(true)

	rec := doGet(t, h, "/readyz")

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 when checker panics, got %d", rec.Code)
	}
	resp := decodeReadiness(t, rec)
	if resp.Components["panicking"].Status != "down" {
		t.Fatalf("panicking: want 'down', got %q", resp.Components["panicking"].Status)
	}
	if resp.Components["panicking"].Error == "" {
		t.Fatal("panicking: want error message with panic details")
	}
	if resp.Components["postgres"].Status != "up" {
		t.Fatalf("postgres: want 'up' despite other checker panic, got %q", resp.Components["postgres"].Status)
	}
}

func TestReadiness_NonCorePanic_StillReady(t *testing.T) {
	core := map[string]ComponentChecker{
		"postgres": healthyChecker(),
	}
	nonCore := map[string]ComponentChecker{
		"panicking_storage": panicChecker("s3 nil"),
	}
	h := NewHandler(core, nonCore)
	h.SetReady(true)

	rec := doGet(t, h, "/readyz")

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200 when non-core panics, got %d", rec.Code)
	}
	resp := decodeReadiness(t, rec)
	if resp.Components["panicking_storage"].Status != "down" {
		t.Fatalf("panicking_storage: want 'down', got %q", resp.Components["panicking_storage"].Status)
	}
}
