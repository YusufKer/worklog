package ui

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/yusuf/worklog/internal/store"
)

type modalKind int

const (
	modalNone modalKind = iota
	modalNew
	modalDelete
)

const modalWidth = 52

// newTodoForm collects a title and a date (defaulting to today).
type newTodoForm struct {
	title textinput.Model
	date  textinput.Model
	field int // 0 = title, 1 = date
	err   string
}

func newForm() newTodoForm {
	title := textinput.New()
	title.Placeholder = "What needs doing?"
	title.Prompt = ""
	title.CharLimit = 200
	title.Width = modalWidth - 12

	date := textinput.New()
	date.Prompt = ""
	date.CharLimit = 10
	date.Width = 10
	date.SetValue(time.Now().Format(store.DateLayout))

	f := newTodoForm{title: title, date: date}
	f.title.Focus()
	return f
}

func (f *newTodoForm) switchField() {
	f.field = 1 - f.field
	if f.field == 0 {
		f.date.Blur()
		f.title.Focus()
	} else {
		f.title.Blur()
		f.date.Focus()
		f.date.CursorEnd()
	}
}

func (f *newTodoForm) update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	if f.field == 0 {
		f.title, cmd = f.title.Update(msg)
	} else {
		f.date, cmd = f.date.Update(msg)
	}
	return cmd
}

func (f newTodoForm) view() string {
	label := func(s string, active bool) string {
		if active {
			return styleSelected.Render(s)
		}
		return styleMuted.Render(s)
	}
	lines := []string{
		label("Title ", f.field == 0) + f.title.View(),
		label("Date  ", f.field == 1) + f.date.View(),
	}
	if f.err != "" {
		lines = append(lines, "", styleError.Render(f.err))
	}
	return modalBox("New todo", strings.Join(lines, "\n"))
}

func deleteView(t *store.Todo) string {
	title := ansi.Truncate(t.Title, modalWidth-8, "…")
	body := "Delete " + lipgloss.NewStyle().Bold(true).Render(title) + "?\n" +
		styleMuted.Render("It will be moved to .trash/")
	return modalBox("Delete todo", body)
}

func modalBox(title, body string) string {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorAccent).
		Padding(1, 2).
		Width(modalWidth).
		Render(styleSelected.Render(title) + "\n\n" + body)
}

// overlay draws fg centred on top of bg, which is width×height cells.
func overlay(bg, fg string, width, height int) string {
	bgLines := strings.Split(bg, "\n")
	fgLines := strings.Split(fg, "\n")
	fgW := lipgloss.Width(fg)
	x := max((width-fgW)/2, 0)
	y := max((height-len(fgLines))/2, 0)
	for i, fl := range fgLines {
		row := y + i
		if row >= len(bgLines) {
			break
		}
		bl := bgLines[row]
		left := ansi.Truncate(bl, x, "")
		left += strings.Repeat(" ", max(x-lipgloss.Width(left), 0))
		right := ansi.TruncateLeft(bl, x+fgW, "")
		bgLines[row] = left + "\x1b[0m" + fl + "\x1b[0m" + right
	}
	return strings.Join(bgLines, "\n")
}
