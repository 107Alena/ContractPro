package logger

import (
	"errors"
	"strings"
	"testing"
)

func TestSanitize_RedactsAnthropicKey(t *testing.T) {
	in := "request to claude failed: header X-API-Key sk-ant-api03-AbCdEf-12345_qwerty rejected"
	out := Sanitize(in)
	if strings.Contains(out, "sk-ant-api03-AbCdEf-12345_qwerty") {
		t.Fatalf("Anthropic key leaked: %q", out)
	}
	if !strings.Contains(out, redactedMarker) {
		t.Fatalf("redaction marker missing: %q", out)
	}
}

func TestSanitize_RedactsOpenAIKey(t *testing.T) {
	in := "openai 401: invalid api key sk-AbCdEfGhIjKlMnOpQrStUvWxYz123456 supplied"
	out := Sanitize(in)
	if strings.Contains(out, "sk-AbCdEfGhIjKlMnOpQrStUvWxYz123456") {
		t.Fatalf("OpenAI key leaked: %q", out)
	}
	if !strings.Contains(out, redactedMarker) {
		t.Fatalf("redaction marker missing: %q", out)
	}
}

func TestSanitize_RedactsGeminiKey(t *testing.T) {
	in := "gemini call: Authorization header AIzaSyA1B2C3D4E5F6G7H8I9J0K1L2M3N4O5P6Q7R rejected"
	out := Sanitize(in)
	if strings.Contains(out, "AIzaSyA1B2C3D4E5F6G7H8I9J0K1L2M3N4O5P6Q7R") {
		t.Fatalf("Gemini key leaked: %q", out)
	}
	if !strings.Contains(out, redactedMarker) {
		t.Fatalf("redaction marker missing: %q", out)
	}
}

func TestSanitize_RedactsBearerToken(t *testing.T) {
	in := "auth header: Bearer eyJhbGciOiJIUzI1NiJ9.payload.signature sent"
	out := Sanitize(in)
	if strings.Contains(out, "eyJhbGciOiJIUzI1NiJ9.payload.signature") {
		t.Fatalf("Bearer token leaked: %q", out)
	}
	if !strings.Contains(out, redactedMarker) {
		t.Fatalf("redaction marker missing: %q", out)
	}
}

func TestSanitize_RedactsBearerWithBase64StandardAlphabet(t *testing.T) {
	// Opaque bearer tokens (non-JWT) routinely contain `=`, `+`, `/`, `~`.
	// The previous (too-narrow) alphabet would truncate at `=` and leave
	// the secret tail visible.
	in := "auth: Bearer Zm9vK2Jhci9iYXo9PWp3dH4Ab2t0YQ== please retry"
	out := Sanitize(in)
	for _, frag := range []string{"Zm9vK2Jhci", "/iYXo9", "PWp3dH4"} {
		if strings.Contains(out, frag) {
			t.Fatalf("Bearer base64 fragment %q leaked: %q", frag, out)
		}
	}
}

func TestSanitize_RedactsOpenAIProjKey(t *testing.T) {
	in := "openai 401: header sk-proj-AbCdEfGh-1234567890_QWERTYuiopASDFGH rejected"
	out := Sanitize(in)
	if strings.Contains(out, "AbCdEfGh-1234567890") {
		t.Fatalf("sk-proj key leaked: %q", out)
	}
}

func TestSanitize_RedactsOpenAISvcAcctKey(t *testing.T) {
	in := "request to openai sk-svcacct-DEADBEEF_1234-secretvalue rejected"
	out := Sanitize(in)
	if strings.Contains(out, "DEADBEEF_1234-secretvalue") {
		t.Fatalf("sk-svcacct key leaked: %q", out)
	}
}

func TestSanitize_QueryKeyCaseInsensitive(t *testing.T) {
	in := "GET https://example.com/v1?KEY=AIzaSyDEADBEEFCAFEBABE0123456789ABCDEF12345"
	out := Sanitize(in)
	if strings.Contains(out, "AIzaSyDEADBEEFCAFEBABE0123456789ABCDEF12345") {
		t.Fatalf("uppercase ?KEY= not redacted: %q", out)
	}
}

func TestSanitize_RedactsQueryKeyButKeepsPrefix(t *testing.T) {
	in := "GET https://generativelanguage.googleapis.com/v1/models?key=AIzaSyA1B2C3D4E5F6G7&alt=json failed"
	out := Sanitize(in)
	if strings.Contains(out, "AIzaSyA1B2C3D4E5F6G7") {
		t.Fatalf("query key leaked: %q", out)
	}
	if !strings.Contains(out, "?key=") {
		t.Fatalf("?key= prefix lost: %q", out)
	}
	if !strings.Contains(out, "&alt=json") {
		t.Fatalf("subsequent query params lost: %q", out)
	}
}

func TestSanitize_RedactsAmpersandKeyPrefix(t *testing.T) {
	in := "url: https://example.com/v1?model=gemini&key=AIzaSyDEADBEEFCAFEBABE0123456789ABCDEF12345&debug=1"
	out := Sanitize(in)
	if strings.Contains(out, "AIzaSyDEADBEEFCAFEBABE0123456789ABCDEF12345") {
		t.Fatalf("query key leaked: %q", out)
	}
	if !strings.Contains(out, "&key=") {
		t.Fatalf("&key= prefix lost: %q", out)
	}
}

func TestSanitize_RedactsMultipleSecretsInOneMessage(t *testing.T) {
	in := "tried sk-ant-XXXX_abc then Bearer ZZZ.YYY.WWW then sk-aaaaaaaaaaaaaaaaaaaa1"
	out := Sanitize(in)
	for _, leak := range []string{"sk-ant-XXXX_abc", "Bearer ZZZ.YYY.WWW", "sk-aaaaaaaaaaaaaaaaaaaa1"} {
		if strings.Contains(out, leak) {
			t.Fatalf("leak still present (%s): %q", leak, out)
		}
	}
	if strings.Count(out, redactedMarker) < 3 {
		t.Fatalf("expected 3+ redactions, got: %q", out)
	}
}

func TestSanitize_PassesThroughCleanString(t *testing.T) {
	in := "pipeline started for job_id=abc123 organization_id=org-xyz"
	out := Sanitize(in)
	if out != in {
		t.Fatalf("clean string mutated: in=%q out=%q", in, out)
	}
}

func TestSanitize_EmptyString(t *testing.T) {
	if Sanitize("") != "" {
		t.Fatal("empty string should pass through")
	}
}

func TestSanitize_ShortSkPrefixNotMatched(t *testing.T) {
	// Real edge case: product names like "use sk-" shouldn't trigger.
	in := "the prefix sk- by itself is not a key"
	out := Sanitize(in)
	if out != in {
		t.Fatalf("false positive: %q", out)
	}
}

func TestSanitizeError_NilReturnsEmpty(t *testing.T) {
	if got := sanitizeError(nil); got != "" {
		t.Fatalf("nil error should return empty string, got %q", got)
	}
}

func TestSanitizeError_RedactsWrappedKey(t *testing.T) {
	err := errors.New("dial https://api.anthropic.com: x-api-key sk-ant-api03-secretpayload-here")
	got := sanitizeError(err)
	if strings.Contains(got, "sk-ant-api03-secretpayload-here") {
		t.Fatalf("wrapped key leaked: %q", got)
	}
}
