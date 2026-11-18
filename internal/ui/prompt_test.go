package ui

import (
	"testing"
	"time"
)

func TestResolveDate(t *testing.T) {
	now := time.Date(2026, 11, 30, 15, 0, 0, 0, time.Local)
	for in, want := range map[string]string{
		"":           "2026-11-30",
		" Today ":    "2026-11-30",
		"tomorrow":   "2026-12-01",
		"tom":        "2026-12-01",
		"yesterday":  "2026-11-29",
		"+3":         "2026-12-03",
		"-30":        "2026-10-31",
		"2026-01-02": "2026-01-02",
		"3":          "3", // left for the store to reject
		"next week":  "next week",
	} {
		if got := resolveDate(in, now); got != want {
			t.Errorf("resolveDate(%q) = %q, want %q", in, got, want)
		}
	}
}
