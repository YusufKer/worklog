package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/YusufKer/worklog/internal/store"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const trashRows = 10

// trashList shows .trash/ so deleted todos and conflict backups can be
// restored.
type trashList struct {
	items  []store.Trashed
	cursor int
	offset int
	err    string
}

func (f *trashList) move(delta int) {
	f.cursor = clamp(f.cursor+delta, 0, len(f.items)-1)
	if f.cursor < f.offset {
		f.offset = f.cursor
	}
	if f.cursor >= f.offset+trashRows {
		f.offset = f.cursor - trashRows + 1
	}
}

func (f trashList) selected() (store.Trashed, bool) {
	if len(f.items) == 0 {
		return store.Trashed{}, false
	}
	return f.items[f.cursor], true
}

func (f trashList) view(now time.Time) string {
	var lines []string
	if len(f.items) == 0 {
		lines = append(lines, styleMuted.Render("The trash is empty."))
	}
	for i := f.offset; i < min(f.offset+trashRows, len(f.items)); i++ {
		lines = append(lines, f.row(f.items[i], i == f.cursor, now))
	}
	if len(f.items) > trashRows {
		lines = append(lines, styleMuted.Render(fmt.Sprintf("  %d–%d of %d", f.offset+1, min(f.offset+trashRows, len(f.items)), len(f.items))))
	}
	if x, ok := f.selected(); ok {
		detail := "Deleted todo, dated " + shortDate(dateOf(x))
		switch {
		case x.Todo == nil:
			detail = "Can't be read, so can't be restored here: " + x.Name
		case x.Backup:
			detail = "Backup of notes that were saved over on " + x.Modified.Format("Jan 2 15:04") +
				". Restores as a separate todo."
		}
		lines = append(lines, "", styleMuted.Render(lipgloss.NewStyle().Width(60).Render(detail)))
	}
	if f.err != "" {
		lines = append(lines, "", styleError.Render(f.err))
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorAccent).
		Padding(1, 2).
		Render(styleSelected.Render("Trash") + "\n\n" + strings.Join(lines, "\n"))
}

func dateOf(x store.Trashed) string {
	if x.Todo == nil {
		return ""
	}
	return x.Todo.Date
}

func (f trashList) row(x store.Trashed, sel bool, now time.Time) string {
	title := x.Name
	if x.Todo != nil {
		title = x.Todo.Title
	}
	kind := "deleted"
	switch {
	case x.Todo == nil:
		kind = "unreadable"
	case x.Backup:
		kind = "backup"
	}
	marker, style := "  ", lipgloss.NewStyle()
	if sel {
		marker, style = "▶ ", styleSelected
	}
	when := x.Modified.Format("Jan 2")
	if x.Modified.Format(store.DateLayout) == now.Format(store.DateLayout) {
		when = x.Modified.Format("15:04")
	}
	return style.Render(marker+padRight(ansi.Truncate(title, 40, "…"), 41)) +
		styleMuted.Render(fmt.Sprintf("%-10s %6s", kind, when))
}
