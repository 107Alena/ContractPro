package idempotency

import (
	"errors"
	"time"
)

// Config carries the ONLY two intrinsic knobs the Idempotency Guard owns
// (build-spec D9). It is a plain value type, ctor-injected by LIC-TASK-047
// (the pendingconfirmation.Config / consumer.dlqHashKey "local config, NOT an
// internal/config import" precedent — D9/D10). It deliberately does NOT carry
// ProcessingTTL / TTL / PendingConfirmationTTL / UserConfirmedProcessingTTL:
// every TTL is a per-call method parameter on the frozen
// port.IdempotencyStorePort (idempotency.go:48,61,66,73), so the adapter MUST
// NOT hold or hardcode them (R3). The Guard does NOT re-validate
// HeartbeatInterval < ProcessingTTL — that invariant is enforced once in
// config.IdempotencyConfig.validate and ProcessingTTL is not even known to the
// Guard (it arrives per-call as ttl); duplicating it here would be a false
// coupling (D9).
type Config struct {
	// HeartbeatInterval is the EXPIRE cadence StartHeartbeat ticks at (D6);
	// from config.IdempotencyConfig.HeartbeatInterval
	// (LIC_IDEMPOTENCY_HEARTBEAT_INTERVAL, default 30s —
	// configuration.md:64). MUST be > 0.
	HeartbeatInterval time.Duration

	// FallbackEnabled gates the CheckAndAcquire Redis-down degraded path
	// (R1); from config.IdempotencyConfig.FallbackEnabled
	// (LIC_IDEMPOTENCY_FALLBACK_ENABLED, default false —
	// configuration.md:65). It is consulted ONLY by CheckAndAcquire — the
	// frozen SetNX never consults it (R1, preserves pendingconfirmation).
	FallbackEnabled bool
}

// validate reports the single intrinsic invariant the Guard owns:
// HeartbeatInterval must be strictly positive (D9). It returns an
// errors.Join of per-field errors so NewGuard can fail-fast with a single
// joined error mentioning every offending field (the
// pendingconfirmation.Config.validate / consumer precedent — D2/D9).
func (c Config) validate() error {
	var errs []error
	if c.HeartbeatInterval <= 0 {
		errs = append(errs, errors.New("idempotency: Config.HeartbeatInterval must be > 0"))
	}
	return errors.Join(errs...)
}
