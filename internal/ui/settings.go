package ui

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/YusufKer/worklog/internal/agent"
	"github.com/YusufKer/worklog/internal/config"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type settingField int

const (
	sfAgent settingField = iota
	sfModel
	sfClaudePath
	sfName
	sfCommand
	sfDir
	sfStale
)

const settingsWidth = 66

// modelChoices are offered for Claude; "" means the Claude Code default.
var modelChoices = []string{"", "haiku", "sonnet", "opus"}

// settingsForm edits config.toml: a form for the common settings, plus a
// raw TOML editor for everything else.
type settingsForm struct {
	path          string
	dirOverridden bool // -dir, $WORKLOG_DIR or -demo is in effect

	command bool // agent type: false = Claude Code, true = command
	models  []string
	model   int

	claudePath, name, cmdline, dir, stale textinput.Model

	field int // index into fields()
	err   string

	raw bool
	ta  textarea.Model
}

func newSettings(cfg config.Config, path string, dirOverridden bool) settingsForm {
	mk := func(value, placeholder string, width int) textinput.Model {
		in := textinput.New()
		in.Prompt = ""
		in.Width = width
		in.Placeholder = placeholder
		in.PlaceholderStyle = styleMuted
		in.SetValue(value)
		return in
	}
	auto := "claude on PATH"
	if p, err := exec.LookPath("claude"); err == nil {
		auto = p
	}
	if env := os.Getenv("WORKLOG_CLAUDE"); env != "" {
		auto = env
	}
	stale := ""
	if cfg.StaleTimerHours != 0 {
		stale = strconv.FormatFloat(cfg.StaleTimerHours, 'f', -1, 64)
	}

	f := settingsForm{
		path:          path,
		dirOverridden: dirOverridden,
		command:       cfg.Agent.Type == "command",
		models:        append([]string(nil), modelChoices...),
		claudePath:    mk(cfg.Agent.ClaudePath, "auto: "+auto, settingsWidth-22),
		name:          mk(cfg.Agent.Name, "e.g. codex", settingsWidth-22),
		cmdline:       mk(config.JoinArgs(cfg.Agent.Command), "e.g. codex exec {prompt}", settingsWidth-22),
		dir:           mk(shortenHome(cfg.Dir), "~/worklog", settingsWidth-22),
		stale:         mk(stale, "4", 6),
	}
	f.model = indexOf(f.models, cfg.Agent.Model)
	if f.model < 0 { // a custom model ID from the file
		f.models = append(f.models, cfg.Agent.Model)
		f.model = len(f.models) - 1
	}

	ta := textarea.New()
	ta.Prompt = ""
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.MaxHeight = 0
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	f.ta = ta
	return f
}

func indexOf(xs []string, s string) int {
	for i, x := range xs {
		if x == s {
			return i
		}
	}
	return -1
}

func (f *settingsForm) fields() []settingField {
	if f.command {
		return []settingField{sfAgent, sfName, sfCommand, sfDir, sfStale}
	}
	return []settingField{sfAgent, sfModel, sfClaudePath, sfDir, sfStale}
}

func (f *settingsForm) current() settingField { return f.fields()[f.field] }

func (f *settingsForm) input(sf settingField) *textinput.Model {
	switch sf {
	case sfClaudePath:
		return &f.claudePath
	case sfName:
		return &f.name
	case sfCommand:
		return &f.cmdline
	case sfDir:
		return &f.dir
	case sfStale:
		return &f.stale
	}
	return nil
}

// focusCurrent focuses the current field's input, if it has one.
func (f *settingsForm) focusCurrent() tea.Cmd {
	for _, in := range []*textinput.Model{&f.claudePath, &f.name, &f.cmdline, &f.dir, &f.stale} {
		in.Blur()
	}
	if in := f.input(f.current()); in != nil {
		return in.Focus()
	}
	return nil
}

func (f *settingsForm) move(delta int) tea.Cmd {
	n := len(f.fields())
	f.field = (f.field + delta + n) % n
	return f.focusCurrent()
}

// cycle changes a choice field; it reports whether the field was a choice.
func (f *settingsForm) cycle(delta int) bool {
	switch f.current() {
	case sfAgent:
		f.command = !f.command
	case sfModel:
		n := len(f.models)
		f.model = (f.model + delta + n) % n
	default:
		return false
	}
	return true
}

// config builds and validates a Config from the form.
func (f *settingsForm) config() (config.Config, error) {
	var c config.Config
	c.Dir = strings.TrimSpace(f.dir.Value())
	if v := strings.TrimSpace(f.stale.Value()); v != "" {
		h, err := strconv.ParseFloat(v, 64)
		if err != nil || h <= 0 {
			return c, errors.New("Stale timer must be a positive number of hours")
		}
		c.StaleTimerHours = h
	}
	if f.command {
		c.Agent.Type = "command"
		c.Agent.Name = strings.TrimSpace(f.name.Value())
		args, err := config.SplitArgs(strings.TrimSpace(f.cmdline.Value()))
		if err != nil {
			return c, errors.New("Command: " + err.Error())
		}
		if len(args) == 0 {
			return c, errors.New("Command is required, e.g. codex exec {prompt}")
		}
		if _, err := exec.LookPath(config.ExpandHome(args[0])); err != nil {
			return c, errors.New("Command not found: " + args[0])
		}
		args[0] = config.ExpandHome(args[0])
		c.Agent.Command = args
	} else {
		c.Agent.Type = "claude"
		c.Agent.Model = f.models[f.model]
		c.Agent.ClaudePath = strings.TrimSpace(f.claudePath.Value())
		if p := c.Agent.ClaudePath; p != "" {
			if _, err := exec.LookPath(config.ExpandHome(p)); err != nil {
				return c, errors.New("Claude path not found: " + p)
			}
		}
	}
	return c, c.Validate()
}

