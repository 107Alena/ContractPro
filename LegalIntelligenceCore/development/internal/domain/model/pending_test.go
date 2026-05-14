package model

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestPendingTypeConfirmation_EncodeDecode_RoundTrip(t *testing.T) {
	parent := "11111111-1111-1111-1111-111111111111"
	p := &PendingTypeConfirmation{
		JobID:           "j",
		DocumentID:      "d",
		VersionID:       "v",
		OrganizationID:  "o",
		CreatedByUserID: "u",
		CorrelationID:   "c",
		TraceContext:    TraceContext{TraceParent: "00-aaa-bbb-01", TraceState: "vendor=42"},
		ClassificationResult: &ClassificationResult{
			ContractType:            ContractTypeOther,
			Confidence:              0.62,
			Alternatives:            []ClassificationAlternative{{ContractType: ContractTypeSupply, Confidence: 0.55}},
			PromptInjectionDetected: false,
		},
		KeyParameters: &KeyParameters{
			Parties:                 []string{"ООО „Альфа\""},
			Subject:                 "Поставка",
			Price:                   strPtr("500 000 руб."),
			Duration:                nil,
			Penalties:               nil,
			Jurisdiction:            nil,
			PromptInjectionDetected: false,
		},
		InputArtifacts: InputArtifactsCompact{
			ArtifactSemanticTree:  json.RawMessage(`{"id":"root","children":[]}`),
			ArtifactExtractedText: json.RawMessage(`"Договор поставки от 01.04.2026..."`),
		},
		OriginType:      "RE_UPLOAD",
		ParentVersionID: &parent,
	}

	enc, err := p.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if len(enc) == 0 {
		t.Fatal("Encode returned empty bytes")
	}

	// Must be valid base64 (whole alphabet, padded).
	if _, err := base64.StdEncoding.DecodeString(string(enc)); err != nil {
		t.Fatalf("Encode output not valid base64: %v", err)
	}

	dec, err := DecodePendingTypeConfirmation(enc)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !reflect.DeepEqual(dec, p) {
		t.Fatalf("round-trip mismatch:\n got=%+v\nwant=%+v", dec, p)
	}
}

func TestPendingTypeConfirmation_GzipCompressesPayload(t *testing.T) {
	// Sanity: gzip should compress repetitive contract text below the raw size.
	bigText := strings.Repeat("Договор поставки. Стороны достигли соглашения. ", 800)
	p := &PendingTypeConfirmation{
		JobID: "j", DocumentID: "d", VersionID: "v", OrganizationID: "o", CorrelationID: "c",
		ClassificationResult: &ClassificationResult{
			ContractType:            ContractTypeSupply,
			Confidence:              0.5,
			Alternatives:            []ClassificationAlternative{},
			PromptInjectionDetected: false,
		},
		InputArtifacts: InputArtifactsCompact{
			ArtifactExtractedText: json.RawMessage(`"` + bigText + `"`),
		},
	}
	enc, err := p.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	rawJSON, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("json marshal: %v", err)
	}
	if len(enc) >= len(rawJSON) {
		t.Fatalf("expected gzip+base64 to be smaller than raw json for repetitive text: enc=%d raw=%d", len(enc), len(rawJSON))
	}
}

func TestEncode_NilReceiver_Errors(t *testing.T) {
	var p *PendingTypeConfirmation
	if _, err := p.Encode(); err == nil {
		t.Fatal("Encode on nil receiver must error")
	}
}

func TestDecode_EmptyBytes_Errors(t *testing.T) {
	if _, err := DecodePendingTypeConfirmation(nil); err == nil {
		t.Fatal("Decode nil must error")
	}
	if _, err := DecodePendingTypeConfirmation([]byte{}); err == nil {
		t.Fatal("Decode empty must error")
	}
}

func TestDecode_InvalidBase64_Errors(t *testing.T) {
	// '!' is not in the base64 alphabet.
	if _, err := DecodePendingTypeConfirmation([]byte("not-base64-!!!")); err == nil {
		t.Fatal("Decode of invalid base64 must error")
	}
}

func TestDecode_InvalidGzip_Errors(t *testing.T) {
	// Valid base64 of non-gzip content.
	bad := base64.StdEncoding.EncodeToString([]byte("not a gzip stream"))
	if _, err := DecodePendingTypeConfirmation([]byte(bad)); err == nil {
		t.Fatal("Decode of non-gzip content must error")
	}
}

func TestDecode_InvalidJSON_Errors(t *testing.T) {
	// Build a valid base64+gzip envelope around invalid JSON.
	var gzipped bytes.Buffer
	gw := gzip.NewWriter(&gzipped)
	if _, err := gw.Write([]byte("not valid json")); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	enc := base64.StdEncoding.EncodeToString(gzipped.Bytes())
	if _, err := DecodePendingTypeConfirmation([]byte(enc)); err == nil {
		t.Fatal("Decode of invalid json payload must error")
	}
}
