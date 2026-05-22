package logger

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestHashContent_DeterministicAndKeyed(t *testing.T) {
	key := []byte("secret-key-32-bytes-XXXXXXXXXXXX")
	content := "agent invalid output payload"

	h1 := HashContent(content, key)
	h2 := HashContent(content, key)
	if h1 != h2 {
		t.Fatalf("HashContent is not deterministic: %q vs %q", h1, h2)
	}

	otherKey := []byte("a-different-secret-key-32-bytes-Y")
	h3 := HashContent(content, otherKey)
	if h3 == h1 {
		t.Fatalf("HashContent ignored key change: %q == %q", h1, h3)
	}
}

func TestHashContent_TruncatesAtMaxBytes(t *testing.T) {
	key := []byte("k")
	// Content longer than the 1024-byte cap. The tail must NOT influence the
	// digest, because security.md §6.4 hashes only the first 1024 chars.
	prefix := strings.Repeat("a", hashContentMaxBytes)
	longer := prefix + strings.Repeat("b", 4096)

	if got, want := HashContent(longer, key), HashContent(prefix, key); got != want {
		t.Fatalf("tail past byte %d altered the digest:\n got=%s\nwant=%s",
			hashContentMaxBytes, got, want)
	}
}

func TestHashContent_BelowCapHashesEverything(t *testing.T) {
	key := []byte("k")
	in := strings.Repeat("x", hashContentMaxBytes-1)

	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(in))
	want := hex.EncodeToString(mac.Sum(nil))

	if got := HashContent(in, key); got != want {
		t.Fatalf("under-cap input mismatch:\n got=%s\nwant=%s", got, want)
	}
}

func TestHashContent_EmptyKeyReturnsEmpty(t *testing.T) {
	// security.md §6.4 explicitly relies on a per-deployment key to defeat
	// rainbow-table re-identification. An empty key must surface in the log
	// line (empty hash field) so the misconfiguration is visible and
	// caught upstream by SecurityConfig.validate.
	if got := HashContent("anything", nil); got != "" {
		t.Fatalf("HashContent(_, nil) = %q, want empty", got)
	}
	if got := HashContent("anything", []byte{}); got != "" {
		t.Fatalf("HashContent(_, []byte{}) = %q, want empty", got)
	}
}

func TestHashContent_EmptyContentHashesStably(t *testing.T) {
	key := []byte("k")
	h1 := HashContent("", key)
	h2 := HashContent("", key)
	if h1 == "" {
		t.Fatalf("empty content with valid key returned empty hash — should produce a stable digest")
	}
	if h1 != h2 {
		t.Fatalf("empty-content hash is not deterministic: %q vs %q", h1, h2)
	}
	if len(h1) != sha256.Size*2 {
		t.Fatalf("expected %d hex chars (SHA-256), got %d: %q", sha256.Size*2, len(h1), h1)
	}
}

func TestHashContent_OutputShape(t *testing.T) {
	key := []byte("k")
	h := HashContent("hello", key)
	// SHA-256 = 32 bytes = 64 lowercase hex chars.
	if len(h) != sha256.Size*2 {
		t.Errorf("hash length = %d, want %d", len(h), sha256.Size*2)
	}
	for _, r := range h {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			t.Errorf("non-lowercase-hex char %q in hash %q", r, h)
			break
		}
	}
}

func TestHashContent_UnicodeByteTruncation(t *testing.T) {
	// Multi-byte runes straddling the 1024-byte boundary must not panic — we
	// hash bytes, not runes. A 'П' (Cyrillic П) is 2 bytes in UTF-8.
	key := []byte("k")
	in := strings.Repeat("П", 600) // 1200 bytes; cap kicks in at 1024.
	// Must not panic.
	h := HashContent(in, key)
	if len(h) != sha256.Size*2 {
		t.Fatalf("unexpected hash shape on unicode input: %q", h)
	}

	// And the byte-prefix and the full input must produce the same digest.
	bytesPrefix := in[:hashContentMaxBytes]
	if got := HashContent(bytesPrefix, key); got != h {
		t.Fatalf("unicode truncation digest mismatch:\n got=%s\nwant=%s", got, h)
	}
}

func TestHashContent_MatchesSpecSample(t *testing.T) {
	// security.md §6.4 sample (translated to byte-truncation):
	//   mac := hmac.New(sha256.New, key)
	//   mac.Write([]byte(rawResponse[:min(len, 1024)]))
	//   hex.EncodeToString(mac.Sum(nil))
	//
	// HashContent must be the exact same primitive — pin it.
	key := []byte("LIC_DLQ_HASH_KEY-test-value-12345")
	raw := strings.Repeat("Иванов ИНН 7707083893\n", 200) // > 1024 bytes.

	mac := hmac.New(sha256.New, key)
	body := raw
	if len(body) > 1024 {
		body = body[:1024]
	}
	_, _ = mac.Write([]byte(body))
	want := hex.EncodeToString(mac.Sum(nil))

	if got := HashContent(raw, key); got != want {
		t.Fatalf("HashContent diverges from §6.4 reference:\n got=%s\nwant=%s", got, want)
	}
}
