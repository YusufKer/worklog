package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/YusufKer/worklog/internal/store"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const timeLogRows = 10 // entries shown at once

// timeLog lists a todo's time entries and edits them.
type timeLog struct {
	todo     *store.Todo
	cursor   int // index into entries(), newest first
	offset   int
	editing  bool
	old      time.Time // start of the entry being edited; zero when adding
	running  bool      // the entry being edited is the running timer
	start    textinput.Model
	end      textinput.Model
	field    int // 0 = start, 1 = end
	deleting bool
	err      string
}

func newTimeLog(t *store.Todo) timeLog {
	return timeLog{todo: t}
}

// entries returns the todo's entries, newest first.
func (f *timeLog) entries() []store.TimeEntry {
	n := len(f.todo.Time)
	out := make([]store.TimeEntry, n)
	for i, e := range f.todo.Time {
		out[n-1-i] = e
	}
	return out
}

func (f *timeLog) selected() (store.TimeEntry, bool) {
	es := f.entries()
	if len(es) == 0 {
		return store.TimeEntry{}, false
	}
	return es[f.cursor], true
}

// setTodo refreshes the todo after a change, keeping the cursor in range.
func (f *timeLog) setTodo(t *store.Todo) {
	f.todo = t
	f.move(0)
}

func (f *timeLog) move(delta int) {
	f.cursor = clamp(f.cursor+delta, 0, len(f.todo.Time)-1)
	if f.cursor < f.offset {
		f.offset = f.cursor
	}
	if f.cursor >= f.offset+timeLogRows {
		f.offset = f.cursor - timeLogRows + 1
	}
}

func timeInput(value string) textinput.Model {
	in := textinput.New()
	in.Prompt = ""
	in.CharLimit = 16
	in.Width = 16
	in.SetValue(value)
	return in
}

// add opens the form for a new entry: the hour before now.
func (f *timeLog) add(now time.Time) {
	now = now.Truncate(time.Minute)
	start := now.Add(-time.Hour)
	f.openForm(time.Time{}, false, start.Format("2006-01-02 15:04"), endText(start, now))
}

// edit opens the form for the selected entry.
func (f *timeLog) edit() bool {
	e, ok := f.selected()
	if !ok {
		return false
	}
	end := ""
	if e.End != nil {
		end = endText(e.Start, *e.End)
	}
	f.openForm(e.Start, e.End == nil, e.Start.Local().Format("2006-01-02 15:04"), end)
	return true
}

// endText shows end as HH:MM when it's on the start's day.
func endText(start, end time.Time) string {
	start, end = start.Local(), end.Local()
	if start.Format(store.DateLayout) == end.Format(store.DateLayout) {
		return end.Format("15:04")
	}
	return end.Format("2006-01-02 15:04")
}

func (f *timeLog) openForm(old time.Time, running bool, start, end string) {
	f.editing, f.old, f.running, f.err = true, old, running, ""
	f.start, f.end = timeInput(start), timeInput(end)
	// Start with End, the usual fix, unless there is none to change.
	f.field = 1
	if running {
		f.field = 0
	}
	f.input().Focus()
	f.input().CursorEnd()
}

func (f *timeLog) switchField() {
	f.field = 1 - f.field
	if f.field == 0 {
		f.end.Blur()
		f.start.Focus()
	} else {
		f.start.Blur()
		f.end.Focus()
	}
}

func (f *timeLog) input() *textinput.Model {
	if f.field == 0 {
		return &f.start
	}
	return &f.end
}

// entry parses the form. Start is "YYYY-MM-DD HH:MM", or "HH:MM" on the
// entry's day (today when adding). End is "HH:MM" on the start's day,
// "YYYY-MM-DD HH:MM", a duration such as "1h30m", or empty to keep a
// running timer running.
func (f *timeLog) entry(now time.Time) (store.TimeEntry, error) {
	day := now
	if !f.old.IsZero() {
		day = f.old.Local()
	}
	start, err := parseClock(f.start.Value(), day)
	if err != nil {
		return store.TimeEntry{}, fmt.Errorf("Start: %w", err)
	}
	e := store.TimeEntry{Start: start}
	v := strings.TrimSpace(f.end.Value())
	if v == "" {
		if !f.running {
			return e, fmt.Errorf("End is required")
		}
		return e, nil
	}
	end, err := parseClock(v, start)
	if err != nil {
		d, derr := time.ParseDuration(strings.ReplaceAll(v, " ", ""))
		if derr != nil || d <= 0 {
			return e, fmt.Errorf("End: use HH:MM, YYYY-MM-DD HH:MM or a duration like 1h30m")
		}
		end = start.Add(d)
	}
	e.End = &end
	return e, nil
}

