package ui

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/YusufKer/worklog/internal/agent"
	"github.com/YusufKer/worklog/internal/config"
	"github.com/YusufKer/worklog/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

// openModel loads the store's todos into a laid-out model.
func openModel(t *testing.T, s *store.Store) Model {
	t.Helper()
	todos, _, err := s.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	m := New(s, todos, Options{Config: config.Config{Agent: config.Agent{Type: "claude"}}})
	m.width, m.height = 100, 30
	m.layout()
	return m
}

// press sends keys to the model: single characters as typed runes, anything
// else as a named key.
func press(m Model, keys ...string) Model {
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "ctrl+s":
			msg = tea.KeyMsg{Type: tea.KeyCtrlS}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		model, _ := m.Update(msg)
		m = model.(Model)
	}
	return m
}

func notesOnDisk(t *testing.T, path string) string {
	t.Helper()
	td, err := store.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return td.Notes
}

// Opening and closing the editor without typing must not rewrite the file,
// and tabs on untouched lines survive an edit elsewhere.
func TestEditorKeepsTabs(t *testing.T) {
	s, _ := store.New(t.TempDir())
	td, _ := s.Create("Code", "2026-11-18")
	notes := "```go\n\treturn 1\n```\nplain\n"
	if _, err := s.Update(td, func(t *store.Todo) { t.Notes = notes }); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(td.Path)

	m := openModel(t, s)
	m = press(m, "e")
	if !m.notes.editing || m.notes.dirty() {
		t.Fatalf("editing %v dirty %v after opening", m.notes.editing, m.notes.dirty())
	}
	m = press(m, "esc")
	if after, _ := os.Stat(td.Path); after.ModTime() != before.ModTime() || m.notes.editing {
		t.Fatalf("closing an untouched editor rewrote the file")
	}

	m = press(m, "e", "x", "esc")
	if got := notesOnDisk(t, td.Path); got != "x"+notes {
		t.Fatalf("notes after edit:\n%q", got)
	}
	if entries, _ := os.ReadDir(s.Dir + "/.trash"); len(entries) != 0 {
		t.Fatalf("a backup was made although the file hadn't changed on disk")
	}
}

func TestTabMap(t *testing.T) {
	m := tabMap("\ta\n    a\n\tb\n\t\tc\n  \t  c")
	if _, ok := m["    a"]; ok {
		t.Error("expanded form that also appears literally should not be mapped")
	}
	if m["    b"] != "\tb" {
		t.Errorf("b: %q", m["    b"])
	}
	if _, ok := m["        c"]; ok {
		t.Error("two tab layouts with the same expansion should not be mapped")
	}
}

// Notes longer than the textarea can hold must not be opened, since the
// textarea would silently drop the rest.
func TestEditorRefusesLongNotes(t *testing.T) {
	s, _ := store.New(t.TempDir())
	td, _ := s.Create("Long", "2026-11-18")
	long := strings.Repeat("line\n", maxEditLines+1)
	if _, err := s.Update(td, func(t *store.Todo) { t.Notes = long }); err != nil {
		t.Fatal(err)
	}
	m := press(openModel(t, s), "e")
	if m.notes.editing || !strings.Contains(m.status, "too long") {
		t.Fatalf("editing %v, status %q", m.notes.editing, m.status)
	}
	if got := notesOnDisk(t, td.Path); got != long {
		t.Fatal("notes were changed")
	}
}

// If the todo is deleted while its notes are being edited, esc still leaves
// the editor and the text ends up in .trash/ where it can be restored.
func TestEditorSurvivesDeletedTodo(t *testing.T) {
	s, _ := store.New(t.TempDir())
	td, _ := s.Create("Doomed", "2026-11-18")
	m := press(openModel(t, s), "e", "k", "e", "p", "t")
	if err := os.Remove(td.Path); err != nil {
		t.Fatal(err)
	}
	m.reloadKeepSelection()
	m = press(m, "esc")
	if m.notes.editing {
		t.Fatal("still editing after the todo was deleted")
	}
	trash, err := s.Trash()
	if err != nil || len(trash) != 1 || trash[0].Todo == nil {
		t.Fatalf("trash: %v %+v", err, trash)
	}
	if got := trash[0].Todo; got.Title != "Doomed" || got.Notes != "kept\n" {
		t.Fatalf("stashed todo: %+v", got)
	}
	if _, err := s.Restore(trash[0].Name, m.list.all); err != nil {
		t.Fatal(err)
	}
}

// The delete dialog deletes the todo it was opened for, even if the list
// reloaded and the cursor moved, and closes if that todo disappears.
func TestDeleteDialogTracksTodo(t *testing.T) {
	s, _ := store.New(t.TempDir())
	a, _ := s.Create("Alpha", "2026-11-18")
	b, _ := s.Create("Beta", "2026-11-18")

	m := openModel(t, s)
	m.list.selectID(a.ID)
	m = press(m, "d")
	if m.modal != modalDelete || m.deleteID != a.ID {
		t.Fatalf("modal %v for %q", m.modal, m.deleteID)
	}
	m.list.selectID(b.ID) // the cursor moves while the dialog is open
	m = press(m, "y")
	if m.todoByID(a.ID) != nil || m.todoByID(b.ID) == nil {
		t.Fatal("the wrong todo was deleted")
	}

	m.list.selectID(b.ID)
	m = press(m, "d")
	if err := os.Remove(b.Path); err != nil {
		t.Fatal(err)
	}
	m.reloadKeepSelection()
	_ = m.View() // must not panic with nothing to show
	if m.modal != modalNone {
		t.Fatalf("dialog still open for a todo that is gone")
	}

	m.modal, m.deleteID = modalDelete, "nope"
	_ = m.View()
	m = press(m, "y")
	if m.modal != modalNone || !m.isErr {
		t.Fatalf("confirming a vanished todo: modal %v status %q", m.modal, m.status)
	}
}

// A run's session is saved according to the backend that started it, even
// if the settings switched to a non-resumable agent meanwhile.
func TestFinishRunUsesRunBackend(t *testing.T) {
	s, _ := store.New(t.TempDir())
	td, _ := s.Create("Chat", "2026-11-18")
	m := openModel(t, s)
	c := m.agent.conv(td.ID)
	c.session, c.resumable = "sess-1", true
	m.backend = agent.Command{}

	m.finishRun(td.ID, agent.EvDone{})
	if got, _ := store.Load(td.Path); got.Session != "sess-1" {
		t.Fatalf("session = %q", got.Session)
	}
}

// Switching to a data folder with a running timer starts the clock.
func TestSettingsSwitchStartsTimerTick(t *testing.T) {
	s, _ := store.New(t.TempDir())
	m := openModel(t, s)
	if m.ticking {
		t.Fatal("ticking with no timers")
	}

	other, _ := store.New(t.TempDir())
	td, _ := other.Create("Running", "2026-11-18")
	if err := other.StartTimer(td, nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	cfg := m.cfg
	cfg.Dir = other.Dir
	if err := m.applySettings(cfg, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if cmd := m.settingsSaved(); cmd == nil || !m.ticking || m.store.Dir != other.Dir {
		t.Fatalf("cmd %v ticking %v dir %s", cmd != nil, m.ticking, m.store.Dir)
	}
}
