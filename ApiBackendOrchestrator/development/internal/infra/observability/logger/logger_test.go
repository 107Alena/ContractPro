package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

// newTestLogger creates a Logger that writes JSON to the returned buffer
// instead of os.Stdout, using the same shared constructor as production.
func newTestLogger(level string) (*Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	return newLogger(level, buf), buf
}

// parseLine parses a single JSON log line into a map.
func parseLine(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("failed to parse JSON log line: %v\nraw: %s", err, buf.String())
	}
	return m
}

// --- RequestContext tests ---

func TestWithRequestContext_RoundTrip(t *testing.T) {
	rc := RequestContext{
		CorrelationID:  "corr-123",
		OrganizationID: "org-456",
		UserID:         "user-789",
		DocumentID:     "doc-001",
		VersionID:      "ver-002",
		JobID:          "job-003",
	}
	ctx := WithRequestContext(context.Background(), rc)
	got := RequestContextFrom(ctx)

	if got != rc {
		t.Errorf("round-trip mismatch:\ngot:  %+v\nwant: %+v", got, rc)
	}
}

func TestRequestContextFrom_EmptyContext(t *testing.T) {
	got := RequestContextFrom(context.Background())
	if got != (RequestContext{}) {
		t.Errorf("expected zero-value RequestContext, got: %+v", got)
	}
}

func TestWithDocumentID(t *testing.T) {
	rc := RequestContext{CorrelationID: "corr-1", UserID: "user-1"}
	ctx := WithRequestContext(context.Background(), rc)
	ctx = WithDocumentID(ctx, "doc-new")

	got := RequestContextFrom(ctx)
	if got.DocumentID != "doc-new" {
		t.Errorf("DocumentID = %q, want %q", got.DocumentID, "doc-new")
	}
	if got.CorrelationID != "corr-1" {
		t.Errorf("CorrelationID lost: got %q", got.CorrelationID)
	}
	if got.UserID != "user-1" {
		t.Errorf("UserID lost: got %q", got.UserID)
	}
}

func TestWithVersionID(t *testing.T) {
	ctx := WithRequestContext(context.Background(), RequestContext{CorrelationID: "c"})
	ctx = WithVersionID(ctx, "v-42")

	got := RequestContextFrom(ctx)
	if got.VersionID != "v-42" {
		t.Errorf("VersionID = %q, want %q", got.VersionID, "v-42")
	}
	if got.CorrelationID != "c" {
		t.Errorf("CorrelationID lost")
	}
}

func TestWithJobID(t *testing.T) {
	ctx := WithRequestContext(context.Background(), RequestContext{OrganizationID: "org"})
	ctx = WithJobID(ctx, "job-77")

	got := RequestContextFrom(ctx)
	if got.JobID != "job-77" {
		t.Errorf("JobID = %q, want %q", got.JobID, "job-77")
	}
	if got.OrganizationID != "org" {
		t.Errorf("OrganizationID lost")
	}
}

func TestWithDocumentID_NoExistingContext(t *testing.T) {
	ctx := WithDocumentID(context.Background(), "doc-orphan")
	got := RequestContextFrom(ctx)
	if got.DocumentID != "doc-orphan" {
		t.Errorf("DocumentID = %q, want %q", got.DocumentID, "doc-orphan")
	}
}

// --- Logger mandatory fields tests ---

func TestLogger_MandatoryFields_ServiceAndLevel(t *testing.T) {
	l, buf := newTestLogger("info")
	l.Info(context.Background(), "hello")

	m := parseLine(t, buf)

	// time field (slog uses "time" key)
	if _, ok := m["time"]; !ok {
		t.Error("missing 'time' field")
	}
	// level
	if m["level"] != "INFO" {
		t.Errorf("level = %v, want INFO", m["level"])
	}
	// service
	if m["service"] != "api-orchestrator" {
		t.Errorf("service = %v, want api-orchestrator", m["service"])
	}
	// message (slog uses "msg" key)
	if m["msg"] != "hello" {
		t.Errorf("msg = %v, want hello", m["msg"])
	}
}

func TestLogger_ContextFieldsIncluded(t *testing.T) {
	l, buf := newTestLogger("info")

	rc := RequestContext{
		CorrelationID:  "corr-abc",
		OrganizationID: "org-def",
		UserID:         "user-ghi",
		DocumentID:     "doc-jkl",
		VersionID:      "ver-mno",
		JobID:          "job-pqr",
	}
	ctx := WithRequestContext(context.Background(), rc)
	l.Info(ctx, "test")

	m := parseLine(t, buf)

	checks := map[string]string{
		"correlation_id":  "corr-abc",
		"organization_id": "org-def",
		"user_id":         "user-ghi",
		"document_id":     "doc-jkl",
		"version_id":      "ver-mno",
		"job_id":          "job-pqr",
	}
	for key, want := range checks {
		got, ok := m[key]
		if !ok {
			t.Errorf("missing field %q", key)
			continue
		}
		if got != want {
			t.Errorf("%s = %v, want %v", key, got, want)
		}
	}
}

