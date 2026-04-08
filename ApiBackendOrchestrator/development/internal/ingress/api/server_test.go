package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"contractpro/api-orchestrator/internal/config"
	"contractpro/api-orchestrator/internal/infra/health"
	"contractpro/api-orchestrator/internal/infra/observability/logger"
)

// --- test doubles ---

// fakeRedisPinger implements health.RedisPinger for tests.
type fakeRedisPinger struct{ err error }

func (f *fakeRedisPinger) Ping(_ context.Context) error { return f.err }

// fakeBrokerPinger implements health.BrokerPinger for tests.
type fakeBrokerPinger struct{ err error }

func (f *fakeBrokerPinger) Ping() error { return f.err }

// --- helpers ---

func testConfig() config.HTTPConfig {
	return config.HTTPConfig{
		Port:            8080,
		MetricsPort:     9090,
		RequestTimeout:  30 * time.Second,
		UploadTimeout:   60 * time.Second,
		ShutdownTimeout: 5 * time.Second,
	}
}

func testLogger() *logger.Logger {
	return logger.NewLogger("error") // quiet during tests
}

func testHealth() *health.Handler {
	dm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	// Note: dm server is leaked in tests; acceptable for unit tests.
	return health.NewHandler(&fakeRedisPinger{}, &fakeBrokerPinger{}, dm.URL)
}

func newTestServer() *Server {
	return NewServer(Deps{
		Config: testConfig(),
		Health: testHealth(),
		Logger: testLogger(),
	})
}

// doRequest issues a request against the server's router and returns
// the response recorder.
func doRequest(t *testing.T, s *Server, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	return rec
}

// decodeJSON decodes the response body into the provided value.
func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.NewDecoder(rec.Body).Decode(v); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
}

// --- constructor tests ---

func TestNewServer_SetsFields(t *testing.T) {
	s := newTestServer()

	if s.router == nil {
		t.Fatal("NewServer: router must not be nil")
	}
	if s.main == nil {
		t.Fatal("NewServer: main server must not be nil")
	}
	if s.metrics == nil {
		t.Fatal("NewServer: metrics server must not be nil")
	}
	if s.log == nil {
		t.Fatal("NewServer: logger must not be nil")
	}
}

func TestNewServer_MainAddr(t *testing.T) {
	s := newTestServer()
	if s.MainAddr() != ":8080" {
		t.Fatalf("MainAddr: want \":8080\", got %q", s.MainAddr())
	}
}

func TestNewServer_MetricsAddr(t *testing.T) {
	s := newTestServer()
	if s.MetricsAddr() != ":9090" {
		t.Fatalf("MetricsAddr: want \":9090\", got %q", s.MetricsAddr())
	}
}

func TestNewServer_Timeouts(t *testing.T) {
	cfg := testConfig()
	cfg.RequestTimeout = 15 * time.Second

	s := NewServer(Deps{
		Config: cfg,
		Health: testHealth(),
		Logger: testLogger(),
	})

	if s.main.ReadTimeout != 15*time.Second {
		t.Fatalf("ReadTimeout: want 15s, got %v", s.main.ReadTimeout)
	}
	// WriteTimeout = RequestTimeout + 5s
	if s.main.WriteTimeout != 20*time.Second {
		t.Fatalf("WriteTimeout: want 20s, got %v", s.main.WriteTimeout)
	}
	// IdleTimeout = 2 * RequestTimeout
	if s.main.IdleTimeout != 30*time.Second {
		t.Fatalf("IdleTimeout: want 30s, got %v", s.main.IdleTimeout)
	}
}

func TestNewServer_MetricsTimeouts(t *testing.T) {
	s := newTestServer()

	if s.metrics.ReadTimeout != 5*time.Second {
		t.Fatalf("metrics ReadTimeout: want 5s, got %v", s.metrics.ReadTimeout)
	}
	if s.metrics.WriteTimeout != 5*time.Second {
		t.Fatalf("metrics WriteTimeout: want 5s, got %v", s.metrics.WriteTimeout)
	}
	if s.metrics.IdleTimeout != 30*time.Second {
		t.Fatalf("metrics IdleTimeout: want 30s, got %v", s.metrics.IdleTimeout)
	}
}

