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

func decode(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("invalid JSON line %q: %v", line, err)
		}
		out = append(out, m)
	}
	return out
}

func newTestLogger(t *testing.T, level string) (*Logger, *bytes.Buffer) {
	t.Helper()
	buf := &bytes.Buffer{}
	cfg := config.AppConfig{LogLevel: level, Env: config.EnvLocal, HTTPPort: 8080}
	l, err := NewWithWriter(cfg, buf)
	if err != nil {
		t.Fatalf("NewWithWriter: %v", err)
	}
	return l, buf
}

func TestNewLogger_NilWriterFails(t *testing.T) {
	_, err := NewWithWriter(config.AppConfig{LogLevel: "info"}, nil)
	if err == nil {
		t.Fatal("expected error on nil writer")
	}
}

func TestLogger_EmitsMandatoryServiceField(t *testing.T) {
	log, buf := newTestLogger(t, "info")
	log.Info(context.Background(), "boot complete")
	lines := decode(t, buf)
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	if lines[0][KeyService] != ServiceName {
		t.Errorf("service field missing/wrong: %v", lines[0][KeyService])
	}
	if lines[0][slog.MessageKey] != "boot complete" {
		t.Errorf("msg field wrong: %v", lines[0][slog.MessageKey])
	}
	if lines[0]["timestamp"] == nil {
		t.Error("timestamp field missing — observability.md §2.1 mandatory")
	}
	if lines[0]["level"] != "INFO" {
		t.Errorf("level label wrong: %v", lines[0]["level"])
	}
}

func TestLogger_InjectsRequestContextFields(t *testing.T) {
	log, buf := newTestLogger(t, "info")
	rc := RequestContext{
		CorrelationID:  "corr-42",
		JobID:          "job-42",
		VersionID:      "ver-42",
		OrganizationID: "org-42",
		MessageID:      "msg-42",
	}
	ctx := WithRequestContext(context.Background(), rc)
	log.Info(ctx, "agent invocation completed")

	got := decode(t, buf)[0]
	want := map[string]string{
		KeyCorrelationID:  "corr-42",
		KeyJobID:          "job-42",
		KeyVersionID:      "ver-42",
		KeyOrganizationID: "org-42",
		KeyMessageID:      "msg-42",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("field %q = %v, want %q", k, got[k], v)
		}
	}
}

func TestLogger_OmitsEmptyContextFields(t *testing.T) {
	log, buf := newTestLogger(t, "info")
	rc := RequestContext{JobID: "only-job"}
	ctx := WithRequestContext(context.Background(), rc)
	log.Info(ctx, "partial")

	got := decode(t, buf)[0]
	if got[KeyJobID] != "only-job" {
		t.Errorf("job_id missing: %v", got[KeyJobID])
	}
	for _, k := range []string{KeyCorrelationID, KeyVersionID, KeyOrganizationID, KeyDocumentID} {
		if _, ok := got[k]; ok {
			t.Errorf("field %q should be absent for empty value, got %v", k, got[k])
		}
	}
}

func TestLogger_With_BindsComponent(t *testing.T) {
	log, buf := newTestLogger(t, "info")
	pipelineLog := log.With("pipeline.orchestrator")
	pipelineLog.Info(context.Background(), "stage transition")

	got := decode(t, buf)[0]
	if got[KeyComponent] != "pipeline.orchestrator" {
		t.Errorf("component not bound: %v", got[KeyComponent])
	}
}

func TestLogger_With_EmptyReturnsSelf(t *testing.T) {
	log, _ := newTestLogger(t, "info")
	if log.With("") != log {
		t.Error("With(\"\") should be a no-op and return the same logger")
	}
}

func TestLogger_With_NoDuplicateComponentField(t *testing.T) {
	// Regression: Logger.With is the only source of `component`. There must
	// be exactly one field in the JSON object.
	log, buf := newTestLogger(t, "info")
	scoped := log.With("agent.summary")
	scoped.Info(context.Background(), "x")

	// Count occurrences of the component key in the raw JSON line.
	if got := strings.Count(buf.String(), `"component":`); got != 1 {
		t.Fatalf("expected exactly one component field, got %d in: %s", got, buf.String())
	}
}

