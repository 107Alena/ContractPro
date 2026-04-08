package logger

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
)

// serviceName is the constant value for the "service" field in every log entry.
const serviceName = "api-orchestrator"

// Logger is a structured JSON logger that auto-extracts RequestContext fields
// from context.Context and prepends them to every log line.
//
// It wraps the stdlib log/slog package and adds no external dependencies.
// The "service" field is baked in at construction time; the "component" field
// is typically added via With() when creating component-scoped child loggers.
//
// Usage:
//
//	root := logger.NewLogger("info")
//	compLog := root.With("component", "upload-coordinator")
//	compLog.Info(ctx, "upload completed", "duration_ms", 1230)
type Logger struct {
	inner *slog.Logger
}

// NewLogger creates a Logger that writes structured JSON to os.Stdout at the
// specified level. The "service" field is automatically included in every
// log entry.
//
// Supported levels: "debug", "info", "warn", "error".
// Unrecognised values default to "info".
func NewLogger(level string) *Logger {
	return newLogger(level, os.Stdout)
}

// newLogger is the shared constructor used by both NewLogger (stdout) and
// tests (bytes.Buffer). Centralising the handler setup ensures tests
// validate the same construction path as production code.
func newLogger(level string, w io.Writer) *Logger {
	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: parseLevel(level),
	})

	// Bake the service name into the handler so it appears in every entry
	// without per-call overhead.
	handlerWithService := handler.WithAttrs([]slog.Attr{
		slog.String("service", serviceName),
	})

	return &Logger{
		inner: slog.New(handlerWithService),
	}
}

// Info logs a message at INFO level, enriching it with RequestContext fields
// extracted from ctx.
func (l *Logger) Info(ctx context.Context, msg string, args ...any) {
	l.inner.InfoContext(ctx, msg, withRequestAttrs(ctx, args)...)
}

// Warn logs a message at WARN level, enriching it with RequestContext fields
// extracted from ctx.
func (l *Logger) Warn(ctx context.Context, msg string, args ...any) {
	l.inner.WarnContext(ctx, msg, withRequestAttrs(ctx, args)...)
}

// Error logs a message at ERROR level, enriching it with RequestContext fields
// extracted from ctx.
func (l *Logger) Error(ctx context.Context, msg string, args ...any) {
	l.inner.ErrorContext(ctx, msg, withRequestAttrs(ctx, args)...)
}

// Debug logs a message at DEBUG level, enriching it with RequestContext fields
// extracted from ctx.
func (l *Logger) Debug(ctx context.Context, msg string, args ...any) {
	l.inner.DebugContext(ctx, msg, withRequestAttrs(ctx, args)...)
}

// With returns a child Logger that always includes the given key-value attrs.
// Useful for creating component-scoped loggers:
//
//	compLog := logger.With("component", "dm-client")
//
// The child inherits the parent's handler (including "service") and adds
// the new attrs as persistent fields.
func (l *Logger) With(args ...any) *Logger {
	return &Logger{inner: l.inner.With(args...)}
}

// Slog returns the underlying *slog.Logger for cases where callers need
// direct access (e.g., passing to third-party libraries that accept *slog.Logger).
func (l *Logger) Slog() *slog.Logger {
	return l.inner
}

// ErrorAttr returns a slog.Attr for the "error" field. If err is nil, the
// attribute value is an empty string; otherwise it is err.Error().
//
// Use this helper to satisfy the mandatory "error" field without risking
// nil pointer dereferences:
//
//	l.Error(ctx, "DM call failed", logger.ErrorAttr(err), "status_code", 502)
func ErrorAttr(err error) slog.Attr {
	if err == nil {
		return slog.Any("error", nil)
	}
	return slog.String("error", err.Error())
}

// withRequestAttrs prepends non-empty RequestContext fields to the args slice
// so they appear as structured attributes on every log line.
//
// Only non-empty fields are emitted to keep log entries concise. The mandatory
// context fields (correlation_id, organization_id, user_id) are emitted first,
// followed by optional tracing fields (document_id, version_id, job_id).
func withRequestAttrs(ctx context.Context, args []any) []any {
	rc := RequestContextFrom(ctx)

	prefix := make([]any, 0, 6)

	if rc.CorrelationID != "" {
		prefix = append(prefix, slog.String("correlation_id", rc.CorrelationID))
	}
	if rc.OrganizationID != "" {
		prefix = append(prefix, slog.String("organization_id", rc.OrganizationID))
	}
	if rc.UserID != "" {
		prefix = append(prefix, slog.String("user_id", rc.UserID))
	}
	if rc.DocumentID != "" {
		prefix = append(prefix, slog.String("document_id", rc.DocumentID))
	}
	if rc.VersionID != "" {
		prefix = append(prefix, slog.String("version_id", rc.VersionID))
	}
	if rc.JobID != "" {
		prefix = append(prefix, slog.String("job_id", rc.JobID))
	}

	if len(prefix) == 0 {
		return args
	}
	return append(prefix, args...)
}

// parseLevel converts a human-readable level string to slog.Level.
// Defaults to slog.LevelInfo for unrecognised values.
func parseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