func TestLogger_EmptyContextFields_Omitted(t *testing.T) {
	l, buf := newTestLogger("info")

	// Only CorrelationID set, others empty.
	rc := RequestContext{CorrelationID: "corr-only"}
	ctx := WithRequestContext(context.Background(), rc)
	l.Info(ctx, "partial")

	m := parseLine(t, buf)

	if m["correlation_id"] != "corr-only" {
		t.Errorf("correlation_id = %v, want corr-only", m["correlation_id"])
	}
	// Empty fields should not be present.
	for _, key := range []string{"organization_id", "user_id", "document_id", "version_id", "job_id"} {
		if _, ok := m[key]; ok {
			t.Errorf("field %q should be omitted for empty value", key)
		}
	}
}

func TestLogger_NoContext_NoContextFields(t *testing.T) {
	l, buf := newTestLogger("info")
	l.Info(context.Background(), "bare")

	m := parseLine(t, buf)

	for _, key := range []string{"correlation_id", "organization_id", "user_id", "document_id", "version_id", "job_id"} {
		if _, ok := m[key]; ok {
			t.Errorf("field %q should not be present without RequestContext", key)
		}
	}
}

// --- Log level tests ---

func TestLogger_LevelFiltering_DebugNotShownAtInfo(t *testing.T) {
	l, buf := newTestLogger("info")
	l.Debug(context.Background(), "should not appear")

	if buf.Len() != 0 {
		t.Errorf("debug message should be filtered at info level, got: %s", buf.String())
	}
}

func TestLogger_LevelFiltering_DebugShownAtDebug(t *testing.T) {
	l, buf := newTestLogger("debug")
	l.Debug(context.Background(), "visible")

	if buf.Len() == 0 {
		t.Error("debug message should be visible at debug level")
	}
	m := parseLine(t, buf)
	if m["level"] != "DEBUG" {
		t.Errorf("level = %v, want DEBUG", m["level"])
	}
}

func TestLogger_WarnLevel(t *testing.T) {
	l, buf := newTestLogger("info")
	l.Warn(context.Background(), "caution")

	m := parseLine(t, buf)
	if m["level"] != "WARN" {
		t.Errorf("level = %v, want WARN", m["level"])
	}
}

func TestLogger_ErrorLevel(t *testing.T) {
	l, buf := newTestLogger("info")
	l.Error(context.Background(), "failure")

	m := parseLine(t, buf)
	if m["level"] != "ERROR" {
		t.Errorf("level = %v, want ERROR", m["level"])
	}
}

func TestLogger_ErrorLevelFilteredAtErrorOnly(t *testing.T) {
	l, buf := newTestLogger("error")

	l.Info(context.Background(), "skip")
	if buf.Len() != 0 {
		t.Error("info should be filtered at error level")
	}

	l.Warn(context.Background(), "skip")
	if buf.Len() != 0 {
		t.Error("warn should be filtered at error level")
	}

	l.Error(context.Background(), "visible")
	if buf.Len() == 0 {
		t.Error("error should be visible at error level")
	}
}

