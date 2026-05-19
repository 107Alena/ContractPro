// Package idempotency implements the LIC Idempotency Guard (LIC-TASK-038,
// high-architecture.md §6.3/§6.5/§6.10, configuration.md §2.4,
// observability.md §3.6, error-handling.md §3). It is the Redis-backed
// infrastructure adapter for port.IdempotencyStorePort — the exact analog of
// internal/ingress/consumer being the broker adapter: an adapter layer (not an
// internal/application/* hermetic core), so it MAY import internal/infra/
// kvstore for the primitive ONLY (behind the local RedisSeam), and is itself
// hermetic against prometheus/otel/redis/config/logger/application.
//
//   - NewGuard(RedisSeam, Config, Deps) (*Guard, error) — fail-fast
//     constructor (errors.Join of per-arg errors): the required RedisSeam is
//     positional, Config is a validated value, Metrics/Clock/Logger are the
//     optional-with-noop Deps bundle (build-spec D2/D9).
//   - SetNX/Get/ExtendTTL/SetCompleted/SetPaused — the 5 FROZEN
//     port.IdempotencyStorePort methods (idempotency.go:42-74, byte-for-byte;
//     D3). The existing pendingconfirmation.Manager consumer
//     (manager.go:251,325,438,442,622) depends on these EXACTLY.
//   - CheckAndAcquire(ctx,key,ttl)→(status,alreadyExists,err) and
//     StartHeartbeat(ctx,key,ttl)→(stop func()) — the additive ergonomic
//     surface LIC-TASK-040 consumes (R2 reconciles the task naming; TTLs are
//     per-call, NEVER hardcoded — R3).
//
// One atomic Lua via kvstore.Eval does SET-NX-EX-or-return-existing (D4) —
// zero TOCTOU; exactly ONE Eval per SetNX/CheckAndAcquire on the normal path,
// no separate Get round-trip; bounded single retry on the keyspace-eviction
// Lua-nil corner (D4.1). parseStatus maps unknown/garbage non-empty ⇒
// IdempotencyProcessing (defensive — never Absent, never double-analysis, D5).
//
// Error model: nil; the frozen port.ErrIdempotencyKeyExists (SetNX present-key
// ONLY, D3.1); the package-local ErrIdempotencyKeyVanished (D6.1) / errEvalShape
// (D4); or the kvstore error verbatim (*kvstore.RedisError,
// kvstore.ErrKeyNotFound, raw context errors). NEVER a model.ErrorCode (R4) —
// the Manager / 040 owns the model.ErrCodeIdempotencyStoreUnavail mapping.
// SetNX transport-down ⇒ (IdempotencyAbsent, kvstoreErrVerbatim) ALWAYS,
// ignoring FallbackEnabled (R1, preserves pendingconfirmation);
// CheckAndAcquire transport-down consults FallbackEnabled (R1).
//
// Hermetic adapter: stdlib (context/errors/fmt/strings/sync/time) +
// internal/domain/port + internal/infra/kvstore (error helpers/sentinels ONLY
// — never kvstore.NewClient; the primitive is the RedisSeam). It does NOT
// import internal/domain/model, go-redis, internal/config, the concrete
// logger/metrics/tracer (seamed — D7), internal/application/* (its own
// consumers — the dependency is INVERTED), internal/ingress/{consumer,router},
// or prometheus/otel/miniredis (D10).
//
// Design adjudicated by the authoritative build-spec
// (BUILD_SPEC_LIC_038.md — decisions D1..D14, reconciliations R1..R5);
// implemented by subagent golang-pro. The authoritative reconciliations are
// recorded in this package's CLAUDE.md.
package idempotency

import (
	"context"
	"errors"
	"time"

	"contractpro/legal-intelligence-core/internal/domain/port"
	"contractpro/legal-intelligence-core/internal/infra/kvstore"
)

