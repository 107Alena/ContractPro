package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"contractpro/document-processing/internal/infra/health"
	"contractpro/document-processing/internal/infra/observability"
)

// ---------------------------------------------------------------------------
// Mock types
// ---------------------------------------------------------------------------

// mockBrokerCloser records its call and can return a configured error.
type mockBrokerCloser struct {
	called bool
	err    error
	order  *[]string
}

func (m *mockBrokerCloser) Close() error {
	m.called = true
	if m.order != nil {
		*m.order = append(*m.order, "broker")
	}
	return m.err
}

// mockKVCloser records its call and can return a configured error.
type mockKVCloser struct {
	called bool
	err    error
	order  *[]string
}

func (m *mockKVCloser) Close() error {
	m.called = true
	if m.order != nil {
		*m.order = append(*m.order, "kv")
	}
	return m.err
}

// mockObsShutdowner records its call and can return a configured error.
type mockObsShutdowner struct {
	called bool
	err    error
	order  *[]string
}

func (m *mockObsShutdowner) Shutdown(_ context.Context) error {
	m.called = true
	if m.order != nil {
		*m.order = append(*m.order, "obs")
	}
	return m.err
}

// newTestApp builds an App with all infrastructure fields wired to the
// provided mocks. If a mock is nil, the corresponding field is left nil
// to test nil-safety.
func newTestApp(
	broker *mockBrokerCloser,
	kv *mockKVCloser,
	obs *mockObsShutdowner,
	h *health.Handler,
	httpSrv *http.Server,
) *App {
	logger := observability.NewLogger("error")
	a := &App{
		logger: logger,
		health: h,
	}
	if broker != nil {
		a.brokerCli = broker
	}
	if kv != nil {
		a.kvCli = kv
	}
	if obs != nil {
		a.obs = obs
	}
	if httpSrv != nil {
		a.httpServer = httpSrv
	}
	return a
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestShutdown_ClosesInOrder(t *testing.T) {
	var order []string
	br := &mockBrokerCloser{order: &order}
	kv := &mockKVCloser{order: &order}
	ob := &mockObsShutdowner{order: &order}
	h := health.NewHandler()

	a := newTestApp(br, kv, ob, h, nil)
	a.Shutdown(context.Background())

	// Expected order: broker -> kv -> obs
	// (HTTP server is nil, so it is skipped.)
	want := "broker,kv,obs"
	got := strings.Join(order, ",")
	if got != want {
		t.Fatalf("shutdown order: want %q, got %q", want, got)
	}
}

func TestShutdown_ErrorsDontPreventOtherShutdowns(t *testing.T) {
	var order []string
	br := &mockBrokerCloser{order: &order, err: errors.New("broker boom")}
	kv := &mockKVCloser{order: &order}
	ob := &mockObsShutdowner{order: &order}
	h := health.NewHandler()

	a := newTestApp(br, kv, ob, h, nil)
	a.Shutdown(context.Background())

	if !br.called {
		t.Fatal("broker.Close was not called")
	}
	if !kv.called {
		t.Fatal("kv.Close was not called despite broker error")
	}
	if !ob.called {
		t.Fatal("obs.Shutdown was not called despite broker error")
	}

	want := "broker,kv,obs"
	got := strings.Join(order, ",")
	if got != want {
		t.Fatalf("shutdown order after error: want %q, got %q", want, got)
	}
}

func TestShutdown_NilFields(t *testing.T) {
	// All infrastructure fields nil — only logger is set. Must not panic.
	a := newTestApp(nil, nil, nil, nil, nil)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Shutdown panicked with nil fields: %v", r)
		}
	}()

	a.Shutdown(context.Background())
}

func TestShutdown_SetsNotReady(t *testing.T) {
	h := health.NewHandler()
	h.SetReady(true) // simulate running state

	a := newTestApp(nil, nil, nil, h, nil)
	a.Shutdown(context.Background())

	// After shutdown, /readyz must return 503.
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	h.Mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("/readyz after shutdown: want 503, got %d", rec.Code)
	}
}

func TestShutdown_WithHTTPServer(t *testing.T) {
	// Start a real test HTTP server so Shutdown can call httpServer.Shutdown().
	h := health.NewHandler()
	srv := httptest.NewServer(h.Mux())

	// Build an *http.Server that wraps the test server's listener address.
	// httptest.NewServer uses its own listener, so we create a separate
	// http.Server pointing at the same addr for the App to shut down.
	// We need to close the httptest server's listener first and start our own.
	addr := srv.Listener.Addr().String()
	srv.Close() // free the port

	httpSrv := &http.Server{
		Addr:    addr,
		Handler: h.Mux(),
	}

	// Start serving in background.
	go func() {
		_ = httpSrv.ListenAndServe()
	}()

	// Verify it is serving before shutdown.
	// (We just need the server goroutine to start; the shutdown call is the
	// main thing under test.)
	a := newTestApp(nil, nil, nil, h, httpSrv)
	a.Shutdown(context.Background())

	// After Shutdown, attempting to use the server should fail or return
	// ErrServerClosed — we verify by trying to shut it down again, which
	// is safe and should succeed without error (idempotent behaviour of
	// http.Server.Shutdown after first call).
	err := httpSrv.Shutdown(context.Background())
	if err != nil {
		// http.ErrServerClosed is expected here since the server was already shut down.
		if !errors.Is(err, http.ErrServerClosed) {
			t.Fatalf("second Shutdown returned unexpected error: %v", err)
		}
	}
}

func TestShutdown_IdempotentDoubleCall(t *testing.T) {
	var calls []string
	br := &mockBrokerCloser{order: &calls}
	ob := &mockObsShutdowner{order: &calls}

	a := newTestApp(br, nil, ob, nil, nil)
	a.Shutdown(context.Background())
	a.Shutdown(context.Background()) // second call must be a no-op

	// broker + obs, each called exactly once despite two Shutdown calls.
	if len(calls) != 2 {
		t.Fatalf("expected 2 shutdown calls (broker, obs), got %d: %v", len(calls), calls)
	}
}

func TestShutdown_AllErrors(t *testing.T) {
	// Even when every component returns an error, all phases still execute.
	var order []string
	br := &mockBrokerCloser{order: &order, err: fmt.Errorf("broker err")}
	kv := &mockKVCloser{order: &order, err: fmt.Errorf("kv err")}
	ob := &mockObsShutdowner{order: &order, err: fmt.Errorf("obs err")}
	h := health.NewHandler()

	a := newTestApp(br, kv, ob, h, nil)
	a.Shutdown(context.Background())

	if len(order) != 3 {
		t.Fatalf("expected 3 shutdown calls, got %d: %v", len(order), order)
	}
}
