package logger

import (
	"log/slog"
	"strings"
)

// LevelFatal is a custom log level above slog.LevelError. slog has no native
// Fatal — observability.md §2.2 reserves it for unrecoverable boot-time
// errors. We pick 12 (slog.LevelError = 8) so it sorts above ERROR but stays
// in the same numeric family.
const LevelFatal slog.Level = 12

// parseLevel maps the string form of LIC_LOG_LEVEL into a slog.Level.
// Unknown values fall back to INFO (mirrors AppConfig.validate behaviour and
// keeps the service alive on a typo rather than crashing on boot).
func parseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	case "fatal":
		return LevelFatal
	default:
		return slog.LevelInfo
	}
}

// levelLabel returns the canonical uppercase label for a slog.Level. slog
// renders custom levels as "ERROR+4" by default — we override so FATAL emits
// as the human-readable string the alerting stack expects.
func levelLabel(l slog.Level) string {
	switch {
	case l >= LevelFatal:
		return "FATAL"
	case l >= slog.LevelError:
		return "ERROR"
	case l >= slog.LevelWarn:
		return "WARN"
	case l >= slog.LevelInfo:
		return "INFO"
	default:
		return "DEBUG"
	}
}
