package ui

import (
	"testing"
	"time"

	"github.com/YusufKer/worklog/internal/store"
)

func TestTimeLogEntry(t *testing.T) {
	now := time.Date(2026, 11, 18, 17, 0, 0, 0, time.Local)
	at := func(s string) time.Time {
		v, _ := time.ParseInLocation("2006-01-02 15:04", s, time.Local)
		return v
	}
	f := newTimeLog(&store.Todo{})
	form := func(old time.Time, running bool, start, end string) (store.TimeEntry, error) {
		f.openForm(old, running, start, end)
		return f.entry(now)
	}

	for _, c := range []struct {
		start, end string
		want       [2]string // start, end
	}{
		{"2026-11-18 09:00", "10:30", [2]string{"2026-11-18 09:00", "2026-11-18 10:30"}},
		{"09:00", "1h30m", [2]string{"2026-11-18 09:00", "2026-11-18 10:30"}},
		{"2026-11-17 23:00", "2026-11-18 01:00", [2]string{"2026-11-17 23:00", "2026-11-18 01:00"}},
		{"2026-11-17 14:00", "45m", [2]string{"2026-11-17 14:00", "2026-11-17 14:45"}},
	} {
		e, err := form(time.Time{}, false, c.start, c.end)
		if err != nil || !e.Start.Equal(at(c.want[0])) || e.End == nil || !e.End.Equal(at(c.want[1])) {
			t.Errorf("%q–%q = %v–%v (%v)", c.start, c.end, e.Start, e.End, err)
		}
	}

	// HH:MM start keeps the edited entry's day.
	e, err := form(at("2026-11-16 09:00"), false, "08:00", "09:30")
	if err != nil || !e.Start.Equal(at("2026-11-16 08:00")) || !e.End.Equal(at("2026-11-16 09:30")) {
		t.Errorf("edit keeps day: %v–%v (%v)", e.Start, e.End, err)
	}

	// Empty end: only allowed for the running entry.
	if e, err := form(at("2026-11-18 16:00"), true, "15:00", ""); err != nil || e.End != nil {
		t.Errorf("running: %+v (%v)", e, err)
	}
	for _, bad := range [][2]string{{"", "10:00"}, {"9am", "10:00"}, {"09:00", ""}, {"09:00", "soon"}, {"09:00", "-1h"}} {
		if _, err := form(time.Time{}, false, bad[0], bad[1]); err == nil {
			t.Errorf("%q–%q: expected error", bad[0], bad[1])
		}
	}
}
