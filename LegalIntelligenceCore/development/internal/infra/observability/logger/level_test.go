package logger

import (
	"log/slog"
	"testing"
)

func TestParseLevel_KnownLevels(t *testing.T) {
	cases := map[string]slog.Level{
		"debug":   slog.LevelDebug,
		"DEBUG":   slog.LevelDebug,
		"info":    slog.LevelInfo,
		" warn ":  slog.LevelWarn,
		"warning": slog.LevelWarn,
		"error":   slog.LevelError,
		"fatal":   LevelFatal,
	}
	for in, want := range cases {
		if got := parseLevel(in); got != want {
			t.Errorf("parseLevel(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestParseLevel_UnknownDefaultsToInfo(t *testing.T) {
	if got := parseLevel("verbose"); got != slog.LevelInfo {
		t.Errorf("parseLevel(unknown) = %v, want INFO", got)
	}
	if got := parseLevel(""); got != slog.LevelInfo {
		t.Errorf("parseLevel(empty) = %v, want INFO", got)
	}
}

func TestLevelLabel(t *testing.T) {
	cases := map[slog.Level]string{
		slog.LevelDebug: "DEBUG",
		slog.LevelInfo:  "INFO",
		slog.LevelWarn:  "WARN",
		slog.LevelError: "ERROR",
		LevelFatal:      "FATAL",
	}
	for lvl, want := range cases {
		if got := levelLabel(lvl); got != want {
			t.Errorf("levelLabel(%v) = %q, want %q", lvl, got, want)
		}
	}
}
