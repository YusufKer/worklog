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
	write("a.md", "---\ntitle: Old\ndate: 2026-11-14\ndone: true\n---\n")
	write("b.md", "---\ntitle: Today 1\ndate: 2026-11-17\ncreated: 2026-11-17T09:00:00Z\n---\n")
	write("c.md", "---\ntitle: Today 2\ndate: 2026-11-17\ncreated: 2026-11-17T10:00:00Z\n---\n")
	write("d.md", "---\ntitle: Yest\ndate: 2026-11-16\ndone: true\n---\n")

	s, _ := New(dir)
	todos, _, err := s.LoadAll()
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

func TestCreateToggleDelete(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)

	td, err := s.Create("Fix: the Bug!", "2026-11-17")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(td.Path) != "2026-11-17-fix-the-bug.md" {
		t.Fatalf("path = %s", td.Path)
	}
	dup, _ := s.Create("Fix the bug", "2026-11-17")
	if filepath.Base(dup.Path) != "2026-11-17-fix-the-bug-2.md" {
		t.Fatalf("dup path = %s", dup.Path)
	}

	// Simulate an external edit to the notes; Update must preserve it.
	ext := *td
	ext.Notes = "edited elsewhere\n"
	if err := s.Save(&ext); err != nil {
		t.Fatal(err)
	}
	got, err := s.Update(td, func(x *Todo) { x.Done = !x.Done })
	if err != nil {
		t.Fatal(err)
	}
	reloaded, _ := Load(td.Path)
	if !reloaded.Done || reloaded.Notes != "edited elsewhere\n" || reloaded.Title != "Fix: the Bug!" {
		t.Fatalf("reloaded = %+v", reloaded)
	}
	if got.ID != td.ID {
		t.Fatalf("id changed: %s -> %s", td.ID, got.ID)
	}

	if err := s.Delete(td); err != nil {
		t.Fatal(err)
	}
	if fileExists(td.Path) || !fileExists(filepath.Join(dir, ".trash", "2026-11-17-fix-the-bug.md")) {
		t.Fatal("file not moved to trash")
	}
	todos, _, _ := s.LoadAll()
	if len(todos) != 1 {
		t.Fatalf("want 1 todo after delete, got %d", len(todos))
	}

	if _, err := s.Create("  ", "2026-11-17"); err == nil {
		t.Fatal("expected error for empty title")
	}
	if _, err := s.Create("x", "17/11/2026"); err == nil {
		t.Fatal("expected error for bad date")
	}
}

func TestEdit(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)

	td, _ := s.Create("Fix bug", "2026-11-14")
	td, _ = s.Update(td, func(x *Todo) { x.Session = "abc"; x.Notes = "notes\n" })
	id := td.ID

	got, err := s.Edit(td, "  Fix the bug ", "2026-11-18")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got.Path) != "2026-11-18-fix-the-bug.md" || fileExists(td.Path) {
		t.Fatalf("path = %s, old still exists: %v", got.Path, fileExists(td.Path))
	}
	reloaded, _ := Load(got.Path)
	if reloaded.ID != id || reloaded.Title != "Fix the bug" || reloaded.Date != "2026-11-18" ||
		reloaded.Session != "abc" || reloaded.Notes != "notes\n" {
		t.Fatalf("reloaded = %+v", reloaded)
	}

	// Unchanged slug and date keep a -N suffixed path.
	s.Create("Dup", "2026-11-18")
	dup, _ := s.Create("Dup", "2026-11-18")
	same, err := s.Edit(dup, "dup", "2026-11-18")
	if err != nil || same.Path != dup.Path {
		t.Fatalf("path = %s, want %s (err %v)", same.Path, dup.Path, err)
	}

	// Moving onto an existing name picks a free one.
	moved, _ := s.Edit(dup, "Fix the bug", "2026-11-18")
	if filepath.Base(moved.Path) != "2026-11-18-fix-the-bug-2.md" {
		t.Fatalf("moved path = %s", moved.Path)
	}

	if _, err := s.Edit(moved, "x", "tomorrow"); err == nil {
		t.Fatal("expected error for bad date")
	}
}

func TestLoadAllSkipsBadFiles(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)
	s.Create("Good", "2026-11-18")
	os.WriteFile(filepath.Join(dir, "broken.md"), []byte("---\ntitle: [unclosed\n---\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "unterminated.md"), []byte("---\ntitle: x\n"), 0o644)

	todos, bad, err := s.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(todos) != 1 || todos[0].Title != "Good" {
		t.Fatalf("todos = %+v", todos)
	}
	if len(bad) != 2 || bad[0].Name != "broken.md" || bad[1].Name != "unterminated.md" {
		t.Fatalf("bad = %v", bad)
	}
	if got := bad[1].String(); got != "unterminated.md: unterminated frontmatter" {
		t.Errorf("String() = %q", got)
	}
}

func TestTrashAndRestore(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)
	gone, _ := s.Create("Gone", "2026-11-16")
	gone, _ = s.Update(gone, func(x *Todo) { x.Session = "s1"; x.Notes = "deleted notes\n" })
	kept, _ := s.Create("Kept", "2026-11-17")
	kept, _ = s.Update(kept, func(x *Todo) { x.Session = "s2"; x.Notes = "old notes\n" })
	if err := s.Delete(gone); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Backup(kept); err != nil {
		t.Fatal(err)
	}

	trash, err := s.Trash()
	if err != nil || len(trash) != 2 {
		t.Fatalf("trash = %+v (%v)", trash, err)
	}
	var del, bak Trashed
	for _, x := range trash {
		if x.Backup {
			bak = x
		} else {
			del = x
		}
	}
	if del.Todo == nil || del.Todo.Title != "Gone" || bak.Todo == nil || bak.Todo.Title != "Kept" {
		t.Fatalf("deleted %+v backup %+v", del, bak)
	}

	existing, _, _ := s.LoadAll()
	got, err := s.Restore(del.Name, existing)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != gone.ID || got.Session != "s1" || filepath.Base(got.Path) != "2026-11-16-gone.md" {
		t.Fatalf("restored deleted = %+v", got)
	}

	// The backup's todo still exists: restoring gives it a new identity.
	existing, _, _ = s.LoadAll()
	got, err = s.Restore(bak.Name, existing)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID == kept.ID || got.Session != "" || got.Notes != "old notes\n" || filepath.Base(got.Path) != "2026-11-17-kept-2.md" {
		t.Fatalf("restored backup = %+v", got)
	}
	if trash, _ := s.Trash(); len(trash) != 0 {
		t.Fatalf("trash not emptied: %+v", trash)
	}
	if all, _, _ := s.LoadAll(); len(all) != 3 {
		t.Fatalf("want 3 todos, got %d", len(all))
	}
}
