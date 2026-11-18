package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// noConfig returns a config path that doesn't exist, so tests never read
// the user's real ~/.config/worklog.
func noConfig(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "config.toml")
}

func TestReportHelpIsNotAnError(t *testing.T) {
	if err := report([]string{"-h"}, &strings.Builder{}); err != nil {
		t.Errorf("-h returned %v, want nil (exit 0)", err)
	}
	if err := report([]string{"-nope"}, &strings.Builder{}); err == nil {
		t.Error("unknown flag should still be an error")
	}
}

func TestReportDoesNotCreateDataDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing")
	err := report([]string{"-dir", dir, "-config", noConfig(t)}, &strings.Builder{})
	if err == nil || !strings.Contains(err.Error(), "no such directory") {
		t.Errorf("err = %v, want 'no such directory'", err)
	}
	if _, statErr := os.Stat(dir); statErr == nil {
		t.Error("report must not create the data directory")
	}

	// An existing (empty) directory reports fine.
	var out strings.Builder
	if err := report([]string{"-dir", t.TempDir(), "-config", noConfig(t)}, &out); err != nil {
		t.Errorf("report on empty dir: %v", err)
	}
}

func TestDemoIgnoresBrokenConfig(t *testing.T) {
	// run() with -demo must get past config loading even when the config
	// is invalid; the TUI itself can't start in a test, so check the config
	// step alone by calling loadConfig.
	p := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(p, []byte("dir = \n"), 0o644)
	if _, err := loadConfig(p, false); err == nil {
		t.Error("broken config should fail without -demo")
	}
	if _, err := loadConfig(p, true); err != nil {
		t.Errorf("-demo should ignore a broken config, got %v", err)
	}
}
