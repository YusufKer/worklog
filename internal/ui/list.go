package ui

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/yusuf/worklog/internal/store"
)

// listModel is the date-grouped todo list on the left.
type listModel struct {
	groups []store.Group
	todos  []*store.Todo // display order; the cursor indexes into this
	cursor int
	offset int // first visible row
	height int // visible rows
}

type listRow struct {
	header  string
	date    string // group date, for headers
	todo    int    // index into todos, -1 for headers/spacers
	carried bool
}

func (l *listModel) setTodos(todos []*store.Todo) {
	l.groups = store.GroupByDate(todos, time.Now())
	l.todos = store.Flatten(l.groups)
	l.cursor = clamp(l.cursor, 0, len(todos)-1)
	l.scroll()
}

func (l *listModel) selected() *store.Todo {
	if len(l.todos) == 0 {
		return nil
	}
	return l.todos[l.cursor]
}

func (l *listModel) selectID(id string) {
	for i, t := range l.todos {
		if t.ID == id {
			l.cursor = i
			break
		}
	}
	l.scroll()
}

func (l *listModel) setHeight(h int) {
	l.height = h
	l.scroll()
}

func (l *listModel) move(delta int) {
	l.cursor = clamp(l.cursor+delta, 0, len(l.todos)-1)
	l.scroll()
}

func (l *listModel) top()    { l.cursor = 0; l.scroll() }
func (l *listModel) bottom() { l.cursor = max(len(l.todos)-1, 0); l.scroll() }

func (l *listModel) rows() []listRow {
	var rows []listRow
	i := 0
	for gi, g := range l.groups {
		if gi > 0 {
			rows = append(rows, listRow{todo: -1})
		}
		rows = append(rows, listRow{header: g.Label, date: g.Date, todo: -1})
		for _, t := range g.Todos {
			rows = append(rows, listRow{todo: i, carried: g.Carried(t)})
			i++
		}
	}
	return rows
}

// scroll adjusts the offset so the cursor (and its date header, when
// possible) stays within the visible height.
func (l *listModel) scroll() {
	rows := l.rows()
	cursorRow := 0
	for i, r := range rows {
		if r.todo == l.cursor {
			cursorRow = i
			break
		}
	}
	if cursorRow < l.offset {
		l.offset = cursorRow
	}
	if cursorRow > 0 && rows[cursorRow-1].header != "" && cursorRow-1 < l.offset {
		l.offset = cursorRow - 1
	}
	if cursorRow >= l.offset+l.height {
		l.offset = cursorRow - l.height + 1
	}
	l.offset = clamp(l.offset, 0, max(len(rows)-l.height, 0))
}

func (l listModel) view(width int, focused bool) string {
	if len(l.todos) == 0 {
		return styleMuted.Render(" No todos yet.")
	}
	rows := l.rows()
	now := time.Now()
	var out []string
	for _, r := range rows[min(l.offset, len(rows)):min(l.offset+l.height, len(rows))] {
		out = append(out, l.renderRow(r, width, focused, now))
	}
	return strings.Join(out, "\n")
}

func (l listModel) renderRow(r listRow, width int, focused bool, now time.Time) string {
	if r.todo < 0 {
		if r.header == "" {
			return ""
		}
		h := styleHeader.Render(" " + r.header)
		if worked := store.WorkedOn(l.todos, r.date, now); worked >= time.Minute {
			h += styleMuted.Render(" · " + store.FormatDuration(worked))
		}
		return h
	}
	t := l.todos[r.todo]
	sel := r.todo == l.cursor

	// Every segment carries the highlight background itself; wrapping the
	// whole line would be cut short by each segment's style reset.
	plain := lipgloss.NewStyle()
	st := func(s lipgloss.Style) lipgloss.Style {
		if sel && focused {
			return s.Background(colorSelBg)
		}
		return s
	}

	marker := st(plain).Render("  ")
	if sel {
		marker = st(styleSelected).Render("▶ ")
	}
	icon := st(styleMuted).Render("○")
	if t.Done {
		icon = st(styleDoneIcon).Render("✓")
	}

	// Right-hand detail: live timer, else total time, else carried-from date.
	var right string
	switch e := t.Running(); {
	case e != nil:
		right = st(styleSelected).Render("⏱ " + store.FormatClock(now.Sub(e.Start)))
	case t.Total(now) >= time.Minute:
		right = st(styleMuted).Render(store.FormatDuration(t.Total(now)))
	case r.carried:
		right = st(styleMuted).Render("↩ " + shortDate(t.Date))
	}
	rightW := lipgloss.Width(right)

	titleW := max(width-5-rightW-2, 1)
	if rightW == 0 {
		titleW = max(width-5, 1)
	}
	title := ansi.Truncate(t.Title, titleW, "…")
	switch {
	case t.Done:
		title = st(styleDone).Render(title)
	case sel:
		title = st(styleSelected).Render(title)
	default:
		title = st(plain).Render(title)
	}

	sp := func(n int) string { return st(plain).Render(strings.Repeat(" ", max(n, 0))) }
	line := sp(1) + marker + icon + sp(1) + title
	if right != "" {
		line += sp(max(width-lipgloss.Width(line)-rightW-1, 1)) + right
	}
	return line + sp(width-lipgloss.Width(line))
}

func shortDate(date string) string {
	d, err := time.Parse(store.DateLayout, date)
	if err != nil {
		return date
	}
	return d.Format("Jan 2")
}

func clamp(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	return min(max(v, lo), hi)
}
