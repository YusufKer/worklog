package store

import (
	"fmt"
	"time"
)

// TimeEntry is one stretch of tracked time. A nil End means the timer is
// still running.
type TimeEntry struct {
	Start time.Time  `yaml:"start"`
	End   *time.Time `yaml:"end,omitempty"`
}

// Running returns the open time entry, if any.
func (t *Todo) Running() *TimeEntry {
	for i := range t.Time {
		if t.Time[i].End == nil {
			return &t.Time[i]
		}
	}
	return nil
}

// Total is all time tracked on the todo, counting a running timer up to now.
func (t *Todo) Total(now time.Time) time.Duration {
	var d time.Duration
	for _, e := range t.Time {
		d += e.end(now).Sub(e.Start)
	}
	return d
}

// ByDay splits the todo's tracked time by local calendar day (YYYY-MM-DD).
func (t *Todo) ByDay(now time.Time) map[string]time.Duration {
	days := map[string]time.Duration{}
	for _, e := range t.Time {
		splitDays(e.Start.In(now.Location()), e.end(now).In(now.Location()), func(day string, d time.Duration) {
			days[day] += d
		})
	}
	return days
}

// WorkedOn sums time tracked across all todos on the given calendar day.
func WorkedOn(todos []*Todo, date string, now time.Time) time.Duration {
	var d time.Duration
	for _, t := range todos {
		d += t.ByDay(now)[date]
	}
	return d
}

// Stale reports whether a running timer looks forgotten: started on an
// earlier day, or running longer than maxRun.
func (e TimeEntry) Stale(now time.Time, maxRun time.Duration) bool {
	if e.End != nil {
		return false
	}
	return e.Start.In(now.Location()).Format(DateLayout) != now.Format(DateLayout) ||
		now.Sub(e.Start) > maxRun
}

func (e TimeEntry) end(now time.Time) time.Time {
	if e.End != nil {
		return *e.End
	}
	return now
}

// splitDays calls fn for each calendar-day slice of [start, end).
func splitDays(start, end time.Time, fn func(day string, d time.Duration)) {
	for start.Before(end) {
		y, m, d := start.Date()
		next := time.Date(y, m, d+1, 0, 0, 0, 0, start.Location())
		seg := end
		if next.Before(end) {
			seg = next
		}
		fn(start.Format(DateLayout), seg.Sub(start))
		start = seg
	}
}

// StartTimer stops any other running timer and starts one on t.
func (s *Store) StartTimer(t *Todo, all []*Todo, now time.Time) error {
	for _, o := range all {
		if o.Path != t.Path && o.Running() != nil {
			if err := s.StopTimer(o, now); err != nil {
				return err
			}
		}
	}
	_, err := s.Update(t, func(x *Todo) {
		if x.Running() == nil {
			x.Time = append(x.Time, TimeEntry{Start: now.Truncate(time.Second)})
		}
	})
	return err
}

// StopTimer ends t's running timer at the given time.
func (s *Store) StopTimer(t *Todo, at time.Time) error {
	var stopErr error
	_, err := s.Update(t, func(x *Todo) {
		e := x.Running()
		if e == nil {
			return
		}
		if at.Before(e.Start) {
			stopErr = fmt.Errorf("stop time is before the timer started (%s)", e.Start.Format("Mon 15:04"))
			return
		}
		end := at.Truncate(time.Second)
		e.End = &end
	})
	if stopErr != nil {
		return stopErr
	}
	return err
}

// DiscardTimer removes t's running time entry entirely.
func (s *Store) DiscardTimer(t *Todo) error {
	_, err := s.Update(t, func(x *Todo) {
		kept := x.Time[:0]
		for _, e := range x.Time {
			if e.End != nil {
				kept = append(kept, e)
			}
		}
		x.Time = kept
	})
	return err
}

// FormatDuration renders a duration as "1h 20m" or "45m".
func FormatDuration(d time.Duration) string {
	m := int(d.Round(time.Minute) / time.Minute)
	if m < 60 {
		return fmt.Sprintf("%dm", m)
	}
	if m%60 == 0 {
		return fmt.Sprintf("%dh", m/60)
	}
	return fmt.Sprintf("%dh %dm", m/60, m%60)
}

// FormatClock renders a running duration as "0:42" (h:mm).
func FormatClock(d time.Duration) string {
	m := int(d / time.Minute)
	return fmt.Sprintf("%d:%02d", m/60, m%60)
}
