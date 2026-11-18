package ui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/YusufKer/worklog/internal/config"
	"github.com/YusufKer/worklog/internal/store"
)

func TestChangedFiles(t *testing.T) {
	old := map[string]string{"a.md": "1", "b.md": "1", "c.md": "1"}
	cur := map[string]string{"a.md": "1", "b.md": "2", "d.md": "1"}
	got := changedFiles(old, cur)
	slices.Sort(got)
	if strings.Join(got, ",") != "b.md,c.md,d.md" {
		t.Fatalf("changed = %v", got)
	}
}

func TestPollReloadsAndSaveKeepsOtherVersion(t *testing.T) {
	s, _ := store.New(t.TempDir())
	td, _ := s.Create("Fix bug", "2026-11-18")
	td, _ = s.Update(td, func(x *store.Todo) { x.Notes = "original\n" })
	todos, _, _ := s.LoadAll()
	m := New(s, todos, Options{Config: config.Config{Agent: config.Agent{Type: "claude"}}})
	m.width, m.height = 100, 30
	m.layout()
	poll := func() {
		model, _ := m.handleFiles(filesMsg{dir: s.Dir, stamps: fileStamps(s.Dir)})
		m = model.(Model)
	}
	// No baseline poll: New records one, so changes made before the first
	// poll are still noticed.
	// A todo added outside the app shows up on the next poll.
	other := filepath.Join(s.Dir, "2026-11-18-outside.md")
	os.WriteFile(other, []byte("---\nid: outside\ntitle: Made elsewhere\ndate: \"2026-11-18\"\n---\nhi\n"), 0o644)
	poll()
	if m.todoByID("outside") == nil {
		t.Fatal("new file not picked up")
	}

	// Edit, then change the same file on disk: saving keeps both versions.
	m.list.selectID(td.ID)
	m.syncSelection()
	m.notes.startEdit(m.list.selected())
	m.notes.ta.SetValue("mine")
	ext, _ := store.Load(td.Path)
	ext.Notes = "theirs\n"
	s.Save(ext)
	poll()
	if !m.isErr || !strings.Contains(m.status, "changed on disk") {
		t.Fatalf("status = %q", m.status)
	}
	if !m.saveNotes() {
		t.Fatalf("save failed: %s", m.status)
	}
	if got, _ := store.Load(td.Path); got.Notes != "mine\n" {
		t.Fatalf("notes = %q", got.Notes)
	}
	backups, _ := filepath.Glob(filepath.Join(s.Dir, ".trash", "*.md"))
	if len(backups) != 1 {
		t.Fatalf("backups = %v", backups)
	}
	if data, _ := os.ReadFile(backups[0]); !strings.Contains(string(data), "theirs") {
		t.Fatalf("backup = %s", data)
	}

	// Saving again with no outside change makes no new backup, even when the
	// text ends with blank lines.
	m.notes.ta.SetValue("mine again\n\n")
	m.saveNotes()
	if backups, _ := filepath.Glob(filepath.Join(s.Dir, ".trash", "*.md")); len(backups) != 1 {
		t.Fatalf("unexpected backup: %v", backups)
	}
	m.notes.ta.SetValue("third\n")
	m.saveNotes()
	if backups, _ := filepath.Glob(filepath.Join(s.Dir, ".trash", "*.md")); len(backups) != 1 {
		t.Fatalf("unexpected backup after third save: %v", backups)
	}
}
