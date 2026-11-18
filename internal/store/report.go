package store

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Week is tracked time per todo and day for one Monday–Sunday week.
type Week struct {
	Start time.Time // Monday 00:00, local time
	Rows  []WeekRow // todos with time this week, most time first
	Days  [7]time.Duration
	Total time.Duration
}

type WeekRow struct {
	Todo  *Todo
	Days  [7]time.Duration
	Total time.Duration
}

// WeekStart returns 00:00 on the Monday of t's week.
func WeekStart(t time.Time) time.Time {
	y, m, d := t.Date()
	offset := (int(t.Weekday()) + 6) % 7 // days since Monday
	return time.Date(y, m, d-offset, 0, 0, 0, 0, t.Location())
}

// BuildWeek totals time tracked in the week starting at start (a Monday,
// see WeekStart). Running timers count up to now.
func BuildWeek(todos []*Todo, start, now time.Time) Week {
	w := Week{Start: start}
	var dates [7]string
	for i := range dates {
		dates[i] = w.Day(i).Format(DateLayout)
	}
	for _, t := range todos {
		byDay := t.ByDay(now)
		row := WeekRow{Todo: t}
		for i, date := range dates {
			row.Days[i] = byDay[date]
			row.Total += byDay[date]
		}
		if row.Total < time.Minute {
			continue
		}
		for i, d := range row.Days {
			w.Days[i] += d
		}
		w.Total += row.Total
		w.Rows = append(w.Rows, row)
	}
	sort.SliceStable(w.Rows, func(i, j int) bool {
		if w.Rows[i].Total != w.Rows[j].Total {
			return w.Rows[i].Total > w.Rows[j].Total
		}
		return w.Rows[i].Todo.Title < w.Rows[j].Todo.Title
	})
	return w
}

// Day returns midnight on the i-th day of the week (0 = Monday).
func (w Week) Day(i int) time.Time {
	y, m, d := w.Start.Date()
	return time.Date(y, m, d+i, 0, 0, 0, 0, w.Start.Location())
}

// Name is the ISO week, e.g. "2026-W47".
func (w Week) Name() string {
	y, n := w.Start.ISOWeek()
	return fmt.Sprintf("%d-W%02d", y, n)
}

// Label is the week's name and dates, e.g. "2026-W47 · Nov 16 – Nov 22".
func (w Week) Label() string {
	return w.Name() + " · " + w.Start.Format("Jan 2") + " – " + w.Day(6).Format("Jan 2")
}

// HoursMinutes renders a duration as "1:30", or "" for less than a minute.
func HoursMinutes(d time.Duration) string {
	m := int(d.Round(time.Minute) / time.Minute)
	if m == 0 {
		return ""
	}
	return fmt.Sprintf("%d:%02d", m/60, m%60)
}

// Markdown renders the week as a table with h:mm times.
func (w Week) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "## %s\n\n", w.Label())
	b.WriteString("| Todo |")
	for i := range 7 {
		b.WriteString(" " + w.Day(i).Format("Mon 2") + " |")
	}
	b.WriteString(" Total |\n|---|")
	b.WriteString(strings.Repeat("--:|", 8) + "\n")
	cells := func(name string, days [7]time.Duration, total time.Duration) {
		b.WriteString("| " + strings.ReplaceAll(name, "|", "\\|") + " |")
		for _, d := range days {
			b.WriteString(" " + HoursMinutes(d) + " |")
		}
		b.WriteString(" " + HoursMinutes(total) + " |\n")
	}
	for _, r := range w.Rows {
		cells(r.Todo.Title, r.Days, r.Total)
	}
	cells("**Total**", w.Days, w.Total)
	return b.String()
}

// CSV renders the week with decimal hours, for spreadsheets.
func (w Week) CSV() string {
	var buf bytes.Buffer
	c := csv.NewWriter(&buf)
	header := []string{"Todo"}
	for i := range 7 {
		header = append(header, w.Day(i).Format(DateLayout))
	}
	c.Write(append(header, "Total"))
	row := func(name string, days [7]time.Duration, total time.Duration) {
		rec := []string{name}
		for _, d := range days {
			rec = append(rec, decimalHours(d))
		}
		c.Write(append(rec, decimalHours(total)))
	}
	for _, r := range w.Rows {
		row(r.Todo.Title, r.Days, r.Total)
	}
	row("Total", w.Days, w.Total)
	c.Flush()
	return buf.String()
}

func decimalHours(d time.Duration) string {
	if d < time.Minute {
		return "0"
	}
	return fmt.Sprintf("%.2f", d.Round(time.Minute).Hours())
}
