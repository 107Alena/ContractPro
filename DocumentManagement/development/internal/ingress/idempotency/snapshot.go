package idempotency

import (
	"encoding/json"
	"fmt"
)

// SnapshotSchemaVersion is the current envelope version for ConfirmationSnapshot.
// Bumping the version requires a corresponding consumer-side migration to keep
// re-published confirmations interpretable across rolling deploys (DM-TASK-058).
const SnapshotSchemaVersion = "1.0"

// MaxSnapshotSizeBytes caps the serialized ConfirmationSnapshot envelope to keep
// idempotency records compact in Redis (DM-TASK-058). Confirmation events are
// ~500 bytes in practice; the 64 KiB limit leaves ample headroom while
// rejecting accidental oversize payloads that could blow up the KV store.
const MaxSnapshotSizeBytes = 64 * 1024

// ConfirmationSnapshot is the persistent envelope for a producer→DM direct
// response confirmation. It is stored in IdempotencyRecord.ResultSnapshot at
// first-time success and read back on duplicate delivery to re-publish the same
// confirmation payload to the same topic (DM-TASK-058).
//
// Encoding: JSON. SchemaVersion enables future evolution of the envelope.
// Payload is the already-marshaled confirmation event body.
type ConfirmationSnapshot struct {
	SchemaVersion string          `json:"schema_version"`
	Topic         string          `json:"topic"`
	Payload       json.RawMessage `json:"payload"`
}

// EncodeConfirmationSnapshot marshals a confirmation event together with its
// destination topic into a JSON envelope suitable for storage in
// IdempotencyRecord.ResultSnapshot.
//
// Returns an error if topic is empty, event is nil, or the resulting envelope
// exceeds MaxSnapshotSizeBytes (a programming/config error — never silently
// truncated).
func EncodeConfirmationSnapshot(topic string, event any) (string, error) {
	if topic == "" {
		return "", fmt.Errorf("idempotency: confirmation snapshot topic must not be empty")
	}
	if event == nil {
		return "", fmt.Errorf("idempotency: confirmation snapshot event must not be nil")
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return "", fmt.Errorf("idempotency: marshal confirmation payload: %w", err)
	}
	// Defend against payloads that marshal to JSON null (e.g. typed-nil pointer).
	// Re-publish must always have a concrete confirmation body.
	if len(payload) == 0 || string(payload) == "null" {
		return "", fmt.Errorf("idempotency: confirmation payload must not be empty or JSON null")
	}
	envelope := ConfirmationSnapshot{
		SchemaVersion: SnapshotSchemaVersion,
		Topic:         topic,
		Payload:       payload,
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return "", fmt.Errorf("idempotency: marshal confirmation envelope: %w", err)
	}
	if len(encoded) > MaxSnapshotSizeBytes {
		return "", fmt.Errorf(
			"idempotency: confirmation snapshot size %d exceeds limit %d",
			len(encoded), MaxSnapshotSizeBytes,
		)
	}
	return string(encoded), nil
}

// DecodeConfirmationSnapshot parses a stored snapshot envelope. Unknown
// schema_version values are accepted with the payload returned as-is so that
// rolling deploys do not lose confirmations; callers should still warn on
// mismatch.
func DecodeConfirmationSnapshot(raw string) (ConfirmationSnapshot, error) {
	var snap ConfirmationSnapshot
	if raw == "" {
		return snap, fmt.Errorf("idempotency: empty confirmation snapshot")
	}
	if err := json.Unmarshal([]byte(raw), &snap); err != nil {
		return snap, fmt.Errorf("idempotency: unmarshal confirmation snapshot: %w", err)
	}
	if snap.Topic == "" {
		return snap, fmt.Errorf("idempotency: confirmation snapshot missing topic")
	}
	if len(snap.Payload) == 0 {
		return snap, fmt.Errorf("idempotency: confirmation snapshot missing payload")
	}
	return snap, nil
}