// --- parseLevel tests ---

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"  Debug ", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"INFO", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"WARN", slog.LevelWarn},
		{"error", slog.LevelError},
		{"ERROR", slog.LevelError},
		{"", slog.LevelInfo},
		{"unknown", slog.LevelInfo},
		{"trace", slog.LevelInfo},
	}

	for _, tt := range tests {
		got := parseLevel(tt.input)
		if got != tt.want {
			t.Errorf("parseLevel(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

// --- With() child logger tests ---

func TestLogger_With_Component(t *testing.T) {
	l, buf := newTestLogger("info")
	child := l.With("component", "dm-client")
	child.Info(context.Background(), "child log")

	m := parseLine(t, buf)
	if m["component"] != "dm-client" {
		t.Errorf("component = %v, want dm-client", m["component"])
	}
	if m["service"] != "api-orchestrator" {
		t.Errorf("service should be inherited, got %v", m["service"])
	}
}

func TestLogger_With_MultipleAttrs(t *testing.T) {
	l, buf := newTestLogger("info")
	child := l.With("component", "upload-coordinator", "instance", "a1")
	child.Info(context.Background(), "multi")

	m := parseLine(t, buf)
	if m["component"] != "upload-coordinator" {
		t.Errorf("component = %v", m["component"])
	}
	if m["instance"] != "a1" {
		t.Errorf("instance = %v", m["instance"])
	}
}

func TestLogger_With_ContextAndComponent(t *testing.T) {
	l, buf := newTestLogger("info")
	child := l.With("component", "event-consumer")

	rc := RequestContext{
		CorrelationID:  "corr-combo",
		OrganizationID: "org-combo",
	}
	ctx := WithRequestContext(context.Background(), rc)
	child.Info(ctx, "combined")

	m := parseLine(t, buf)
	if m["component"] != "event-consumer" {
		t.Errorf("component = %v", m["component"])
	}
	if m["correlation_id"] != "corr-combo" {
		t.Errorf("correlation_id = %v", m["correlation_id"])
	}
	if m["organization_id"] != "org-combo" {
		t.Errorf("organization_id = %v", m["organization_id"])
	}
	if m["service"] != "api-orchestrator" {
		t.Errorf("service = %v", m["service"])
	}
}

// --- ErrorAttr tests ---

func TestErrorAttr_NonNil(t *testing.T) {
	attr := ErrorAttr(errors.New("connection refused"))
	if attr.Key != "error" {
		t.Errorf("key = %q, want 'error'", attr.Key)
	}
	if attr.Value.String() != "connection refused" {
		t.Errorf("value = %q, want 'connection refused'", attr.Value.String())
	}
}

func TestErrorAttr_Nil(t *testing.T) {
	attr := ErrorAttr(nil)
	if attr.Key != "error" {
		t.Errorf("key = %q, want 'error'", attr.Key)
	}
	if attr.Value.Kind() != slog.KindAny {
		t.Errorf("value kind = %v, want KindAny (for nil)", attr.Value.Kind())
	}
	if attr.Value.Any() != nil {
		t.Errorf("value = %v, want nil", attr.Value.Any())
	}
}

func TestErrorAttr_Nil_InLogOutput(t *testing.T) {
	l, buf := newTestLogger("info")
	l.Info(context.Background(), "success", ErrorAttr(nil))

	m := parseLine(t, buf)
	// JSON null is parsed as nil by encoding/json.
	if m["error"] != nil {
		t.Errorf("error = %v, want null", m["error"])
	}
	// But the key should be present.
	if _, ok := m["error"]; !ok {
		t.Error("error field should be present (as null)")
	}
}

func TestErrorAttr_InLogOutput(t *testing.T) {
	l, buf := newTestLogger("info")
	l.Error(context.Background(), "dm failed", ErrorAttr(errors.New("timeout")))

	m := parseLine(t, buf)
	if m["error"] != "timeout" {
		t.Errorf("error = %v, want 'timeout'", m["error"])
	}
}

// --- Slog() escape hatch test ---

func TestLogger_Slog_NotNil(t *testing.T) {
	l, _ := newTestLogger("info")
	if l.Slog() == nil {
		t.Error("Slog() returned nil")
	}
}

// --- Extra args passthrough test ---

func TestLogger_ExtraArgs_Passthrough(t *testing.T) {
	l, buf := newTestLogger("info")

	rc := RequestContext{CorrelationID: "corr-extra"}
	ctx := WithRequestContext(context.Background(), rc)
	l.Info(ctx, "with extras", "duration_ms", 42, "status_code", 200)

	m := parseLine(t, buf)
	if m["correlation_id"] != "corr-extra" {
		t.Errorf("correlation_id = %v", m["correlation_id"])
	}
	// JSON numbers are float64.
	if m["duration_ms"] != float64(42) {
		t.Errorf("duration_ms = %v, want 42", m["duration_ms"])
	}
	if m["status_code"] != float64(200) {
		t.Errorf("status_code = %v, want 200", m["status_code"])
	}
}

// --- JSON format test ---

func TestLogger_OutputIsValidJSON(t *testing.T) {
	l, buf := newTestLogger("info")

	rc := RequestContext{
		CorrelationID:  "c-json",
		OrganizationID: "o-json",
		UserID:         "u-json",
	}
	ctx := WithRequestContext(context.Background(), rc)
	l.Info(ctx, "json check", "key", "value")

	raw := buf.String()
	if !strings.HasPrefix(strings.TrimSpace(raw), "{") {
		t.Errorf("output is not JSON: %s", raw)
	}

	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Errorf("invalid JSON: %v", err)
	}
}

// --- NewLogger with default level test ---

func TestNewLogger_DefaultLevel(t *testing.T) {
	// NewLogger writes to os.Stdout, so we can't capture its output easily.
	// Instead, verify it doesn't panic and returns non-nil.
	l := NewLogger("info")
	if l == nil {
		t.Fatal("NewLogger returned nil")
	}
	if l.inner == nil {
		t.Fatal("inner slog.Logger is nil")
	}
}

func TestNewLogger_UnknownLevel_DefaultsToInfo(t *testing.T) {
	// Verify unknown level doesn't panic.
	l := NewLogger("garbage")
	if l == nil {
		t.Fatal("NewLogger returned nil for unknown level")
	}
}
