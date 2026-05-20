package health

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// --- Test doubles ---

// fakeChecker is a deterministic, goroutine-safe Checker double. It records
// invocation counts and supports configured success / configured-error /
// sleep-then-honour-ctx behaviour without pulling Redis/RabbitMQ into the
// hermetic health package's test surface.
type fakeChecker struct {
	name    string
	err     error
	sleep   time.Duration
	invokes atomic.Int64
}

func (f *fakeChecker) Name() string { return f.name }

func (f *fakeChecker) Check(ctx context.Context) error {
	f.invokes.Add(1)
	if f.sleep > 0 {
		select {
		case <-time.After(f.sleep):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return f.err
}

var _ Checker = (*fakeChecker)(nil)

// fakeMetricsHandler is the injected /metrics forward — proves D6: the
// health package never imports promhttp; whatever http.Handler the wiring
// passes is mounted verbatim.
type fakeMetricsHandler struct {
	body string
	code int
}

func (f *fakeMetricsHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	if f.code == 0 {
		f.code = http.StatusOK
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	w.WriteHeader(f.code)
	_, _ = w.Write([]byte(f.body))
}

var _ http.Handler = (*fakeMetricsHandler)(nil)

// --- Helpers ---

func doGet(t *testing.T, h *Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.Mux().ServeHTTP(rec, req)
	return rec
}

func decodeReady(t *testing.T, rec *httptest.ResponseRecorder) readyResponse {
	t.Helper()
	var body readyResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode readyResponse: %v", err)
	}
	return body
}

func decodeRaw(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode raw JSON: %v", err)
	}
	return body
}

// --- Liveness ---

func TestLiveness_AlwaysReturns200(t *testing.T) {
	t.Parallel()
	h := NewHandler(nil, nil)
	rec := doGet(t, h, "/healthz")
	if rec.Code != http.StatusOK {
		t.Fatalf("/healthz: want 200, got %d", rec.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("/healthz: want status=ok, got %q", body["status"])
	}
}

func TestLiveness_ContentTypeJSON(t *testing.T) {
	t.Parallel()
	h := NewHandler(nil, nil)
	rec := doGet(t, h, "/healthz")
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type: want application/json, got %q", ct)
	}
}

// --- Readiness happy path ---

func TestReadiness_NoCheckers_Returns200(t *testing.T) {
	t.Parallel()
	h := NewHandler(nil, nil)
	rec := doGet(t, h, "/readyz")
	if rec.Code != http.StatusOK {
		t.Fatalf("/readyz: want 200, got %d", rec.Code)
	}
	body := decodeReady(t, rec)
	if body.Status != "ok" {
		t.Fatalf("status: want ok, got %q", body.Status)
	}
	if body.Checks == nil {
		t.Fatal("checks field must be non-nil ([]) even when empty")
	}
	if len(body.Checks) != 0 {
		t.Fatalf("len(checks) = %d, want 0", len(body.Checks))
	}
}

func TestReadiness_AllCheckersPass_Returns200(t *testing.T) {
	t.Parallel()
	checkers := []Checker{
		&fakeChecker{name: "redis"},
		&fakeChecker{name: "rabbitmq"},
	}
	h := NewHandler(checkers, nil)
	rec := doGet(t, h, "/readyz")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	body := decodeReady(t, rec)
	if body.Status != "ok" {
		t.Fatalf("status: want ok, got %q", body.Status)
	}
	if len(body.Checks) != 2 {
		t.Fatalf("len(checks) = %d, want 2", len(body.Checks))
	}
	for _, c := range body.Checks {
		if c.Status != "ok" {
			t.Errorf("check %s status = %q, want ok", c.Name, c.Status)
		}
		if c.Error != "" {
			t.Errorf("check %s should have no error, got %q", c.Name, c.Error)
		}
	}
}

func TestReadiness_OneCheckerFails_Returns503_WithPerDepStatus(t *testing.T) {
	t.Parallel()
	checkers := []Checker{
		&fakeChecker{name: "redis"},
		&fakeChecker{name: "rabbitmq", err: errors.New("Ping/Channel: connection refused")},
	}
	h := NewHandler(checkers, nil)
	rec := doGet(t, h, "/readyz")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", rec.Code)
	}
	body := decodeReady(t, rec)
	if body.Status != "not_ready" {
		t.Fatalf("status: want not_ready, got %q", body.Status)
	}
	statuses := map[string]checkJSON{}
	for _, c := range body.Checks {
		statuses[c.Name] = c
	}
	if statuses["redis"].Status != "ok" {
		t.Errorf("redis status = %q, want ok", statuses["redis"].Status)
	}
	if statuses["rabbitmq"].Status != "failed" {
		t.Errorf("rabbitmq status = %q, want failed", statuses["rabbitmq"].Status)
	}
	if statuses["rabbitmq"].Error == "" {
		t.Error("rabbitmq error must be populated on failure")
	}
}

