package config

import (
	"errors"
	"fmt"
	"time"
)

// PipelineConfig holds in-process pipeline orchestration limits.
type PipelineConfig struct {
	Concurrency               int           // LIC_PIPELINE_CONCURRENCY — max parallel jobs per instance
	JobTimeout                time.Duration // LIC_JOB_TIMEOUT — overall job deadline
	DMRequestTimeout          time.Duration // LIC_DM_REQUEST_TIMEOUT — async artifact request timeout
	DMPersistConfirmTimeout   time.Duration // LIC_DM_PERSIST_CONFIRM_TIMEOUT — DM persist-confirmation timeout
	PendingConfirmationTTL    time.Duration // LIC_PENDING_CONFIRMATION_TTL — pending type-confirmation TTL (Redis safety net)
}

func loadPipelineConfig() PipelineConfig {
	return PipelineConfig{
		Concurrency:             envInt("LIC_PIPELINE_CONCURRENCY", 5),
		JobTimeout:              envDuration("LIC_JOB_TIMEOUT", 90*time.Second),
		DMRequestTimeout:        envDuration("LIC_DM_REQUEST_TIMEOUT", 30*time.Second),
		DMPersistConfirmTimeout: envDuration("LIC_DM_PERSIST_CONFIRM_TIMEOUT", 30*time.Second),
		PendingConfirmationTTL:  envDuration("LIC_PENDING_CONFIRMATION_TTL", 25*time.Hour),
	}
}

func (p PipelineConfig) validate() error {
	var errs []error
	if p.Concurrency < 1 {
		errs = append(errs, fmt.Errorf("config: LIC_PIPELINE_CONCURRENCY must be >= 1, got %d", p.Concurrency))
	}
	for _, dv := range []struct {
		name string
		d    time.Duration
	}{
		{"LIC_JOB_TIMEOUT", p.JobTimeout},
		{"LIC_DM_REQUEST_TIMEOUT", p.DMRequestTimeout},
		{"LIC_DM_PERSIST_CONFIRM_TIMEOUT", p.DMPersistConfirmTimeout},
		{"LIC_PENDING_CONFIRMATION_TTL", p.PendingConfirmationTTL},
	} {
		if dv.d <= 0 {
			errs = append(errs, fmt.Errorf("config: %s must be > 0, got %s", dv.name, dv.d))
		}
	}
	return errors.Join(errs...)
}
