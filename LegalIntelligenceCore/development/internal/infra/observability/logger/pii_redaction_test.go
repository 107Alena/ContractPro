package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"contractpro/legal-intelligence-core/internal/config"
)

// pii_redaction_test.go covers LIC-TASK-052 acceptance criteria #1, #4, #5
// and test_steps 2-3:
//
//   - попытка залогировать `key_parameters`, `risks[].description`,
//     `parties`, raw LLM response, API ключ → редактируется или log fails;
//   - `raw_response_hash` HMAC-SHA-256 first 1024 chars;
//   - error sanitization end-to-end: LLM error с API key в URL → log clean.
//
// The runtime test cannot DROP a forbidden-key attribute (the handler is
// allowlist-by-discipline per security.md §6.3, not a runtime filter) — so
// the runtime tests here assert two related guarantees:
//
//   1. Any forbidden-shaped CONTENT (API keys, bearer tokens, ?key= URLs)
//      logged via the auto-sanitized keys (KeyError / KeyErrorMessage /
//      KeyRequestBody / KeyResponseBody) is redacted by Sanitize.
//   2. raw_response_hash uses HashContent (HMAC-SHA-256 byte-truncated),
//      never raw content.
//
// The static-analyzer (static_analyzer_test.go) is the bulwark that prevents
// forbidden keys from being written at call sites in the first place.

func newPIITestLogger(t *testing.T) (*Logger, *bytes.Buffer) {
	t.Helper()
	buf := &bytes.Buffer{}
	cfg := config.AppConfig{LogLevel: "debug", Env: config.EnvLocal, HTTPPort: 8080}
	l, err := NewWithWriter(cfg, buf)
	if err != nil {
		t.Fatalf("NewWithWriter: %v", err)
	}
	return l, buf
}

// piiSamples is a small fixture set mimicking the kinds of payloads
// security.md §6.2 forbids logging. The strings purposely combine ИНН,
// names, prices — the test treats these as opaque "PII content" and only
// asserts they don't escape through the allowlisted auto-sanitized
// channels in their entirety. The actual structural defense (don't log
// these keys at all) is enforced by the AST analyzer test.
var piiSamples = map[string]string{
	"key_parameters_serialised": `{"parties":[{"role":"Заказчик","inn":"7707083893"}],"price":"1500000 RUB"}`,
	"risk_description":          `Сторона "ООО Заказчик" (ИНН 7707083893) обязана выплатить 1 500 000 руб.`,
	"parties_block":             `Иванов Иван Иванович, паспорт 4509 123456, ИНН 770708388300`,
	"semantic_tree_excerpt":     `{"node":"clause-3.1","text":"Заказчик: ИНН 7707083893"}`,
	"raw_llm_response":          `{"contract_type":"АГЕНТСКИЙ","parties":[{"name":"Иванов И.И."}]}`,
}

// TestPII_RawResponseHash_NeverLeaksContent asserts the LIC-TASK-052
// criterion #4 mechanism: at the point a call site would be tempted to log
// an invalid LLM response, the safe primitive is HashContent + log under
// the allowlisted `raw_response_hash` key. The hash field is opaque
// (deterministic but not invertible).
func TestPII_RawResponseHash_NeverLeaksContent(t *testing.T) {
	log, buf := newPIITestLogger(t)
	key := []byte("test-hash-key-32-bytes-XXXXXXXXX")

	for label, raw := range piiSamples {
		buf.Reset()
		h := HashContent(raw, key)
		// Allowlisted shape: hash only, the raw value never reaches slog.
		log.Warn(context.Background(), "agent invalid output",
			slog.String("raw_response_hash", h))

		// Hash present and shaped correctly.
		got := decodeOne(t, buf)
		if got["raw_response_hash"] != h {
			t.Errorf("[%s] raw_response_hash absent / wrong: got=%v want=%s",
				label, got["raw_response_hash"], h)
		}
		if len(h) != 64 {
			t.Errorf("[%s] hash length = %d, want 64", label, len(h))
		}

		// Raw content must not appear anywhere in the line.
		assertNoFragments(t, label, buf.String(), raw)
	}
}

