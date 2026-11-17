package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseFrontmatter(t *testing.T) {
	src := "---\nid: abc\ntitle: Fix bug\ndate: 2026-11-17\ndone: true\n---\n\n# Notes\nhello\n"
	td, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if td.ID != "abc" || td.Title != "Fix bug" || td.Date != "2026-11-17" || !td.Done {
		t.Fatalf("unexpected todo: %+v", td)
	}
	if td.Notes != "# Notes\nhello\n" {
		t.Fatalf("unexpected notes: %q", td.Notes)
	}
}

func TestParseNoFrontmatter(t *testing.T) {
	td, err := Parse([]byte("just notes"))
	if err != nil {
		t.Fatal(err)
	}
	if td.Notes != "just notes" {
		t.Fatalf("got %q", td.Notes)
	}
}

func TestLoadAllSortsAndGroups(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.md", "---\ntitle: Old\ndate: 2026-11-14\n---\n")
	write("b.md", "---\ntitle: Today 1\ndate: 2026-11-17\ncreated: 2026-11-17T09:00:00Z\n---\n")
	write("c.md", "---\ntitle: Today 2\ndate: 2026-11-17\ncreated: 2026-11-17T10:00:00Z\n---\n")
	write("d.md", "---\ntitle: Yest\ndate: 2026-11-16\n---\n")

	s, _ := New(dir)
	todos, err := s.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, td := range todos {
		titles = append(titles, td.Title)
	}
	want := []string{"Today 2", "Today 1", "Yest", "Old"}
	for i := range want {
		if titles[i] != want[i] {
			t.Fatalf("order = %v, want %v", titles, want)
		}
	}

	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.Local)
	groups := GroupByDate(todos, now)
	labels := []string{"Today", "Yesterday", "Sat Nov 14"}
	if len(groups) != len(labels) {
		t.Fatalf("got %d groups", len(groups))
	}
	for i, g := range groups {
		if g.Label != labels[i] {
			t.Errorf("group %d label = %q, want %q", i, g.Label, labels[i])
		}
	}
}
