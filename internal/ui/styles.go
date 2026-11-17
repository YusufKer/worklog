package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var (
	colorAccent = lipgloss.AdaptiveColor{Light: "#5A3FD6", Dark: "#9D8CFF"}
	colorBorder = lipgloss.AdaptiveColor{Light: "#C4C4CC", Dark: "#45454F"}
	colorMuted  = lipgloss.AdaptiveColor{Light: "#7A7A85", Dark: "#8A8A96"}
	colorSelBg  = lipgloss.AdaptiveColor{Light: "#E8E4FF", Dark: "#2E2A4A"}
	colorDone   = lipgloss.AdaptiveColor{Light: "#3C9A5F", Dark: "#6BCB8B"}
	colorError  = lipgloss.AdaptiveColor{Light: "#C23B3B", Dark: "#FF7A7A"}

	styleHeader   = lipgloss.NewStyle().Bold(true).Foreground(colorMuted)
	styleMuted    = lipgloss.NewStyle().Foreground(colorMuted)
	styleSelected = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	styleDone     = lipgloss.NewStyle().Foreground(colorMuted).Strikethrough(true)
	styleDoneIcon = lipgloss.NewStyle().Foreground(colorDone)
	styleError    = lipgloss.NewStyle().Foreground(colorError)
	styleHelpKey  = lipgloss.NewStyle().Bold(true)
	styleHelp     = lipgloss.NewStyle().Foreground(colorMuted)
)

// renderPane draws a rounded box of exactly w×h cells with the title set
// into the top border. Body lines are truncated and padded to fit.
func renderPane(title, body string, w, h int, focused bool) string {
	if w < 4 || h < 2 {
		return ""
	}
	iw, ih := w-2, h-2

	border := lipgloss.NewStyle().Foreground(colorBorder)
	titleStyle := lipgloss.NewStyle().Foreground(colorMuted)
	if focused {
		border = border.Foreground(colorAccent)
		titleStyle = titleStyle.Foreground(colorAccent).Bold(true)
	}

	title = ansi.Truncate(title, max(iw-4, 0), "…")
	top := border.Render("╭─") + " " + titleStyle.Render(title) + " "
	fill := iw - 1 - 2 - lipgloss.Width(title)
	if fill < 0 {
		fill = 0
	}
	top += border.Render(strings.Repeat("─", fill) + "╮")
	if lipgloss.Width(top) != w { // title too long for a tiny pane
		top = border.Render("╭" + strings.Repeat("─", iw) + "╮")
	}

	lines := strings.Split(body, "\n")
	var b strings.Builder
	b.WriteString(top)
	side := border.Render("│")
	for i := 0; i < ih; i++ {
		line := ""
		if i < len(lines) {
			line = ansi.Truncate(lines[i], iw, "")
		}
		pad := iw - lipgloss.Width(line)
		b.WriteString("\n" + side + line + strings.Repeat(" ", max(pad, 0)) + side)
	}
	b.WriteString("\n" + border.Render("╰"+strings.Repeat("─", iw)+"╯"))
	return b.String()
}
