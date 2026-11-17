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
	all := func() []*Todo { ts, _ := s.LoadAll(); return ts }
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
