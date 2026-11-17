package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestMissingFileGivesDefaults(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "nope.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if c.StaleAfter() != 4*time.Hour || c.Agent.Type != "" {
		t.Fatalf("defaults = %+v", c)
	}
}

func TestLoadFull(t *testing.T) {
	c, err := Load(write(t, `
dir = "~/notes"
stale_timer_hours = 2.5

[agent]
type = "command"
name = "codex"
command = ["codex", "exec", "{prompt}"]
`))
	if err != nil {
		t.Fatal(err)
	}
	if c.Dir != "~/notes" || c.StaleAfter() != 150*time.Minute || c.Agent.Name != "codex" || len(c.Agent.Command) != 3 {
		t.Fatalf("got %+v", c)
	}
}

func TestErrors(t *testing.T) {
	for body, want := range map[string]string{
		"[agent]\ntype = \"command\"\n": "needs agent.command",
		"[agent]\ntype = \"gpt\"\n":     "must be",
		"modle = \"x\"\n":               "unknown setting",
		"dir = \n":                      "config.toml",
	} {
		_, err := Load(write(t, body))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: err = %v, want %q", body, err, want)
		}
	}
}

func TestExpandHome(t *testing.T) {
	home, _ := os.UserHomeDir()
	if got := ExpandHome("~/worklog"); got != filepath.Join(home, "worklog") {
		t.Errorf("got %s", got)
	}
	if got := ExpandHome("/abs"); got != "/abs" {
		t.Errorf("got %s", got)
	}
}