// --- Timeout semantics ---

func TestReadiness_TimeoutCheckerLabelledTimeout(t *testing.T) {
	t.Parallel()
	slow := &fakeChecker{name: "slow", sleep: 200 * time.Millisecond}
	h := NewHandler(
		[]Checker{slow},
		nil,
		WithDefaultCheckerTimeout(400*time.Millisecond),
		WithCheckerTimeout("slow", 30*time.Millisecond),
		WithReadyDeadline(500*time.Millisecond),
	)
	rec := doGet(t, h, "/readyz")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", rec.Code)
	}
	body := decodeReady(t, rec)
	if len(body.Checks) != 1 {
		t.Fatalf("len(checks) = %d, want 1", len(body.Checks))
	}
	if got := body.Checks[0].Status; got != "timeout" {
		t.Fatalf("status: want timeout, got %q", got)
	}
}

// panickingChecker reproduces a buggy adapter that panics during Check —
// code-reviewer M3 pins that the recovery converts the panic to a "failed"
// check rather than letting it crash the process.
type panickingChecker struct {
	name    string
	panicV  any
	invokes atomic.Int64
}

func (p *panickingChecker) Name() string { return p.name }

func (p *panickingChecker) Check(_ context.Context) error {
	p.invokes.Add(1)
	panic(p.panicV)
}

var _ Checker = (*panickingChecker)(nil)

func TestReadiness_CheckerPanic_RecoveredAsFailed(t *testing.T) {
	t.Parallel()
	bad := &panickingChecker{name: "bad", panicV: "boom"}
	ok := &fakeChecker{name: "ok"}
	h := NewHandler(
		[]Checker{bad, ok},
		nil,
		WithDefaultCheckerTimeout(100*time.Millisecond),
		WithReadyDeadline(200*time.Millisecond),
	)
	rec := doGet(t, h, "/readyz")

	// The process must NOT crash; we get a normal 503 with per-check status.
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", rec.Code)
	}
	body := decodeReady(t, rec)
	if len(body.Checks) != 2 {
		t.Fatalf("len(checks) = %d, want 2", len(body.Checks))
	}
	var badCheck, okCheck checkJSON
	for _, c := range body.Checks {
		switch c.Name {
		case "bad":
			badCheck = c
		case "ok":
			okCheck = c
		}
	}
	if badCheck.Status != "failed" {
		t.Errorf("bad check status: want failed, got %q", badCheck.Status)
	}
	if badCheck.Error == "" {
		t.Error("bad check error: want non-empty, got empty")
	}
	if !contains(badCheck.Error, "boom") {
		t.Errorf("bad check error: want to contain panic value %q, got %q", "boom", badCheck.Error)
	}
	if okCheck.Status != "ok" {
		t.Errorf("ok check status: want ok, got %q", okCheck.Status)
	}
	if bad.invokes.Load() != 1 {
		t.Errorf("bad invokes = %d, want 1", bad.invokes.Load())
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestReadiness_RespectsRequestDeadline(t *testing.T) {
	t.Parallel()
	// Per-checker timeout equals readyDeadline (both 50ms — the MF-3 guard
	// pins `readyDeadline >= max(checker timeouts)`). A 1s sleeping checker
	// that honours its ctx is cancelled by whichever deadline fires first
	// — both fire at ~50ms — and labelled "timeout". The elapsed bound
	// (200ms) proves the request did not hang for the full 1s sleep.
	slow := &fakeChecker{name: "slow", sleep: 1 * time.Second}
	h := NewHandler(
		[]Checker{slow},
		nil,
		WithDefaultCheckerTimeout(50*time.Millisecond),
		WithReadyDeadline(50*time.Millisecond),
	)
	start := time.Now()
	rec := doGet(t, h, "/readyz")
	elapsed := time.Since(start)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", rec.Code)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("request deadline did not cut the slow check: elapsed=%v", elapsed)
	}
	body := decodeReady(t, rec)
	if got := body.Checks[0].Status; got != "timeout" {
		t.Fatalf("status: want timeout, got %q", got)
	}
}

