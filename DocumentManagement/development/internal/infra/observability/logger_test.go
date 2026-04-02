package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

// newTestLogger creates a Logger that writes to a buffer for test inspection.
func newTestLogger(buf *bytes.Buffer, level string) *Logger {
	return &Logger{
		inner: slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{
			Level: parseLevel(level),
		})),
	}
}

func TestLogger_Info(t *testing.T) {
	var buf bytes.Buffer
	l := newTestLogger(&buf, "info")

	l.Info("hello", "key", "value")

	m := parseLogLine(t, buf.Bytes())
	assertField(t, m, "msg", "hello")
	assertField(t, m, "key", "value")
	assertField(t, m, "level", "INFO")
}

func TestLogger_Warn(t *testing.T) {
	var buf bytes.Buffer
	l := newTestLogger(&buf, "info")

	l.Warn("caution", "count", float64(42))

	m := parseLogLine(t, buf.Bytes())
	assertField(t, m, "msg", "caution")
	assertField(t, m, "level", "WARN")
}

func TestLogger_Error(t *testing.T) {
	var buf bytes.Buffer
	l := newTestLogger(&buf, "info")

	l.Error("bad things")

	m := parseLogLine(t, buf.Bytes())
	assertField(t, m, "msg", "bad things")
	assertField(t, m, "level", "ERROR")
}

func TestLogger_Debug_BelowLevel(t *testing.T) {
	var buf bytes.Buffer
	l := newTestLogger(&buf, "info")

	l.Debug("should not appear")

	if buf.Len() > 0 {
		t.Fatal("debug message appeared at info level")
	}
}

func TestLogger_Debug_AtLevel(t *testing.T) {
	var buf bytes.Buffer
	l := newTestLogger(&buf, "debug")

	l.Debug("visible")

	m := parseLogLine(t, buf.Bytes())
	assertField(t, m, "msg", "visible")
	assertField(t, m, "level", "DEBUG")
}

func TestLogger_With(t *testing.T) {
	var buf bytes.Buffer
	l := newTestLogger(&buf, "info")

	child := l.With("component", "ingestion")
	child.Info("test")

	m := parseLogLine(t, buf.Bytes())
	assertField(t, m, "component", "ingestion")
}

func TestLogger_With_Chain(t *testing.T) {
	var buf bytes.Buffer
	l := newTestLogger(&buf, "info")

	child := l.With("service", "dm").With("component", "query")
	child.Info("test")

	m := parseLogLine(t, buf.Bytes())
	assertField(t, m, "service", "dm")
	assertField(t, m, "component", "query")
}

func TestLogger_InfoContext_EnrichesFromEventContext(t *testing.T) {
	var buf bytes.Buffer
	l := newTestLogger(&buf, "info")

	ctx := WithEventContext(context.Background(), EventContext{
		CorrelationID:  "corr-1",
		JobID:          "job-2",
		DocumentID:     "doc-3",
		VersionID:      "ver-4",
		OrganizationID: "org-5",
		Stage:          "test-stage",
	})

	l.InfoContext(ctx, "enriched", "extra", "val")

	m := parseLogLine(t, buf.Bytes())
	assertField(t, m, "correlation_id", "corr-1")
	assertField(t, m, "job_id", "job-2")
	assertField(t, m, "document_id", "doc-3")
	assertField(t, m, "version_id", "ver-4")
	assertField(t, m, "organization_id", "org-5")
	assertField(t, m, "stage", "test-stage")
	assertField(t, m, "extra", "val")
}

func TestLogger_InfoContext_EmptyContext(t *testing.T) {
	var buf bytes.Buffer
	l := newTestLogger(&buf, "info")

	l.InfoContext(context.Background(), "no enrichment")

	m := parseLogLine(t, buf.Bytes())
	assertField(t, m, "msg", "no enrichment")
	if _, ok := m["correlation_id"]; ok {
		t.Fatal("correlation_id should not be present for empty context")
	}
}