// TestPII_APIKey_InErrorURL_RedactedEndToEnd covers criterion #5: an LLM
// HTTP client wrapping the request URL in an error message must never
// surface the API key in the log line, regardless of which provider the
// secret belongs to.
func TestPII_APIKey_InErrorURL_RedactedEndToEnd(t *testing.T) {
	cases := []struct {
		name   string
		secret string
		errMsg string
	}{
		{
			name:   "gemini_query_key",
			secret: "AIzaSyA1B2C3D4E5F6G7H8I9J0K1L2M3N4O5P6Q7R",
			errMsg: "dial GET https://generativelanguage.googleapis.com/v1/models?key=AIzaSyA1B2C3D4E5F6G7H8I9J0K1L2M3N4O5P6Q7R&alt=json: 401",
		},
		{
			name:   "openai_authorization_bearer",
			secret: "sk-proj-AbCdEfGh-1234567890_QWERTYuiopASDFGH",
			errMsg: "POST https://api.openai.com/v1/chat/completions: Authorization: Bearer sk-proj-AbCdEfGh-1234567890_QWERTYuiopASDFGH failed",
		},
		{
			name:   "anthropic_x_api_key_header",
			secret: "sk-ant-api03-leaktest-XYZ_payload",
			errMsg: "POST https://api.anthropic.com/v1/messages: x-api-key sk-ant-api03-leaktest-XYZ_payload rejected with 401",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			log, buf := newPIITestLogger(t)
			err := errors.New(tc.errMsg)

			log.Error(context.Background(), "llm call failed", slog.Any(KeyError, err))

			raw := buf.String()
			if strings.Contains(raw, tc.secret) {
				t.Fatalf("API key leaked: %s", raw)
			}
			if !strings.Contains(raw, redactedMarker) {
				t.Fatalf("redaction marker missing: %s", raw)
			}
		})
	}
}

// TestPII_AllowlistAttrs_PassThroughClean asserts the positive half of the
// allowlist: when call sites use only the allowlisted metadata keys, the
// emitted JSON is unmolested (no false-positive sanitization). This is the
// "happy path" companion of TestPII_APIKey_InErrorURL_RedactedEndToEnd —
// it pins that we don't over-redact valid IDs that happen to share
// prefixes with secret patterns.
func TestPII_AllowlistAttrs_PassThroughClean(t *testing.T) {
	log, buf := newPIITestLogger(t)
	rc := RequestContext{
		CorrelationID:  "corr-allowlist",
		JobID:          "job-allowlist",
		OrganizationID: "org-allowlist",
		VersionID:      "ver-allowlist",
	}
	ctx := WithRequestContext(context.Background(), rc)

	log.Info(ctx, "agent invocation completed",
		slog.String(KeyAgentID, "AGENT_RISK_DETECTION"),
		slog.String(KeyProviderID, "claude"),
		slog.String("model", "claude-sonnet-4-6"),
		slog.Int("tokens_in", 15234),
		slog.Int("tokens_out", 2891),
		slog.Int64("latency_ms", 4321),
		slog.Float64("cost_usd", 0.0421),
		slog.String(KeyStage, "STAGE_AGENT_RISK_DETECTION"),
		slog.String(KeyOutcome, "success"),
		slog.Bool("prompt_injection_detected", false),
	)

	got := decodeOne(t, buf)

	checks := map[string]any{
		KeyCorrelationID:            "corr-allowlist",
		KeyJobID:                    "job-allowlist",
		KeyOrganizationID:           "org-allowlist",
		KeyVersionID:                "ver-allowlist",
		KeyAgentID:                  "AGENT_RISK_DETECTION",
		KeyProviderID:               "claude",
		"model":                     "claude-sonnet-4-6",
		"tokens_in":                 float64(15234),
		"tokens_out":                float64(2891),
		"latency_ms":                float64(4321),
		KeyStage:                    "STAGE_AGENT_RISK_DETECTION",
		KeyOutcome:                  "success",
		"prompt_injection_detected": false,
	}
	for k, want := range checks {
		if got[k] != want {
			t.Errorf("field %q = %v (%T), want %v", k, got[k], got[k], want)
		}
	}
	// Spot-check the float — JSON numbers parse back as float64.
	if cost, ok := got["cost_usd"].(float64); !ok || cost < 0.04 || cost > 0.05 {
		t.Errorf("cost_usd = %v, want ~0.0421", got["cost_usd"])
	}
}

