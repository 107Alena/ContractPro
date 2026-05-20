// Package health provides HTTP probe endpoints for the Legal Intelligence Core
// service: GET /healthz (liveness), GET /readyz (readiness), and a forwarded
// GET /metrics. It is HERMETIC: stdlib only. Dependency checks (Redis,
// RabbitMQ, LLM providers, ...) are inverted behind the minimal Checker seam
// so the package never imports infra/broker, infra/kvstore, llm/router, or
// prometheus/promhttp. Concrete adapters live in app-wiring (LIC-TASK-047).
//
// The Handler starts in a READY state and exposes exactly one transition —
// SetNotReady() — implemented as a sticky-once atomic flip. This deliberately
// diverges from DocumentProcessing's toggle SetReady(bool): in LIC there is
// no path back to ready, which structurally guarantees that a Kubernetes
// shutdown sequence cannot accidentally revert mid-drain (architect D5).
//
// Readiness is wait-all with two layers of timeouts:
//
//   - per-checker: WithCheckerTimeout("redis", 100ms) overrides
//     WithDefaultCheckerTimeout (1s default). The handler wraps the request
//     context with this deadline and passes the wrapped ctx to Check; the
//     checker MUST honour ctx.
//   - request-level: WithReadyDeadline (2s default) caps the total /readyz
//     latency. Per-checker contexts are nested inside the request context,
//     so a request-deadline timeout cancels every in-flight Check.
//
// All goroutines write into an index-stable results slice (one slot per
// checker) — no shared mutable state on the hot path; -race clean.
package health

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// Checker is the dependency-probe seam. Implementations are constructed in
// app-wiring (LIC-TASK-047) — typical concretes are redis/rabbitmq/llm-router
// adapters; the health package never imports their packages.
//
// Name MUST return a stable, lowercase identifier (e.g. "redis", "rabbitmq",
// "llm-router"). It is used as the JSON "name" field, as the lookup key for
// WithCheckerTimeout, and to detect duplicate-registration bugs at
// NewHandler-time (a duplicate name is a wiring bug — see D2).
//
// Check MUST be safe for concurrent calls (the handler invokes every
// checker's Check in its own goroutine on every /readyz hit). It MUST honour
// the provided ctx (return promptly when ctx.Done() fires). The returned
// error semantics are:
//
//   - nil                            → status "ok"
//   - errors.Is(err, context.DeadlineExceeded) → status "timeout"
//   - any other non-nil err          → status "failed" (err.Error() into "error")
//
// Check MUST NOT measure or return latency — the health package times every
// invocation externally via time.Since(start) (architect MF-6).
//
// A panicking Check is treated as a failed check (the panic value is
// formatted into the "error" field) rather than allowed to crash the
// service: a readiness probe whose entire job is detecting dependency
// failure must not be a vector for killing the pod (code-reviewer M3).
// Checker authors should nevertheless treat panics as bugs to fix at the
// source — the recovery is a safety net, not a contract.
type Checker interface {
	Name() string
	Check(ctx context.Context) error
}

// Option customizes a Handler at construction. The variadic form keeps the
// acceptance signature NewHandler(checkers, metricsHandler) a valid call.
type Option func(*Handler)

// WithDefaultCheckerTimeout overrides the 1s default per-checker timeout.
// Applies to every Checker that does not have a per-name override registered
// via WithCheckerTimeout. A value <= 0 is rejected at NewHandler-time (panic).
func WithDefaultCheckerTimeout(d time.Duration) Option {
	return func(h *Handler) { h.defaultCheckerTimeout = d }
}

// WithCheckerTimeout registers a per-name timeout override. The name MUST
// match a registered Checker.Name() exactly. Example: WithCheckerTimeout(
// "redis", 100*time.Millisecond) realises high-architecture §10.2's "Redis
// SHOULD respond in under 100ms" budget without bleeding that constant into
// the rest of the suite.
func WithCheckerTimeout(name string, d time.Duration) Option {
	return func(h *Handler) {
		if h.perCheckerTimeout == nil {
			h.perCheckerTimeout = make(map[string]time.Duration)
		}
		h.perCheckerTimeout[name] = d
	}
}

