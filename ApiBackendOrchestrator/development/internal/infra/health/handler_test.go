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

// --- test doubles ---

// fakeRedisPinger implements RedisPinger for tests.
type fakeRedisPinger struct {
	err   error
	delay time.Duration
}

func (f *fakeRedisPinger) Ping(ctx context.Context) error {
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return f.err
}

// fakeBrokerPinger implements BrokerPinger for tests.
type fakeBrokerPinger struct {
	err error
}

func (f *fakeBrokerPinger) Ping() error { return f.err }

// helper: issue a GET request and return the recorder.
func doGet(t *testing.T, h *Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.Mux().ServeHTTP(rec, req)
	return rec
}

// helper: decode readiness response body.
func decodeReadiness(t *testing.T, rec *httptest.ResponseRecorder) readinessResponse {
	t.Helper()
	var body readinessResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode readiness response: %v", err)
	}
	return body
}

// helper: decode liveness response body.
func decodeLiveness(t *testing.T, rec *httptest.ResponseRecorder) livenessResponse {
	t.Helper()
	var body livenessResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode liveness response: %v", err)
	}
	return body
}

// newTestHandler creates a Handler with a real DM test server.
func newTestHandler(redis RedisPinger, broker BrokerPinger, dmServer *httptest.Server) *Handler {
	return NewHandler(redis, broker, dmServer.URL)
}

// --- liveness tests ---

func TestLiveness_AlwaysReturns200(t *testing.T) {
	dm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer dm.Close()

	h := newTestHandler(&fakeRedisPinger{}, &fakeBrokerPinger{}, dm)

	rec := doGet(t, h, "/healthz")

	if rec.Code != http.StatusOK {
		t.Fatalf("/healthz: want 200, got %d", rec.Code)
	}
	body := decodeLiveness(t, rec)
	if body.Status != "ok" {
		t.Fatalf("/healthz: want status \"ok\", got %q", body.Status)
	}
}

func TestLiveness_ReturnsJSON(t *testing.T) {
	dm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer dm.Close()

	h := newTestHandler(&fakeRedisPinger{}, &fakeBrokerPinger{}, dm)
	rec := doGet(t, h, "/healthz")

	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("/healthz: want Content-Type \"application/json\", got %q", ct)
	}
}

func TestLiveness_IgnoresNotReady(t *testing.T) {
	dm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer dm.Close()

	h := newTestHandler(&fakeRedisPinger{}, &fakeBrokerPinger{}, dm)
	h.SetNotReady()

	rec := doGet(t, h, "/healthz")

	if rec.Code != http.StatusOK {
		t.Fatalf("/healthz after SetNotReady: want 200, got %d", rec.Code)
	}
}

// --- readiness tests: all healthy ---

func TestReadiness_AllHealthy(t *testing.T) {
	dm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer dm.Close()

	h := newTestHandler(&fakeRedisPinger{}, &fakeBrokerPinger{}, dm)

	rec := doGet(t, h, "/readyz")

	if rec.Code != http.StatusOK {
		t.Fatalf("/readyz: want 200, got %d", rec.Code)
	}
	body := decodeReadiness(t, rec)
	if body.Status != "ready" {
		t.Fatalf("/readyz: want status \"ready\", got %q", body.Status)
	}
	for name, status := range body.Checks {
		if status != "ok" {
			t.Fatalf("/readyz: check %q want \"ok\", got %q", name, status)
		}
	}
	// Verify all three checks are present.
	for _, name := range []string{"redis", "rabbitmq", "dm"} {
		if _, ok := body.Checks[name]; !ok {
			t.Fatalf("/readyz: missing check %q", name)
		}
	}
}

func TestReadiness_ReturnsJSON(t *testing.T) {
	dm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer dm.Close()

	h := newTestHandler(&fakeRedisPinger{}, &fakeBrokerPinger{}, dm)
	rec := doGet(t, h, "/readyz")

	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("/readyz: want Content-Type \"application/json\", got %q", ct)
	}
}

// --- readiness tests: individual failures ---

func TestReadiness_RedisDown(t *testing.T) {
	dm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer dm.Close()

	h := newTestHandler(
		&fakeRedisPinger{err: errors.New("connection refused")},
		&fakeBrokerPinger{},
		dm,
	)

	rec := doGet(t, h, "/readyz")

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("/readyz redis down: want 503, got %d", rec.Code)
	}
	body := decodeReadiness(t, rec)
	if body.Status != "not_ready" {
		t.Fatalf("/readyz redis down: want status \"not_ready\", got %q", body.Status)
	}
	if body.Checks["redis"] == "ok" {
		t.Fatal("/readyz redis down: redis check should not be \"ok\"")
	}
	if body.Checks["rabbitmq"] != "ok" {
		t.Fatalf("/readyz redis down: rabbitmq check want \"ok\", got %q", body.Checks["rabbitmq"])
	}
	if body.Checks["dm"] != "ok" {
		t.Fatalf("/readyz redis down: dm check want \"ok\", got %q", body.Checks["dm"])
	}
}