func TestLogger_LevelFilter(t *testing.T) {
	log, buf := newTestLogger(t, "warn")
	log.Debug(context.Background(), "should be filtered")
	log.Info(context.Background(), "should be filtered")
	log.Warn(context.Background(), "should pass")
	log.Error(context.Background(), "should pass")

	lines := decode(t, buf)
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines (warn+error), got %d:\n%s", len(lines), buf.String())
	}
	if lines[0]["level"] != "WARN" {
		t.Errorf("first line level = %v", lines[0]["level"])
	}
	if lines[1]["level"] != "ERROR" {
		t.Errorf("second line level = %v", lines[1]["level"])
	}
}

func TestLogger_ErrorAttrIsAutoSanitized_String(t *testing.T) {
	log, buf := newTestLogger(t, "info")
	err := errors.New("dial https://api.anthropic.com: x-api-key sk-ant-api03-secret_payload_here rejected")
	log.Error(context.Background(), "llm call failed", slog.String(KeyError, err.Error()))

	got := decode(t, buf)[0]
	errMsg, _ := got[KeyError].(string)
	if strings.Contains(errMsg, "sk-ant-api03-secret_payload_here") {
		t.Fatalf("sanitization did not run on error attr: %q", errMsg)
	}
	if !strings.Contains(errMsg, redactedMarker) {
		t.Fatalf("redaction marker missing in error attr: %q", errMsg)
	}
}

func TestLogger_ErrorAttrIsAutoSanitized_AnyError(t *testing.T) {
	// Regression for security finding: slog.Any("error", err) is the
	// idiomatic call shape and must NOT be a leak channel.
	log, buf := newTestLogger(t, "info")
	err := errors.New("openai 401: sk-AbCdEfGhIjKlMnOpQrStUvWxYz123456 rejected")
	log.Error(context.Background(), "llm call failed", slog.Any(KeyError, err))

	got := decode(t, buf)[0]
	errMsg, _ := got[KeyError].(string)
	if strings.Contains(errMsg, "sk-AbCdEfGhIjKlMnOpQrStUvWxYz123456") {
		t.Fatalf("KindAny error not sanitized: %q", errMsg)
	}
	if !strings.Contains(errMsg, redactedMarker) {
		t.Fatalf("redaction marker missing: %q", errMsg)
	}
}

func TestLogger_ExtraSensitiveAttrKeysAreSanitized(t *testing.T) {
	log, buf := newTestLogger(t, "info")
	log.Error(context.Background(), "request failed",
		slog.String(KeyErrorMessage, "auth failed: Bearer eyJhbGciOiJI.payload.signature=="),
		slog.String(KeyResponseBody, "401 unauthorized: AIzaSyA1B2C3D4E5F6G7H8I9J0K1L2M3N4O5P6Q7R"),
		slog.String(KeyRequestBody, "POST /v1/messages: x-api-key sk-ant-api03-leaktest-XYZ"),
	)

	raw := buf.String()
	for _, leak := range []string{
		"eyJhbGciOiJI.payload.signature==",
		"AIzaSyA1B2C3D4E5F6G7H8I9J0K1L2M3N4O5P6Q7R",
		"sk-ant-api03-leaktest-XYZ",
	} {
		if strings.Contains(raw, leak) {
			t.Fatalf("secret %q leaked through extra-sensitive key: %s", leak, raw)
		}
	}
}

func TestLogger_NonErrorAttrsLeftUntouched(t *testing.T) {
	// Strings logged under non-sensitive keys must NOT be sanitized — that
	// would mangle legitimate IDs that happen to contain "sk-" or "key=".
	log, buf := newTestLogger(t, "info")
	rawValue := "Bearer abcdef.ghi.jkl"
	log.Info(context.Background(), "trace", slog.String("custom_field", rawValue))

	got := decode(t, buf)[0]
	if got["custom_field"] != rawValue {
		t.Errorf("non-sensitive attr was mutated: %v", got["custom_field"])
	}
}

func TestLogger_MessageSanitizedAtWarnAndAbove(t *testing.T) {
	// Regression for security finding: a call site that builds the msg via
	// fmt.Sprintf("...: %v", err) must not leak a secret through r.Message.
	log, buf := newTestLogger(t, "info")
	leakyMsg := "request failed: x-api-key sk-ant-api03-leakedhere returned 401"
	log.Warn(context.Background(), leakyMsg)
	log.Error(context.Background(), leakyMsg)

	raw := buf.String()
	if strings.Contains(raw, "sk-ant-api03-leakedhere") {
		t.Fatalf("WARN/ERROR msg was not sanitized: %s", raw)
	}
}