// parseStatus maps a stored value to a port.IdempotencyStatus (build-spec D5).
// Stored values are exactly the port.IdempotencyStatus const strings
// (idempotency.go:16-19); the empty string is never STORED — it is the absent
// sentinel. A known status maps to itself; an unknown/garbage non-empty value
// maps DEFENSIVELY to IdempotencyProcessing (NEVER IdempotencyAbsent): a key
// exists but its value is garbage ⇒ "something owns this slot" ⇒ the caller
// NACK-retries rather than re-runs an in-flight/done pipeline (the §6.3
// double-analysis invariant this guard exists to prevent;
// pendingconfirmation switch: PROCESSING ⇒ retryable NACK, manager.go:333-340).
func parseStatus(v string) port.IdempotencyStatus {
	switch port.IdempotencyStatus(v) {
	case port.IdempotencyProcessing, port.IdempotencyPaused, port.IdempotencyCompleted:
		return port.IdempotencyStatus(v)
	default:
		return port.IdempotencyProcessing
	}
}

// Guard is the single exported type — the Redis-backed
// port.IdempotencyStorePort adapter (build-spec D13). It has no mutable
// per-instance state; all 7 methods are goroutine-safe for distinct keys (the
// RedisSeam/*kvstore.Client is concurrency-safe — kvstore/client.go:55-57;
// each StartHeartbeat call owns its own ticker + stopCh + goroutine).
type Guard struct {
	redis RedisSeam
	cfg   Config

	metrics Metrics
	clock   Clock
	log     Logger
}

// var _ port.IdempotencyStorePort = (*Guard)(nil) is the ONE in-package
// satisfaction assertion permitted (build-spec D13): the asserted interface
// (domain/port) is in the hermetic allowlist, unlike consumer's broker/router.
// It is a pure compile-time check that catches frozen-port drift early; the
// RedisSeam-satisfaction assertion is the LIC-TASK-047 wiring package's (D10).
var _ port.IdempotencyStorePort = (*Guard)(nil)

// NewGuard constructs a *Guard. redis is REQUIRED (the load-bearing
// collaborator — a Guard with no Redis seam cannot perform its contract;
// positional + fail-fast non-nil, the consumer/pendingconfirmation rule). cfg
// is a validated value (D9). deps is optional-with-noop (nil fields → noop).
// On any failure it returns (nil, errors.Join(...)) mentioning every offending
// arg (the consumer.NewConsumer / pendingconfirmation.NewManager precedent —
// build-spec D2).
func NewGuard(redis RedisSeam, cfg Config, deps Deps) (*Guard, error) {
	var errs []error
	if redis == nil {
		errs = append(errs, errors.New("idempotency: redis (RedisSeam) must not be nil"))
	}
	if err := cfg.validate(); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	d := deps.withDefaults()
	return &Guard{
		redis:   redis,
		cfg:     cfg,
		metrics: d.Metrics,
		clock:   d.Clock,
		log:     d.Logger,
	}, nil
}

// classifyLookup emits the D8 lic_idempotency_lookups_total{result} for a
// present-key status (the SetNX/CheckAndAcquire shared classifier). COMPLETED
// ⇒ "completed"; PROCESSING/PAUSED/unparseable(==Processing per D5) ⇒
// "in_progress" — there is no "paused" metric value (the SSOT enum is exactly
// new|in_progress|completed|fallback_db, labels.go:117-120, D8).
func (g *Guard) classifyLookup(existing port.IdempotencyStatus) {
	if existing == port.IdempotencyCompleted {
		g.metrics.Lookup(lookupCompleted)
		return
	}
	g.metrics.Lookup(lookupInProgress)
}