// --- Parallelism ---

func TestReadiness_ParallelExecution(t *testing.T) {
	t.Parallel()
	// Three 50ms checkers must complete in roughly 50ms (parallel), not
	// 150ms (sequential). Allow a generous upper bound to absorb CI jitter
	// but stay well below sequential time.
	checkers := []Checker{
		&fakeChecker{name: "a", sleep: 50 * time.Millisecond},
		&fakeChecker{name: "b", sleep: 50 * time.Millisecond},
		&fakeChecker{name: "c", sleep: 50 * time.Millisecond},
	}
	h := NewHandler(checkers, nil)
	start := time.Now()
	rec := doGet(t, h, "/readyz")
	elapsed := time.Since(start)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if elapsed > 120*time.Millisecond {
		t.Fatalf("checks ran sequentially: elapsed=%v (want < 120ms)", elapsed)
	}
}

// --- SetNotReady ---

func TestSetNotReady_Returns503_WithReason(t *testing.T) {
	t.Parallel()
	h := NewHandler(nil, nil)
	h.SetNotReady()
	rec := doGet(t, h, "/readyz")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", rec.Code)
	}
	body := decodeReady(t, rec)
	if body.Status != "not_ready" {
		t.Fatalf("status: want not_ready, got %q", body.Status)
	}
	if body.Reason != "shutting_down" {
		t.Fatalf("reason: want shutting_down, got %q", body.Reason)
	}
	if body.Checks == nil {
		t.Fatal("checks field must be present as [] even on shutdown")
	}
	if len(body.Checks) != 0 {
		t.Fatalf("len(checks) = %d, want 0 (no probes after SetNotReady)", len(body.Checks))
	}
}

func TestSetNotReady_SkipsCheckerExecution(t *testing.T) {
	t.Parallel()
	c := &fakeChecker{name: "redis"}
	h := NewHandler([]Checker{c}, nil)
	h.SetNotReady()
	doGet(t, h, "/readyz")
	doGet(t, h, "/readyz")
	if got := c.invokes.Load(); got != 0 {
		t.Fatalf("Check invocations after SetNotReady: %d, want 0 (short-circuit)", got)
	}
}

func TestSetNotReady_SecondCallNoop(t *testing.T) {
	t.Parallel()
	h := NewHandler(nil, nil)
	h.SetNotReady()
	// A second call must not panic and must keep the state not_ready.
	h.SetNotReady()
	rec := doGet(t, h, "/readyz")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", rec.Code)
	}
}

func TestSetNotReady_DoesNotRevertToReady(t *testing.T) {
	t.Parallel()
	h := NewHandler(nil, nil)
	h.SetNotReady()
	// Multiple calls — sticky-once means we stay not_ready forever.
	for i := 0; i < 5; i++ {
		h.SetNotReady()
		rec := doGet(t, h, "/readyz")
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("iter %d: want 503, got %d (sticky-once breached)", i, rec.Code)
		}
	}
}

// --- NewHandler fail-fast ---

func TestNewHandler_DuplicateName_Panics(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Fatal("NewHandler must panic on duplicate Checker.Name()")
		}
	}()
	NewHandler([]Checker{
		&fakeChecker{name: "redis"},
		&fakeChecker{name: "redis"},
	}, nil)
}

