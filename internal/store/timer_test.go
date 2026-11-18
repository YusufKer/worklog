package store

import (
	"testing"
	"time"
)

func at(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", s, time.Local)
	if err != nil {
		panic(err)
	}
	return t
}

func ptr(t time.Time) *time.Time { return &t }

func TestCarryOver(t *testing.T) {
	now := at("2026-11-17 12:00")
	todos := []*Todo{
		{ID: "tomorrow", Date: "2026-11-18"},
		{ID: "today", Date: "2026-11-17"},
		{ID: "open-old", Date: "2026-11-14"},
		{ID: "done-old", Date: "2026-11-14", Done: true},
		{ID: "open-older", Date: "2026-11-10"},
	}
	groups := GroupByDate(todos, now)

	var got []string
	for _, g := range groups {
		s := g.Label + ":"
		for _, td := range g.Todos {
			s += " " + td.ID
			if g.Carried(td) {
				s += "↩"
			}
		}
		got = append(got, s)
	}
	want := []string{
		"Tomorrow: tomorrow",
		"Today: today open-old↩ open-older↩",
		"Sat Nov 14: done-old",
	}
	if len(got) != len(want) {
		t.Fatalf("groups = %q", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("group %d = %q, want %q", i, got[i], want[i])
		}
	}
	if todos[2].Date != "2026-11-14" {
		t.Error("carry-over must not change the stored date")
	}
}

func TestCarryOverWithoutTodayTodos(t *testing.T) {
	groups := GroupByDate([]*Todo{{ID: "x", Date: "2026-11-01"}}, at("2026-11-17 09:00"))
	if len(groups) != 1 || groups[0].Label != "Today" {
		t.Fatalf("groups = %+v", groups)
	}
}

func TestTotalsAcrossMidnight(t *testing.T) {
	now := at("2026-11-17 12:00")
	td := &Todo{Time: []TimeEntry{
		{Start: at("2026-11-16 23:00"), End: ptr(at("2026-11-17 01:30"))}, // 1h + 1h30
		{Start: at("2026-11-17 11:15")},                                   // running: 45m
	}}
	if got := td.Total(now); got != 3*time.Hour+15*time.Minute {
		t.Errorf("total = %v", got)
	}
	days := td.ByDay(now)
	if days["2026-11-16"] != time.Hour || days["2026-11-17"] != 2*time.Hour+15*time.Minute {
		t.Errorf("by day = %v", days)
	}
	other := &Todo{Time: []TimeEntry{{Start: at("2026-11-17 08:00"), End: ptr(at("2026-11-17 09:00"))}}}
	if got := WorkedOn([]*Todo{td, other}, "2026-11-17", now); got != 3*time.Hour+15*time.Minute {
		t.Errorf("worked today = %v", got)
	}
}

func TestSplitDaysAcrossSkippedMidnight(t *testing.T) {
	// In Chile DST starts at midnight: 2026-09-06 00:00 doesn't exist, the
	// clocks jump from 23:59 -04 to 01:00 -03. time.Date normalises the
	// missing midnight to 23:00 the day before, which used to stall ByDay.
	loc, err := time.LoadLocation("America/Santiago")
	if err != nil {
		t.Skip(err)
	}
	td := &Todo{Time: []TimeEntry{{
		Start: time.Date(2026, 9, 4, 12, 0, 0, 0, loc),
		End:   ptr(time.Date(2026, 9, 6, 2, 0, 0, 0, loc)),
	}}}
	now := time.Date(2026, 9, 7, 12, 0, 0, 0, loc)

	done := make(chan map[string]time.Duration, 1)
	go func() { done <- td.ByDay(now) }()
	var days map[string]time.Duration
	select {
	case days = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ByDay did not return: splitDays is looping")
	}
	want := map[string]time.Duration{
		"2026-09-04": 12 * time.Hour,
		"2026-09-05": 24 * time.Hour, // 00:00 -04 … 01:00 -03 is a full 24h
		"2026-09-06": time.Hour,
	}
	if len(days) != len(want) {
		t.Fatalf("by day = %v, want %v", days, want)
	}
	for day, d := range want {
		if days[day] != d {
			t.Errorf("%s = %v, want %v", day, days[day], d)
		}
	}
	if got := td.Total(now); got != 37*time.Hour {
		t.Errorf("total = %v", got)
	}
}

func TestStale(t *testing.T) {
	now := at("2026-11-17 18:00")
	cases := []struct {
		e    TimeEntry
		want bool
	}{
		{TimeEntry{Start: at("2026-11-17 16:00")}, false},
		{TimeEntry{Start: at("2026-11-17 09:00")}, true}, // > 4h
		{TimeEntry{Start: at("2026-11-16 17:30")}, true}, // previous day
		{TimeEntry{Start: at("2026-11-16 09:00"), End: ptr(at("2026-11-16 10:00"))}, false},
	}
	for i, c := range cases {
		if got := c.e.Stale(now, 4*time.Hour); got != c.want {
			t.Errorf("case %d: stale = %v, want %v", i, got, c.want)
		}
	}
}

