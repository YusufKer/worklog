package ui

import (
	"testing"
	"time"

	"github.com/YusufKer/worklog/internal/store"
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
