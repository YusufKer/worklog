package ui

import "strings"

// agentModel is the AI pane. For now it is a placeholder; streaming from
// Claude Code arrives in a later stage.
type agentModel struct{}

func (a *agentModel) view(width, height int, focused bool) string {
	lines := []string{styleMuted.Render(" Ask your agent to research or edit these notes.")}
	for len(lines) < height-1 {
		lines = append(lines, "")
	}
	prompt := styleMuted.Render(" > ")
	if focused {
		prompt = styleSelected.Render(" > ") + styleMuted.Render("(agent coming soon)")
	}
	lines = append(lines, prompt)
	return strings.Join(lines, "\n")
}
