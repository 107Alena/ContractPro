package dlq

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

// TestHashPayload_Determinism pins that two calls with the same (payload, key)
// produce byte-for-byte identical output — the reproducibility contract
// task acceptance step 3 ("HMAC reproducible с одинаковым key").
func TestHashPayload_Determinism(t *testing.T) {
	t.Parallel()
	payload := []byte(`{"correlation_id":"abc","error":"boom"}`)
	key := []byte("test-dlq-hmac-secret-32-bytes-xx")
	first := HashPayload(payload, key)
	second := HashPayload(payload, key)
	if first != second {
		t.Fatalf("HashPayload not deterministic: %q != %q", first, second)
	}
	if len(first) != 64 {
		t.Fatalf("HashPayload returned %d hex chars, want 64 (SHA-256 = 32 bytes)", len(first))
	}
}

// TestHashPayload_DifferentKeysDiffer pins that the same payload under two
// different keys produces different digests — the §6.4 rainbow-table
// resistance contract.
func TestHashPayload_DifferentKeysDiffer(t *testing.T) {
	t.Parallel()
	payload := []byte("identical payload")
	a := HashPayload(payload, []byte("key-A"))
	b := HashPayload(payload, []byte("key-B"))
	if a == b {
		t.Fatalf("HashPayload produced identical digest under different keys: %q", a)
	}
}

// TestHashPayload_DifferentPayloadsDiffer pins that different payloads under
// the same key produce different digests (basic HMAC sanity).
func TestHashPayload_DifferentPayloadsDiffer(t *testing.T) {
	t.Parallel()
	key := []byte("same key")
	a := HashPayload([]byte("payload one"), key)
	b := HashPayload([]byte("payload two"), key)
	if a == b {
		t.Fatalf("HashPayload produced identical digest for different payloads: %q", a)
	}
}

// TestHashPayload_EmptyKeyEmptyHash pins the defensive misconfig-visible
// behaviour: an empty key returns an empty string (NOT a weak unkeyed digest).
// SecurityConfig.validate enforces the key at startup; this is defense-in-depth.
func TestHashPayload_EmptyKeyEmptyHash(t *testing.T) {
	t.Parallel()
	out := HashPayload([]byte("payload"), nil)
	if out != "" {
		t.Errorf("HashPayload(_, nil) = %q, want \"\"", out)
	}
	out = HashPayload([]byte("payload"), []byte{})
	if out != "" {
		t.Errorf("HashPayload(_, []byte{}) = %q, want \"\"", out)
	}
}

// TestHashPayload_NilPayloadValid pins that a nil payload is a legal input
// — the HMAC over zero bytes is well-defined. Real call sites should never
// pass nil (the consumer always has the raw bytes it received), but a panic
// here would be a poor failure mode for a defensive forensics primitive.
func TestHashPayload_NilPayloadValid(t *testing.T) {
	t.Parallel()
	out := HashPayload(nil, []byte("k"))
	if len(out) != 64 {
		t.Fatalf("HashPayload(nil, key) returned %d hex chars, want 64", len(out))
	}
	// Compare to HMAC-SHA-256(k, []) for SSOT.
	mac := hmac.New(sha256.New, []byte("k"))
	_, _ = mac.Write(nil)
	want := hex.EncodeToString(mac.Sum(nil))
	if out != want {
		t.Fatalf("HashPayload(nil, key) = %q, want %q (HMAC over empty bytes)", out, want)
	}
}

// TestHashPayload_FullPayloadNotTruncated pins that HashPayload hashes the
// FULL payload — unlike HashRawLLMResponse it does NOT cap at 1024 bytes.
// A truncation here would silently make the original_message_hash collide
// for any two payloads sharing their first 1024 bytes.
func TestHashPayload_FullPayloadNotTruncated(t *testing.T) {
	t.Parallel()
	key := []byte("k")
	prefix := strings.Repeat("a", RawLLMResponseHashMaxBytes)
	a := HashPayload([]byte(prefix+"tail-A"), key)
	b := HashPayload([]byte(prefix+"tail-B"), key)
	if a == b {
		t.Fatalf("HashPayload truncated past %d bytes — tails A and B collided", RawLLMResponseHashMaxBytes)
	}
}

// TestHashPayload_KnownVector cross-checks the implementation against an
// independent HMAC-SHA-256 computation (stdlib primitive). If the call-site
// helper ever diverges from the SSOT primitive, this test fails.
func TestHashPayload_KnownVector(t *testing.T) {
	t.Parallel()
	payload := []byte("hello world")
	key := []byte("k")
	got := HashPayload(payload, key)

	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(payload)
	want := hex.EncodeToString(mac.Sum(nil))

	if got != want {
		t.Fatalf("HashPayload diverged from stdlib HMAC: got %q, want %q", got, want)
	}
}

