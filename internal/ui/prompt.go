package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/YusufKer/worklog/internal/store"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type modalKind int

const (
	modalNone modalKind = iota
	modalTodo           // new or edit todo form
	modalDelete
	modalStaleTimer
	modalSettings
	modalKeys
)

const modalWidth = 52

// todoForm collects a title and a date, for a new todo or for editing one.
type todoForm struct {
	edit  *store.Todo // nil when creating
	title textinput.Model
	date  textinput.Model
	field int // 0 = title, 1 = date
	err   string
}

// newForm is the form for a new todo, dated today.
func newForm() todoForm {
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

	f := todoForm{title: title, date: date}
	f.title.Focus()
	return f
}

// editForm is the form for renaming or rescheduling t.
func editForm(t *store.Todo) todoForm {
	f := newForm()
	f.edit = t
	f.title.SetValue(t.Title)
	f.date.SetValue(t.Date)
	return f
}

// resolveDate expands today, tomorrow, yesterday and +N/-N (days from
// today) into YYYY-MM-DD. Anything else is returned trimmed, for the store
// to validate.
func resolveDate(s string, now time.Time) string {
	s = strings.ToLower(strings.TrimSpace(s))
	days := 0
	switch s {
	case "", "today", "tod":
	case "tomorrow", "tom":
		days = 1
	case "yesterday":
		days = -1
	default:
		n, err := strconv.Atoi(s)
		if err != nil || (s[0] != '+' && s[0] != '-') {
			return s
		}
		days = n
	}
	return now.AddDate(0, 0, days).Format(store.DateLayout)
}

func (f *todoForm) switchField() {
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

func (f *todoForm) update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	if f.field == 0 {
		f.title, cmd = f.title.Update(msg)
	} else {
		f.date, cmd = f.date.Update(msg)
	}
	return cmd
}

func (f todoForm) view() string {
	label := func(s string, active bool) string {
		if active {
			return styleSelected.Render(s)
		}
		return styleMuted.Render(s)
	}
	lines := []string{
		label("Title ", f.field == 0) + f.title.View(),
		label("Date  ", f.field == 1) + f.date.View(),
		styleMuted.Render("      YYYY-MM-DD, today, tomorrow, +N, -N"),
	}
	if f.err != "" {
		lines = append(lines, "", styleError.Render(f.err))
	}
	heading := "New todo"
	if f.edit != nil {
		heading = "Edit todo"
	}
	return modalBox(heading, strings.Join(lines, "\n"))
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

// staleTimerForm asks what to do with a timer that looks forgotten.
type staleTimerForm struct {
	todo  *store.Todo
	start time.Time
	input textinput.Model
	err   string
}

func newStaleForm(t *store.Todo, now time.Time) staleTimerForm {
	start := t.Running().Start.In(now.Location())
	// Suggest 17:00 on the day it started, or an hour in if it started later.
	y, mo, d := start.Date()
	suggest := time.Date(y, mo, d, 17, 0, 0, 0, start.Location())
	if !suggest.After(start) {
		suggest = start.Add(time.Hour)
	}
	if suggest.After(now) {
		suggest = now
	}
	in := textinput.New()
	in.Prompt = ""
	in.CharLimit = 16
	in.Width = 16
	in.SetValue(suggest.Format("2006-01-02 15:04"))
	in.Focus()
	return staleTimerForm{todo: t, start: start, input: in}
}

// stopTime parses "HH:MM" (on the start day) or "YYYY-MM-DD HH:MM".
func (f staleTimerForm) stopTime(now time.Time) (time.Time, error) {
	v := strings.TrimSpace(f.input.Value())
	loc := now.Location()
	t, err := time.ParseInLocation("2006-01-02 15:04", v, loc)
	if err != nil {
		hm, err2 := time.ParseInLocation("15:04", v, loc)
		if err2 != nil {
			return time.Time{}, fmt.Errorf("Use HH:MM or YYYY-MM-DD HH:MM")
		}
		y, mo, d := f.start.Date()
		t = time.Date(y, mo, d, hm.Hour(), hm.Minute(), 0, 0, loc)
	}
	switch {
	case !t.After(f.start):
		return time.Time{}, fmt.Errorf("Must be after it started (%s)", f.start.Format("Mon 15:04"))
	case t.After(now):
		return time.Time{}, fmt.Errorf("Can't be in the future")
	}
	return t, nil
}

func (f staleTimerForm) view(now time.Time) string {
	title := ansi.Truncate(f.todo.Title, modalWidth-8, "…")
	body := "Timer still running on " + lipgloss.NewStyle().Bold(true).Render(title) + "\n" +
		styleMuted.Render(fmt.Sprintf("since %s (%s)", f.start.Format("Mon Jan 2 15:04"),
			store.FormatDuration(now.Sub(f.start)))) + "\n\n" +
		"Stop at " + f.input.View()
	if f.err != "" {
		body += "\n" + styleError.Render(f.err)
	}
	return modalBox("Forgotten timer?", body)
}
