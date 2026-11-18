package store

import (
	"strings"
	"testing"
	"time"
)

func TestBuildWeek(t *testing.T) {
	now := at("2026-11-20 12:00") // Friday
	start := WeekStart(now)
	if got := start.Format("2006-01-02 15:04 Mon"); got != "2026-11-16 00:00 Mon" {
		t.Fatalf("WeekStart = %s", got)
	}
	if got := WeekStart(at("2026-11-22 23:00")); !got.Equal(start) {
		t.Fatalf("Sunday belongs to the same week, got %v", got)
	}

	todos := []*Todo{
		{Title: "Small", Time: []TimeEntry{{Start: at("2026-11-16 09:00"), End: ptr(at("2026-11-16 09:30"))}}},
		{Title: "Big | pipes", Time: []TimeEntry{
			{Start: at("2026-11-15 23:00"), End: ptr(at("2026-11-16 01:00"))}, // 1h before the week
			{Start: at("2026-11-20 10:00")},                                   // running: 2h up to now
		}},
		{Title: "Last week", Time: []TimeEntry{{Start: at("2026-11-10 09:00"), End: ptr(at("2026-11-10 17:00"))}}},
		{Title: "None"},
	}
	w := BuildWeek(todos, start, now)
	if len(w.Rows) != 2 || w.Rows[0].Todo.Title != "Big | pipes" || w.Rows[1].Todo.Title != "Small" {
		t.Fatalf("rows = %+v", w.Rows)
	}
	if w.Rows[0].Days[0] != time.Hour || w.Rows[0].Days[4] != 2*time.Hour || w.Rows[0].Total != 3*time.Hour {
		t.Errorf("big = %+v", w.Rows[0])
	}
	if w.Days[0] != 90*time.Minute || w.Total != 210*time.Minute {
		t.Errorf("days = %v total = %v", w.Days, w.Total)
	}
	if w.Name() != "2026-W47" || w.Label() != "2026-W47 · Nov 16 – Nov 22" {
		t.Errorf("name %q label %q", w.Name(), w.Label())
	}

	md := w.Markdown()
	for _, want := range []string{"| Todo | Mon 16 |", "| Big \\| pipes | 1:00 |  |  |  | 2:00 |  |  | 3:00 |", "| **Total** | 1:30 |"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q:\n%s", want, md)
		}
	}
	csv := w.CSV()
	for _, want := range []string{"Todo,2026-11-16,", "Big | pipes,1.00,0,0,0,2.00,0,0,3.00", "Total,1.50,0,0,0,2.00,0,0,3.50"} {
		if !strings.Contains(csv, want) {
			t.Errorf("csv missing %q:\n%s", want, csv)
		}
	}
}

func TestBuildWeekAcrossSkippedMidnight(t *testing.T) {
	// Sunday 2026-09-06 has no midnight in Chile (DST starts at 00:00), so a
	// naive Day(6) lands on Saturday 23:00 and Sunday vanishes from the week.
	loc, err := time.LoadLocation("America/Santiago")
	if err != nil {
		t.Skip(err)
	}
	now := time.Date(2026, 9, 7, 12, 0, 0, 0, loc)
	start := WeekStart(time.Date(2026, 9, 6, 12, 0, 0, 0, loc))
	if got := start.Format("2006-01-02 15:04 Mon"); got != "2026-08-31 00:00 Mon" {
		t.Fatalf("WeekStart = %s", got)
	}
	todos := []*Todo{{Title: "Sunday", Time: []TimeEntry{
		{Start: time.Date(2026, 9, 5, 22, 0, 0, 0, loc), End: ptr(time.Date(2026, 9, 6, 3, 0, 0, 0, loc))}, // 2h Sat, 2h Sun
	}}}
	w := BuildWeek(todos, start, now)

	var dates []string
	for i := range 7 {
		dates = append(dates, w.Day(i).Format(DateLayout))
	}
	want := []string{"2026-08-31", "2026-09-01", "2026-09-02", "2026-09-03", "2026-09-04", "2026-09-05", "2026-09-06"}
	for i := range want {
		if dates[i] != want[i] {
			t.Fatalf("days = %v, want %v", dates, want)
		}
	}
	if got := w.Day(6).Format("2006-01-02 15:04"); got != "2026-09-06 01:00" {
		t.Errorf("Day(6) = %s, want the first instant of Sunday", got)
	}
	if w.Label() != "2026-W36 · Aug 31 – Sep 6" {
		t.Errorf("label = %q", w.Label())
	}
	if w.Days[5] != 2*time.Hour || w.Days[6] != 2*time.Hour || w.Total != 4*time.Hour {
		t.Errorf("days = %v total = %v", w.Days, w.Total)
	}
	if csv := w.CSV(); !strings.Contains(csv, "2026-09-05,2026-09-06,Total") {
		t.Errorf("csv header:\n%s", csv)
	}
}

func TestParseWeek(t *testing.T) {
	now := at("2026-11-18 12:00") // Wednesday of 2026-W47
	for in, want := range map[string]string{
		"":           "2026-11-16",
		"this":       "2026-11-16",
		"Last":       "2026-11-09",
		"-0":         "2026-11-16",
		"-3":         "2026-10-26",
		"2026-W47":   "2026-11-16",
		"2026-w01":   "2025-12-29", // week 1 can start in the previous year
		"2020-W53":   "2020-12-28",
		"2026-11-22": "2026-11-16", // Sunday
		"2026-11-23": "2026-11-23",
	} {
		got, err := ParseWeek(in, now)
		if err != nil || got.Format("2006-01-02 15:04") != want+" 00:00" {
			t.Errorf("ParseWeek(%q) = %v, %v; want %s", in, got, err, want)
		}
	}
	for _, bad := range []string{"next", "2026-W54", "2025-W53", "-1x", "2026-13-01"} {
		if _, err := ParseWeek(bad, now); err == nil {
			t.Errorf("ParseWeek(%q): expected error", bad)
		}
	}
}