// TestHashRawLLMResponse_Cap pins the 1024-byte input cap. Two payloads
// sharing the first 1024 bytes MUST hash identically — that is the
// deduplication contract of §6.4.
func TestHashRawLLMResponse_Cap(t *testing.T) {
	t.Parallel()
	key := []byte("k")
	prefix := strings.Repeat("x", RawLLMResponseHashMaxBytes)
	a := HashRawLLMResponse([]byte(prefix+"tail-A"), key)
	b := HashRawLLMResponse([]byte(prefix+"tail-B"), key)
	if a != b {
		t.Fatalf("HashRawLLMResponse failed to cap at %d bytes: A=%q B=%q", RawLLMResponseHashMaxBytes, a, b)
	}
	// Sanity: a different prefix MUST differ.
	c := HashRawLLMResponse([]byte(strings.Repeat("y", RawLLMResponseHashMaxBytes)+"tail-A"), key)
	if c == a {
		t.Fatal("HashRawLLMResponse produced identical digest under different first-1024-byte content")
	}
}

// TestHashRawLLMResponse_ShortPayload pins that inputs under the cap are
// hashed verbatim (no padding, no truncation).
func TestHashRawLLMResponse_ShortPayload(t *testing.T) {
	t.Parallel()
	key := []byte("k")
	short := []byte(`{"error":"unparseable JSON"}`)
	got := HashRawLLMResponse(short, key)

	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(short)
	want := hex.EncodeToString(mac.Sum(nil))

	if got != want {
		t.Fatalf("HashRawLLMResponse short-payload diverged from stdlib: got %q, want %q", got, want)
	}
	if len(got) != 64 {
		t.Fatalf("HashRawLLMResponse short returned %d hex chars, want 64", len(got))
	}
}

// TestHashRawLLMResponse_EmptyKeyEmptyHash pins the same defensive misconfig-
// visible behaviour as HashPayload.
func TestHashRawLLMResponse_EmptyKeyEmptyHash(t *testing.T) {
	t.Parallel()
	if got := HashRawLLMResponse([]byte("payload"), nil); got != "" {
		t.Errorf("HashRawLLMResponse(_, nil) = %q, want \"\"", got)
	}
	if got := HashRawLLMResponse([]byte("payload"), []byte{}); got != "" {
		t.Errorf("HashRawLLMResponse(_, []byte{}) = %q, want \"\"", got)
	}
}

// TestHashRawLLMResponse_BoundaryByte pins exact behaviour at the 1024-byte
// boundary: a 1024-byte payload (== cap) hashes the full 1024 bytes; a
// 1025-byte payload hashes only the first 1024.
func TestHashRawLLMResponse_BoundaryByte(t *testing.T) {
	t.Parallel()
	key := []byte("k")
	exactly1024 := []byte(strings.Repeat("z", RawLLMResponseHashMaxBytes))
	exactly1025 := []byte(strings.Repeat("z", RawLLMResponseHashMaxBytes) + "X")

	gotAt := HashRawLLMResponse(exactly1024, key)
	gotPast := HashRawLLMResponse(exactly1025, key)
	if gotAt != gotPast {
		t.Fatalf("HashRawLLMResponse: 1024 and 1025-byte inputs sharing the first 1024 should hash identically; got %q vs %q", gotAt, gotPast)
	}

	// Cross-check that the cap-hash matches an independent HMAC over the
	// first 1024 bytes only.
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(exactly1024)
	want := hex.EncodeToString(mac.Sum(nil))
	if gotAt != want {
		t.Fatalf("HashRawLLMResponse at-cap diverged from stdlib HMAC over first 1024 bytes: got %q, want %q", gotAt, want)
	}
}

// TestHashRawLLMResponseHashMaxBytes_Pinned pins the 1024-byte cap constant
// against the security.md §6.4 spec value. A drift here would silently
// change the dedup behaviour across DLQ replays.
func TestHashRawLLMResponseHashMaxBytes_Pinned(t *testing.T) {
	t.Parallel()
	const specCap = 1024 // security.md §6.4 explicit
	if RawLLMResponseHashMaxBytes != specCap {
		t.Fatalf("RawLLMResponseHashMaxBytes = %d, want %d (security.md §6.4)", RawLLMResponseHashMaxBytes, specCap)
	}
}
