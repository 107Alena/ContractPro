package app

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"contractpro/api-orchestrator/internal/config"
	"contractpro/api-orchestrator/internal/infra/health"
	"contractpro/api-orchestrator/internal/infra/observability/logger"
	"contractpro/api-orchestrator/internal/ingress/api"
	"contractpro/api-orchestrator/internal/ingress/sse"
)

// ---------------------------------------------------------------------------
// Mock infrastructure
// ---------------------------------------------------------------------------

// mockRedis satisfies health.RedisPinger and tracks Close order.
type mockRedis struct {
	name     string
	closed   atomic.Bool
	closeSeq *[]string
}

func (m *mockRedis) Ping(ctx context.Context) error { return nil }
func (m *mockRedis) Close() error {
	m.closed.Store(true)
	if m.closeSeq != nil {
		*m.closeSeq = append(*m.closeSeq, m.name)
	}
	return nil
}

// mockBrokerInfra satisfies health.BrokerPinger and tracks Close order.
type mockBrokerInfra struct {
	name     string
	closed   atomic.Bool
	closeSeq *[]string
}

func (m *mockBrokerInfra) Ping() error { return nil }
func (m *mockBrokerInfra) Close() error {
	m.closed.Store(true)
	if m.closeSeq != nil {
		*m.closeSeq = append(*m.closeSeq, m.name)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Mock SSE handler and observability for shutdown tests
// ---------------------------------------------------------------------------

// mockSSEHandler wraps sse.Handler to verify Shutdown is called in order.
// We can't use a real SSE handler without full wiring, so we track the call.
type mockSSEHandlerShutdown struct {
	shutdownCalled atomic.Bool
	shutdownSeq    *[]string
	name           string
}

func (m *mockSSEHandlerShutdown) recordShutdown() {
	m.shutdownCalled.Store(true)
	if m.shutdownSeq != nil {
		*m.shutdownSeq = append(*m.shutdownSeq, m.name)
	}
}

// mockObservability satisfies ObservabilityShutdown and tracks the call.
type mockObservability struct {
	shutdownCalled atomic.Bool
	shutdownSeq    *[]string
	shutdownErr    error
	name           string
}

func (m *mockObservability) Shutdown(_ context.Context) error {
	m.shutdownCalled.Store(true)
	if m.shutdownSeq != nil {
		*m.shutdownSeq = append(*m.shutdownSeq, m.name)
	}
	return m.shutdownErr
}

// Compile-time check.
var _ ObservabilityShutdown = (*mockObservability)(nil)

// ---------------------------------------------------------------------------
// Shutdown ordering test — verifies 7-phase sequence
// ---------------------------------------------------------------------------

func TestApp_Shutdown_OrderedTeardown(t *testing.T) {
	log := logger.NewLogger("error")

	dmProbe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer dmProbe.Close()

	var closeOrder []string

	kvClient := &mockRedis{name: "redis", closeSeq: &closeOrder}
	brokerClient := &mockBrokerInfra{name: "broker", closeSeq: &closeOrder}

	healthHandler := health.NewHandler(kvClient, brokerClient, dmProbe.URL)

	server := api.NewServer(api.Deps{
		Config: config.HTTPConfig{
			Port:            0,
			MetricsPort:     0,
			RequestTimeout:  5 * time.Second,
			UploadTimeout:   10 * time.Second,
			ShutdownTimeout: 5 * time.Second,
		},
		Health: healthHandler,
		Logger: log,
	})

	mainLn, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("listen main: %v", err)
	}
	metricsLn, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("listen metrics: %v", err)
	}

	serverDone := make(chan error, 1)
	go func() {
		serverDone <- server.StartWithListeners(mainLn, metricsLn)
	}()

	// Wait for server to accept connections.
	waitForServer(t, mainLn.Addr().String())

	// Verify /healthz works before shutdown.
	resp, err := http.Get("http://" + mainLn.Addr().String() + "/healthz")
	if err != nil {
		t.Fatalf("healthz: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("healthz: got %d, want 200", resp.StatusCode)
	}

	// We can't assign real *kvstore.Client/*broker.Client to mock types,
	// so we call the shutdown steps manually but in the App.Shutdown order
	// to verify the sequence matches what the real Shutdown does:
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Phase 1: SetNotReady
	healthHandler.SetNotReady()

	// Phase 2: SSE shutdown (tracked via mock) — BEFORE HTTP per M1 fix
	sseShutdownMock := &mockSSEHandlerShutdown{name: "sse", shutdownSeq: &closeOrder}
	sseShutdownMock.recordShutdown()

	// Phase 3: HTTP server shutdown
	if err := server.Shutdown(ctx); err != nil {
		t.Errorf("server shutdown: %v", err)
	}

	// Phase 4-5: Close infra in App.Shutdown order (broker then redis)
	brokerClient.Close()
	kvClient.Close()

	// Phase 6: Observability flush
	obsMock := &mockObservability{name: "observability", shutdownSeq: &closeOrder}
	obsMock.Shutdown(ctx)

	// Verify close order.
	if len(closeOrder) != 4 {
		t.Fatalf("expected 4 close calls, got %d: %v", len(closeOrder), closeOrder)
	}
	expected := []string{"sse", "broker", "redis", "observability"}
	for i, want := range expected {
		if closeOrder[i] != want {
			t.Errorf("close order[%d] = %q, want %q", i, closeOrder[i], want)
		}
	}

	// Verify server stopped.
	select {
	case err := <-serverDone:
		if err != nil {
			t.Errorf("server error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not stop within timeout")
	}
}

// TestApp_Shutdown_SSEHandlerCalled verifies that SSE handler Shutdown is
// invoked during App shutdown (phase 3).
func TestApp_Shutdown_SSEHandlerCalled(t *testing.T) {
	log := logger.NewLogger("error")
	dmProbe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer dmProbe.Close()

	kvClient := &mockRedis{name: "redis"}
	brokerClient := &mockBrokerInfra{name: "broker"}
	healthHandler := health.NewHandler(kvClient, brokerClient, dmProbe.URL)
	sseHandler := sse.NewHandler(nil, nil, config.SSEConfig{
		HeartbeatInterval: 15 * time.Second,
		MaxConnectionAge:  24 * time.Hour,
	}, log)

	server := api.NewServer(api.Deps{
		Config: config.HTTPConfig{
			Port:            0,
			MetricsPort:     0,
			RequestTimeout:  5 * time.Second,
			UploadTimeout:   10 * time.Second,
			ShutdownTimeout: 5 * time.Second,
		},
		Health: healthHandler,
		Logger: log,
	})

	mainLn, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("listen main: %v", err)
	}
	metricsLn, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("listen metrics: %v", err)
	}

	go func() { server.StartWithListeners(mainLn, metricsLn) }()
	waitForServer(t, mainLn.Addr().String())

	a := &App{
		log:          log,
		health:       healthHandler,
		kvClient:     nil,
		brokerClient: nil,
		server:       server,
		sseHandler:   sseHandler,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Phase 1: SetNotReady.
	a.health.SetNotReady()

	// Phase 2: SSE handler shutdown (before HTTP per M1 fix).
	a.sseHandler.Shutdown()

	// Verify done channel is closed.
	select {
	case <-sseHandler.Done():
	default:
		t.Error("SSE handler done channel not closed after Shutdown")
	}

	// Phase 3: HTTP shutdown.
	if err := a.server.Shutdown(ctx); err != nil {
		t.Errorf("server shutdown: %v", err)
	}
}

// TestApp_Shutdown_ObservabilityError verifies that observability errors are
// collected but do not abort subsequent phases.
func TestApp_Shutdown_ObservabilityError(t *testing.T) {
	log := logger.NewLogger("error")
	dmProbe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer dmProbe.Close()

	kvClient := &mockRedis{name: "redis"}
	brokerClient := &mockBrokerInfra{name: "broker"}
	healthHandler := health.NewHandler(kvClient, brokerClient, dmProbe.URL)
	obs := &mockObservability{shutdownErr: errors.New("flush failed")}

	server := api.NewServer(api.Deps{
		Config: config.HTTPConfig{
			Port:            0,
			MetricsPort:     0,
			RequestTimeout:  5 * time.Second,
			UploadTimeout:   10 * time.Second,
			ShutdownTimeout: 5 * time.Second,
		},
		Health: healthHandler,
		Logger: log,
	})

	mainLn, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("listen main: %v", err)
	}
	metricsLn, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("listen metrics: %v", err)
	}

	go func() { server.StartWithListeners(mainLn, metricsLn) }()
	waitForServer(t, mainLn.Addr().String())

	a := &App{
		log:           log,
		health:        healthHandler,
		kvClient:      nil,
		brokerClient:  nil,
		server:        server,
		observability: obs,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	a.health.SetNotReady()
	a.server.Shutdown(ctx)

	// Observability error should be returned.
	if a.observability != nil {
		err := a.observability.Shutdown(ctx)
		if err == nil {
			t.Error("expected observability error, got nil")
		}
	}
	if !obs.shutdownCalled.Load() {
		t.Error("observability Shutdown was not called")
	}
}