func TestNewHandler_EmptyName_Panics(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Fatal("NewHandler must panic on empty Checker.Name()")
		}
	}()
	NewHandler([]Checker{
		&fakeChecker{name: ""},
	}, nil)
}

func TestNewHandler_ReadyDeadlineLessThanCheckerTimeout_Panics(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Fatal("NewHandler must panic when readyDeadline < per-checker timeout")
		}
	}()
	NewHandler(
		[]Checker{&fakeChecker{name: "redis"}},
		nil,
		WithDefaultCheckerTimeout(2*time.Second),
		WithReadyDeadline(1*time.Second),
	)
}

func TestNewHandler_ReadyDeadlineLessThanPerNameOverride_Panics(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Fatal("NewHandler must panic when readyDeadline < a WithCheckerTimeout override")
		}
	}()
	NewHandler(
		[]Checker{&fakeChecker{name: "slow"}},
		nil,
		WithCheckerTimeout("slow", 5*time.Second),
		WithReadyDeadline(2*time.Second),
	)
}

// --- Metrics forward ---

func TestMetrics_ProxiesToInjectedHandler(t *testing.T) {
	t.Parallel()
	mh := &fakeMetricsHandler{body: "lic_test 1\n"}
	h := NewHandler(nil, mh)
	rec := doGet(t, h, "/metrics")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if got := rec.Body.String(); got != "lic_test 1\n" {
		t.Fatalf("body = %q, want %q", got, "lic_test 1\n")
	}
}

func TestMetrics_NilHandler_Returns404(t *testing.T) {
	t.Parallel()
	// When wiring opts out of metrics, /metrics is simply not registered;
	// the default ServeMux returns 404. This guards against accidentally
	// mounting a nil handler (would panic on first call).
	h := NewHandler(nil, nil)
	rec := doGet(t, h, "/metrics")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", rec.Code)
	}
}

// --- Per-checker timeout override ---

func TestPerCheckerTimeoutOverride(t *testing.T) {
	t.Parallel()
	// "slow" gets a 30ms override; "fast" runs under the 1s default.
	// "slow" must time out, "fast" must succeed.
	slow := &fakeChecker{name: "slow", sleep: 200 * time.Millisecond}
	fast := &fakeChecker{name: "fast", sleep: 10 * time.Millisecond}
	h := NewHandler(
		[]Checker{slow, fast},
		nil,
		WithCheckerTimeout("slow", 30*time.Millisecond),
	)
	rec := doGet(t, h, "/readyz")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 (slow times out), got %d", rec.Code)
	}
	body := decodeReady(t, rec)
	statuses := map[string]string{}
	for _, c := range body.Checks {
		statuses[c.Name] = c.Status
	}
	if statuses["slow"] != "timeout" {
		t.Errorf("slow status = %q, want timeout", statuses["slow"])
	}
	if statuses["fast"] != "ok" {
		t.Errorf("fast status = %q, want ok (default 1s applies)", statuses["fast"])
	}
}

// --- JSON shape invariants (MF-7) ---

func TestErrorOmitemptyWhenOK(t *testing.T) {
	t.Parallel()
	h := NewHandler([]Checker{&fakeChecker{name: "redis"}}, nil)
	rec := doGet(t, h, "/readyz")
	raw := decodeRaw(t, rec)
	checks, ok := raw["checks"].([]any)
	if !ok || len(checks) != 1 {
		t.Fatalf("checks not a 1-element array: %v", raw["checks"])
	}
	first, _ := checks[0].(map[string]any)
	if _, hasErr := first["error"]; hasErr {
		t.Fatalf("ok check must omit \"error\" key, got %v", first)
	}
}