// --- health endpoint integration ---

func TestHealthz_Returns200(t *testing.T) {
	s := newTestServer()
	rec := doRequest(t, s, http.MethodGet, "/healthz")

	if rec.Code != http.StatusOK {
		t.Fatalf("/healthz: want 200, got %d", rec.Code)
	}
}

func TestReadyz_Returns200(t *testing.T) {
	s := newTestServer()
	rec := doRequest(t, s, http.MethodGet, "/readyz")

	if rec.Code != http.StatusOK {
		t.Fatalf("/readyz: want 200, got %d", rec.Code)
	}
}

// --- public auth endpoints ---

func TestAuthLogin_Returns501(t *testing.T) {
	s := newTestServer()
	rec := doRequest(t, s, http.MethodPost, "/api/v1/auth/login")

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("POST /api/v1/auth/login: want 501, got %d", rec.Code)
	}
	assertJSONError(t, rec, "NOT_IMPLEMENTED")
}

func TestAuthRefresh_Returns501(t *testing.T) {
	s := newTestServer()
	rec := doRequest(t, s, http.MethodPost, "/api/v1/auth/refresh")

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("POST /api/v1/auth/refresh: want 501, got %d", rec.Code)
	}
}

func TestAuthLogout_Returns501(t *testing.T) {
	s := newTestServer()
	rec := doRequest(t, s, http.MethodPost, "/api/v1/auth/logout")

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("POST /api/v1/auth/logout: want 501, got %d", rec.Code)
	}
}

// --- protected contract endpoints ---

func TestContractsUpload_Returns501(t *testing.T) {
	s := newTestServer()
	rec := doRequest(t, s, http.MethodPost, "/api/v1/contracts/upload")

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("POST /api/v1/contracts/upload: want 501, got %d", rec.Code)
	}
}

func TestContractsList_Returns501(t *testing.T) {
	s := newTestServer()
	rec := doRequest(t, s, http.MethodGet, "/api/v1/contracts")

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("GET /api/v1/contracts: want 501, got %d", rec.Code)
	}
}

func TestContractsGet_Returns501(t *testing.T) {
	s := newTestServer()
	rec := doRequest(t, s, http.MethodGet, "/api/v1/contracts/abc-123")

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("GET /api/v1/contracts/{id}: want 501, got %d", rec.Code)
	}
}

func TestContractsDelete_Returns501(t *testing.T) {
	s := newTestServer()
	rec := doRequest(t, s, http.MethodDelete, "/api/v1/contracts/abc-123")

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("DELETE /api/v1/contracts/{id}: want 501, got %d", rec.Code)
	}
}

func TestContractsArchive_Returns501(t *testing.T) {
	s := newTestServer()
	rec := doRequest(t, s, http.MethodPost, "/api/v1/contracts/abc-123/archive")

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("POST /api/v1/contracts/{id}/archive: want 501, got %d", rec.Code)
	}
}

// --- protected version endpoints ---

func TestVersionsList_Returns501(t *testing.T) {
	s := newTestServer()
	rec := doRequest(t, s, http.MethodGet, "/api/v1/contracts/abc-123/versions")

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("GET /api/v1/contracts/{id}/versions: want 501, got %d", rec.Code)
	}
}

func TestVersionsUpload_Returns501(t *testing.T) {
	s := newTestServer()
	rec := doRequest(t, s, http.MethodPost, "/api/v1/contracts/abc-123/versions/upload")

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("POST /api/v1/contracts/{id}/versions/upload: want 501, got %d", rec.Code)
	}
}

func TestVersionGet_Returns501(t *testing.T) {
	s := newTestServer()
	rec := doRequest(t, s, http.MethodGet, "/api/v1/contracts/abc-123/versions/v-456")

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("GET /api/v1/contracts/{id}/versions/{vid}: want 501, got %d", rec.Code)
	}
}

func TestVersionStatus_Returns501(t *testing.T) {
	s := newTestServer()
	rec := doRequest(t, s, http.MethodGet, "/api/v1/contracts/abc-123/versions/v-456/status")

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("GET .../versions/{vid}/status: want 501, got %d", rec.Code)
	}
}