func TestLogger_InfoContext_PartialContext(t *testing.T) {
	var buf bytes.Buffer
	l := newTestLogger(&buf, "info")

	ctx := WithEventContext(context.Background(), EventContext{
		JobID: "only-job",
	})

	l.InfoContext(ctx, "partial")

	m := parseLogLine(t, buf.Bytes())
	assertField(t, m, "job_id", "only-job")
	if _, ok := m["correlation_id"]; ok {
		t.Fatal("empty correlation_id should be omitted")
	}
}

func TestLogger_WarnContext(t *testing.T) {
	var buf bytes.Buffer
	l := newTestLogger(&buf, "info")

	ctx := WithEventContext(context.Background(), EventContext{CorrelationID: "c1"})
	l.WarnContext(ctx, "warning")

	m := parseLogLine(t, buf.Bytes())
	assertField(t, m, "level", "WARN")
	assertField(t, m, "correlation_id", "c1")
}

func TestLogger_ErrorContext(t *testing.T) {
	var buf bytes.Buffer
	l := newTestLogger(&buf, "info")

	ctx := WithEventContext(context.Background(), EventContext{CorrelationID: "c2"})
	l.ErrorContext(ctx, "error")

	m := parseLogLine(t, buf.Bytes())
	assertField(t, m, "level", "ERROR")
	assertField(t, m, "correlation_id", "c2")
}

func TestLogger_DebugContext(t *testing.T) {
	var buf bytes.Buffer
	l := newTestLogger(&buf, "debug")

	ctx := WithEventContext(context.Background(), EventContext{CorrelationID: "c3"})
	l.DebugContext(ctx, "debug")

	m := parseLogLine(t, buf.Bytes())
	assertField(t, m, "level", "DEBUG")
	assertField(t, m, "correlation_id", "c3")
}

func TestLogger_Slog(t *testing.T) {
	l := NewLogger("info")
	if l.Slog() == nil {
		t.Fatal("Slog() returned nil")
	}
}

func TestNewLogger_OutputsJSON(t *testing.T) {
	var buf bytes.Buffer
	l := newTestLogger(&buf, "info")

	l.Info("json check")

	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"INFO", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"error", slog.LevelError},
		{"ERROR", slog.LevelError},
		{"unknown", slog.LevelInfo},
		{"", slog.LevelInfo},
		{"  info  ", slog.LevelInfo},
	}

	for _, tt := range tests {
		got := parseLevel(tt.input)
		if got != tt.want {
			t.Errorf("parseLevel(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestLogger_WithAndContext_Combined(t *testing.T) {
	var buf bytes.Buffer
	l := newTestLogger(&buf, "info")

	child := l.With("component", "consumer")
	ctx := WithEventContext(context.Background(), EventContext{JobID: "j1"})
	child.InfoContext(ctx, "combined")

	m := parseLogLine(t, buf.Bytes())
	assertField(t, m, "component", "consumer")
	assertField(t, m, "job_id", "j1")
}

// --- helpers ---

func parseLogLine(t *testing.T, data []byte) map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 0 {
		t.Fatal("no log output")
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &m); err != nil {
		t.Fatalf("invalid JSON log line: %v\nraw: %s", err, lines[len(lines)-1])
	}
	return m
}

func assertField(t *testing.T, m map[string]any, key string, want any) {
	t.Helper()
	got, ok := m[key]
	if !ok {
		t.Fatalf("field %q not found in log output: %v", key, m)
	}
	// JSON numbers are float64, so compare as strings for simplicity.
	if wantStr, gotStr := toString(want), toString(got); wantStr != gotStr {
		t.Fatalf("field %q: got %v, want %v", key, got, want)
	}
}

func toString(v any) string {
	switch val := v.(type) {
	case string:
		return val
	default:
		data, _ := json.Marshal(val)
		return string(data)
	}
}
