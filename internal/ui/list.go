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
	todos  []*store.Todo // sorted; the cursor indexes into this
	cursor int
	offset int // first visible row
	height int // visible rows
}

type listRow struct {
	header string
	todo   int // index into todos, -1 for headers/spacers
}

func (l *listModel) setTodos(todos []*store.Todo) {
	l.todos = todos
	l.cursor = clamp(l.cursor, 0, len(todos)-1)
	l.scroll()
}

func (l *listModel) selected() *store.Todo {
	if len(l.todos) == 0 {
		return nil
	}
	return l.todos[l.cursor]
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
	for gi, g := range store.GroupByDate(l.todos, time.Now()) {
		if gi > 0 {
			rows = append(rows, listRow{todo: -1})
		}
		rows = append(rows, listRow{header: g.Label, todo: -1})
		for range g.Todos {
			rows = append(rows, listRow{todo: i})
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
	var out []string
	for _, r := range rows[min(l.offset, len(rows)):min(l.offset+l.height, len(rows))] {
		out = append(out, l.renderRow(r, width, focused))
	}
	return strings.Join(out, "\n")
}

func (l listModel) renderRow(r listRow, width int, focused bool) string {
	if r.todo < 0 {
		if r.header == "" {
			return ""
		}
		return styleHeader.Render(" " + r.header)
	}
	t := l.todos[r.todo]
	sel := r.todo == l.cursor

	marker := "  "
	if sel {
		marker = styleSelected.Render("▶ ")
	}
	icon := styleMuted.Render("○")
	if t.Done {
		icon = styleDoneIcon.Render("✓")
	}

	titleW := max(width-5, 1)
	title := ansi.Truncate(t.Title, titleW, "…")
	switch {
	case t.Done:
		title = styleDone.Render(title)
	case sel:
		title = styleSelected.Render(title)
	}

	line := " " + marker + icon + " " + title
	if sel && focused {
		line = lipgloss.NewStyle().Background(colorSelBg).Width(width).Render(line)
	}
	return line
}

func clamp(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	return min(max(v, lo), hi)
}
