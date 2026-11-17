package ui

import "strings"

type hint struct{ key, desc string }

func helpFor(f focus) []hint {
	switch f {
	case focusNotes:
		return []hint{{"j/k", "scroll"}, {"g/G", "top/bottom"}, {"h/esc", "back"}, {"tab", "focus"}, {"q", "quit"}}
	case focusAgent:
		return []hint{{"esc", "back"}, {"tab", "focus"}, {"q", "quit"}}
	default:
		return []hint{{"j/k", "move"}, {"n", "new"}, {"x", "done"}, {"d", "delete"}, {"enter", "open"}, {"tab", "focus"}, {"q", "quit"}}
	}
}

func renderHelp(hints []hint) string {
	parts := make([]string, len(hints))
	for i, h := range hints {
		parts[i] = styleHelpKey.Render(h.key) + " " + styleHelp.Render(h.desc)
	}
	return " " + strings.Join(parts, styleHelp.Render(" · "))
}