func TestVersionRecheck_Returns501(t *testing.T) {
	s := newTestServer()
	rec := doRequest(t, s, http.MethodPost, "/api/v1/contracts/abc-123/versions/v-456/recheck")

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("POST .../versions/{vid}/recheck: want 501, got %d", rec.Code)
	}
}

// --- protected result endpoints ---

func TestResults_Returns501(t *testing.T) {
	s := newTestServer()
	rec := doRequest(t, s, http.MethodGet, "/api/v1/contracts/abc-123/versions/v-456/results")

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("GET .../results: want 501, got %d", rec.Code)
	}
}

func TestRisks_Returns501(t *testing.T) {
	s := newTestServer()
	rec := doRequest(t, s, http.MethodGet, "/api/v1/contracts/abc-123/versions/v-456/risks")

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("GET .../risks: want 501, got %d", rec.Code)
	}
}

func TestSummary_Returns501(t *testing.T) {
	s := newTestServer()
	rec := doRequest(t, s, http.MethodGet, "/api/v1/contracts/abc-123/versions/v-456/summary")

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("GET .../summary: want 501, got %d", rec.Code)
	}
}

func TestRecommendations_Returns501(t *testing.T) {
	s := newTestServer()
	rec := doRequest(t, s, http.MethodGet, "/api/v1/contracts/abc-123/versions/v-456/recommendations")

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("GET .../recommendations: want 501, got %d", rec.Code)
	}
}

// --- protected comparison endpoints ---

func TestCompare_Returns501(t *testing.T) {
	s := newTestServer()
	rec := doRequest(t, s, http.MethodPost, "/api/v1/contracts/abc-123/compare")

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("POST .../compare: want 501, got %d", rec.Code)
	}
}

func TestDiff_Returns501(t *testing.T) {
	s := newTestServer()
	rec := doRequest(t, s, http.MethodGet, "/api/v1/contracts/abc-123/versions/v1/diff/v2")

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("GET .../diff/{target}: want 501, got %d", rec.Code)
	}
}

// --- protected export endpoint ---

func TestExport_Returns501(t *testing.T) {
	s := newTestServer()
	rec := doRequest(t, s, http.MethodGet, "/api/v1/contracts/abc-123/versions/v-456/export/pdf")

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("GET .../export/{format}: want 501, got %d", rec.Code)
	}
}

// --- protected feedback endpoint ---

func TestFeedback_Returns501(t *testing.T) {
	s := newTestServer()
	rec := doRequest(t, s, http.MethodPost, "/api/v1/contracts/abc-123/versions/v-456/feedback")

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("POST .../feedback: want 501, got %d", rec.Code)
	}
}

// --- protected admin endpoints ---

func TestAdminPoliciesList_Returns501(t *testing.T) {
	s := newTestServer()
	rec := doRequest(t, s, http.MethodGet, "/api/v1/admin/policies")

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("GET /admin/policies: want 501, got %d", rec.Code)
	}
}

func TestAdminPoliciesUpdate_Returns501(t *testing.T) {
	s := newTestServer()
	rec := doRequest(t, s, http.MethodPut, "/api/v1/admin/policies/pol-1")

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("PUT /admin/policies/{id}: want 501, got %d", rec.Code)
	}
}

func TestAdminChecklistsList_Returns501(t *testing.T) {
	s := newTestServer()
	rec := doRequest(t, s, http.MethodGet, "/api/v1/admin/checklists")

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("GET /admin/checklists: want 501, got %d", rec.Code)
	}
}

func TestAdminChecklistsUpdate_Returns501(t *testing.T) {
	s := newTestServer()
	rec := doRequest(t, s, http.MethodPut, "/api/v1/admin/checklists/cl-1")

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("PUT /admin/checklists/{id}: want 501, got %d", rec.Code)
	}
}

// --- protected user endpoint ---

func TestUsersMe_Returns501(t *testing.T) {
	s := newTestServer()
	rec := doRequest(t, s, http.MethodGet, "/api/v1/users/me")

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("GET /api/v1/users/me: want 501, got %d", rec.Code)
	}
}

// --- SSE endpoint ---

