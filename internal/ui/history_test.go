package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YusufKer/worklog/internal/config"
	"github.com/YusufKer/worklog/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

func TestHistoryRoundTrip(t *testing.T) {
	dir := t.TempDir()
	var entries []entry
	for i := range historyMax + 5 {
		entries = append(entries, entry{kind: entUser, text: fmt.Sprint(i)})
	}
	entries = append(entries, entry{kind: entText, text: "**reply**"}, entry{kind: entTool, text: "Edit x.md"},
		entry{kind: entError, text: "oops"}, entry{kind: entInfo, text: "done in 3s"})
	if err := saveHistory(dir, "todo-1", entries); err != nil {
		t.Fatal(err)
	}
	got := loadHistory(dir, "todo-1")
	if len(got) != historyMax || got[0].text != "9" || got[len(got)-1].kind != entInfo || !got[len(got)-4].final {
		t.Fatalf("loaded %d entries, first %+v, last %+v", len(got), got[0], got[len(got)-1])
	}

	for _, id := range []string{"", "..", "a/b", `a\b`} {
		if historyPath(dir, id) != "" {
			t.Errorf("unsafe id %q allowed", id)
		}
	}
	if err := deleteHistory(dir, "todo-1"); err != nil || loadHistory(dir, "todo-1") != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := deleteHistory(dir, "missing"); err != nil {
		t.Fatalf("deleting missing history: %v", err)
	}
}

// A chat saved by one run of the app shows up in the next, and /clear
// removes it.
func TestHistorySurvivesRestart(t *testing.T) {
	s, _ := store.New(t.TempDir())
	td, _ := s.Create("Fix bug", "2026-11-18")
	open := func() Model {
		todos, _, _ := s.LoadAll()
		m := New(s, todos, Options{Config: config.Config{Agent: config.Agent{Type: "claude"}}})
		m.width, m.height = 100, 30
		m.layout()
		return m
	}

	m := open()
	c := m.agent.conv(td.ID)
	c.add(entUser, "add a checklist")
	c.add(entText, "Added three items.")
	c.add(entInfo, "done in 4s")
	m.saveChat(td.ID)

	m = open()
	if view := m.agent.render(); !strings.Contains(view, "add a checklist") || !strings.Contains(view, "Added three items.") {
		t.Fatalf("history not shown after restart:\n%s", view)
	}

	m.setFocus(focusAgent)
	m.agent.input.SetValue("/clear")
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = model.(Model)
	if _, err := os.Stat(filepath.Join(s.Dir, ".chats", td.ID+".json")); !os.IsNotExist(err) {
		t.Fatalf("history file still exists after /clear: %v", err)
	}
	if m = open(); strings.Contains(m.agent.render(), "add a checklist") {
		t.Fatal("history shown after /clear")
	}
}
