package model

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
)

// PendingTypeConfirmation is the per-version state persisted to Redis when
// classification confidence is below LIC_CONFIDENCE_THRESHOLD and the pipeline
// is paused awaiting orch.commands.user-confirmed-type
// (high-architecture.md §6.10, ASSUMPTION-LIC-05).
//
// Key:   lic-pending-state:{version_id}
// Value: Encode(PendingTypeConfirmation) — JSON → gzip → base64.
// TTL:   25h (LIC_PENDING_CONFIRMATION_TTL).
//
// The encoded form is what gets stored; round-trip preservation is verified
// by pending_test.go.
//
// TraceContext is a value (not *TraceContext) on purpose — pending state is
// always created from an active OTel span, so an absent trace context here
// indicates a bug, not a valid state. The zero TraceContext (both fields empty)
// remains JSON-marshalable for forward compatibility but TraceContext.IsZero()
// is the canonical "no trace" test, not field nil-ness.
type PendingTypeConfirmation struct {
	JobID                string                `json:"job_id"`
	DocumentID           string                `json:"document_id"`
	VersionID            string                `json:"version_id"`
	OrganizationID       string                `json:"organization_id"`
	CreatedByUserID      string                `json:"created_by_user_id,omitempty"`
	CorrelationID        string                `json:"correlation_id"`
	TraceContext         TraceContext          `json:"trace_context"`
	ClassificationResult *ClassificationResult `json:"classification_result"`
	KeyParameters        *KeyParameters        `json:"key_parameters,omitempty"`
	InputArtifacts       InputArtifactsCompact `json:"input_artifacts_compact,omitempty"`
	OriginType           string                `json:"origin_type,omitempty"`
	ParentVersionID      *string               `json:"parent_version_id,omitempty"`
}

// Encode serialises p as canonical JSON, gzips the bytes, and base64-encodes
// the result. The returned slice is what gets SET into Redis under
// lic-pending-state:{version_id}.
//
// gzip level is the default (gzip.DefaultCompression) — typical pending payloads
// are 50-500 KB of repetitive contract text where the default codec is well
// suited; tuning would not move the needle at the volumes we see in v1.
func (p *PendingTypeConfirmation) Encode() ([]byte, error) {
	if p == nil {
		return nil, fmt.Errorf("encode pending: nil receiver")
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("encode pending: json marshal: %w", err)
	}

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(raw); err != nil {
		_ = gz.Close()
		return nil, fmt.Errorf("encode pending: gzip write: %w", err)
	}
	if err := gz.Close(); err != nil {
		return nil, fmt.Errorf("encode pending: gzip close: %w", err)
	}

	// EncodedLen is exact for StdEncoding (4*ceil(n/3) with padding) — unlike
	// RawStdEncoding, no slicing of the output is required.
	encoded := make([]byte, base64.StdEncoding.EncodedLen(buf.Len()))
	base64.StdEncoding.Encode(encoded, buf.Bytes())
	return encoded, nil
}

// DecodePendingTypeConfirmation reverses Encode: base64-decode, gunzip,
// JSON-unmarshal. Used by the Resume flow to reconstruct pipeline state
// from Redis (high-architecture.md §6.10).
func DecodePendingTypeConfirmation(encoded []byte) (*PendingTypeConfirmation, error) {
	if len(encoded) == 0 {
		return nil, fmt.Errorf("decode pending: empty input")
	}

	gzipped := make([]byte, base64.StdEncoding.DecodedLen(len(encoded)))
	n, err := base64.StdEncoding.Decode(gzipped, encoded)
	if err != nil {
		return nil, fmt.Errorf("decode pending: base64 decode: %w", err)
	}
	gzipped = gzipped[:n]

	gz, err := gzip.NewReader(bytes.NewReader(gzipped))
	if err != nil {
		return nil, fmt.Errorf("decode pending: gzip open: %w", err)
	}
	defer func() { _ = gz.Close() }()

	raw, err := io.ReadAll(gz)
	if err != nil {
		return nil, fmt.Errorf("decode pending: gzip read: %w", err)
	}

	var p PendingTypeConfirmation
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("decode pending: json unmarshal: %w", err)
	}
	return &p, nil
}