func TestSSE_ReturnsEventStream(t *testing.T) {
	s := newTestServer()
	rec := doRequest(t, s, http.MethodGet, "/api/v1/events/stream")

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /events/stream: want 200, got %d", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if ct != "text/event-stream" {
		t.Fatalf("SSE Content-Type: want \"text/event-stream\", got %q", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, ": connected") {
		t.Fatalf("SSE body should contain heartbeat comment, got %q", body)
	}
}

// --- 404 for unknown routes ---

func TestUnknownRoute_Returns404(t *testing.T) {
	s := newTestServer()
	rec := doRequest(t, s, http.MethodGet, "/api/v1/unknown")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /api/v1/unknown: want 404, got %d", rec.Code)
	}
}

// --- wrong method returns 405 ---

func TestWrongMethod_Returns405(t *testing.T) {
	s := newTestServer()
	rec := doRequest(t, s, http.MethodGet, "/api/v1/auth/login")

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET /api/v1/auth/login: want 405, got %d", rec.Code)
	}
}

// --- notImplemented response format ---

func TestNotImplemented_JSONFormat(t *testing.T) {
	s := newTestServer()
	rec := doRequest(t, s, http.MethodPost, "/api/v1/auth/login")

	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Fatalf("notImplemented Content-Type: want \"application/json\", got %q", ct)
	}

	var body map[string]any
	decodeJSON(t, rec, &body)

	if body["error_code"] != "NOT_IMPLEMENTED" {
		t.Fatalf("notImplemented error_code: want \"NOT_IMPLEMENTED\", got %v", body["error_code"])
	}
	if body["message"] == nil || body["message"] == "" {
		t.Fatal("notImplemented message should not be empty")
	}
}

// --- metrics endpoint ---

func TestMetrics_Returns200(t *testing.T) {
	// Test the metrics handler directly since the metrics server
	// runs on a separate port.
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	handleMetricsStub(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("/metrics: want 200, got %d", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("/metrics Content-Type: want text/plain, got %q", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "orch_placeholder") {
		t.Fatalf("/metrics: want placeholder metric, got %q", body)
	}
}

// --- Start and Shutdown integration test ---

func TestStartAndShutdown(t *testing.T) {
	s := newTestServer()

	// Bind to ephemeral ports to avoid conflicts.
	mainLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen main: %v", err)
	}
	metricsLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen metrics: %v", err)
	}

	mainAddr := mainLn.Addr().String()
	metricsAddr := metricsLn.Addr().String()

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.StartWithListeners(mainLn, metricsLn)
	}()

	// Wait briefly for servers to start.
	time.Sleep(50 * time.Millisecond)

	// Verify main server is accepting requests.
	resp, err := http.Get(fmt.Sprintf("http://%s/healthz", mainAddr))
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /healthz: want 200, got %d", resp.StatusCode)
	}

	// Verify metrics server is accepting requests.
	resp, err = http.Get(fmt.Sprintf("http://%s/metrics", metricsAddr))
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /metrics: want 200, got %d", resp.StatusCode)
	}

	// Shutdown.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	// Verify Start returned without error.
	if err := <-errCh; err != nil {
		t.Fatalf("Start returned error after shutdown: %v", err)
	}
}

// --- concurrent access ---

func TestConcurrentAccess(t *testing.T) {
	s := newTestServer()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			doRequest(t, s, http.MethodGet, "/healthz")
			doRequest(t, s, http.MethodPost, "/api/v1/auth/login")
			doRequest(t, s, http.MethodGet, "/api/v1/contracts")
		}()
	}
	wg.Wait()
}

// --- route count verification ---

