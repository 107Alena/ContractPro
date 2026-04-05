package main

import (
	"testing"
)

// --- parseCommand tests (no DB connection needed) ---

func TestParseCommand_NoArgs(t *testing.T) {
	_, _, err := parseCommand(nil)
	if err == nil {
		t.Error("expected error for nil args")
	}
}

func TestParseCommand_EmptyArgs(t *testing.T) {
	_, _, err := parseCommand([]string{})
	if err == nil {
		t.Error("expected error for empty args")
	}
}

func TestParseCommand_Up(t *testing.T) {
	cmd, gotoVer, err := parseCommand([]string{"up"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmd != "up" {
		t.Errorf("cmd = %q, want %q", cmd, "up")
	}
	if gotoVer != "" {
		t.Errorf("gotoVer = %q, want empty", gotoVer)
	}
}

func TestParseCommand_Version(t *testing.T) {
	cmd, _, err := parseCommand([]string{"version"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmd != "version" {
		t.Errorf("cmd = %q, want %q", cmd, "version")
	}
}

func TestParseCommand_DownWithoutConfirm(t *testing.T) {
	_, _, err := parseCommand([]string{"down"})
	if err == nil {
		t.Error("expected error for 'down' without --confirm-destroy")
	}
}

func TestParseCommand_DownWithWrongFlag(t *testing.T) {
	_, _, err := parseCommand([]string{"down", "--force"})
	if err == nil {
		t.Error("expected error for 'down' with wrong flag")
	}
}

func TestParseCommand_DownWithConfirm(t *testing.T) {
	cmd, _, err := parseCommand([]string{"down", "--confirm-destroy"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmd != "down" {
		t.Errorf("cmd = %q, want %q", cmd, "down")
	}
}

func TestParseCommand_GotoWithVersion(t *testing.T) {
	cmd, gotoVer, err := parseCommand([]string{"goto", "3"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmd != "goto" {
		t.Errorf("cmd = %q, want %q", cmd, "goto")
	}
	if gotoVer != "3" {
		t.Errorf("gotoVer = %q, want %q", gotoVer, "3")
	}
}

func TestParseCommand_GotoMissingVersion(t *testing.T) {
	_, _, err := parseCommand([]string{"goto"})
	if err == nil {
		t.Error("expected error for 'goto' without version")
	}
}

func TestParseCommand_UnknownCommand(t *testing.T) {
	_, _, err := parseCommand([]string{"reset"})
	if err == nil {
		t.Error("expected error for unknown command")
	}
}

// --- run() tests (environment-level) ---

func TestRun_NoArgs_ReturnsOne(t *testing.T) {
	code := run(nil)
	if code != 1 {
		t.Errorf("run(nil) = %d, want 1", code)
	}
}

func TestRun_NoDSN_ReturnsOne(t *testing.T) {
	t.Setenv("DM_DB_DSN", "")
	code := run([]string{"version"})
	if code != 1 {
		t.Errorf("run(version) without DSN = %d, want 1", code)
	}
}

func TestRun_UnknownCommand_ReturnsOne(t *testing.T) {
	// parseCommand catches unknown commands before DSN check.
	code := run([]string{"unknown"})
	if code != 1 {
		t.Errorf("run(unknown) = %d, want 1", code)
	}
}

func TestRun_DownWithoutConfirm_ReturnsOne(t *testing.T) {
	// parseCommand catches missing --confirm-destroy before DSN check.
	code := run([]string{"down"})
	if code != 1 {
		t.Errorf("run(down) without confirm = %d, want 1", code)
	}
}

func TestRun_GotoMissingVersion_ReturnsOne(t *testing.T) {
	// parseCommand catches missing version before DSN check.
	code := run([]string{"goto"})
	if code != 1 {
		t.Errorf("run(goto) without version = %d, want 1", code)
	}
}
