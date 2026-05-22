package logger

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// hashContentMaxBytes is the byte cap on input fed into the HMAC, per
// security.md §6.4 ("first 1024 chars"). The cap is BYTE-based to match the
// spec sample (`rawResponse[:min(len(rawResponse), 1024)]`) and to keep the
// hash domain deterministic regardless of input encoding. A multi-byte rune
// straddling byte 1024 is split mid-codepoint — that is fine because we hash
// bytes, never display them.
const hashContentMaxBytes = 1024

// HashContent computes hex(HMAC-SHA-256(key, content[:min(len, 1024)])).
//
// The function is the primitive behind the `raw_response_hash` and
// `raw_fragment_hash` log fields (security.md §6.4, observability.md §2.4).
// It exists so that audit signal (deduplication of repeated invalid LLM
// responses, prompt-injection fragments) can be logged without ever
// committing raw PII to the log aggregator.
//
// An empty key returns an empty string deliberately: a missing
// LIC_DLQ_HASH_KEY / LIC_PROMPT_INJECTION_HASH_KEY at the call site would
// otherwise produce a weak unkeyed digest that is open to rainbow-table
// re-identification (the very threat §6.4 mitigates). Returning "" makes the
// misconfiguration visible in the log line and is caught upstream by
// SecurityConfig.validate (config/security.go), which already requires both
// keys at startup.
func HashContent(content string, key []byte) string {
	if len(key) == 0 {
		return ""
	}
	body := content
	if len(body) > hashContentMaxBytes {
		body = body[:hashContentMaxBytes]
	}
	mac := hmac.New(sha256.New, key)
	// hmac.Hash.Write never errors (documented contract).
	_, _ = mac.Write([]byte(body))
	return hex.EncodeToString(mac.Sum(nil))
}
