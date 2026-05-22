package dlq

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// RawLLMResponseHashMaxBytes is the byte cap on input fed into the HMAC for
// raw LLM responses, per security.md §6.4 ("first 1024 chars"). The cap is
// BYTE-based to match the spec sample (`rawResponse[:min(len(rawResponse),
// 1024)]`) and to keep the hash domain deterministic regardless of input
// encoding. A multi-byte rune straddling byte 1024 is split mid-codepoint —
// that is fine because we hash bytes, never display them.
const RawLLMResponseHashMaxBytes = 1024

// HashPayload computes hex(HMAC-SHA-256(key, payload)) over the FULL payload
// bytes — no truncation. It is the primitive behind
// LICDLQEnvelope.OriginalMessageHash (integration-contracts.md §10.1,
// event-catalog.md §3.1), keyed by LIC_DLQ_HASH_KEY (32 bytes).
//
// Used at the four DLQ call sites:
//
//   - lic.dlq.invalid-message: hash of the raw bytes the consumer received
//     (even when JSON-parsing failed entirely — we still have bytes).
//   - lic.dlq.consumer-failed: hash of the raw bytes the consumer received.
//   - lic.dlq.publish-failed: hash of the marshalled outbound envelope that
//     failed to publish (LegalAnalysisArtifactsReady with PII).
//   - lic.dlq.agent-output-invalid: hash of the marshalled agent input
//     envelope (the prompt body, not the LLM response — the LLM response
//     hash uses HashRawLLMResponse below with its own 1024-byte cap).
//
// An empty key returns an empty string deliberately: a missing
// LIC_DLQ_HASH_KEY at the call site would otherwise produce a weak unkeyed
// digest that is open to rainbow-table re-identification (the very threat
// §6.4 mitigates). Returning "" makes the misconfiguration visible in the
// envelope and is caught upstream by SecurityConfig.validate
// (config/security.go), which already requires the key at startup.
//
// The result is hex(sha256-output) → 64 lowercase hex characters
// ("first 64 chars hex" per §10.1 means "the full 32-byte digest in hex",
// not a truncation of the digest itself; the digest IS 32 bytes / 64 hex
// chars by construction).
func HashPayload(payload []byte, key []byte) string {
	if len(key) == 0 {
		return ""
	}
	mac := hmac.New(sha256.New, key)
	// hmac.Hash.Write never errors (documented contract).
	_, _ = mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

// HashRawLLMResponse computes
// hex(HMAC-SHA-256(key, response[:min(len, 1024)])) — the byte-capped
// primitive behind LICDLQEnvelope.RawLLMResponseHash for the
// lic.dlq.agent-output-invalid topic (security.md §6.4 worked example;
// integration-contracts.md §10.1; event-catalog.md §3.1 footnote — sha256
// has been replaced by HMAC-SHA-256 for rainbow-table resistance, but the
// 1024-byte input cap is preserved). Same LIC_DLQ_HASH_KEY as HashPayload.
//
// The cap is by-design — raw LLM responses can be tens of KB but the
// deduplication signal lives in the head bytes (JSON structure +
// model-name-shaped first tokens), so 1024 bytes is sufficient to
// distinguish unique invalid-output incidents while bounding the hash
// computation cost and (more importantly) bounding how much PII we
// transitively read before discarding.
//
// An empty key returns an empty string for the same reason as HashPayload.
func HashRawLLMResponse(response []byte, key []byte) string {
	if len(key) == 0 {
		return ""
	}
	body := response
	if len(body) > RawLLMResponseHashMaxBytes {
		body = body[:RawLLMResponseHashMaxBytes]
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
