package health

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

const defaultCheckTimeout = 5 * time.Second

// ComponentChecker checks the health of an infrastructure component.
// Return nil for healthy, non-nil error for unhealthy.
type ComponentChecker func(ctx context.Context) error

// Handler serves HTTP health and readiness probe endpoints.
//
//   - GET /healthz — liveness probe; always returns 200 OK when the process is alive.
//   - GET /readyz  — readiness probe; checks core components (PostgreSQL, Redis,
//     RabbitMQ) and reports Object Storage separately. Core failure → 503.
//     Object Storage failure does not block readiness (REV-024).
//
// The handler starts in a not-ready state; call SetReady(true) after all
// subscriptions have been established.
type Handler struct {
	ready   atomic.Bool
	mux     *http.ServeMux
	core    map[string]ComponentChecker
	nonCore map[string]ComponentChecker
	timeout time.Duration
}

// HandlerOption configures the health Handler.
type HandlerOption func(*Handler)

// WithCheckTimeout sets the per-component health check timeout.
// Values ≤ 0 are ignored.
func WithCheckTimeout(d time.Duration) HandlerOption {
	return func(h *Handler) {
		if d > 0 {
			h.timeout = d
		}
	}
}

// NewHandler creates a Handler with /healthz and /readyz routes.
//
// coreCheckers block readiness: if any fails, /readyz returns 503.
// nonCoreCheckers are reported in the response but do not block readiness (REV-024).
//
// Both maps may be nil or empty. Panics if a component name appears in both maps.
func NewHandler(coreCheckers map[string]ComponentChecker, nonCoreCheckers map[string]ComponentChecker, opts ...HandlerOption) *Handler {
	// Validate: no name collision between core and non-core.
	for name := range coreCheckers {
		if _, exists := nonCoreCheckers[name]; exists {
			panic(fmt.Sprintf("health: component %q registered in both core and non-core checkers", name))
		}
	}

	h := &Handler{
		core:    coreCheckers,
		nonCore: nonCoreCheckers,
		timeout: defaultCheckTimeout,
	}
	for _, opt := range opts {
		opt(h)
	}
	if h.core == nil {
		h.core = make(map[string]ComponentChecker)
	}
	if h.nonCore == nil {
		h.nonCore = make(map[string]ComponentChecker)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.handleLiveness)
	mux.HandleFunc("GET /readyz", h.handleReadiness)
	h.mux = mux
	return h
}

// SetReady toggles the readiness flag. Call with true after all consumers
// have started, and with false at the beginning of graceful shutdown.
func (h *Handler) SetReady(v bool) { h.ready.Store(v) }

// Mux returns the http.ServeMux for use with http.Server.
func (h *Handler) Mux() *http.ServeMux { return h.mux }

// ComponentStatus represents the health status of a single component.
type ComponentStatus struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// ReadinessResponse is the JSON response body for /readyz.
type ReadinessResponse struct {
	Status     string                     `json:"status"`
	Components map[string]ComponentStatus `json:"components"`
}

func (h *Handler) handleLiveness(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) handleReadiness(w http.ResponseWriter, r *http.Request) {
	resp := ReadinessResponse{
		Status:     "ready",
		Components: make(map[string]ComponentStatus),
	}

	if !h.ready.Load() {
		resp.Status = "not_ready"
		writeJSON(w, http.StatusServiceUnavailable, resp)
		return
	}

	ctx := r.Context()

	// Check core components (block readiness on failure).
	coreResults := h.checkAll(ctx, h.core)
	coreHealthy := true
	for _, res := range coreResults {
		cs := ComponentStatus{Status: "up"}
		if res.err != nil {
			cs.Status = "down"
			cs.Error = res.err.Error()
			coreHealthy = false
		}
		resp.Components[res.name] = cs
	}

	// Check non-core components (report only, do not block readiness — REV-024).
	nonCoreResults := h.checkAll(ctx, h.nonCore)
	for _, res := range nonCoreResults {
		cs := ComponentStatus{Status: "up"}
		if res.err != nil {
			cs.Status = "down"
			cs.Error = res.err.Error()
		}
		resp.Components[res.name] = cs
	}

	if !coreHealthy {
		resp.Status = "not_ready"
		writeJSON(w, http.StatusServiceUnavailable, resp)
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// componentResult holds the health check result for a single component.
type componentResult struct {
	name string
	err  error
}

// checkAll runs all checkers concurrently with a per-component timeout.
// Results are returned in deterministic (sorted by name) order.
// If a checker panics, the panic is recovered and reported as an error.
func (h *Handler) checkAll(ctx context.Context, checkers map[string]ComponentChecker) []componentResult {
	results := make([]componentResult, 0, len(checkers))
	if len(checkers) == 0 {
		return results
	}

	var mu sync.Mutex
	var wg sync.WaitGroup

	for name, checker := range checkers {
		wg.Add(1)
		go func(n string, chk ComponentChecker) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					mu.Lock()
					results = append(results, componentResult{name: n, err: fmt.Errorf("panic: %v", r)})
					mu.Unlock()
				}
			}()
			checkCtx, cancel := context.WithTimeout(ctx, h.timeout)
			defer cancel()
			err := chk(checkCtx)
			mu.Lock()
			results = append(results, componentResult{name: n, err: err})
			mu.Unlock()
		}(name, checker)
	}

	wg.Wait()

	// Sort results by name for deterministic JSON output.
	sort.Slice(results, func(i, j int) bool {
		return results[i].name < results[j].name
	})

	return results
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