func TestTimerLifecycle(t *testing.T) {
	s, _ := New(t.TempDir())
	a, _ := s.Create("A", "2026-11-17")
	b, _ := s.Create("B", "2026-11-17")
	all := func() []*Todo { ts, _, _ := s.LoadAll(); return ts }
	find := func(id string) *Todo {
		for _, x := range all() {
			if x.ID == id {
				return x
			}
		}
		return nil
	}

	if err := s.StartTimer(a, all(), at("2026-11-17 09:00")); err != nil {
		t.Fatal(err)
	}
	// Starting B stops A: only one timer at a time.
	if err := s.StartTimer(b, all(), at("2026-11-17 10:00")); err != nil {
		t.Fatal(err)
	}
	if find(a.ID).Running() != nil || find(b.ID).Running() == nil {
		t.Fatal("expected only B running")
	}
	if got := find(a.ID).Total(at("2026-11-17 12:00")); got != time.Hour {
		t.Errorf("A total = %v", got)
	}

	if err := s.StopTimer(find(b.ID), at("2026-11-17 09:59")); err == nil {
		t.Error("expected error stopping before start")
	}
	if err := s.DiscardTimer(find(b.ID)); err != nil {
		t.Fatal(err)
	}
	if bb := find(b.ID); bb.Running() != nil || len(bb.Time) != 0 {
		t.Errorf("discard left %+v", bb.Time)
	}
}

func TestFormat(t *testing.T) {
	for d, want := range map[time.Duration]string{
		45 * time.Minute: "45m",
		80 * time.Minute: "1h 20m",
		2 * time.Hour:    "2h",
		20 * time.Second: "0m",
	} {
		if got := FormatDuration(d); got != want {
			t.Errorf("FormatDuration(%v) = %q, want %q", d, got, want)
		}
	}
	if got := FormatClock(42*time.Minute + 30*time.Second); got != "0:42" {
		t.Errorf("FormatClock = %q", got)
	}
}

func TestPutAndDeleteTimeEntry(t *testing.T) {
	s, _ := New(t.TempDir())
	td, _ := s.Create("A", "2026-11-17")
	now := at("2026-11-17 18:00")
	load := func() *Todo { x, _ := Load(td.Path); return x }
	entry := func(start, end string) TimeEntry {
		e := TimeEntry{Start: at(start)}
		if end != "" {
			e.End = ptr(at(end))
		}
		return e
	}

	// Added out of order; stored by start time.
	if err := s.PutTimeEntry(td, time.Time{}, entry("2026-11-17 13:00", "2026-11-17 14:00"), now); err != nil {
		t.Fatal(err)
	}
	if err := s.PutTimeEntry(td, time.Time{}, entry("2026-11-17 09:00", "2026-11-17 10:30"), now); err != nil {
		t.Fatal(err)
	}
	if got := load(); len(got.Time) != 2 || !got.Time[0].Start.Equal(at("2026-11-17 09:00")) || got.Total(now) != 150*time.Minute {
		t.Fatalf("after add: %+v", got.Time)
	}

	for name, e := range map[string]TimeEntry{
		"overlap":      entry("2026-11-17 10:00", "2026-11-17 11:00"),
		"end <= start": entry("2026-11-17 15:00", "2026-11-17 15:00"),
		"future end":   entry("2026-11-17 17:00", "2026-11-17 19:00"),
		"no end":       entry("2026-11-17 15:00", ""),
	} {
		if err := s.PutTimeEntry(td, time.Time{}, e, now); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}

	// Editing an entry may move it within its own old span.
	if err := s.PutTimeEntry(td, at("2026-11-17 09:00"), entry("2026-11-17 09:30", "2026-11-17 12:00"), now); err != nil {
		t.Fatal(err)
	}
	if got := load().Total(now); got != 3*time.Hour+30*time.Minute {
		t.Fatalf("after edit total = %v", got)
	}
	if err := s.PutTimeEntry(td, at("2026-11-17 08:00"), entry("2026-11-17 08:00", "2026-11-17 08:30"), now); err == nil {
		t.Error("expected error editing a missing entry")
	}

	// A running entry may keep running when edited, e.g. to fix its start.
	if err := s.StartTimer(load(), nil, at("2026-11-17 16:00")); err != nil {
		t.Fatal(err)
	}
	if err := s.PutTimeEntry(td, at("2026-11-17 16:00"), entry("2026-11-17 15:00", ""), now); err != nil {
		t.Fatal(err)
	}
	if r := load().Running(); r == nil || !r.Start.Equal(at("2026-11-17 15:00")) {
		t.Fatalf("running = %+v", r)
	}

	if err := s.DeleteTimeEntry(td, at("2026-11-17 13:00")); err != nil {
		t.Fatal(err)
	}
	if got := load(); len(got.Time) != 2 {
		t.Fatalf("after delete: %+v", got.Time)
	}
}
