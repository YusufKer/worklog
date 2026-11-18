package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/YusufKer/worklog/internal/store"
	"github.com/atotto/clipboard"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const (
	reportDayW   = 7 // "Mon 16" plus a gap
	reportTotalW = 8
)

// weekReport shows time per todo and day for one week.
type weekReport struct {
	start  time.Time // Monday of the shown week
	offset int       // first visible row
}

func newWeekReport(now time.Time) weekReport {
	return weekReport{start: store.WeekStart(now)}
}

// shift moves by n weeks.
func (r *weekReport) shift(n int) {
	r.start = store.WeekStart(r.start.AddDate(0, 0, 7*n+1))
	r.offset = 0
}

func (r *weekReport) scroll(delta, rows, visible int) {
	r.offset = clamp(r.offset+delta, 0, max(rows-visible, 0))
}

// visibleRows is how many todo rows fit in a report of the given height.
func visibleRows(height int) int {
	return max(height-12, 1) // border, padding, title, label, header, total, gaps
}

// view renders the report no wider than maxWidth, fitting the title column
// to the longest title.
func (r weekReport) view(w store.Week, maxWidth, height int, now time.Time) string {
	longest := lipgloss.Width(w.Label()) - 7*reportDayW - reportTotalW
	for _, row := range w.Rows {
		longest = max(longest, lipgloss.Width(row.Todo.Title)+2)
	}
	titleW := clamp(longest, 12, maxWidth-6-7*reportDayW-reportTotalW)
	today := now.Format(store.DateLayout)

	cell := func(s string, w int, style lipgloss.Style) string {
		return style.Render(fmt.Sprintf("%*s", w, s))
	}
	line := func(name string, days [7]time.Duration, total time.Duration, style lipgloss.Style) string {
		s := style.Render(padRight(ansi.Truncate(name, titleW-1, "…"), titleW))
		for _, d := range days {
			v := store.HoursMinutes(d)
			if v == "" {
				s += cell("·", reportDayW, styleMuted)
			} else {
				s += cell(v, reportDayW, style)
			}
		}
		return s + cell(store.HoursMinutes(total), reportTotalW, style.Bold(true))
	}

	header := strings.Repeat(" ", titleW)
	for i := range 7 {
		day := w.Day(i)
		style := styleHeader
		if day.Format(store.DateLayout) == today {
			style = styleSelected
		}
		header += cell(day.Format("Mon 2"), reportDayW, style)
	}
	header += cell("Total", reportTotalW, styleHeader)

	lines := []string{styleMuted.Render(w.Label()), "", header}
	if len(w.Rows) == 0 {
		lines = append(lines, styleMuted.Render("No time tracked this week."))
	}
	visible := visibleRows(height)
	for _, row := range w.Rows[min(r.offset, len(w.Rows)):min(r.offset+visible, len(w.Rows))] {
		lines = append(lines, line(row.Todo.Title, row.Days, row.Total, lipgloss.NewStyle()))
	}
	if len(w.Rows) > visible {
		lines = append(lines, styleMuted.Render(fmt.Sprintf("%d–%d of %d todos", r.offset+1, min(r.offset+visible, len(w.Rows)), len(w.Rows))))
	}
	lines = append(lines, "", line("Total", w.Days, w.Total, styleHeader))

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorAccent).
		Padding(1, 2).
		Render(styleSelected.Render("Week") + "\n\n" + strings.Join(lines, "\n"))
}

// saveCSV writes the week to <dir>/reports/<week>.csv and returns the path.
func saveCSV(dir string, w store.Week) (string, error) {
	reports := filepath.Join(dir, "reports")
	if err := os.MkdirAll(reports, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(reports, w.Name()+".csv")
	return path, os.WriteFile(path, []byte(w.CSV()), 0o644)
}

func copyMarkdown(w store.Week) error {
	return clipboard.WriteAll(w.Markdown())
}
