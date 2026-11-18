package config

import (
	"math"
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

func TestRenderRoundTrip(t *testing.T) {
	for _, c := range []Config{
		{},
		{Dir: "~/notes", StaleTimerHours: 2.5, Agent: Agent{Type: "claude", Model: "haiku", ClaudePath: "/x/claude"}},
		{Agent: Agent{Type: "command", Name: "codex", Command: []string{"codex", "exec", `say "{prompt}"`}}},
	} {
		got, err := Parse([]byte(Render(c)))
		if err != nil {
			t.Fatalf("parse rendered %+v: %v\n%s", c, err, Render(c))
		}
		if c.Agent.Type == "" {
			c.Agent.Type = "claude" // Render writes the default explicitly
		}
		if got.Dir != c.Dir || got.StaleTimerHours != c.StaleTimerHours || got.Agent.Type != c.Agent.Type ||
			got.Agent.Model != c.Agent.Model || got.Agent.ClaudePath != c.Agent.ClaudePath ||
			got.Agent.Name != c.Agent.Name || strings.Join(got.Agent.Command, "|") != strings.Join(c.Agent.Command, "|") {
			t.Errorf("round trip: got %+v, want %+v", got, c)
		}
	}
}

// quote used strconv.Quote, whose \a, \v and \xNN escapes aren't valid TOML,
// so Render could write a config that Load then rejected.
func TestRenderQuotesTOMLStrings(t *testing.T) {
	odd := "bell\a vt\v tab\t quote\" back\\ nl\n del\x7f ünï 日本 \U0001F600"
	c := Config{
		Dir:   odd,
		Agent: Agent{Model: odd, ClaudePath: "/p\a", Name: "n\v", Command: []string{"sh", "-c", odd, "bad\xffutf8"}},
	}
	got, err := Parse([]byte(Render(c)))
	if err != nil {
		t.Fatalf("parse rendered: %v\n%s", err, Render(c))
	}
	if got.Dir != odd || got.Agent.Model != odd || got.Agent.ClaudePath != "/p\a" || got.Agent.Name != "n\v" {
		t.Errorf("round trip: got %+v", got)
	}
	if len(got.Agent.Command) != 4 || got.Agent.Command[2] != odd {
		t.Errorf("command round trip: %q", got.Agent.Command)
	}
	if got.Agent.Command[3] != "bad�utf8" {
		t.Errorf("invalid UTF-8 should become U+FFFD, got %q", got.Agent.Command[3])
	}
	if strings.Contains(Render(c), `\x`) {
		t.Errorf("rendered TOML contains a \\x escape:\n%s", Render(c))
	}
}

func TestStaleTimerHoursValidation(t *testing.T) {
	for _, h := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), -1, MaxStaleTimerHours + 1, 1e300} {
		if err := (Config{StaleTimerHours: h}).Validate(); err == nil || !strings.Contains(err.Error(), "stale_timer_hours") {
			t.Errorf("stale_timer_hours = %v: err = %v", h, err)
		}
	}
	for _, h := range []float64{0, 0.5, 4, MaxStaleTimerHours} {
		if err := (Config{StaleTimerHours: h}).Validate(); err != nil {
			t.Errorf("stale_timer_hours = %v: unexpected %v", h, err)
		}
	}
	for _, body := range []string{"stale_timer_hours = nan\n", "stale_timer_hours = inf\n", "stale_timer_hours = 1e9\n"} {
		if _, err := Load(write(t, body)); err == nil || !strings.Contains(err.Error(), "stale_timer_hours") {
			t.Errorf("%q: err = %v", body, err)
		}
	}
}

func TestSaveRejectsInvalid(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "config.toml")
	if err := SaveRaw(p, []byte("[agent]\ntype = \"nope\"\n")); err == nil {
		t.Fatal("expected validation error")
	}
	if _, err := os.Stat(p); err == nil {
		t.Fatal("invalid config must not be written")
	}
	if err := Save(p, Config{Agent: Agent{Model: "sonnet"}}); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil || c.Agent.Model != "sonnet" {
		t.Fatalf("load after save: %+v %v", c, err)
	}
}

func TestSplitJoinArgs(t *testing.T) {
	cases := map[string][]string{
		`codex exec {prompt}`:           {"codex", "exec", "{prompt}"},
		`aider --message "{message}" x`: {"aider", "--message", "{message}", "x"},
		`sh -c 'echo "hi there"'`:       {"sh", "-c", `echo "hi there"`},
		`a\ b  c`:                       {"a b", "c"},
		`''`:                            {""},
	}
	for in, want := range cases {
		got, err := SplitArgs(in)
		if err != nil || strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("SplitArgs(%q) = %q, %v; want %q", in, got, err, want)
		}
		back, _ := SplitArgs(JoinArgs(want))
		if strings.Join(back, "|") != strings.Join(want, "|") {
			t.Errorf("JoinArgs round trip for %q gave %q", want, back)
		}
	}
	if _, err := SplitArgs(`echo "oops`); err == nil {
		t.Error("expected unclosed quote error")
	}
}