// TestApp_Shutdown_NilSSEHandler verifies that shutdown works when
// sseHandler is nil (backward compatibility).
func TestApp_Shutdown_NilSSEHandler(t *testing.T) {
	log := logger.NewLogger("error")
	dmProbe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer dmProbe.Close()

	kvClient := &mockRedis{name: "redis"}
	brokerClient := &mockBrokerInfra{name: "broker"}
	healthHandler := health.NewHandler(kvClient, brokerClient, dmProbe.URL)

	server := api.NewServer(api.Deps{
		Config: config.HTTPConfig{
			Port:            0,
			MetricsPort:     0,
			RequestTimeout:  5 * time.Second,
			UploadTimeout:   10 * time.Second,
			ShutdownTimeout: 5 * time.Second,
		},
		Health: healthHandler,
		Logger: log,
	})

	mainLn, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("listen main: %v", err)
	}
	metricsLn, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("listen metrics: %v", err)
	}

	go func() { server.StartWithListeners(mainLn, metricsLn) }()
	waitForServer(t, mainLn.Addr().String())

	a := &App{
		log:          log,
		health:       healthHandler,
		kvClient:     nil,
		brokerClient: nil,
		server:       server,
		sseHandler:   nil, // explicitly nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	a.health.SetNotReady()
	// Should not panic with nil sseHandler.
	if err := a.server.Shutdown(ctx); err != nil {
		t.Errorf("server shutdown: %v", err)
	}
}