func TestAllRoutesRegistered(t *testing.T) {
	s := newTestServer()

	// Verify a comprehensive set of endpoints are reachable (not 404).
	routes := []struct {
		method string
		path   string
	}{
		// Public auth
		{http.MethodPost, "/api/v1/auth/login"},
		{http.MethodPost, "/api/v1/auth/refresh"},
		{http.MethodPost, "/api/v1/auth/logout"},
		// User
		{http.MethodGet, "/api/v1/users/me"},
		// Contracts
		{http.MethodPost, "/api/v1/contracts/upload"},
		{http.MethodGet, "/api/v1/contracts"},
		{http.MethodGet, "/api/v1/contracts/c1"},
		{http.MethodDelete, "/api/v1/contracts/c1"},
		{http.MethodPost, "/api/v1/contracts/c1/archive"},
		// Versions
		{http.MethodGet, "/api/v1/contracts/c1/versions"},
		{http.MethodPost, "/api/v1/contracts/c1/versions/upload"},
		{http.MethodGet, "/api/v1/contracts/c1/versions/v1"},
		{http.MethodGet, "/api/v1/contracts/c1/versions/v1/status"},
		{http.MethodPost, "/api/v1/contracts/c1/versions/v1/recheck"},
		// Results
		{http.MethodGet, "/api/v1/contracts/c1/versions/v1/results"},
		{http.MethodGet, "/api/v1/contracts/c1/versions/v1/risks"},
		{http.MethodGet, "/api/v1/contracts/c1/versions/v1/summary"},
		{http.MethodGet, "/api/v1/contracts/c1/versions/v1/recommendations"},
		// Comparison
		{http.MethodPost, "/api/v1/contracts/c1/compare"},
		{http.MethodGet, "/api/v1/contracts/c1/versions/v1/diff/v2"},
		// Export
		{http.MethodGet, "/api/v1/contracts/c1/versions/v1/export/pdf"},
		// Feedback
		{http.MethodPost, "/api/v1/contracts/c1/versions/v1/feedback"},
		// Admin
		{http.MethodGet, "/api/v1/admin/policies"},
		{http.MethodPut, "/api/v1/admin/policies/p1"},
		{http.MethodGet, "/api/v1/admin/checklists"},
		{http.MethodPut, "/api/v1/admin/checklists/cl1"},
		// SSE
		{http.MethodGet, "/api/v1/events/stream"},
		// Health
		{http.MethodGet, "/healthz"},
		{http.MethodGet, "/readyz"},
	}

	for _, rt := range routes {
		rec := doRequest(t, s, rt.method, rt.path)
		if rec.Code == http.StatusNotFound {
			t.Errorf("%s %s: route not registered (404)", rt.method, rt.path)
		}
	}
}

// --- live server route test ---

func TestLiveServer_ContractEndpoint(t *testing.T) {
	s := newTestServer()

	mainLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	metricsLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen metrics: %v", err)
	}

	mainAddr := mainLn.Addr().String()

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.StartWithListeners(mainLn, metricsLn)
	}()

	time.Sleep(50 * time.Millisecond)

	// Test a protected endpoint via live HTTP.
	resp, err := http.Get(fmt.Sprintf("http://%s/api/v1/contracts", mainAddr))
	if err != nil {
		t.Fatalf("GET /api/v1/contracts: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("GET /api/v1/contracts: want 501, got %d (body: %s)", resp.StatusCode, body)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.Shutdown(ctx)
	<-errCh
}

// --- middleware chain verification ---

func TestMiddlewareChain_StubsPassThrough(t *testing.T) {
	// Verify that all middleware stubs are no-ops: a request to a
	// protected endpoint reaches the handler and gets 501.
	s := newTestServer()

	endpoints := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/users/me"},
		{http.MethodGet, "/api/v1/contracts"},
		{http.MethodGet, "/api/v1/admin/policies"},
	}

	for _, ep := range endpoints {
		rec := doRequest(t, s, ep.method, ep.path)
		if rec.Code != http.StatusNotImplemented {
			t.Errorf("%s %s: want 501 (middleware pass-through), got %d",
				ep.method, ep.path, rec.Code)
		}
	}
}

// --- Router accessor ---

func TestRouter_ReturnsNonNil(t *testing.T) {
	s := newTestServer()
	if s.Router() == nil {
		t.Fatal("Router() must not return nil")
	}
}

// --- helper ---

func assertJSONError(t *testing.T, rec *httptest.ResponseRecorder, wantError string) {
	t.Helper()
	var body map[string]any
	decodeJSON(t, rec, &body)
	if body["error_code"] != wantError {
		t.Fatalf("JSON error_code: want %q, got %v", wantError, body["error_code"])
	}
}

// --- compile-time interface checks ---

var _ health.RedisPinger = (*fakeRedisPinger)(nil)
var _ health.BrokerPinger = (*fakeBrokerPinger)(nil)