// WithReadyDeadline overrides the 2s default request-level deadline that
// caps total /readyz latency across all checkers. It MUST be >= the largest
// per-checker timeout, otherwise checkers can never finish in time and
// /readyz becomes a fixed-latency timeout — caught fail-fast at NewHandler
// (panic; see MF-3).
func WithReadyDeadline(d time.Duration) Option {
	return func(h *Handler) { h.readyDeadline = d }
}

// Handler serves /healthz, /readyz and /metrics. Construct it via NewHandler;
// the zero value is not usable. After construction every field is immutable
// EXCEPT notReady, which transitions ready → not_ready exactly once via the
// sticky-once SetNotReady().
type Handler struct {
	checkers              []Checker
	perCheckerTimeout     map[string]time.Duration
	defaultCheckerTimeout time.Duration
	readyDeadline         time.Duration
	metricsHandler        http.Handler
	notReady              atomic.Bool // false = ready (default); flips to true sticky on SetNotReady
	mux                   *http.ServeMux
}

// NewHandler builds a Handler wired with the given checkers and a metrics
// forward handler. The metrics handler is injected (not imported) so this
// package stays Prometheus-free (architect D6); typical wiring passes
// promhttp.HandlerFor(registry, ...) from observability/metrics.
//
// PANICS at construction on any of:
//
//   - any Checker has an empty Name() (MF-2)
//   - two checkers share the same Name() (MF-2)
//   - readyDeadline < largest per-checker timeout (MF-3)
//
// These are wiring bugs and there is no operator input that can produce them,
// so panic is the right "no silent degradation" discipline (the
// concurrency.New / aggregator.NewAggregator precedent).
func NewHandler(checkers []Checker, metricsHandler http.Handler, opts ...Option) *Handler {
	h := &Handler{
		checkers:              checkers,
		metricsHandler:        metricsHandler,
		defaultCheckerTimeout: 1 * time.Second,
		readyDeadline:         2 * time.Second,
	}
	for _, opt := range opts {
		opt(h)
	}

	// MF-2 — validate Name() invariants before computing the per-checker
	// timeout view (the validator needs unique non-empty names to map
	// timeouts deterministically).
	seen := make(map[string]struct{}, len(h.checkers))
	for _, c := range h.checkers {
		name := c.Name()
		if name == "" {
			panic("health.NewHandler: Checker.Name() must be non-empty")
		}
		if _, dup := seen[name]; dup {
			panic("health.NewHandler: duplicate Checker.Name(): " + name)
		}
		seen[name] = struct{}{}
	}

	// Validate defaultCheckerTimeout BEFORE the max-loop so a zero/negative
	// default never participates in the readyDeadline comparison
	// (code-reviewer M1).
	if h.defaultCheckerTimeout <= 0 {
		panic("health.NewHandler: defaultCheckerTimeout must be > 0")
	}

	// MF-3 — readyDeadline must be >= every effective per-checker timeout.
	// Compute the max across (default, per-name overrides) and compare.
	// Per-name overrides for unknown names are tolerated (a stale option is
	// harmless and lets wiring stay declarative across rolling changes), but
	// they still participate in the readyDeadline guard so a misconfigured
	// huge override is caught.
	maxCheckerTimeout := h.defaultCheckerTimeout
	for _, d := range h.perCheckerTimeout {
		if d > maxCheckerTimeout {
			maxCheckerTimeout = d
		}
	}
	if h.readyDeadline < maxCheckerTimeout {
		panic("health.NewHandler: readyDeadline must be >= max per-checker timeout")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", h.handleLiveness)
	mux.HandleFunc("/readyz", h.handleReadiness)
	// D6 — /metrics is a forward to an injected http.Handler; no promhttp
	// import. A nil metricsHandler is tolerated by simply not registering
	// the route (lets unit tests construct a Handler without a registry).
	if h.metricsHandler != nil {
		mux.Handle("/metrics", h.metricsHandler)
	}
	h.mux = mux
	return h
}

// Mux returns the http.ServeMux to mount in the app's HTTP server. The
// HTTP server itself (with shutdown, timeouts, TLS) is built by app-wiring
// (LIC-TASK-047) — this package owns only the routes (architect D7).
func (h *Handler) Mux() *http.ServeMux { return h.mux }

// SetNotReady flips the handler into the not_ready state. STICKY-ONCE:
// subsequent calls are no-ops, and there is no path back to ready (architect
// D5; MF-4). Safe for concurrent use.
//
// Typical wiring (LIC-TASK-047) calls SetNotReady from the first signal of
// graceful shutdown (SIGTERM handler) so /readyz starts failing immediately
// while the broker drains in-flight jobs.
func (h *Handler) SetNotReady() { h.notReady.Store(true) }

func (h *Handler) handleLiveness(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, livenessResponse{Status: "ok"})
}