// SetNX atomically registers key=PROCESSING with the per-call ttl (FROZEN
// port contract — idempotency.go:43-48; build-spec D3.1). It does ONE atomic
// Lua Eval (D4):
//
//   - acquired (key was absent) ⇒ (IdempotencyAbsent, nil) — the caller now
//     owns the slot; Lookup("new").
//   - present ⇒ (existing parsed status, port.ErrIdempotencyKeyExists) — the
//     EXACT frozen carrier pendingconfirmation branches on via errors.Is then
//     switch status (manager.go:325-350); Lookup("completed") if COMPLETED
//     else Lookup("in_progress").
//   - transport error (Redis unreachable / context error / errEvalShape) ⇒
//     (IdempotencyAbsent, transportErr) ALWAYS — never a model.ErrorCode,
//     never wrapping ErrIdempotencyKeyExists, NEVER consulting
//     FallbackEnabled (R1): returning Absent,nil here would make
//     pendingconfirmation.Manager silently acquire a slot it never wrote
//     (double-resume risk). pendingconfirmation maps a
//     non-ErrIdempotencyKeyExists error to model.ErrCodeIdempotencyStoreUnavail
//     retryable NACK (manager.go:346-349).
func (g *Guard) SetNX(ctx context.Context, key string, ttl time.Duration) (port.IdempotencyStatus, error) {
	existing, acquired, err := g.evalSetNXOrGet(ctx, key, ttl)
	if err != nil {
		// Transport-class fault (R1/R4): verbatim, no fallback for the
		// frozen SetNX, no metric for this path.
		return port.IdempotencyAbsent, err
	}
	if acquired {
		g.metrics.Lookup(lookupNew)
		return port.IdempotencyAbsent, nil
	}
	g.classifyLookup(existing)
	return existing, port.ErrIdempotencyKeyExists
}

// CheckAndAcquire is the ergonomic restart-decision surface LIC-TASK-040's
// §6.5 decision tree consumes (high-architecture.md:628-634; build-spec
// D3.2). It performs the SAME atomic SETNX-or-return-existing as SetNX (D4) —
// it does NOT add a second round-trip — and returns the decision as a
// (status, alreadyExists, err) triple instead of the frozen
// (status, ErrIdempotencyKeyExists) shape. ttl is REQUIRED (the per-call TTL
// the caller would pass to SetNX; the task's no-ttl wording is reconciled in
// R2 — TTLs are per-call/per-key and the adapter never hardcodes them, R3).
//
// On present, it returns err == nil (the caller reads alreadyExists/status);
// it does NOT return ErrIdempotencyKeyExists — that is the frozen SetNX
// carrier pendingconfirmation depends on; CheckAndAcquire is a NEW 040-only
// surface, so the cleaner triple is chosen (D3.2 binding split — a reviewer
// must NOT "unify" them and break pendingconfirmation). It DOES consult
// FallbackEnabled on a transport error (R1):
//
//   - FallbackEnabled==true  ⇒ (IdempotencyAbsent,false,nil) ("proceed/ack
//     without dedup"); Lookup("fallback_db") AND Fallback(); ERROR log (the
//     configuration.md:65 "alert" — proceeding without dedup is a
//     degraded-correctness state operators must see).
//   - FallbackEnabled==false ⇒ (IdempotencyAbsent,false,errVerbatim) (040
//     maps it to model.ErrCodeIdempotencyStoreUnavail retryable NACK, exactly
//     as pendingconfirmation.Manager does for SetNX); WARN log; NO fallback_db
//     metric, NO Fallback(). Context errors pass through RAW (R1 — the Guard
//     never wraps context errors in a local type).
func (g *Guard) CheckAndAcquire(
	ctx context.Context, key string, ttl time.Duration,
) (status port.IdempotencyStatus, alreadyExists bool, err error) {
	existing, acquired, evalErr := g.evalSetNXOrGet(ctx, key, ttl)
	if evalErr != nil {
		// Transport-class fault — consult FallbackEnabled (R1).
		if g.cfg.FallbackEnabled {
			g.metrics.Lookup(lookupFallbackDB)
			g.metrics.Fallback()
			g.log.Error(ctx,
				"idempotency fallback: Redis unreachable, proceeding WITHOUT dedup (LIC_IDEMPOTENCY_FALLBACK_ENABLED=true)",
				"key", key, "cause", evalErr)
			return port.IdempotencyAbsent, false, nil
		}
		g.log.Warn(ctx,
			"idempotency: Redis unreachable, NACKing (fallback disabled)",
			"key", key, "cause", evalErr)
		return port.IdempotencyAbsent, false, evalErr
	}
	if acquired {
		g.metrics.Lookup(lookupNew)
		return port.IdempotencyAbsent, false, nil
	}
	g.classifyLookup(existing)
	return existing, true, nil
}

