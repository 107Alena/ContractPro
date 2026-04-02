package observability

import (
	"context"
	"log/slog"
	"os"
	"strings"
)

// Logger is a structured JSON logger that wraps the stdlib log/slog package.
//
// It provides two sets of methods:
//   - Info/Warn/Error/Debug — context-free, satisfies existing consumer-side
//     Logger interfaces across DM application services.
//   - InfoContext/WarnContext/ErrorContext/DebugContext — auto-extracts
//     EventContext fields from the context and prepends them to every log line.
//
// Use With() to create component-scoped child loggers with prepended attrs:
//
//	logger.With("component", "ingestion")
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

// Info logs a message at INFO level.
func (l *Logger) Info(msg string, args ...any) {
	l.inner.Info(msg, args...)
}

// Warn logs a message at WARN level.
func (l *Logger) Warn(msg string, args ...any) {
	l.inner.Warn(msg, args...)
}

// Error logs a message at ERROR level.
func (l *Logger) Error(msg string, args ...any) {
	l.inner.Error(msg, args...)
}

// Debug logs a message at DEBUG level.
func (l *Logger) Debug(msg string, args ...any) {
	l.inner.Debug(msg, args...)
}

// InfoContext logs at INFO level, enriching with EventContext fields from ctx.
func (l *Logger) InfoContext(ctx context.Context, msg string, args ...any) {
	l.inner.InfoContext(ctx, msg, withEventAttrs(ctx, args)...)
}

// WarnContext logs at WARN level, enriching with EventContext fields from ctx.
func (l *Logger) WarnContext(ctx context.Context, msg string, args ...any) {
	l.inner.WarnContext(ctx, msg, withEventAttrs(ctx, args)...)
}

// ErrorContext logs at ERROR level, enriching with EventContext fields from ctx.
func (l *Logger) ErrorContext(ctx context.Context, msg string, args ...any) {
	l.inner.ErrorContext(ctx, msg, withEventAttrs(ctx, args)...)
}

// DebugContext logs at DEBUG level, enriching with EventContext fields from ctx.
func (l *Logger) DebugContext(ctx context.Context, msg string, args ...any) {
	l.inner.DebugContext(ctx, msg, withEventAttrs(ctx, args)...)
}

// With returns a child Logger that always includes the given attrs.
// Useful for component-scoped loggers: logger.With("component", "ingestion").
func (l *Logger) With(args ...any) *Logger {
	return &Logger{inner: l.inner.With(args...)}
}

// Slog returns the underlying *slog.Logger for cases where callers need
// direct access (e.g., passing to third-party libraries).
func (l *Logger) Slog() *slog.Logger {
	return l.inner
}

// withEventAttrs prepends non-empty EventContext fields to the args slice
// so they appear as structured attrs on every log line.
func withEventAttrs(ctx context.Context, args []any) []any {
	ec := EventContextFrom(ctx)

	var prefix []any
	if ec.CorrelationID != "" {
		prefix = append(prefix, slog.String("correlation_id", ec.CorrelationID))
	}
	if ec.JobID != "" {
		prefix = append(prefix, slog.String("job_id", ec.JobID))
	}
	if ec.DocumentID != "" {
		prefix = append(prefix, slog.String("document_id", ec.DocumentID))
	}
	if ec.VersionID != "" {
		prefix = append(prefix, slog.String("version_id", ec.VersionID))
	}
	if ec.OrganizationID != "" {
		prefix = append(prefix, slog.String("organization_id", ec.OrganizationID))
	}
	if ec.Stage != "" {
		prefix = append(prefix, slog.String("stage", ec.Stage))
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
