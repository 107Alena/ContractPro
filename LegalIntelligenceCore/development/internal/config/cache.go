package config

import (
	"fmt"
	"time"
)

// CacheConfig holds optional cache toggles and TTLs.
type CacheConfig struct {
	LLMCacheEnabled     bool          // LIC_LLM_CACHE_ENABLED — cache LLM responses (off by default, ASSUMPTION-LIC-15)
	VersionMetaCacheTTL time.Duration // LIC_VERSION_META_CACHE_TTL — origin_type + parent_version_id cache TTL
}

func loadCacheConfig() CacheConfig {
	return CacheConfig{
		LLMCacheEnabled:     envBool("LIC_LLM_CACHE_ENABLED", false),
		VersionMetaCacheTTL: envDuration("LIC_VERSION_META_CACHE_TTL", 24*time.Hour),
	}
}

// validate is invoked through CacheConfig — currently only TTL must be positive.
func (c CacheConfig) validate() error {
	if c.VersionMetaCacheTTL <= 0 {
		return fmt.Errorf("config: LIC_VERSION_META_CACHE_TTL must be > 0, got %s", c.VersionMetaCacheTTL)
	}
	return nil
}