// Get fetches the current status of key (FROZEN port contract —
// idempotency.go:50-53; build-spec R5). A miss maps to
// (IdempotencyAbsent, nil) — NOT a Go-level error — via a clean
// errors.Is(err, kvstore.ErrKeyNotFound) (kvstore/CLAUDE.md:41-44). A present
// value is parsed defensively (D5). Any other (transport/context) error
// surfaces verbatim as (IdempotencyAbsent, err) (R4) — Get does NOT consult
// FallbackEnabled (only CheckAndAcquire does, R1) and emits NO metric (the
// §3.6 SSOT counts the SETNX-class decision only, not a passive read — D8).
func (g *Guard) Get(ctx context.Context, key string) (port.IdempotencyStatus, error) {
	val, err := g.redis.Get(ctx, key)
	if err != nil {
		if errors.Is(err, kvstore.ErrKeyNotFound) {
			return port.IdempotencyAbsent, nil
		}
		return port.IdempotencyAbsent, err
	}
	return parseStatus(val), nil
}

// ExtendTTL refreshes key's expiration without touching its value (FROZEN
// port contract — idempotency.go:55-61; build-spec D6.1). It is the per-tick
// primitive StartHeartbeat's loop calls. kvstore.Expire returns (bool,error)
// where false = key gone (ops.go:74-78): a true result ⇒ nil; a
// (false, nil) ⇒ the package-local ErrIdempotencyKeyVanished (NOT a
// model.ErrorCode, NOT kvstore.ErrKeyNotFound — that is Get-specific); a
// transport error ⇒ the kvstore error verbatim (R4). NO metric (D8).
func (g *Guard) ExtendTTL(ctx context.Context, key string, ttl time.Duration) error {
	ok, err := g.redis.Expire(ctx, key, ttl)
	if err != nil {
		return err
	}
	if !ok {
		return ErrIdempotencyKeyVanished
	}
	return nil
}

// SetCompleted moves key to status=COMPLETED with the per-call ttl (FROZEN
// port contract — idempotency.go:63-66; build-spec D12). It is a plain
// unconditional RedisSeam.Set (a status switch PROCESSING/PAUSED→COMPLETED is
// exactly what §6.3:565 / §6.10:782 require — "SET ... = COMPLETED EX 24h",
// not conditional). ttl is per-call (R3 — the task's "EX 24h" is the value
// 047/040 passes, not an adapter constant). On Set error the kvstore error is
// returned verbatim (NOT a model code — R4); pendingconfirmation
// logs-and-continues it (manager.go:438-444,622-625). NO metric (D8).
func (g *Guard) SetCompleted(ctx context.Context, key string, ttl time.Duration) error {
	return g.redis.Set(ctx, key, string(port.IdempotencyCompleted), ttl)
}

// SetPaused moves key from PROCESSING to PAUSED with the per-call ttl (FROZEN
// port contract — idempotency.go:68-73; build-spec D12). Specific to
// lic-trigger:{version_id} (only pendingconfirmation.Manager/040 calls it,
// manager.go:251) — the key-agnostic adapter does not special-case that; the
// "2-status keys" are simply keys their callers never SetPaused (D12). Plain
// unconditional RedisSeam.Set (§6.10:782 "SET lic-trigger = PAUSED EX 25h",
// not conditional); ttl per-call (R3); Set error verbatim (R4). NO metric (D8).
func (g *Guard) SetPaused(ctx context.Context, key string, ttl time.Duration) error {
	return g.redis.Set(ctx, key, string(port.IdempotencyPaused), ttl)
}
