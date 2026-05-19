package idempotency

import (
	"context"
	"time"
)

// This file holds every Idempotency Guard SEAM — an adapter-local interface
// (plus, where applicable, a zero-dependency noop default) for collaborators
// that are the Redis primitive, telemetry, or the deterministic-time /
// log sinks, declared adapter-side so kvstore/prometheus/otel/logger are
// injected behind interfaces for hermetic unit testing against an in-memory
// fakeRedis (build-spec D7/D10/D11). Mirrors consumer/seams.go +
// pendingconfirmation/seams.go.
//
// var _ Seam = noop{} assertions follow each noop pair (the universal
// pendingconfirmation precedent). The
// var _ idempotency.RedisSeam = (*kvstore.Client)(nil) structural-satisfaction
// assertion lives in the LIC-TASK-047 WIRING package, NOT here — asserting it
// here would force the kvstore import to be load-bearing for compilation
// rather than types-only, mirroring consumer D5 (build-spec D7/D10/D14). The
// one in-package satisfaction assertion permitted is
// var _ port.IdempotencyStorePort = (*Guard)(nil) (guard.go) because the
// asserted interface (domain/port) is in the allowlist (D13).

// RedisSeam is the SUBSET of *kvstore.Client this adapter uses (the exact
// kvstore/CLAUDE.md:76-79 §6.3/§6.10 op coverage). Declared adapter-side so
// the Redis primitive is injected behind an interface for hermetic unit
// testing against an in-memory fakeRedis (D11). *kvstore.Client structurally
// satisfies it; the var _ RedisSeam = (*kvstore.Client)(nil) assertion lives
// in the LIC-TASK-047 WIRING package, NOT here (D10/D14). There is NO noop
// default — a Guard with no Redis seam cannot perform its contract; it is a
// REQUIRED positional NewGuard param (D2). SetNX/Delete are included for
// completeness/forward-use even though the primary path is Eval (D4) + Set
// (SetCompleted/SetPaused) + Expire (ExtendTTL) + Get (Get); the implementer
// MUST NOT widen it beyond these 6 (D7).
type RedisSeam interface {
	SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error)
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key, value string, ttl time.Duration) error
	Expire(ctx context.Context, key string, ttl time.Duration) (bool, error)
	Delete(ctx context.Context, keys ...string) (int64, error)
	Eval(ctx context.Context, script string, keys []string, args ...any) (any, error)
}

// Lookup-result string constants (build-spec D8). labels.go is in the
// forbidden metrics package (D10); these are value-identical to
// metrics.IdempLookup{New,InProgress,Completed,FallbackDB}
// (labels.go:117-120). Typed as untyped string consts so a typo is a compile
// error at the g.metrics.Lookup call sites. Pinned by
// TestLookupConstantsMatchSSOT WITHOUT a metrics import (the consumer D18
// precedent).
const (
	lookupNew        = "new"         // == metrics.IdempLookupNew        (labels.go:117)
	lookupInProgress = "in_progress" // == metrics.IdempLookupInProgress (labels.go:118)
	lookupCompleted  = "completed"   // == metrics.IdempLookupCompleted  (labels.go:119)
	lookupFallbackDB = "fallback_db" // == metrics.IdempLookupFallbackDB (labels.go:120)
)

// Metrics is the lic_idempotency_* seam (observability.md §3.6). NOT the
// concrete *prometheus.CounterVec and NOT *metrics.Metrics (hermeticity —
// D10; the consumer.Metrics / pendingconfirmation.Metrics precedent).
// LIC-TASK-047 wires an adapter over *metrics.IdempotencyMetrics
// (Lookup → LookupsTotal.WithLabelValues(result).Inc(); Fallback →
// FallbackTotal.Inc()).
type Metrics interface {
	// Lookup increments lic_idempotency_lookups_total{result}. result MUST
	// be one of the D8 constants (== labels.go:117-120 string values).
	Lookup(result string)
	// Fallback increments lic_idempotency_fallback_total (no labels —
	// the metric is a plain Counter, metrics/idempotency.go:13).
	Fallback()
}

// noopMetrics is the zero-dependency default so the hot path never nil-checks.
type noopMetrics struct{}

func (noopMetrics) Lookup(string) {}
func (noopMetrics) Fallback()     {}

var _ Metrics = noopMetrics{}

// Ticker is the injectable time-ticker seam. time.Ticker exposes its channel
// as a FIELD (C), not a method, so the seam wraps it (realTicker below) — this
// wrapper is the only reason Ticker is an interface: it makes the heartbeat
// goroutine (D6) deterministic and -race clean under an injected,
// manually-driven test ticker.
type Ticker interface {
	C() <-chan time.Time
	Stop()
}

// Clock is the deterministic-time seam. Unlike pendingconfirmation.Clock
// (Now() only) this seam exposes ONLY NewTicker — the binding surface the
// heartbeat goroutine (D6) needs; Now() is intentionally omitted (the Guard
// has no wall-clock use and go vet would flag an unused method — D7 sanctions
// omitting Now()).
type Clock interface {
	NewTicker(d time.Duration) Ticker
}

// realTicker wraps a *time.Ticker behind the Ticker seam (D7). It is the sole
// reason Ticker is an interface: time.Ticker.C is a struct field, not a
// method.
type realTicker struct {
	t *time.Ticker
}

func (rt realTicker) C() <-chan time.Time { return rt.t.C }
func (rt realTicker) Stop()               { rt.t.Stop() }

// systemClock is the production default; NewTicker wraps time.NewTicker behind
// the Ticker seam.
type systemClock struct{}

func (systemClock) NewTicker(d time.Duration) Ticker {
	return realTicker{t: time.NewTicker(d)}
}

var _ Clock = systemClock{}

// Logger is the structured WARN/ERROR seam — NO Info: unlike
// pendingconfirmation (which needs Info for the §11.2 mandatory audit trail),
// the Guard has no audit obligation. It logs ONLY the fallback alert
// (ERROR/WARN, R1) and the heartbeat vanished/transient (WARN, D6). Mirrors
// pipeline.Logger (Warn/Error). No WithRequestContext — that is the
// consumer's ingress-once concern; the Guard is invoked by 040 which already
// attached the ctx and the Guard never re-attaches (D7).
type Logger interface {
	Warn(ctx context.Context, msg string, kv ...any)
	Error(ctx context.Context, msg string, kv ...any)
}

// noopLogger is the zero-dependency default.
type noopLogger struct{}

func (noopLogger) Warn(context.Context, string, ...any)  {}
func (noopLogger) Error(context.Context, string, ...any) {}

var _ Logger = noopLogger{}