// parseClock parses "YYYY-MM-DD HH:MM", or "HH:MM" on day's date.
func parseClock(s string, day time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	loc := day.Location()
	if t, err := time.ParseInLocation("2006-01-02 15:04", s, loc); err == nil {
		return t, nil
	}
	hm, err := time.ParseInLocation("15:04", s, loc)
	if err != nil {
		return time.Time{}, fmt.Errorf("use HH:MM or YYYY-MM-DD HH:MM")
	}
	y, m, d := day.Date()
	return time.Date(y, m, d, hm.Hour(), hm.Minute(), 0, 0, loc), nil
}

func (f timeLog) view(now time.Time) string {
	title := ansi.Truncate(f.todo.Title, modalWidth-8, "…")
	var lines []string
	lines = append(lines, lipgloss.NewStyle().Bold(true).Render(title))
	if f.editing {
		label := func(s string, active bool) string {
			if active {
				return styleSelected.Render(s)
			}
			return styleMuted.Render(s)
		}
		lines = append(lines, "",
			label("Start ", f.field == 0)+f.start.View(),
			label("End   ", f.field == 1)+f.end.View(),
			styleMuted.Render("      HH:MM, YYYY-MM-DD HH:MM or 1h30m"))
		if f.running {
			lines = append(lines, styleMuted.Render("      Leave End empty to keep it running"))
		}
		if f.err != "" {
			lines = append(lines, "", styleError.Render(f.err))
		}
		heading := "Add time"
		if !f.old.IsZero() {
			heading = "Edit time"
		}
		return modalBox(heading, strings.Join(lines, "\n"))
	}

	lines = append(lines, "")
	es := f.entries()
	if len(es) == 0 {
		lines = append(lines, styleMuted.Render("No time tracked yet. Press a to add some."))
	}
	for i := f.offset; i < min(f.offset+timeLogRows, len(es)); i++ {
		lines = append(lines, f.row(es[i], i == f.cursor, now))
	}
	if len(es) > timeLogRows {
		lines = append(lines, styleMuted.Render(fmt.Sprintf("  %d–%d of %d", f.offset+1, min(f.offset+timeLogRows, len(es)), len(es))))
	}
	lines = append(lines, "", styleMuted.Render("Total ")+store.FormatDuration(f.todo.Total(now)))
	if f.deleting {
		if e, ok := f.selected(); ok {
			lines = append(lines, "", styleError.Render("Delete "+store.FormatDuration(e.Duration(now))+" from "+
				e.Start.Local().Format("Mon Jan 2 15:04")+"? y/n"))
		}
	}
	if f.err != "" {
		lines = append(lines, "", styleError.Render(f.err))
	}
	return modalBox("Time", strings.Join(lines, "\n"))
}

func (f timeLog) row(e store.TimeEntry, sel bool, now time.Time) string {
	start := e.Start.Local()
	day := store.DateLabel(start.Format(store.DateLayout), now)
	span := start.Format("15:04") + "–"
	dur := store.FormatDuration(e.Duration(now))
	switch {
	case e.End == nil:
		span += "now"
		dur = "⏱ " + store.FormatClock(e.Duration(now))
	case e.End.Local().Format(store.DateLayout) != start.Format(store.DateLayout):
		span += e.End.Local().Format("15:04") + " +1d"
	default:
		span += e.End.Local().Format("15:04")
	}
	marker := "  "
	style := lipgloss.NewStyle()
	if sel {
		marker = "▶ "
		style = styleSelected
	}
	return style.Render(fmt.Sprintf("%s%-11s %-16s %8s", marker, ansi.Truncate(day, 11, "…"), span, dur))
}