// startRaw opens the TOML editor with the file's current text, or a
// commented template if it doesn't exist yet.
func (f *settingsForm) startRaw(height int) tea.Cmd {
	text := ""
	if b, err := os.ReadFile(f.path); err == nil {
		text = string(b)
	} else if c, err := f.config(); err == nil {
		text = config.Render(c)
	} else {
		text = config.Render(config.Config{})
	}
	f.raw, f.err = true, ""
	f.ta.SetWidth(settingsWidth - 6)
	f.ta.SetHeight(max(height, 5))
	f.ta.SetValue(strings.TrimRight(text, "\n"))
	cmd := f.ta.Focus()
	f.ta, _ = f.ta.Update(tea.KeyMsg{Type: tea.KeyCtrlHome})
	return cmd
}

func (f *settingsForm) update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	if f.raw {
		f.ta, cmd = f.ta.Update(msg)
		return cmd
	}
	if in := f.input(f.current()); in != nil {
		*in, cmd = in.Update(msg)
	}
	return cmd
}

func (f settingsForm) view() string {
	var lines []string
	if f.raw {
		lines = append(lines, f.ta.View())
	} else {
		for i, sf := range f.fields() {
			lines = append(lines, f.row(sf, i == f.field))
			if sf == sfClaudePath || sf == sfCommand {
				lines = append(lines, "")
			}
		}
		lines = append(lines, "", f.hint())
	}
	if f.err != "" {
		lines = append(lines, "", styleError.Render(ansi.Wrap(f.err, settingsWidth-6, "")))
	}
	lines = append(lines, "", styleMuted.Render(ansi.Truncate(shortenHome(f.path), settingsWidth-6, "…")))

	title := "Settings"
	if f.raw {
		title = "Settings · config.toml"
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorAccent).
		Padding(1, 2).
		Width(settingsWidth).
		Render(styleSelected.Render(title) + "\n\n" + strings.Join(lines, "\n"))
}

func (f settingsForm) row(sf settingField, active bool) string {
	labels := map[settingField]string{
		sfAgent: "Agent", sfModel: "Model", sfClaudePath: "Claude path",
		sfName: "Name", sfCommand: "Command", sfDir: "Data folder", sfStale: "Stale timer",
	}
	label := lipgloss.NewStyle().Width(14).Render(labels[sf])
	if active {
		label = styleSelected.Render(label)
	} else {
		label = styleMuted.Render(label)
	}

	choice := func(v string) string {
		if active {
			return styleSelected.Render("◀ ") + v + styleSelected.Render(" ▶")
		}
		return "  " + v
	}
	var value string
	switch sf {
	case sfAgent:
		value = choice(map[bool]string{false: "Claude Code", true: "Command"}[f.command])
	case sfModel:
		m := f.models[f.model]
		if m == "" {
			m = "default"
		}
		value = choice(m)
	case sfStale:
		value = "  " + f.stale.View() + styleMuted.Render(" hours")
	default:
		value = "  " + f.input(sf).View()
	}
	return label + value
}

// hint explains the focused field.
func (f settingsForm) hint() string {
	var h string
	switch f.current() {
	case sfAgent:
		h = "Claude Code streams replies, shows tool calls and cost, and remembers each todo's conversation. Command runs any CLI agent."
	case sfModel:
		h = "default uses your Claude Code setting. haiku is fastest and cheapest (~$0.02/message), opus the most capable (~$0.20)."
	case sfClaudePath:
		h = "Leave empty to find claude automatically."
	case sfName:
		h = "Shown in the agent pane title."
	case sfCommand:
		h = "Placeholders: {prompt} (message + context), {message}, {notes_file}, {title}, {dir}."
	case sfDir:
		h = "Where todos are stored. Switching loads the todos in that folder."
		if f.dirOverridden {
			h = "Currently overridden by -dir, $WORKLOG_DIR or -demo; saved for next time."
		}
	case sfStale:
		h = "Ask about timers that have run longer than this."
	}
	return styleMuted.Render(ansi.Wrap(h, settingsWidth-6, ""))
}

func shortenHome(p string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, home+"/") {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}

// BackendFor builds the agent backend described by the config.
func BackendFor(a config.Agent) agent.Backend {
	if a.Type == "command" {
		return agent.Command{Label: a.Name, Argv: a.Command}
	}
	return agent.Claude{Bin: config.ExpandHome(a.ClaudePath), Model: a.Model}
}
