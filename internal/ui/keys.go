package ui

import "strings"

type hint struct{ key, desc string }

func helpFor(f focus) []hint {
	switch f {
	case focusNotes:
		return []hint{{"j/k", "scroll"}, {"g/G", "top/bottom"}, {"e", "edit"}, {"a", "ask"}, {"h/esc", "back"}, {"tab", "focus"}, {"q", "quit"}}
	case focusAgent:
		return []hint{{"enter", "send"}, {"esc", "stop/back"}, {"↑/↓", "scroll"}, {"/clear", "new chat"}, {"tab", "focus"}, {"ctrl+c", "quit"}}
	default:
		return []hint{{"j/k", "move"}, {"n", "new"}, {"x", "done"}, {"d", "delete"}, {"e", "edit"}, {"a", "ask"}, {"tab", "focus"}, {"q", "quit"}}
	}
}

func renderHelp(hints []hint) string {
	parts := make([]string, len(hints))
	for i, h := range hints {
		parts[i] = styleHelpKey.Render(h.key) + " " + styleHelp.Render(h.desc)
	}
	return " " + strings.Join(parts, styleHelp.Render(" · "))
}
