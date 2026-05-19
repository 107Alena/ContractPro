package consumer

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
)

// dlq_envelope.go builds the PII-safe port.LICDLQEnvelope for the
// lic.dlq.invalid-message topic (build-spec D13) and computes the
// HMAC-SHA-256 payload reference the consumer owns for the invalid-message
// path (build-spec R3 — the only component holding the raw inbound bytes; the
// frozen port.DLQPublisherPort.PublishDLQ signature passes no raw bytes so the
// publisher physically cannot hash them).

// maxDLQErrorMessageBytes caps LICDLQEnvelope.ErrorMessage so a pathological
// reason string cannot bloat the envelope (build-spec D13). The reason is
// already a sanitized field list / decode-error description — NEVER raw body
// bytes (PII — integration-contracts.md:319).
const maxDLQErrorMessageBytes = 512

// hmacFirst64 computes HMAC-SHA-256(body, key) and returns the first 64 hex
// chars (build-spec R3, integration-contracts.md:331,349-351). SHA-256 is 32
// bytes ⇒ exactly 64 lowercase-hex chars; the length guard is defensive.
func hmacFirst64(body []byte, key string) string {
	m := hmac.New(sha256.New, []byte(key))
	_, _ = m.Write(body) // hmac.Write never errors
	full := hex.EncodeToString(m.Sum(nil))
	if len(full) > 64 {
		return full[:64]
	}
	return full
}

// truncateUTF8Safe trims s to at most maxBytes bytes without splitting a
// multi-byte rune (the reason is ASCII field names today, but defense-in-depth
// keeps the envelope valid UTF-8).
func truncateUTF8Safe(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	cut := maxBytes
	for cut > 0 && (s[cut]&0xC0) == 0x80 { // back up off a UTF-8 continuation byte
		cut--
	}
	return s[:cut]
}

// adoptCanonicalID returns id only if it is a clean canonical UUID, else ""
// (build-spec D12 — a forged body must not inject arbitrary strings into the
// PII-safe forensic IDs; a present-but-not-a-UUID probe value is dropped,
// matching the events.go:281-282 best-effort contract).
func adoptCanonicalID(id string) string {
	if isCanonicalUUID(id) {
		return id
	}
	return ""
}

// buildInvalidEnvelope constructs the lic.dlq.invalid-message envelope
// (build-spec D13, every field). topic is the D7 frozen topic string (=
// routing key = metric label). verr is the single internal *model.DomainError
// (D15); its Error() is a sanitized field list — NEVER raw payload content.
// ids is the best-effort tolerant probe (D12); only clean canonical UUIDs are
// adopted. clock supplies FailedAt (D14). dlqHashKey is LIC_DLQ_HASH_KEY
// (R3 — injected as the NewConsumer dlqHashKey param).
func buildInvalidEnvelope(
	topic string,
	body []byte,
	verr error,
	ids idProbe,
	clock Clock,
	dlqHashKey string,
) port.LICDLQEnvelope {
	reason := ""
	if verr != nil {
		reason = verr.Error()
	}
	return port.LICDLQEnvelope{
		OriginalTopic:            topic,
		OriginalMessageHash:      hmacFirst64(body, dlqHashKey),
		OriginalMessageSizeBytes: len(body),

		ErrorCode:    model.ErrCodeInvalidMessageSchema,
		ErrorMessage: truncateUTF8Safe(reason, maxDLQErrorMessageBytes),
		// 039 is the first failure; retry-budget tracking is 040/x-death-owned
		// (build-spec R1).
		RetryCount: 0,

		// Best-effort forensic IDs — empty unless a clean canonical UUID
		// (build-spec D12).
		CorrelationID:  adoptCanonicalID(ids.CorrelationID),
		JobID:          adoptCanonicalID(ids.JobID),
		DocumentID:     adoptCanonicalID(ids.DocumentID),
		VersionID:      adoptCanonicalID(ids.VersionID),
		OrganizationID: adoptCanonicalID(ids.OrganizationID),

		// AgentID / Stage / RawLLMResponseHash / PayloadStorageKey are left
		// zero — they are agent-output-invalid / publish-failed only
		// (events.go:289-298, build-spec D13).

		FailedAt: clock.Now().UTC().Format(time.RFC3339),
	}
}
