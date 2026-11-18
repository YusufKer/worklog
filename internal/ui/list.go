package ui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/YusufKer/worklog/internal/store"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// listModel is the date-grouped todo list on the left.
type listModel struct {
	all    []*store.Todo // every todo, filtered or not
	groups []store.Group
	todos  []*store.Todo // visible todos in display order; the cursor indexes into this
	cursor int
	offset int // first visible row
	height int // pane body height, including the filter bar

	filter   textinput.Model
	typing   bool   // the filter input has focus
	query    string // applied filter: every word must appear in the title or notes
	hideDone bool
}

func newList() listModel {
	in := textinput.New()
	in.Prompt = "/"
	in.Placeholder = "search titles and notes"
	in.CharLimit = 100
	return listModel{filter: in}
}

type listRow struct {
	header  string
	date    string // group date, for headers
	sub     string // sub-header within a group (the carried-over divider)
	todo    int    // index into todos, -1 for headers/spacers
	carried bool
}

func (l *listModel) setTodos(todos []*store.Todo) {
	l.all = todos
	l.apply()
}

// filtered reports whether some todos may be hidden.
func (l *listModel) filtered() bool { return l.query != "" || l.hideDone }

// showBar reports whether the filter bar takes the top row.
func (l *listModel) showBar() bool { return l.typing || l.query != "" }

// rowsHeight is the number of rows available for todos.
func (l *listModel) rowsHeight() int {
	if l.showBar() {
		return max(l.height-1, 0)
	}
	return l.height
}

// setQuery applies a new filter query.
func (l *listModel) setQuery(q string) {
	l.query = strings.TrimSpace(q)
	l.apply()
}

// clearQuery stops typing and removes the search query.
func (l *listModel) clearQuery() {
	l.typing, l.query = false, ""
	l.filter.SetValue("")
	l.filter.Blur()
	l.apply()
}

// clearFilter removes the search query and shows done todos again.
func (l *listModel) clearFilter() {
	l.hideDone = false
	l.clearQuery()
}

// apply rebuilds the visible list from all, keeping the selected todo if
// it is still visible.
func (l *listModel) apply() {
	keep := l.selected()
	terms := strings.Fields(strings.ToLower(l.query))
	var visible []*store.Todo
	for _, t := range l.all {
		if (!l.hideDone || !t.Done) && matches(t, terms) {
			visible = append(visible, t)
		}
	}
	l.groups = store.GroupByDate(visible, time.Now())
	l.todos = store.Flatten(l.groups)
	l.cursor = clamp(l.cursor, 0, len(l.todos)-1)
	if keep != nil {
		l.selectID(keep.ID)
	}
	l.scroll()
}

func matches(t *store.Todo, terms []string) bool {
	if len(terms) == 0 {
		return true
	}
	text := strings.ToLower(t.Title + "\n" + t.Notes)
	for _, term := range terms {
		if !strings.Contains(text, term) {
			return false
		}
	}
	return true
}

// reordered returns the selected todo's day (every todo with its date, in
// current order) with the selected one moved past its nearest visible
// neighbour in direction delta, or nil if there is nowhere to go. Carried-over
// todos have other dates, so a todo can't be moved among them.
func (l *listModel) reordered(delta int) []*store.Todo {
	sel := l.selected()
	if sel == nil {
		return nil
	}
	visible := map[string]bool{}
	for _, t := range l.todos {
		visible[t.ID] = true
	}
	var day []*store.Todo
	from := -1
	for _, t := range l.all {
		if t.Date != sel.Date {
			continue
		}
		if t.ID == sel.ID {
			from = len(day)
		}
		day = append(day, t)
	}
	to := -1
	for j := from + delta; j >= 0 && j < len(day); j += delta {
		if visible[day[j].ID] {
			to = j
			break
		}
	}
	if from < 0 || to < 0 {
		return nil
	}
	seq := slices.Delete(slices.Clone(day), from, from+1)
	return slices.Insert(seq, to, sel)
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
		divided := false
		for _, t := range g.Todos {
			carried := g.Carried(t)
			if carried && !divided {
				// Carried-over todos keep their own dates, so they can't be
				// reordered with today's; a divider shows where that starts.
				n := 0
				for _, x := range g.Todos {
					if g.Carried(x) {
						n++
					}
				}
				rows = append(rows, listRow{sub: fmt.Sprintf("↩ Carried over · %d", n), todo: -1})
				divided = true
			}
			rows = append(rows, listRow{todo: i, carried: carried})
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
	if cursorRow > 0 && (rows[cursorRow-1].header != "" || rows[cursorRow-1].sub != "") && cursorRow-1 < l.offset {
		l.offset = cursorRow - 1
	}
	h := l.rowsHeight()
	if cursorRow >= l.offset+h {
		l.offset = cursorRow - h + 1
	}
	l.offset = clamp(l.offset, 0, max(len(rows)-h, 0))
}

func (l listModel) view(width int, focused bool) string {
	var out []string
	if l.showBar() {
		bar := l.filter
		bar.Width = max(width-3, 1)
		if !l.typing {
			bar.PromptStyle = styleMuted
			bar.TextStyle = styleMuted
		}
		out = append(out, " "+bar.View())
	}
	switch {
	case len(l.all) == 0:
		out = append(out, styleMuted.Render(" No todos yet."))
	case len(l.todos) == 0 && l.query != "":
		out = append(out, styleMuted.Render(" No matches."))
	case len(l.todos) == 0:
		out = append(out, styleMuted.Render(" Nothing open. Press . to show done todos."))
	}
	rows := l.rows()
	now := time.Now()
	for _, r := range rows[min(l.offset, len(rows)):min(l.offset+l.rowsHeight(), len(rows))] {
		out = append(out, l.renderRow(r, width, focused, now))
	}
	return strings.Join(out, "\n")
}

func (l listModel) renderRow(r listRow, width int, focused bool, now time.Time) string {
	if r.todo < 0 {
		if r.sub != "" {
			return styleMuted.Render(" " + r.sub)
		}
		if r.header == "" {
			return ""
		}
		h := styleHeader.Render(" " + r.header)
		if worked := store.WorkedOn(l.all, r.date, now); worked >= time.Minute {
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
