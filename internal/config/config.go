// Package config loads worklog's optional TOML config file.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
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
	var c Config
	md, err := toml.DecodeFile(path, &c)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return c, fmt.Errorf("%s: %w", path, err)
	}
	if err == nil {
		if undec := md.Undecoded(); len(undec) > 0 {
			keys := make([]string, len(undec))
			for i, k := range undec {
				keys[i] = k.String()
			}
			return c, fmt.Errorf("%s: unknown setting(s): %s", path, strings.Join(keys, ", "))
		}
	}
	return c, c.validate()
}

func (c Config) validate() error {
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
