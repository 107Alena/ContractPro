package config

import (
	"fmt"
	"time"
)

// IdempotencyConfig holds Redis-backed idempotency TTLs.
//
// ProcessingTTL must exceed Pipeline.JobTimeout + Pipeline.DMPersistConfirmTimeout
// plus a safety buffer; configuration.md §2.4 documents the rationale.
type IdempotencyConfig struct {
	TTL               time.Duration // LIC_IDEMPOTENCY_TTL — TTL for COMPLETED/PAUSED keys
	ProcessingTTL     time.Duration // LIC_IDEMPOTENCY_PROCESSING_TTL — TTL for PROCESSING keys
	HeartbeatInterval time.Duration // LIC_IDEMPOTENCY_HEARTBEAT_INTERVAL — TTL refresh cadence
	FallbackEnabled   bool          // LIC_IDEMPOTENCY_FALLBACK_ENABLED — ack-without-check on Redis outage
}

func loadIdempotencyConfig() IdempotencyConfig {
	return IdempotencyConfig{
		TTL:               envDuration("LIC_IDEMPOTENCY_TTL", 24*time.Hour),
		ProcessingTTL:     envDuration("LIC_IDEMPOTENCY_PROCESSING_TTL", 150*time.Second),
		HeartbeatInterval: envDuration("LIC_IDEMPOTENCY_HEARTBEAT_INTERVAL", 30*time.Second),
		FallbackEnabled:   envBool("LIC_IDEMPOTENCY_FALLBACK_ENABLED", false),
	}
}

func (i IdempotencyConfig) validate() error {
	if i.TTL <= 0 {
		return fmt.Errorf("config: LIC_IDEMPOTENCY_TTL must be > 0, got %s", i.TTL)
	}
	if i.ProcessingTTL <= 0 {
		return fmt.Errorf("config: LIC_IDEMPOTENCY_PROCESSING_TTL must be > 0, got %s", i.ProcessingTTL)
	}
	if i.HeartbeatInterval <= 0 {
		return fmt.Errorf("config: LIC_IDEMPOTENCY_HEARTBEAT_INTERVAL must be > 0, got %s", i.HeartbeatInterval)
	}
	if i.HeartbeatInterval >= i.ProcessingTTL {
		return fmt.Errorf("config: LIC_IDEMPOTENCY_HEARTBEAT_INTERVAL (%s) must be < LIC_IDEMPOTENCY_PROCESSING_TTL (%s)", i.HeartbeatInterval, i.ProcessingTTL)
	}
	return nil
}