func TestStatusValuesAreBounded(t *testing.T) {
	t.Parallel()
	// Construct a suite covering all three per-check states:
	// ok / failed / timeout, plus the overall "ok" / "not_ready" branches.
	okC := &fakeChecker{name: "ok"}
	failC := &fakeChecker{name: "fail", err: errors.New("boom")}
	tmoC := &fakeChecker{name: "tmo", sleep: 200 * time.Millisecond}
	h := NewHandler(
		[]Checker{okC, failC, tmoC},
		nil,
		WithCheckerTimeout("tmo", 30*time.Millisecond),
	)
	rec := doGet(t, h, "/readyz")
	body := decodeReady(t, rec)
	if body.Status != "not_ready" {
		t.Fatalf("overall status = %q, want not_ready", body.Status)
	}
	allowed := map[string]struct{}{"ok": {}, "failed": {}, "timeout": {}}
	for _, c := range body.Checks {
		if _, ok := allowed[c.Status]; !ok {
			t.Errorf("check %s: status %q outside bounded set", c.Name, c.Status)
		}
	}
	// And the overall "ok" branch.
	h2 := NewHandler([]Checker{&fakeChecker{name: "x"}}, nil)
	rec2 := doGet(t, h2, "/readyz")
	body2 := decodeReady(t, rec2)
	if body2.Status != "ok" {
		t.Fatalf("overall status (all-pass) = %q, want ok", body2.Status)
	}
}

func TestChecksFieldAlwaysPresent(t *testing.T) {
	t.Parallel()
	// SetNotReady path renders checks: [] (not omitted) so clients can
	// branch on len(checks) uniformly.
	h := NewHandler(nil, nil)
	h.SetNotReady()
	rec := doGet(t, h, "/readyz")
	raw := decodeRaw(t, rec)
	if _, has := raw["checks"]; !has {
		t.Fatalf("checks key missing from shutdown response: %v", raw)
	}
}

// --- Content-Type ---

func TestReadiness_ContentTypeJSON(t *testing.T) {
	t.Parallel()
	h := NewHandler(nil, nil)
	rec := doGet(t, h, "/readyz")
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("want application/json, got %q", ct)
	}
}

// --- Latency measurement ---

func TestLatencyMs_MeasuredByHealthPackage(t *testing.T) {
	t.Parallel()
	// A 30ms sleeping checker must report latency_ms ≈ 30 — proves the
	// health package times it externally (MF-6). Allow a wide upper bound
	// for CI jitter but tight enough that a zeroed/uninitialised value
	// would fail.
	c := &fakeChecker{name: "slowish", sleep: 30 * time.Millisecond}
	h := NewHandler([]Checker{c}, nil)
	rec := doGet(t, h, "/readyz")
	body := decodeReady(t, rec)
	if len(body.Checks) != 1 {
		t.Fatalf("len(checks) = %d, want 1", len(body.Checks))
	}
	if body.Checks[0].LatencyMs < 20 {
		t.Errorf("latency_ms = %d, want >= 20 (sleep was 30ms)", body.Checks[0].LatencyMs)
	}
}

// --- -race coverage ---

func TestReadiness_ConcurrentRequests_RaceClean(t *testing.T) {
	t.Parallel()
	c := &fakeChecker{name: "redis"}
	h := NewHandler([]Checker{c}, nil)
	const goroutines = 50
	const iterations = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				rec := doGet(t, h, "/readyz")
				if rec.Code != http.StatusOK {
					t.Errorf("want 200, got %d", rec.Code)
					return
				}
			}
		}()
	}
	wg.Wait()
	if got := c.invokes.Load(); got != int64(goroutines*iterations) {
		t.Fatalf("invokes = %d, want %d", got, goroutines*iterations)
	}
}

func TestSetNotReady_ConcurrentWithReadyz_RaceClean(t *testing.T) {
	t.Parallel()
	c := &fakeChecker{name: "redis"}
	h := NewHandler([]Checker{c}, nil)
	const goroutines = 25
	var wg sync.WaitGroup
	wg.Add(goroutines * 2)
	// Half fire /readyz, half fire SetNotReady — must be -race clean and
	// the post-flip readyz must consistently return 503.
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				doGet(t, h, "/readyz")
			}
		}()
		go func() {
			defer wg.Done()
			h.SetNotReady()
		}()
	}
	wg.Wait()
	rec := doGet(t, h, "/readyz")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("after all SetNotReady: want 503, got %d", rec.Code)
	}
}
