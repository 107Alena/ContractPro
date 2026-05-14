package port

import (
	"context"
	"errors"
	"time"
)

// IdempotencyStatus enumerates the four states of an idempotency key
// (high-architecture.md §6.3). Only lic-trigger:{version_id} uses all four;
// the other five keys (version-created / artifacts-resp / persist-resp /
// persist-fail / user-confirmed) collapse to {absent, PROCESSING, COMPLETED}.
type IdempotencyStatus string

const (
	IdempotencyAbsent     IdempotencyStatus = ""           // sentinel — key did not exist
	IdempotencyProcessing IdempotencyStatus = "PROCESSING" // in-flight, with heartbeat
	IdempotencyPaused     IdempotencyStatus = "PAUSED"     // long pause awaiting UserConfirmedType (lic-trigger only)
	IdempotencyCompleted  IdempotencyStatus = "COMPLETED"  // terminal — success or final FAILED
)

// IsTerminal reports whether s represents a terminal state where the
// router ACKs without re-running the pipeline (high-architecture.md §6.3).
func (s IdempotencyStatus) IsTerminal() bool {
	return s == IdempotencyCompleted
}

// ErrIdempotencyKeyExists is returned by SetNX when the key was already
// present. The caller MUST inspect the returned Status to decide whether
// to NACK to retry-DLX (PROCESSING — still in-flight elsewhere), republish
// pause events and ACK (PAUSED — see §6.5 restart semantics), or ACK
// without work (COMPLETED).
var ErrIdempotencyKeyExists = errors.New("idempotency key already exists")

// IdempotencyStorePort guards against at-least-once duplicate deliveries
// (high-architecture.md §6.3, integration-contracts.md §1.2). Every consumer
// hands the message through this gate before doing real work.
//
// Implementations are Redis-backed (LIC-TASK-038). Domain code stays unaware
// of Lua scripts, EX/EXPIRE semantics or backoff details — those concerns
// live in the adapter.
type IdempotencyStorePort interface {
	// SetNX atomically registers `key` with status=PROCESSING and the
	// given TTL. Returns (IdempotencyAbsent, nil) on success — the
	// caller now owns the in-flight slot. Returns the existing status
	// wrapped in ErrIdempotencyKeyExists otherwise — the caller branches
	// per §6.3.
	SetNX(ctx context.Context, key string, ttl time.Duration) (IdempotencyStatus, error)

	// Get fetches the current status of `key`. Returns IdempotencyAbsent
	// without error when the key is missing — adapters MUST translate
	// Redis nil-reply into IdempotencyAbsent, not a Go-level error.
	Get(ctx context.Context, key string) (IdempotencyStatus, error)

	// ExtendTTL refreshes the key's expiration without touching its
	// value. Called by the heartbeat goroutine every
	// LIC_IDEMPOTENCY_HEARTBEAT_INTERVAL=30s while a pipeline holds
	// PROCESSING; on crash the heartbeat stops and the key expires
	// after at most ttl (LIC_IDEMPOTENCY_PROCESSING_TTL=150s by default,
	// high-architecture.md §6.3).
	ExtendTTL(ctx context.Context, key string, ttl time.Duration) error

	// SetCompleted moves `key` to status=COMPLETED with TTL (typically
	// 24h, LIC_IDEMPOTENCY_TTL). Called once after the pipeline
	// publishes its terminal status event (COMPLETED or FAILED).
	SetCompleted(ctx context.Context, key string, ttl time.Duration) error

	// SetPaused moves `key` from PROCESSING to PAUSED with TTL
	// LIC_PENDING_CONFIRMATION_TTL=25h. Specific to lic-trigger:
	// {version_id}; the orchestrator calls it once it has confirmed
	// (via publisher confirms) that classification-uncertain and
	// status-changed events reached the broker (§6.5 strict ordering).
	SetPaused(ctx context.Context, key string, ttl time.Duration) error
}