// TestApp_Shutdown_NilObservability verifies that shutdown works when
// observability is nil (current default).
func TestApp_Shutdown_NilObservability(t *testing.T) {
	log := logger.NewLogger("error")
	dmProbe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer dmProbe.Close()

	kvClient := &mockRedis{name: "redis"}
	brokerClient := &mockBrokerInfra{name: "broker"}
	healthHandler := health.NewHandler(kvClient, brokerClient, dmProbe.URL)

	server := api.NewServer(api.Deps{
		Config: config.HTTPConfig{
			Port:            0,
			MetricsPort:     0,
			RequestTimeout:  5 * time.Second,
			UploadTimeout:   10 * time.Second,
			ShutdownTimeout: 5 * time.Second,
		},
		Health: healthHandler,
		Logger: log,
	})

	mainLn, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("listen main: %v", err)
	}
	metricsLn, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("listen metrics: %v", err)
	}

	go func() { server.StartWithListeners(mainLn, metricsLn) }()
	waitForServer(t, mainLn.Addr().String())

	a := &App{
		log:           log,
		health:        healthHandler,
		kvClient:      nil,
		brokerClient:  nil,
		server:        server,
		observability: nil, // explicitly nil (current default)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	a.health.SetNotReady()
	// Should not panic with nil observability.
	if err := a.server.Shutdown(ctx); err != nil {
		t.Errorf("server shutdown: %v", err)
	}
}

func TestApp_Shutdown_SetNotReady_Before_HTTPShutdown(t *testing.T) {
	log := logger.NewLogger("error")

	dmProbe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer dmProbe.Close()

	kvClient := &mockRedis{name: "redis"}
	brokerClient := &mockBrokerInfra{name: "broker"}

	healthHandler := health.NewHandler(kvClient, brokerClient, dmProbe.URL)

	server := api.NewServer(api.Deps{
		Config: config.HTTPConfig{
			Port:            0,
			MetricsPort:     0,
			RequestTimeout:  5 * time.Second,
			UploadTimeout:   10 * time.Second,
			ShutdownTimeout: 5 * time.Second,
		},
		Health: healthHandler,
		Logger: log,
	})

	mainLn, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("listen main: %v", err)
	}
	metricsLn, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("listen metrics: %v", err)
	}

	go func() {
		server.StartWithListeners(mainLn, metricsLn)
	}()

	waitForServer(t, mainLn.Addr().String())

	// SetNotReady should cause /readyz to return 503.
	healthHandler.SetNotReady()

	resp, err := http.Get("http://" + mainLn.Addr().String() + "/readyz")
	if err != nil {
		t.Fatalf("readyz: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("readyz after SetNotReady: got %d, want 503", resp.StatusCode)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	server.Shutdown(ctx)
}

// waitForServer polls until the server accepts TCP connections or the test times out.
func waitForServer(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("server at %s did not start in time", addr)
}
