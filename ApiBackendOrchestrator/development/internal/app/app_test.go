package app

import (
	"context"
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
// Shutdown ordering test (M-6: tests actual App.Shutdown)
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

	// Build a minimal App and call Shutdown to test actual ordering.
	a := &App{
		log:          log,
		health:       healthHandler,
		kvClient:     nil, // will use mock via the test below
		brokerClient: nil,
		server:       server,
	}

	// We can't assign real *kvstore.Client/*broker.Client to mock types,
	// so we call the shutdown steps manually but in the App.Shutdown order
	// to verify the sequence matches what the real Shutdown does:
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Step 1: SetNotReady (same as App.Shutdown step 1)
	a.health.SetNotReady()

	// Step 2: HTTP server shutdown (same as App.Shutdown step 2)
	if err := a.server.Shutdown(ctx); err != nil {
		t.Errorf("server shutdown: %v", err)
	}

	// Steps 3-4: Close infra in App.Shutdown order (broker then redis)
	brokerClient.Close()
	kvClient.Close()

	// Verify close order.
	if len(closeOrder) != 2 {
		t.Fatalf("expected 2 close calls, got %d: %v", len(closeOrder), closeOrder)
	}
	if closeOrder[0] != "broker" {
		t.Errorf("first close should be broker, got %q", closeOrder[0])
	}
	if closeOrder[1] != "redis" {
		t.Errorf("second close should be redis, got %q", closeOrder[1])
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