func TestReadiness_BrokerDown(t *testing.T) {
	dm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer dm.Close()

	h := newTestHandler(
		&fakeRedisPinger{},
		&fakeBrokerPinger{err: errors.New("connection closed")},
		dm,
	)

	rec := doGet(t, h, "/readyz")

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("/readyz broker down: want 503, got %d", rec.Code)
	}
	body := decodeReadiness(t, rec)
	if body.Status != "not_ready" {
		t.Fatalf("/readyz broker down: want status \"not_ready\", got %q", body.Status)
	}
	if body.Checks["rabbitmq"] == "ok" {
		t.Fatal("/readyz broker down: rabbitmq check should not be \"ok\"")
	}
	if body.Checks["redis"] != "ok" {
		t.Fatalf("/readyz broker down: redis check want \"ok\", got %q", body.Checks["redis"])
	}
}

func TestReadiness_DMDown_Non200(t *testing.T) {
	dm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer dm.Close()

	h := newTestHandler(&fakeRedisPinger{}, &fakeBrokerPinger{}, dm)

	rec := doGet(t, h, "/readyz")

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("/readyz dm 500: want 503, got %d", rec.Code)
	}
	body := decodeReadiness(t, rec)
	if body.Checks["dm"] == "ok" {
		t.Fatal("/readyz dm 500: dm check should not be \"ok\"")
	}
	if body.Checks["redis"] != "ok" {
		t.Fatalf("/readyz dm 500: redis check want \"ok\", got %q", body.Checks["redis"])
	}
}

func TestReadiness_DMDown_ConnectionRefused(t *testing.T) {
	// Use a server that is immediately closed so connections fail.
	dm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	dm.Close()

	h := newTestHandler(&fakeRedisPinger{}, &fakeBrokerPinger{}, dm)

	rec := doGet(t, h, "/readyz")

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("/readyz dm unreachable: want 503, got %d", rec.Code)
	}
	body := decodeReadiness(t, rec)
	if body.Checks["dm"] == "ok" {
		t.Fatal("/readyz dm unreachable: dm check should not be \"ok\"")
	}
}

// --- readiness tests: multiple failures ---

func TestReadiness_AllDown(t *testing.T) {
	dm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	dm.Close()

	h := newTestHandler(
		&fakeRedisPinger{err: errors.New("redis timeout")},
		&fakeBrokerPinger{err: errors.New("broker closed")},
		dm,
	)

	rec := doGet(t, h, "/readyz")

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("/readyz all down: want 503, got %d", rec.Code)
	}
	body := decodeReadiness(t, rec)
	if body.Status != "not_ready" {
		t.Fatalf("/readyz all down: want status \"not_ready\", got %q", body.Status)
	}
	for name, status := range body.Checks {
		if status == "ok" {
			t.Fatalf("/readyz all down: check %q should not be \"ok\"", name)
		}
	}
}

// --- SetNotReady ---

func TestReadiness_SetNotReady_Returns503(t *testing.T) {
	dm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer dm.Close()

	h := newTestHandler(&fakeRedisPinger{}, &fakeBrokerPinger{}, dm)
	h.SetNotReady()

	rec := doGet(t, h, "/readyz")

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("/readyz after SetNotReady: want 503, got %d", rec.Code)
	}
	body := decodeReadiness(t, rec)
	if body.Status != "not_ready" {
		t.Fatalf("/readyz after SetNotReady: want status \"not_ready\", got %q", body.Status)
	}
}

func TestReadiness_SetNotReady_SkipsChecks(t *testing.T) {
	// If SetNotReady skips checks, the checks map should be empty.
	dm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer dm.Close()

	h := newTestHandler(&fakeRedisPinger{}, &fakeBrokerPinger{}, dm)
	h.SetNotReady()

	rec := doGet(t, h, "/readyz")

	body := decodeReadiness(t, rec)
	if len(body.Checks) != 0 {
		t.Fatalf("/readyz SetNotReady: want empty checks, got %v", body.Checks)
	}
}

// --- error status format ---

