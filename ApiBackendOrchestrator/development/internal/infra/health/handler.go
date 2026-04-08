package health

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// readinessTimeout is the maximum time allowed for all readiness checks
// to complete before the probe returns 503.
const readinessTimeout = 3 * time.Second

// dmHTTPTimeout is the timeout for the HTTP client used to probe the DM
// service health endpoint. Kept shorter than readinessTimeout so the
// context-based deadline remains the dominant bound.
const dmHTTPTimeout = 2 * time.Second

// RedisPinger checks Redis connectivity. Satisfied by kvstore.Client.Ping.
type RedisPinger interface {
	Ping(ctx context.Context) error
}

// BrokerPinger checks RabbitMQ connectivity. Satisfied by broker.Client.Ping.
type BrokerPinger interface {
	Ping() error
}

// checkResult holds the outcome of a single readiness sub-check.
type checkResult struct {
	name   string
	status string // "ok" or "error: <message>"
}

// livenessResponse is the JSON body for GET /healthz.
type livenessResponse struct {
	Status string `json:"status"`
}

// readinessResponse is the JSON body for GET /readyz.
type readinessResponse struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

// Handler serves HTTP health and readiness probe endpoints.
//
//   - GET /healthz -- liveness probe; always returns 200 OK.
//   - GET /readyz  -- readiness probe; checks Redis, RabbitMQ, and DM
//     service health in parallel with a 3-second timeout.
//
// Call SetNotReady during graceful shutdown to force /readyz to return
// 503 immediately without running any checks.
type Handler struct {
	notReady atomic.Bool
	mux      *http.ServeMux

	redis    RedisPinger
	broker   BrokerPinger
	dmURL    string       // full URL: dmBaseURL + "/healthz"
	dmClient *http.Client // dedicated HTTP client for DM probes
}

// NewHandler creates a Handler wired to the given dependencies.
// dmBaseURL is the base URL of the Document Management service
// (e.g. "http://dm-service:8080"); the handler appends "/healthz".
//
// The handler starts in a ready state. Call SetNotReady to transition
// to not-ready during graceful shutdown.
func NewHandler(redis RedisPinger, broker BrokerPinger, dmBaseURL string) *Handler {
	h := &Handler{
		redis:  redis,
		broker: broker,
		dmURL:  strings.TrimRight(dmBaseURL, "/") + "/healthz",
		dmClient: &http.Client{
			Timeout: dmHTTPTimeout,
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.handleLiveness)
	mux.HandleFunc("GET /readyz", h.handleReadiness)
	h.mux = mux

	return h
}

// SetNotReady forces /readyz to return 503 immediately without running
// dependency checks. Intended for graceful shutdown. Thread-safe.
func (h *Handler) SetNotReady() { h.notReady.Store(true) }

// Mux returns the http.ServeMux for use with http.Server.
func (h *Handler) Mux() *http.ServeMux { return h.mux }

// handleLiveness always returns 200 OK. The process is alive if it can
// serve this response.
func (h *Handler) handleLiveness(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, livenessResponse{Status: "ok"})
}

// handleReadiness checks all dependencies in parallel. Returns 200 if all
// healthy, 503 if any fail or if SetNotReady has been called.
func (h *Handler) handleReadiness(w http.ResponseWriter, r *http.Request) {
	if h.notReady.Load() {
		writeJSON(w, http.StatusServiceUnavailable, readinessResponse{
			Status: "not_ready",
			Checks: map[string]string{},
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), readinessTimeout)
	defer cancel()

	checks := h.runChecks(ctx)

	allOK := true
	checksMap := make(map[string]string, len(checks))
	for _, c := range checks {
		checksMap[c.name] = c.status
		if c.status != "ok" {
			allOK = false
		}
	}

	if allOK {
		writeJSON(w, http.StatusOK, readinessResponse{
			Status: "ready",
			Checks: checksMap,
		})
		return
	}

	writeJSON(w, http.StatusServiceUnavailable, readinessResponse{
		Status: "not_ready",
		Checks: checksMap,
	})
}

// runChecks executes all dependency checks concurrently and collects
// results. Each check respects the parent context deadline.
func (h *Handler) runChecks(ctx context.Context) []checkResult {
	results := make([]checkResult, 3)

	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		results[0] = h.checkRedis(ctx)
	}()

	go func() {
		defer wg.Done()
		results[1] = h.checkBroker()
	}()

	go func() {
		defer wg.Done()
		results[2] = h.checkDM(ctx)
	}()

	wg.Wait()
	return results
}

// checkRedis pings Redis and returns the result.
func (h *Handler) checkRedis(ctx context.Context) checkResult {
	if err := h.redis.Ping(ctx); err != nil {
		return checkResult{name: "redis", status: "unavailable"}
	}
	return checkResult{name: "redis", status: "ok"}
}

// checkBroker pings RabbitMQ and returns the result.
func (h *Handler) checkBroker() checkResult {
	if err := h.broker.Ping(); err != nil {
		return checkResult{name: "rabbitmq", status: "unavailable"}
	}
	return checkResult{name: "rabbitmq", status: "ok"}
}

// checkDM performs an HTTP GET to the DM service /healthz endpoint.
// A non-200 status code is treated as unhealthy.
func (h *Handler) checkDM(ctx context.Context) checkResult {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.dmURL, nil)
	if err != nil {
		return checkResult{name: "dm", status: "unavailable"}
	}

	resp, err := h.dmClient.Do(req)
	if err != nil {
		return checkResult{name: "dm", status: "unavailable"}
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return checkResult{name: "dm", status: "unavailable"}
	}
	return checkResult{name: "dm", status: "ok"}
}

// writeJSON encodes v as JSON and writes it with the given HTTP status code.
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
