package config

import "errors"

// SecurityConfig holds HMAC secrets required by the PII fragment-hash logger
// and the DLQ envelope hasher (see security.md §4.3, §6.4).
//
// Both keys are mandatory at all environments — they protect signal that
// must survive across pod restarts (log de-duplication, DLQ traceability).
type SecurityConfig struct {
	PromptInjectionHashKey string // LIC_PROMPT_INJECTION_HASH_KEY (required)
	DLQHashKey             string // LIC_DLQ_HASH_KEY (required)
}

func loadSecurityConfig() SecurityConfig {
	return SecurityConfig{
		PromptInjectionHashKey: envString("LIC_PROMPT_INJECTION_HASH_KEY", ""),
		DLQHashKey:             envString("LIC_DLQ_HASH_KEY", ""),
	}
}

func (s SecurityConfig) validate() error {
	var errs []error
	if s.PromptInjectionHashKey == "" {
		errs = append(errs, missingVarErr("LIC_PROMPT_INJECTION_HASH_KEY"))
	}
	if s.DLQHashKey == "" {
		errs = append(errs, missingVarErr("LIC_DLQ_HASH_KEY"))
	}
	return errors.Join(errs...)
}
