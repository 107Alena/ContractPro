package health

import (
	"encoding/json"
	"net/http"
	"sync/atomic"
)

// Handler serves HTTP health and readiness probe endpoints.
//
//   - GET /healthz — liveness probe; always returns 200 OK when the process is alive.
//   - GET /readyz  — readiness probe; returns 200 OK only after the service has
//     started consuming messages and returns 503 during startup or shutdown.
type Handler struct {
	ready atomic.Bool
	mux   *http.ServeMux
}

// NewHandler creates a Handler with /healthz and /readyz routes.
// The handler starts in a not-ready state; call SetReady(true) after
// subscriptions have been established.
func NewHandler() *Handler {
	h := &Handler{}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", h.handleLiveness)
	mux.HandleFunc("/readyz", h.handleReadiness)
	h.mux = mux
	return h
}

// SetReady toggles the readiness flag. Call with true after all consumers
// have started, and with false at the beginning of graceful shutdown.
func (h *Handler) SetReady(v bool) { h.ready.Store(v) }

// Mux returns the http.ServeMux for use with http.Server.
func (h *Handler) Mux() *http.ServeMux { return h.mux }

func (h *Handler) handleLiveness(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) handleReadiness(w http.ResponseWriter, _ *http.Request) {
	if h.ready.Load() {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
		return
	}
	writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready"})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
