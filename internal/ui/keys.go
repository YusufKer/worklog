package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type hint struct{ key, desc string }

// helpFor is the footer for a pane: the most-used keys only. The ? overlay
// lists everything.
func helpFor(f focus) []hint {
	switch f {
	case focusNotes:
		return []hint{{"j/k", "scroll"}, {"e", "edit"}, {"t", "timer"}, {"a", "ask"}, {"h/esc", "back"}, {"?", "keys"}, {"q", "quit"}}
	case focusAgent:
		return []hint{{"enter", "send"}, {"esc", "stop/back"}, {"↑/↓", "scroll"}, {"/clear", "new chat"}, {"tab", "focus"}, {"ctrl+c", "quit"}}
	default:
		return []hint{{"j/k", "move"}, {"n", "new"}, {"x", "done"}, {"e", "edit"}, {"t", "timer"}, {"a", "ask"}, {"/", "search"}, {"?", "keys"}, {"q", "quit"}}
	}
}

func renderHelp(hints []hint) string {
	parts := make([]string, len(hints))
	for i, h := range hints {
		parts[i] = styleHelpKey.Render(h.key) + " " + styleHelp.Render(h.desc)
	}
	return " " + strings.Join(parts, styleHelp.Render(" · "))
}

type keySection struct {
	title string
	keys  []hint
}

// Every key, for the ? overlay, in two columns.
var keysLeft = []keySection{
	{"List", []hint{
		{"j/k ↑/↓", "move"}, {"g/G", "top/bottom"}, {"n", "new todo"}, {"r", "rename/reschedule"},
		{"x space", "toggle done"}, {"d", "delete"}, {"/", "search"}, {".", "hide done"},
		{"esc", "clear filter"}, {"enter l", "go to notes"}, {"q", "quit"},
	}},
	{"Notes", []hint{{"j/k g/G", "scroll"}, {"esc h", "back to list"}}},
}

var keysRight = []keySection{
	{"List and notes", []hint{
		{"e", "edit notes"}, {"t", "start/stop timer"}, {"T", "time entries"}, {"a i", "ask the agent"}, {",", "settings"}, {"?", "this help"},
	}},
	{"Editing notes", []hint{{"esc", "save and close"}, {"ctrl+s", "save"}}},
	{"Agent", []hint{{"enter", "send"}, {"esc", "stop / back"}, {"↑/↓ pgup", "scroll"}, {"/clear", "new chat"}}},
	{"Everywhere", []hint{{"tab", "next pane"}, {"ctrl+c", "quit"}}},
}

func keysColumn(sections []keySection) string {
	var lines []string
	for i, s := range sections {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, styleHeader.Render(s.title))
		for _, h := range s.keys {
			lines = append(lines, styleHelpKey.Render(padRight(h.key, 9))+" "+styleHelp.Render(h.desc))
		}
	}
	return strings.Join(lines, "\n")
}

func padRight(s string, w int) string {
	return s + strings.Repeat(" ", max(w-lipgloss.Width(s), 0))
}

func keysView() string {
	body := lipgloss.JoinHorizontal(lipgloss.Top,
		keysColumn(keysLeft), "    ", keysColumn(keysRight))
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorAccent).
		Padding(1, 2).
		Render(styleSelected.Render("Keys") + "\n\n" + body)
}