func TestReadiness_ErrorStatusIsUnavailable(t *testing.T) {
	dm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer dm.Close()

	h := newTestHandler(
		&fakeRedisPinger{err: errors.New("READONLY You can't write against a read only replica")},
		&fakeBrokerPinger{},
		dm,
	)

	rec := doGet(t, h, "/readyz")

	body := decodeReadiness(t, rec)
	// Error details must NOT leak to the response (security).
	if body.Checks["redis"] != "unavailable" {
		t.Fatalf("/readyz error format: want \"unavailable\", got %q", body.Checks["redis"])
	}
}

// --- timeout ---

func TestReadiness_SlowCheckRespectTimeout(t *testing.T) {
	// The Redis pinger takes longer than the readiness timeout.
	dm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer dm.Close()

	h := newTestHandler(
		&fakeRedisPinger{delay: 5 * time.Second},
		&fakeBrokerPinger{},
		dm,
	)

	start := time.Now()
	rec := doGet(t, h, "/readyz")
	elapsed := time.Since(start)

	// Must complete within readinessTimeout + some tolerance, not 5s.
	if elapsed > readinessTimeout+500*time.Millisecond {
		t.Fatalf("/readyz timeout: took %v, expected <= %v", elapsed, readinessTimeout+500*time.Millisecond)
	}

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("/readyz timeout: want 503, got %d", rec.Code)
	}
	body := decodeReadiness(t, rec)
	if body.Checks["redis"] == "ok" {
		t.Fatal("/readyz timeout: redis check should not be \"ok\" after timeout")
	}
}

// --- compile-time interface checks ---

// Verify that the interfaces are minimal and correct.
var _ RedisPinger = (*fakeRedisPinger)(nil)
var _ BrokerPinger = (*fakeBrokerPinger)(nil)

// --- DM endpoint path ---

func TestDM_HealthzPathAppended(t *testing.T) {
	var gotPath string
	dm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer dm.Close()

	h := newTestHandler(&fakeRedisPinger{}, &fakeBrokerPinger{}, dm)
	_ = doGet(t, h, "/readyz")

	if gotPath != "/healthz" {
		t.Fatalf("DM health check: want path \"/healthz\", got %q", gotPath)
	}
}

// --- HTTP method restriction ---

func TestLiveness_PostReturns405(t *testing.T) {
	dm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer dm.Close()

	h := newTestHandler(&fakeRedisPinger{}, &fakeBrokerPinger{}, dm)

	req := httptest.NewRequest(http.MethodPost, "/healthz", nil)
	rec := httptest.NewRecorder()
	h.Mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /healthz: want 405, got %d", rec.Code)
	}
}

func TestReadiness_PostReturns405(t *testing.T) {
	dm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer dm.Close()

	h := newTestHandler(&fakeRedisPinger{}, &fakeBrokerPinger{}, dm)

	req := httptest.NewRequest(http.MethodPost, "/readyz", nil)
	rec := httptest.NewRecorder()
	h.Mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /readyz: want 405, got %d", rec.Code)
	}
}

// --- DM URL normalization ---

func TestDM_TrailingSlashNormalized(t *testing.T) {
	var gotPath string
	dm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer dm.Close()

	// dmBaseURL with trailing slash — should NOT produce "//healthz"
	h := NewHandler(&fakeRedisPinger{}, &fakeBrokerPinger{}, dm.URL+"/")
	_ = doGet(t, h, "/readyz")

	if gotPath != "/healthz" {
		t.Fatalf("DM URL normalization: want \"/healthz\", got %q", gotPath)
	}
}

// --- slow DM server ---

func TestReadiness_SlowDMRespectsTimeout(t *testing.T) {
	dm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer dm.Close()

	h := newTestHandler(&fakeRedisPinger{}, &fakeBrokerPinger{}, dm)

	start := time.Now()
	rec := doGet(t, h, "/readyz")
	elapsed := time.Since(start)

	if elapsed > readinessTimeout+500*time.Millisecond {
		t.Fatalf("/readyz slow DM: took %v, expected <= %v", elapsed, readinessTimeout+500*time.Millisecond)
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("/readyz slow DM: want 503, got %d", rec.Code)
	}
	body := decodeReadiness(t, rec)
	if body.Checks["dm"] == "ok" {
		t.Fatal("/readyz slow DM: dm check should not be \"ok\" after timeout")
	}
}

// --- concurrent safety ---

func TestHandler_ConcurrentAccess(t *testing.T) {
	dm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer dm.Close()

	h := newTestHandler(&fakeRedisPinger{}, &fakeBrokerPinger{}, dm)

	done := make(chan struct{})
	for i := 0; i < 50; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			doGet(t, h, "/healthz")
			doGet(t, h, "/readyz")
		}()
	}

	// Toggle not-ready mid-flight.
	go func() {
		h.SetNotReady()
		done <- struct{}{}
	}()

	for i := 0; i < 51; i++ {
		<-done
	}
}
