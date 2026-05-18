package config

import (
	"strings"
	"testing"
	"time"
)

// validIdempotencyConfig is a baseline that passes validate(); individual
// tests perturb a single field.
func validIdempotencyConfig() IdempotencyConfig {
	return IdempotencyConfig{
		TTL:                        24 * time.Hour,
		ProcessingTTL:              150 * time.Second,
		HeartbeatInterval:          30 * time.Second,
		UserConfirmedProcessingTTL: 90 * time.Second,
	}
}

// D.C-24: LIC_USER_CONFIRMED_PROCESSING_TTL default is 90s (high-arch §6.10
// Resume step 2; build-spec R4/D12).
func TestLoadIdempotencyConfig_UserConfirmedProcessingTTL_Default(t *testing.T) {
	t.Setenv("LIC_USER_CONFIRMED_PROCESSING_TTL", "")
	got := loadIdempotencyConfig()
	if got.UserConfirmedProcessingTTL != 90*time.Second {
		t.Fatalf("default UserConfirmedProcessingTTL = %s, want 90s", got.UserConfirmedProcessingTTL)
	}
}

// D.C-24: an explicit env override is honoured.
func TestLoadIdempotencyConfig_UserConfirmedProcessingTTL_EnvOverride(t *testing.T) {
	t.Setenv("LIC_USER_CONFIRMED_PROCESSING_TTL", "45s")
	got := loadIdempotencyConfig()
	if got.UserConfirmedProcessingTTL != 45*time.Second {
		t.Fatalf("override UserConfirmedProcessingTTL = %s, want 45s", got.UserConfirmedProcessingTTL)
	}
}

// D.C-24: validate() rejects a non-positive UserConfirmedProcessingTTL.
func TestIdempotencyConfig_Validate_RejectsNonPositiveUserConfirmedTTL(t *testing.T) {
	cfg := validIdempotencyConfig()
	cfg.UserConfirmedProcessingTTL = 0
	err := cfg.validate()
	if err == nil || !strings.Contains(err.Error(), "LIC_USER_CONFIRMED_PROCESSING_TTL") {
		t.Fatalf("validate(): expected error about LIC_USER_CONFIRMED_PROCESSING_TTL, got: %v", err)
	}
}

func TestIdempotencyConfig_Validate_Valid(t *testing.T) {
	if err := validIdempotencyConfig().validate(); err != nil {
		t.Fatalf("valid idempotency config rejected: %v", err)
	}
}
