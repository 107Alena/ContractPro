package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
)

// newTestLogger creates a Logger backed by a slog.JSONHandler that writes to
// the provided buffer, making it possible to inspect structured JSON output.
func newTestLogger(buf *bytes.Buffer, level string) *Logger {
	return &Logger{
		inner: slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{
			Level: parseLevel(level),
		})),
	}
}

// parseLine unmarshals the last complete JSON line from the buffer.
func parseLine(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	raw := bytes.TrimSpace(buf.Bytes())
	lines := bytes.Split(raw, []byte("\n"))
	last := lines[len(lines)-1]

	var m map[string]any
	if err := json.Unmarshal(last, &m); err != nil {
		t.Fatalf("failed to parse JSON log line: %v\nraw: %s", err, last)
	}
	return m
}

// --- NewLogger level parsing ---

func TestNewLogger_levelParsing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"INFO", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"WARN", slog.LevelWarn},
		{"error", slog.LevelError},
		{"ERROR", slog.LevelError},
		{"unknown", slog.LevelInfo},
		{"", slog.LevelInfo},
		{"  debug  ", slog.LevelDebug},
	}

	for _, tc := range tests {
		t.Run("level="+tc.input, func(t *testing.T) {
			t.Parallel()
			got := parseLevel(tc.input)
			if got != tc.want {
				t.Errorf("parseLevel(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestNewLogger_returnsNonNil(t *testing.T) {
	t.Parallel()

	l := NewLogger("info")
	if l == nil {
		t.Fatal("NewLogger returned nil")
	}
}

// --- JSON output structure ---

func TestLogger_writesJSONWithExpectedFields(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	l := newTestLogger(&buf, "debug")

	l.Info(context.Background(), "test message", "key1", "val1")

	m := parseLine(t, &buf)

	if m["msg"] != "test message" {
		t.Errorf("msg = %q, want %q", m["msg"], "test message")
	}
	if m["key1"] != "val1" {
		t.Errorf("key1 = %q, want %q", m["key1"], "val1")
	}
	if _, ok := m["time"]; !ok {
		t.Error("expected 'time' field in JSON output")
	}
	if _, ok := m["level"]; !ok {
		t.Error("expected 'level' field in JSON output")
	}
}

// --- All log levels produce output ---

func TestLogger_allLevelsWriteOutput(t *testing.T) {
	t.Parallel()

	levels := []struct {
		name   string
		logFn  func(l *Logger, ctx context.Context)
		expect string
	}{
		{"debug", func(l *Logger, ctx context.Context) { l.Debug(ctx, "dbg") }, "DEBUG"},
		{"info", func(l *Logger, ctx context.Context) { l.Info(ctx, "inf") }, "INFO"},
		{"warn", func(l *Logger, ctx context.Context) { l.Warn(ctx, "wrn") }, "WARN"},
		{"error", func(l *Logger, ctx context.Context) { l.Error(ctx, "err") }, "ERROR"},
	}

	for _, tc := range levels {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			l := newTestLogger(&buf, "debug")

			tc.logFn(l, context.Background())

			m := parseLine(t, &buf)
			if m["level"] != tc.expect {
				t.Errorf("level = %q, want %q", m["level"], tc.expect)
			}
		})
	}
}

// --- JobContext auto-extraction ---

func TestLogger_extractsJobContextFromContext(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	l := newTestLogger(&buf, "info")

	jc := JobContext{
		JobID:         "j-001",
		DocumentID:    "d-002",
		CorrelationID: "c-003",
		Stage:         "validation",
	}
	ctx := WithJobContext(context.Background(), jc)

	l.Info(ctx, "processing started")

	m := parseLine(t, &buf)
	if m["job_id"] != "j-001" {
		t.Errorf("job_id = %q, want %q", m["job_id"], "j-001")
	}
	if m["document_id"] != "d-002" {
		t.Errorf("document_id = %q, want %q", m["document_id"], "d-002")
	}
	if m["correlation_id"] != "c-003" {
		t.Errorf("correlation_id = %q, want %q", m["correlation_id"], "c-003")
	}
	if m["stage"] != "validation" {
		t.Errorf("stage = %q, want %q", m["stage"], "validation")
	}
}

func TestLogger_partialJobContext_onlyNonEmptyFieldsAppear(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	l := newTestLogger(&buf, "info")

	jc := JobContext{
		JobID: "j-partial",
		// DocumentID, CorrelationID, Stage left empty
	}
	ctx := WithJobContext(context.Background(), jc)

	l.Info(ctx, "partial context")

	m := parseLine(t, &buf)
	if m["job_id"] != "j-partial" {
		t.Errorf("job_id = %q, want %q", m["job_id"], "j-partial")
	}
	if _, ok := m["document_id"]; ok {
		t.Error("document_id should not be present for empty value")
	}
	if _, ok := m["correlation_id"]; ok {
		t.Error("correlation_id should not be present for empty value")
	}
	if _, ok := m["stage"]; ok {
		t.Error("stage should not be present for empty value")
	}
}

// --- Empty context ---

func TestLogger_emptyContext_noJobFields(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	l := newTestLogger(&buf, "info")

	l.Info(context.Background(), "no context")

	m := parseLine(t, &buf)
	for _, field := range []string{"job_id", "document_id", "correlation_id", "stage"} {
		if _, ok := m[field]; ok {
			t.Errorf("field %q should not be present with empty context", field)
		}
	}
}

// --- With() creates child logger ---

func TestLogger_With_createsChildWithAdditionalAttrs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	l := newTestLogger(&buf, "info")

	child := l.With("component", "ocr", "version", "v2")
	child.Info(context.Background(), "child log")

	m := parseLine(t, &buf)
	if m["component"] != "ocr" {
		t.Errorf("component = %q, want %q", m["component"], "ocr")
	}
	if m["version"] != "v2" {
		t.Errorf("version = %q, want %q", m["version"], "v2")
	}
}

func TestLogger_With_childAndJobContext(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	l := newTestLogger(&buf, "info")

	child := l.With("component", "fetcher")
	jc := JobContext{JobID: "j-child", Stage: "fetch"}
	ctx := WithJobContext(context.Background(), jc)

	child.Info(ctx, "fetching document")

	m := parseLine(t, &buf)
	if m["component"] != "fetcher" {
		t.Errorf("component = %q, want %q", m["component"], "fetcher")
	}
	if m["job_id"] != "j-child" {
		t.Errorf("job_id = %q, want %q", m["job_id"], "j-child")
	}
	if m["stage"] != "fetch" {
		t.Errorf("stage = %q, want %q", m["stage"], "fetch")
	}
}

// --- Slog() returns underlying logger ---

func TestLogger_Slog_returnsUnderlyingSlogLogger(t *testing.T) {
	t.Parallel()

	l := NewLogger("info")
	s := l.Slog()

	if s == nil {
		t.Fatal("Slog() returned nil")
	}

	// Verify it is a *slog.Logger.
	var _ *slog.Logger = s
}

// --- Level filtering ---

func TestLogger_levelFiltering_debugNotLoggedAtInfoLevel(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	l := newTestLogger(&buf, "info")

	l.Debug(context.Background(), "should be filtered")

	if buf.Len() != 0 {
		t.Errorf("expected no output for debug at info level, got: %s", buf.String())
	}
}

func TestLogger_levelFiltering_infoLoggedAtInfoLevel(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	l := newTestLogger(&buf, "info")

	l.Info(context.Background(), "should appear")

	if buf.Len() == 0 {
		t.Error("expected output for info at info level")
	}
}
