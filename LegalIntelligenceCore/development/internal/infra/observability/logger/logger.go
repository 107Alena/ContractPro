// Package logger provides the LIC service's structured JSON logger.
//
// Design highlights:
//   - log/slog (stdlib) — hermetic, no extra deps.
//   - Allowlist policy: only IDs and metadata named in security.md §6.2
//     reach the log stream. The exported KeyXxx constants are the only
//     supported keys for those fields.
//   - Auto-injected RequestContext: ingress builds it once, every line is
//     enriched without manual plumbing.
//   - Auto-redacted error attribute: any slog.Attr keyed by KeyError
//     ("error") goes through Sanitize so leaked API keys never reach the
//     log aggregator.
package logger

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"

	"contractpro/legal-intelligence-core/internal/config"
)

// Logger is the LIC structured logger. Wraps an *slog.Logger so consumers
// get the familiar slog API (`slog.Attr`, `slog.Group`) while we keep
// control over the handler chain.
type Logger struct {
	inner *slog.Logger
}

// New builds a Logger that writes JSON to os.Stdout at the level taken from
// cfg.LogLevel. The level string must be one of: debug, info, warn, error,
// fatal — anything else falls back to INFO. cfg is taken by value because
// it's a small POD and we want zero shared mutable state.
func New(cfg config.AppConfig) (*Logger, error) {
	return NewWithWriter(cfg, os.Stdout)
}

// NewWithWriter is the test/production seam: production passes os.Stdout,
// tests pass *bytes.Buffer. Returning (*Logger, error) keeps the signature
// uniform with constructor conventions across LIC even though the only
// failure mode today is a nil writer.
func NewWithWriter(cfg config.AppConfig, w io.Writer) (*Logger, error) {
	if w == nil {
		return nil, fmt.Errorf("logger: writer must not be nil")
	}
	jsonHandler := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level:       parseLevel(cfg.LogLevel),
		ReplaceAttr: replaceAttr,
	})
	return &Logger{inner: slog.New(newHandler(jsonHandler))}, nil
}

// Debug logs at DEBUG level. DEBUG is off in production by default
// (observability.md §2.5).
func (l *Logger) Debug(ctx context.Context, msg string, attrs ...slog.Attr) {
	l.log(ctx, slog.LevelDebug, msg, attrs)
}

// Info logs at INFO level — pipeline starts/stops, agent invocations, event
// publications.
func (l *Logger) Info(ctx context.Context, msg string, attrs ...slog.Attr) {
	l.log(ctx, slog.LevelInfo, msg, attrs)
}

// Warn logs at WARN level — repair triggered, fallback to other provider,
// degradation, prompt_injection_detected.
func (l *Logger) Warn(ctx context.Context, msg string, attrs ...slog.Attr) {
	l.log(ctx, slog.LevelWarn, msg, attrs)
}

// Error logs at ERROR level — failed pipelines, fatal LLM errors after
// fallback, DLQ publication. Any attribute keyed by KeyError is sanitized
// before emission.
func (l *Logger) Error(ctx context.Context, msg string, attrs ...slog.Attr) {
	l.log(ctx, slog.LevelError, msg, attrs)
}

// Fatal logs at FATAL level and exits the process with code 1. Reserve for
// unrecoverable boot-time errors (config invalid, broker unreachable on
// startup) per observability.md §2.2. Calls os.Exit; defers do NOT run.
func (l *Logger) Fatal(ctx context.Context, msg string, attrs ...slog.Attr) {
	l.log(ctx, LevelFatal, msg, attrs)
	exitFn(1)
}

// With returns a child logger with `component` pre-bound. Useful for
// component-scoped loggers: `pipelineLog := log.With("pipeline.orchestrator")`.
// The component label is the only slog attr we bind via With — IDs are
// always pulled from ctx.
func (l *Logger) With(component string) *Logger {
	if component == "" {
		return l
	}
	return &Logger{inner: l.inner.With(slog.String(KeyComponent, component))}
}

// Slog exposes the underlying *slog.Logger for callers that need to pass it
// to a third-party library expecting *slog.Logger. Use sparingly — prefer
// the typed methods so the allowlist policy stays enforceable by review.
func (l *Logger) Slog() *slog.Logger {
	return l.inner
}

// log is the private fan-in for all leveled methods. Goes through
// LogAttrs so the call site's slog.Attr slice is honored without the
// `args ...any` -> attr conversion overhead.
func (l *Logger) log(ctx context.Context, lvl slog.Level, msg string, attrs []slog.Attr) {
	if !l.inner.Enabled(ctx, lvl) {
		return
	}
	l.inner.LogAttrs(ctx, lvl, msg, attrs...)
}

// exitFn is a package-level var so tests can swap os.Exit for a fake to
// assert the FATAL path without killing the test process.
var exitFn = os.Exit
