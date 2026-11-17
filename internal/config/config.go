// Package config loads worklog's optional TOML config file.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Config is the contents of config.toml. Every field is optional.
type Config struct {
	// Dir is where todos are stored. Default ~/worklog.
	Dir string `toml:"dir"`
	// StaleTimerHours is how long a timer may run before worklog asks
	// whether it was forgotten. Default 4.
	StaleTimerHours float64 `toml:"stale_timer_hours"`

	Agent Agent `toml:"agent"`
}

// Agent selects and configures the agent behind the AI pane.
type Agent struct {
	// Type is "claude" (Claude Code, the default) or "command" (any CLI).
	Type string `toml:"type"`

	// Claude Code options.
	Model      string `toml:"model"`       // e.g. "sonnet"; empty uses your Claude Code default
	ClaudePath string `toml:"claude_path"` // empty uses $WORKLOG_CLAUDE or claude on PATH

	// Command options: argv with placeholders {prompt}, {message},
	// {notes_file}, {title} and {dir}.
	Command []string `toml:"command"`
	Name    string   `toml:"name"` // label shown in the pane title
}

// DefaultPath is $XDG_CONFIG_HOME/worklog/config.toml or
// ~/.config/worklog/config.toml.
func DefaultPath() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "worklog", "config.toml")
}

// Load reads the config at path. A missing file yields the defaults.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, err
	}
	c, err := Parse(data)
	if err != nil {
		return c, fmt.Errorf("%s: %w", path, err)
	}
	return c, nil
}

// Parse decodes and validates TOML config text.
func Parse(data []byte) (Config, error) {
	var c Config
	md, err := toml.Decode(string(data), &c)
	if err != nil {
		return c, err
	}
	if undec := md.Undecoded(); len(undec) > 0 {
		keys := make([]string, len(undec))
		for i, k := range undec {
			keys[i] = k.String()
		}
		return c, fmt.Errorf("unknown setting(s): %s", strings.Join(keys, ", "))
	}
	return c, c.Validate()
}

// Save writes c to path as commented TOML, atomically.
func Save(path string, c Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	return SaveRaw(path, []byte(Render(c)))
}

// SaveRaw validates TOML text and writes it to path atomically.
func SaveRaw(path string, data []byte) error {
	if _, err := Parse(data); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.toml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Render produces a commented config file for c. Unset values are written
// as commented-out examples.
func Render(c Config) string {
	var b strings.Builder
	line := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }
	opt := func(key, val, example string) {
		if val != "" {
			line("%s = %s", key, val)
		} else {
			line("# %s = %s", key, example)
		}
	}
	str := func(s string) string {
		if s == "" {
			return ""
		}
		return quote(s)
	}

	line("# worklog settings. Edit here or press , in the app.")
	line("")
	line("# Where todos live (overridden by -dir and $WORKLOG_DIR).")
	opt("dir", str(c.Dir), `"~/worklog"`)
	line("")
	line("# Ask about timers that have run longer than this many hours.")
	stale := ""
	if c.StaleTimerHours != 0 {
		stale = strconvFloat(c.StaleTimerHours)
	}
	opt("stale_timer_hours", stale, "4")
	line("")
	line("[agent]")
	line(`# "claude" (Claude Code) or "command" (any CLI agent).`)
	typ := c.Agent.Type
	if typ == "" {
		typ = "claude"
	}
	line("type = %s", quote(typ))
	line("")
	line("# Claude Code: model alias or ID; empty uses your Claude Code default.")
	opt("model", str(c.Agent.Model), `"haiku"`)
	line("# Path to claude; empty uses $WORKLOG_CLAUDE or claude on PATH.")
	opt("claude_path", str(c.Agent.ClaudePath), `"/opt/homebrew/bin/claude"`)
	line("")
	line("# Command agents. Placeholders: {prompt} {message} {notes_file} {title} {dir}")
	opt("name", str(c.Agent.Name), `"codex"`)
	cmd := ""
	if len(c.Agent.Command) > 0 {
		parts := make([]string, len(c.Agent.Command))
		for i, a := range c.Agent.Command {
			parts[i] = quote(a)
		}
		cmd = "[" + strings.Join(parts, ", ") + "]"
	}
	opt("command", cmd, `["codex", "exec", "{prompt}"]`)
	return b.String()
}

func quote(s string) string { return strconv.Quote(s) }

func strconvFloat(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

// SplitArgs splits a command line into arguments, honouring single and
// double quotes and backslash escapes.
func SplitArgs(s string) ([]string, error) {
	var args []string
	var cur strings.Builder
	inArg, quote := false, rune(0)
	escaped := false
	for _, r := range s {
		switch {
		case escaped:
			cur.WriteRune(r)
			escaped = false
		case r == '\\' && quote != '\'':
			escaped, inArg = true, true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '"' || r == '\'':
			quote, inArg = r, true
		case r == ' ' || r == '\t':
			if inArg {
				args = append(args, cur.String())
				cur.Reset()
				inArg = false
			}
		default:
			cur.WriteRune(r)
			inArg = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unclosed %c quote", quote)
	}
	if escaped {
		cur.WriteRune('\\')
	}
	if inArg {
		args = append(args, cur.String())
	}
	return args, nil
}

// JoinArgs is the inverse of SplitArgs, quoting where needed.
func JoinArgs(args []string) string {
	out := make([]string, len(args))
	for i, a := range args {
		if a != "" && !strings.ContainsAny(a, " \t\"'\\") {
			out[i] = a
		} else {
			out[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		}
	}
	return strings.Join(out, " ")
}

// Validate checks settings that TOML decoding alone can't.
func (c Config) Validate() error {
	switch c.Agent.Type {
	case "", "claude":
	case "command":
		if len(c.Agent.Command) == 0 {
			return errors.New(`config: agent.type = "command" needs agent.command, e.g. ["codex", "exec", "{prompt}"]`)
		}
	default:
		return fmt.Errorf("config: agent.type must be \"claude\" or \"command\", not %q", c.Agent.Type)
	}
	if c.StaleTimerHours < 0 {
		return errors.New("config: stale_timer_hours must be positive")
	}
	return nil
}

// StaleAfter returns the forgotten-timer threshold.
func (c Config) StaleAfter() time.Duration {
	if c.StaleTimerHours == 0 {
		return 4 * time.Hour
	}
	return time.Duration(c.StaleTimerHours * float64(time.Hour))
}

// ExpandHome turns a leading ~ into the home directory.
func ExpandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}
