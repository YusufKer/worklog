package ui

import (
	"testing"
	"time"
)

func TestWeekShift(t *testing.T) {
	// Spans a DST change in zones that have one; weeks must stay on Mondays.
	r := newWeekReport(time.Date(2026, 11, 4, 15, 0, 0, 0, time.Local))
	for _, want := range []string{"2026-10-26", "2026-10-19"} {
		r.shift(-1)
		if got := r.start.Format("2006-01-02 15:04 Mon"); got != want+" 00:00 Mon" {
			t.Fatalf("shift(-1) = %s, want %s", got, want)
		}
	}
	r.shift(3)
	if got := r.start.Format("2006-01-02"); got != "2026-11-09" {
		t.Fatalf("shift(3) = %s", got)
	}
}