// checkResult is the per-checker fan-out result. Captured into an
// index-stable []checkResult so each goroutine writes its own slot — no
// shared mutable state, no mutex.
type checkResult struct {
	name    string
	err     error
	latency time.Duration
}

func (h *Handler) handleReadiness(w http.ResponseWriter, r *http.Request) {
	if h.notReady.Load() {
		writeJSON(w, http.StatusServiceUnavailable, readyResponse{
			Status: "not_ready",
			Reason: "shutting_down",
			Checks: []checkJSON{},
		})
		return
	}

	reqCtx, cancel := context.WithTimeout(r.Context(), h.readyDeadline)
	defer cancel()

	results := make([]checkResult, len(h.checkers))
	var wg sync.WaitGroup
	wg.Add(len(h.checkers))
	for i := range h.checkers {
		go func(i int) {
			defer wg.Done()
			c := h.checkers[i]
			ctx, ccancel := context.WithTimeout(reqCtx, h.timeoutFor(c.Name()))
			defer ccancel()
			start := time.Now()
			// A panicking Checker must NOT crash the lic-service process —
			// the entire purpose of /readyz is to detect dependency failure,
			// and a buggy adapter killing the pod is the opposite of that
			// signal (code-reviewer M3). Convert the panic into a "failed"
			// check whose error carries the panic value, so the operator
			// sees the bug in the response body instead of in a crash log.
			defer func() {
				if rv := recover(); rv != nil {
					results[i] = checkResult{
						name:    c.Name(),
						err:     fmt.Errorf("checker panic: %v", rv),
						latency: time.Since(start),
					}
				}
			}()
			err := c.Check(ctx)
			results[i] = checkResult{
				name:    c.Name(),
				err:     err,
				latency: time.Since(start),
			}
		}(i)
	}
	wg.Wait()

	checks := make([]checkJSON, len(results))
	overallOK := true
	for i, r := range results {
		cj := checkJSON{
			Name:      r.name,
			LatencyMs: r.latency.Milliseconds(),
		}
		switch {
		case r.err == nil:
			cj.Status = "ok"
		case errors.Is(r.err, context.DeadlineExceeded):
			cj.Status = "timeout"
			cj.Error = r.err.Error()
			overallOK = false
		default:
			cj.Status = "failed"
			cj.Error = r.err.Error()
			overallOK = false
		}
		checks[i] = cj
	}

	code := http.StatusOK
	status := "ok"
	if !overallOK {
		code = http.StatusServiceUnavailable
		status = "not_ready"
	}
	writeJSON(w, code, readyResponse{
		Status: status,
		Checks: checks,
	})
}

// timeoutFor returns the per-checker timeout, falling back to the default.
// The map is populated only at NewHandler-time and read-only thereafter — no
// lock needed.
func (h *Handler) timeoutFor(name string) time.Duration {
	if d, ok := h.perCheckerTimeout[name]; ok {
		return d
	}
	return h.defaultCheckerTimeout
}

// livenessResponse is the /healthz body. Kept as a distinct type from
// readyResponse so /healthz never accidentally grows a "checks" field.
type livenessResponse struct {
	Status string `json:"status"`
}

// readyResponse is the /readyz body. The "checks" field is ALWAYS present
// (even empty []) so clients can branch on len(checks) without nil-checking;
// "reason" is omitempty so the success path stays a 2-key object.
type readyResponse struct {
	Status string      `json:"status"`
	Reason string      `json:"reason,omitempty"`
	Checks []checkJSON `json:"checks"`
}

// checkJSON is the per-dependency probe result. The "error" field is
// omitempty so successful checks render as a 3-key object
// {name, status, latency_ms} (architect MF-7).
type checkJSON struct {
	Name      string `json:"name"`
	Status    string `json:"status"`
	LatencyMs int64  `json:"latency_ms"`
	Error     string `json:"error,omitempty"`
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
