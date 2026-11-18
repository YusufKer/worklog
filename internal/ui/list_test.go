package ui

import (
	"testing"
	"time"

	"github.com/YusufKer/worklog/internal/config"
	"github.com/YusufKer/worklog/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

func TestListFilter(t *testing.T) {
	today := time.Now().Format(store.DateLayout)
	todos := []*store.Todo{
		{ID: "a", Title: "Fix checkout bug", Date: today, Notes: "Stripe idempotency keys"},
		{ID: "b", Title: "Write sync spec", Date: today, Notes: "polling vs webhooks"},
		{ID: "c", Title: "Deploy API", Date: today, Done: true, Notes: "stripe webhooks configured"},
	}
	ids := func(l listModel) (s string) {
		for _, t := range l.todos {
			s += t.ID
		}
		return s
	}

	l := newList()
	l.setHeight(20)
	l.setTodos(todos)
	l.selectID("c")

	l.setQuery("STRIPE")
	if got := ids(l); got != "ac" {
		t.Fatalf("stripe: %q", got)
	}
	if l.selected().ID != "c" {
		t.Fatalf("selection lost: %s", l.selected().ID)
	}
	l.setQuery("stripe webhooks") // every word must match
	if got := ids(l); got != "c" {
		t.Fatalf("stripe webhooks: %q", got)
	}

	l.hideDone = true
	l.apply()
	if got := ids(l); got != "" || l.selected() != nil {
		t.Fatalf("hide done: %q", got)
	}

	l.clearQuery()
	if got := ids(l); got != "ab" || !l.filtered() {
		t.Fatalf("after clearQuery: %q (filtered %v)", got, l.filtered())
	}
	l.clearFilter()
	if got := ids(l); got != "abc" || l.filtered() {
		t.Fatalf("after clearFilter: %q", got)
	}
}

// Fast typing arrives as one multi-rune message; words that are also key
// names ("end", "home") must still be typed, not treated as keys.
func TestFastTypingIntoInputs(t *testing.T) {
	m := New(nil, nil, Options{Config: config.Config{Agent: config.Agent{Type: "claude"}}})
	m.width, m.height = 100, 30
	m.layout()

	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	for _, chunk := range []string{"the ", "end"} {
		model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(chunk)})
	}
	if got := model.(Model).list.filter.Value(); got != "the end" {
		t.Fatalf("filter = %q", got)
	}

	m = model.(Model)
	m.list.clearQuery()
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	for _, chunk := range []string{"go ", "home"} {
		model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(chunk)})
	}
	if got := model.(Model).agent.input.Value(); got != "go home" {
		t.Fatalf("agent input = %q", got)
	}
}

func TestReorderWithinDay(t *testing.T) {
	today := time.Now().Format(store.DateLayout)
	yesterday := time.Now().AddDate(0, 0, -1).Format(store.DateLayout)
	todos := []*store.Todo{
		{ID: "a", Title: "A", Date: today},
		{ID: "b", Title: "B", Date: today, Done: true},
		{ID: "c", Title: "C", Date: today},
		{ID: "old", Title: "Carried", Date: yesterday},
	}
	ids := func(seq []*store.Todo) (s string) {
		for _, t := range seq {
			s += t.ID
		}
		return s
	}
	l := newList()
	l.setHeight(20)
	l.setTodos(todos)

	l.selectID("c")
	if got := l.reordered(1); got != nil {
		t.Errorf("moving C down past a carried-over todo should do nothing, got %s", ids(got))
	}
	if got := ids(l.reordered(-1)); got != "acb" {
		t.Errorf("C up = %s, want acb", got)
	}
	l.selectID("a")
	if got := ids(l.reordered(1)); got != "bac" {
		t.Errorf("A down = %s, want bac", got)
	}
	if got := l.reordered(-1); got != nil {
		t.Errorf("A is already first, got %s", ids(got))
	}

	// With done todos hidden, A moves past the hidden B to the next visible one.
	l.hideDone = true
	l.apply()
	l.selectID("a")
	if got := ids(l.reordered(1)); got != "bca" {
		t.Errorf("A down with B hidden = %s, want bca", got)
	}
}

func TestReorderKeysPersist(t *testing.T) {
	s, _ := store.New(t.TempDir())
	first, _ := s.Create("First", time.Now().Format(store.DateLayout))
	second, _ := s.Create("Second", time.Now().Format(store.DateLayout))
	_, _ = s.Update(second, func(x *store.Todo) { x.Created = first.Created.Add(time.Minute) })
	m := openModel(t, s)
	if got := m.list.todos[0].ID; got != second.ID {
		t.Fatalf("newest should be first, got %s", got)
	}
	m = press(m, "J")
	if got := m.list.selected().ID; got != second.ID {
		t.Fatalf("selection moved to %s", got)
	}
	todos, _, _ := s.LoadAll()
	if todos[0].ID != first.ID || todos[1].ID != second.ID || todos[1].Order != 2 {
		t.Fatalf("after J on disk: %s(%d) %s(%d)", todos[0].Title, todos[0].Order, todos[1].Title, todos[1].Order)
	}
	m = press(m, "K")
	todos, _, _ = s.LoadAll()
	if todos[0].ID != second.ID {
		t.Fatalf("after K on disk: %s first", todos[0].Title)
	}
}

func TestCarriedOverDivider(t *testing.T) {
	today := time.Now().Format(store.DateLayout)
	old := time.Now().AddDate(0, 0, -2).Format(store.DateLayout)
	l := newList()
	l.setHeight(20)
	l.setTodos([]*store.Todo{
		{ID: "a", Title: "A", Date: today},
		{ID: "b", Title: "B", Date: old},
		{ID: "c", Title: "C", Date: old, Done: true},
		{ID: "d", Title: "D", Date: old},
	})
	var subs []string
	for _, r := range l.rows() {
		if r.sub != "" {
			subs = append(subs, r.sub)
		}
	}
	if len(subs) != 1 || subs[0] != "↩ Carried over · 2" {
		t.Fatalf("dividers = %q", subs)
	}
	rows := l.rows()
	if rows[1].todo != 0 || rows[2].sub == "" || rows[3].todo != 1 {
		t.Fatalf("divider should sit between today's todos and the carried-over ones: %+v", rows[:4])
	}

	// No divider when nothing is carried over, or when all are.
	l.setTodos([]*store.Todo{{ID: "a", Title: "A", Date: today}})
	for _, r := range l.rows() {
		if r.sub != "" {
			t.Fatal("divider without carried-over todos")
		}
	}
	l.setTodos([]*store.Todo{{ID: "b", Title: "B", Date: old}})
	if rows := l.rows(); rows[1].sub == "" {
		t.Fatal("expected a divider when every todo under Today is carried over")
	}
}
