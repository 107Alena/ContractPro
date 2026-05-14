package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"contractpro/legal-intelligence-core/internal/config"
)

// These tests target the slog.Handler implementation directly, separately
// from the Logger facade. They cover edge cases the higher-level tests
// don't exercise.

func newRawHandler(t *testing.T) (*licHandler, *bytes.Buffer) {
	t.Helper()
	buf := &bytes.Buffer{}
	cfg := config.AppConfig{LogLevel: "debug"}
	jh := slog.NewJSONHandler(buf, &slog.HandlerOptions{
		Level:       parseLevel(cfg.LogLevel),
		ReplaceAttr: replaceAttr,
	})
	return newHandler(jh), buf
}

func TestHandler_ServiceFieldAlwaysPresent(t *testing.T) {
	h, buf := newRawHandler(t)
	r := slog.NewRecord(time.Now(), slog.LevelInfo, "boot", 0)
	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &got); err != nil {
		t.Fatalf("invalid JSON: %v\nraw: %s", err, buf.String())
	}
	if got[KeyService] != ServiceName {
		t.Errorf("service field missing: %v", got[KeyService])
	}
}

func TestHandler_TimestampFieldRenamed(t *testing.T) {
	h, buf := newRawHandler(t)
	r := slog.NewRecord(time.Now(), slog.LevelInfo, "x", 0)
	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !strings.Contains(buf.String(), `"timestamp":`) {
		t.Errorf("expected `timestamp` field, got: %s", buf.String())
	}
	if strings.Contains(buf.String(), `"time":`) {
		t.Errorf("`time` field should have been renamed, got: %s", buf.String())
	}
}

func TestHandler_FatalLevelLabel(t *testing.T) {
	h, buf := newRawHandler(t)
	r := slog.NewRecord(time.Now(), LevelFatal, "boot fail", 0)
	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !strings.Contains(buf.String(), `"level":"FATAL"`) {
		t.Errorf("FATAL label missing, got: %s", buf.String())
	}
}

func TestHandler_WithAttrsChainsCorrectly(t *testing.T) {
	h, buf := newRawHandler(t)
	scoped := h.WithAttrs([]slog.Attr{slog.String("subsystem", "broker")})

	r := slog.NewRecord(time.Now(), slog.LevelInfo, "x", 0)
	if err := scoped.Handle(context.Background(), r); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !strings.Contains(buf.String(), `"subsystem":"broker"`) {
		t.Errorf("WithAttrs didn't propagate, got: %s", buf.String())
	}
	if !strings.Contains(buf.String(), `"service":"lic-service"`) {
		t.Errorf("service field absent after WithAttrs, got: %s", buf.String())
	}
}

func TestHandler_EnabledDelegates(t *testing.T) {
	h, _ := newRawHandler(t)
	if !h.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("INFO should be enabled at debug-threshold handler")
	}
}

func TestHandler_RebuildOnlyWhenSanitizing(t *testing.T) {
	h, buf := newRawHandler(t)

	clean := slog.NewRecord(time.Now(), slog.LevelInfo, "ok", 0)
	clean.AddAttrs(slog.String("note", "nothing to redact"))
	if err := h.Handle(context.Background(), clean); err != nil {
		t.Fatalf("Handle clean: %v", err)
	}

	dirty := slog.NewRecord(time.Now(), slog.LevelError, "bad", 0)
	dirty.AddAttrs(slog.String(KeyError, "fail with sk-ant-api03-leakedhere"))
	if err := h.Handle(context.Background(), dirty); err != nil {
		t.Fatalf("Handle dirty: %v", err)
	}

	if strings.Contains(buf.String(), "sk-ant-api03-leakedhere") {
		t.Fatalf("secret leaked: %s", buf.String())
	}
	if !strings.Contains(buf.String(), `"note":"nothing to redact"`) {
		t.Errorf("clean attr lost: %s", buf.String())
	}
}
