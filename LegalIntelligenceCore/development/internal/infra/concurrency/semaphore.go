// Package concurrency provides a counting semaphore that bounds the number of
// concurrently executing operations on a single LIC instance.
//
// It serves the two backpressure sites of high-architecture.md §6.14 with a
// single primitive (ai-agents-pipeline.md §"Семафоры"):
//
//   - job-level — LIC_PIPELINE_CONCURRENCY (default 5): the cap on
//     simultaneously processed contract versions. The job-level instance is
//     constructed with WithGauge(metrics.Pipeline.ConcurrentJobs) so the
//     lic_pipeline_concurrent_jobs gauge (observability.md §3.2) tracks
//     in-flight jobs (wired by LIC-TASK-036).
//   - LLM-level per provider — LIC_LLM_CONCURRENCY_PER_PROVIDER (default 10):
//     the cap on simultaneous HTTP calls to one provider. The Provider Router
//     (LIC-TASK-016/017/047) builds one gauge-free instance per provider; the
//     SSOT defines no LLM-level concurrency gauge, so none is invented.
//
// The package is HERMETIC: it imports only the standard library. Observability
// is inverted behind the minimal Gauge seam (the aggregator.Metrics /
// schemavalidator.Metrics precedent), so the hermetic application/llm
// consumers can depend on it without pulling Prometheus into their import
// graph. Unlike infra/broker and infra/kvstore (which import irreducible
// protocol clients amqp/go-redis), concurrency's only external dependency — a
// two-method gauge — is reducible to a seam; that is why it is hermetic and
// ships TestHermeticImports while its infra siblings do not (code-architect
// D3/D9). It implements NO domain port — LIC has none for concurrency,
// mirroring infra/broker and infra/kvstore; the DocumentProcessing
// var _ port.ConcurrencyLimiterPort assertion is deliberately NOT copied
// (code-architect D1).
package concurrency

import "context"

// Gauge is the observability seam for the in-flight holder count. The
// job-level Semaphore is wired with a real gauge (Prometheus
// lic_pipeline_concurrent_jobs, which satisfies this interface structurally);
// gauge-free instances run on noopGauge.
//
// Implementations MUST be safe for concurrent use by multiple goroutines:
// the Semaphore calls Inc/Dec without any locking (many job goroutines hit
// the same gauge at once). prometheus.Gauge satisfies this (atomic Inc/Dec);
// noopGauge is trivially safe (stateless).
type Gauge interface {
	Inc()
	Dec()
}

// noopGauge is the zero-dependency default so a gauge-free Semaphore never
// nil-checks on the hot path (the base.noopMetrics / aggregator.noopMetrics
// precedent).
type noopGauge struct{}

func (noopGauge) Inc() {}
func (noopGauge) Dec() {}

var _ Gauge = noopGauge{}

// Semaphore is a counting semaphore backed by a buffered channel: a buffer of
// capacity max admits exactly max successful Acquires before the next blocks;
// Release frees exactly one slot. The buffered channel is the sole
// synchronization primitive (no sync import needed). Safe for concurrent use
// by many goroutines; immutable after New.
type Semaphore struct {
	slots chan struct{}
	gauge Gauge
}

// Option customizes a Semaphore at construction. The variadic form keeps the
// acceptance signature `New(max int)` a valid call (`New(5)`) while letting
// the job-level wiring attach a gauge without a second constructor
// (code-architect D2).
type Option func(*Semaphore)

// WithGauge attaches the in-flight-holder gauge (the job-level instance wires
// lic_pipeline_concurrent_jobs here). A nil gauge is ignored so the noopGauge
// default is preserved — the LLM-level per-provider instances simply omit
// this option.
func WithGauge(g Gauge) Option {
	return func(s *Semaphore) {
		if g != nil {
			s.gauge = g
		}
	}
}

// New returns a Semaphore admitting at most max concurrent holders.
//
// It PANICS if max < 1: config validation already fail-fast-guarantees
// LIC_PIPELINE_CONCURRENCY >= 1 and LIC_LLM_CONCURRENCY_PER_PROVIDER >= 1
// (config/pipeline.go, config/llm.go), so a smaller value can only be a LIC
// wiring/build defect, never operator input. Clamping it (the
// DocumentProcessing behaviour) would silently mask the defect and run the
// whole pipeline at the wrong limit — the "no silent degradation" fail-fast
// discipline of NewExecutor/NewAggregator (code-architect D6, deliberate
// divergence from DP).
func New(max int, opts ...Option) *Semaphore {
	if max < 1 {
		panic("concurrency.New: max must be >= 1")
	}
	s := &Semaphore{
		slots: make(chan struct{}, max),
		gauge: noopGauge{},
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Acquire blocks until a slot is free or ctx is done. On success it returns
// nil with the gauge incremented; the caller MUST then call Release exactly
// once. On cancellation/deadline it returns ctx.Err() verbatim
// (context.Canceled or context.DeadlineExceeded, unwrapped — the codebase-wide
// "context errors pass through raw" convention; this package never imports
// internal/domain/model) and does NOT consume a slot.
func (s *Semaphore) Acquire(ctx context.Context) error {
	// Deterministic precedence: an already-done ctx must return its error
	// and consume no slot, regardless of slot availability. A bare select
	// over both ready cases would pick pseudo-randomly (Go spec), so this
	// pre-check is mandatory, not an optimization (code-architect D7,
	// golang-pro §1).
	if err := ctx.Err(); err != nil {
		return err
	}

	select {
	case s.slots <- struct{}{}:
		s.gauge.Inc()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Release frees one slot previously taken by a successful Acquire. Calling
// Release without a matching Acquire is an unrecoverable programmer error and
// PANICS (the sync.WaitGroup-negative-counter / Mutex-unlock-of-unlocked
// idiom; LIC fail-fast). This deliberately diverges from DocumentProcessing,
// which logs a warning — concurrency is hermetic and has no logger, and the
// panic structurally prevents lic_pipeline_concurrent_jobs from going negative
// (the gauge is decremented only after a token is actually received, so the
// panic path never reaches Dec — code-architect D5, golang-pro §2).
func (s *Semaphore) Release() {
	select {
	case <-s.slots:
		s.gauge.Dec()
	default:
		panic("concurrency.Semaphore: Release called without a matching Acquire")
	}
}
