package observability

import (
	"context"
	"log/slog"
	"os"
	"strings"
)

// Logger is a structured JSON logger that auto-extracts JobContext fields
// from the context and prepends them to every log line.
//
// It wraps the stdlib log/slog package and adds no external dependencies.
type Logger struct {
	inner *slog.Logger
}

// NewLogger creates a Logger that writes JSON to os.Stderr at the
// specified level. Supported levels: "debug", "info", "warn", "error".
// Unrecognised values default to "info".
func NewLogger(level string) *Logger {
	return &Logger{
		inner: slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
			Level: parseLevel(level),
		})),
	}
}

// Info logs a message at INFO level, enriching it with JobContext fields.
func (l *Logger) Info(ctx context.Context, msg string, args ...any) {
	l.inner.InfoContext(ctx, msg, withJobAttrs(ctx, args)...)
}

// Warn logs a message at WARN level, enriching it with JobContext fields.
func (l *Logger) Warn(ctx context.Context, msg string, args ...any) {
	l.inner.WarnContext(ctx, msg, withJobAttrs(ctx, args)...)
}

// Error logs a message at ERROR level, enriching it with JobContext fields.
func (l *Logger) Error(ctx context.Context, msg string, args ...any) {
	l.inner.ErrorContext(ctx, msg, withJobAttrs(ctx, args)...)
}

// Debug logs a message at DEBUG level, enriching it with JobContext fields.
func (l *Logger) Debug(ctx context.Context, msg string, args ...any) {
	l.inner.DebugContext(ctx, msg, withJobAttrs(ctx, args)...)
}

// With returns a child Logger that always includes the given attrs.
// Useful for component-scoped loggers: logger.With("component", "ocr").
func (l *Logger) With(args ...any) *Logger {
	return &Logger{inner: l.inner.With(args...)}
}

// Slog returns the underlying *slog.Logger for cases where callers need
// direct access (e.g., passing to third-party libraries).
func (l *Logger) Slog() *slog.Logger {
	return l.inner
}

// withJobAttrs prepends non-empty JobContext fields to the args slice
// so they appear as structured attrs on every log line.
func withJobAttrs(ctx context.Context, args []any) []any {
	jc := JobContextFrom(ctx)

	var prefix []any
	if jc.JobID != "" {
		prefix = append(prefix, slog.String("job_id", jc.JobID))
	}
	if jc.DocumentID != "" {
		prefix = append(prefix, slog.String("document_id", jc.DocumentID))
	}
	if jc.CorrelationID != "" {
		prefix = append(prefix, slog.String("correlation_id", jc.CorrelationID))
	}
	if jc.Stage != "" {
		prefix = append(prefix, slog.String("stage", jc.Stage))
	}

	if len(prefix) == 0 {
		return args
	}
	return append(prefix, args...)
}

// parseLevel converts a human-readable level string to slog.Level.
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