func TestLogger_MessageNotSanitizedAtInfoLevel(t *testing.T) {
	// At INFO/DEBUG we don't sanitize msg — those logs are dev-only and
	// the cost shouldn't be paid in the hot path. Documented behaviour.
	log, buf := newTestLogger(t, "info")
	rawMsg := "boot config: Bearer abc.def.ghi"
	log.Info(context.Background(), rawMsg)

	got := decode(t, buf)[0]
	if got[slog.MessageKey] != rawMsg {
		t.Errorf("INFO msg unexpectedly mutated: %v", got[slog.MessageKey])
	}
}

func TestLogger_FatalCallsExit(t *testing.T) {
	log, buf := newTestLogger(t, "info")

	exits := 0
	originalExit := exitFn
	exitFn = func(code int) {
		exits++
		if code != 1 {
			t.Errorf("Fatal exit code = %d, want 1", code)
		}
	}
	defer func() { exitFn = originalExit }()

	log.Fatal(context.Background(), "boot config invalid")

	if exits != 1 {
		t.Fatalf("expected exitFn to be called once, got %d", exits)
	}
	got := decode(t, buf)[0]
	if got["level"] != "FATAL" {
		t.Errorf("Fatal level label wrong: %v", got["level"])
	}
}

func TestLogger_RequestContextSurvivesWith(t *testing.T) {
	log, buf := newTestLogger(t, "info")
	rc := RequestContext{JobID: "job-99"}
	ctx := WithRequestContext(context.Background(), rc)

	scoped := log.With("agent.risk_detection")
	scoped.Info(ctx, "scoped msg")

	got := decode(t, buf)[0]
	if got[KeyJobID] != "job-99" {
		t.Errorf("ctx ID lost after With: %v", got[KeyJobID])
	}
	if got[KeyComponent] != "agent.risk_detection" {
		t.Errorf("component lost: %v", got[KeyComponent])
	}
}

func TestLogger_IDsStayTopLevelAfterSlogWithGroup(t *testing.T) {
	// Regression for golang-pro finding: someone obtaining the underlying
	// *slog.Logger and calling .WithGroup must not nest the service / IDs
	// inside that group — observability.md §2.1 mandates them top-level.
	// We achieve that by making WithGroup a documented no-op (see
	// licHandler.WithGroup); IDs and user attrs alike stay top-level.
	log, buf := newTestLogger(t, "info")
	rc := RequestContext{JobID: "top-level-job", CorrelationID: "top-level-corr"}
	ctx := WithRequestContext(context.Background(), rc)

	grouped := log.Slog().WithGroup("nested")
	grouped.LogAttrs(ctx, slog.LevelInfo, "msg", slog.String("inner_field", "v"))

	got := decode(t, buf)[0]
	if got[KeyService] != ServiceName {
		t.Errorf("service field is no longer top-level after WithGroup: %v", got)
	}
	if got[KeyJobID] != "top-level-job" {
		t.Errorf("job_id is no longer top-level after WithGroup: %v", got)
	}
	if got[KeyCorrelationID] != "top-level-corr" {
		t.Errorf("correlation_id is no longer top-level after WithGroup: %v", got)
	}
	// WithGroup is a no-op — inner_field is top-level too.
	if got["inner_field"] != "v" {
		t.Errorf("inner_field lost after WithGroup no-op: %v", got)
	}
	if _, ok := got["nested"]; ok {
		t.Errorf("WithGroup should be a no-op, but `nested` group materialized: %v", got)
	}
}

func TestLogger_ExtendedBearerAlphabetSanitized(t *testing.T) {
	// Regression for security finding: opaque bearer tokens with base64
	// standard alphabet (`+`, `/`, `=`) must be redacted entirely, not
	// truncated leaving the secret tail visible.
	log, buf := newTestLogger(t, "info")
	leak := "Bearer abc+def/ghi=jklMNO_pqr.stu"
	log.Error(context.Background(), "x", slog.String(KeyError, leak))

	got := decode(t, buf)[0]
	errMsg, _ := got[KeyError].(string)
	for _, frag := range []string{"abc+def", "ghi=", "jklMNO"} {
		if strings.Contains(errMsg, frag) {
			t.Fatalf("Bearer alphabet truncated, fragment %q leaked: %q", frag, errMsg)
		}
	}
}