// TestPII_ContentInSanitizedChannel_StillSanitized covers a defense-in-depth
// angle: even when a call site (incorrectly) packs PII-looking content into
// one of the auto-sanitized keys (KeyError / KeyErrorMessage / KeyRequestBody
// / KeyResponseBody), any embedded API key or bearer token is redacted.
// The PII itself (names, ИНН) is NOT redacted by Sanitize — that's a
// regex-based redactor for SECRETS, not for personal data. The deny-list
// + AST analyzer is what keeps personal-data fields out of logs.
func TestPII_ContentInSanitizedChannel_StillSanitized(t *testing.T) {
	log, buf := newPIITestLogger(t)
	mixed := "request to https://api.openai.com?key=AIzaSyDEADBEEFCAFEBABE0123456789ABCDEF12345 failed: Иванов И.И."

	log.Error(context.Background(), "external call failed",
		slog.String(KeyError, mixed))

	got := decodeOne(t, buf)
	errStr, _ := got[KeyError].(string)
	if strings.Contains(errStr, "AIzaSyDEADBEEFCAFEBABE0123456789ABCDEF12345") {
		t.Fatalf("API key leaked through KeyError: %q", errStr)
	}
	if !strings.Contains(errStr, redactedMarker) {
		t.Fatalf("redaction marker missing: %q", errStr)
	}
}

// TestPII_ForbiddenKeysList_NonEmpty is a sanity check: the deny-list must
// not be empty (a future refactor that accidentally clears it would silently
// disable the static analyzer test).
func TestPII_ForbiddenKeysList_NonEmpty(t *testing.T) {
	if len(ForbiddenLogKeys) == 0 {
		t.Fatal("ForbiddenLogKeys is empty — analyzer would never flag anything")
	}
	required := []string{
		"key_parameters", "risks", "parties", "subject", "price",
		"extracted_text", "semantic_tree", "raw_llm_response",
	}
	have := make(map[string]struct{}, len(ForbiddenLogKeys))
	for _, k := range ForbiddenLogKeys {
		have[k] = struct{}{}
	}
	for _, k := range required {
		if _, ok := have[k]; !ok {
			t.Errorf("ForbiddenLogKeys missing security.md §6.2 entry %q", k)
		}
	}
}

// --- helpers ---

func decodeOne(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	line := strings.TrimSpace(buf.String())
	if line == "" {
		t.Fatal("no log line emitted")
	}
	// First non-empty line; tests in this file emit exactly one record per
	// log call.
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatalf("invalid JSON %q: %v", line, err)
	}
	return m
}

// assertNoFragments fails the test if any "long enough" substring of `raw`
// surfaces verbatim in the log line. We slice raw into rolling 24-byte
// windows — short enough to catch a partial leak, long enough that the
// allowlisted ID fields don't accidentally match a generic substring.
func assertNoFragments(t *testing.T, label, line, raw string) {
	t.Helper()
	const window = 24
	if len(raw) < window {
		// Whole-string check.
		if strings.Contains(line, raw) {
			t.Errorf("[%s] raw payload leaked verbatim: %s", label, line)
		}
		return
	}
	// Cap probe count so the test stays fast on big inputs.
	probes := 0
	for i := 0; i+window <= len(raw); i += window / 2 {
		probes++
		if probes > 80 {
			break
		}
		frag := raw[i : i+window]
		// Skip fragments that are pure whitespace/punctuation — they're not
		// signal.
		if strings.TrimSpace(frag) == "" {
			continue
		}
		if strings.Contains(line, frag) {
			t.Fatalf("[%s] raw payload fragment leaked: %q in:\n%s",
				label, frag, line)
		}
	}
}
