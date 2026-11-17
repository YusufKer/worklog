package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/glamour"
	"github.com/yusuf/worklog/internal/store"
)

// notesModel renders the selected todo's Markdown notes in a scrollable viewport.
type notesModel struct {
	vp        viewport.Model
	glamStyle string // "dark" or "light"

	renderer *glamour.TermRenderer
	rendW    int
	cache    map[string]string // todo ID -> rendered notes at rendW
	shownID  string
}

func newNotes(glamStyle string) notesModel {
	return notesModel{vp: viewport.New(0, 0), glamStyle: glamStyle, cache: map[string]string{}}
}

func (n *notesModel) setSize(w, h int) {
	n.vp.Width, n.vp.Height = w, h
	if w != n.rendW {
		n.rendW = w
		n.renderer = nil
		n.cache = map[string]string{}
		n.shownID = ""
	}
}

// show displays the given todo, resetting scroll when it changes.
func (n *notesModel) show(t *store.Todo) {
	if t == nil {
		n.vp.SetContent(styleMuted.Render(" Nothing selected."))
		n.shownID = ""
		return
	}
	if t.ID == n.shownID {
		return
	}
	n.shownID = t.ID
	n.vp.SetContent(n.render(t))
	n.vp.GotoTop()
}

func (n *notesModel) render(t *store.Todo) string {
	if s, ok := n.cache[t.ID]; ok {
		return s
	}
	if strings.TrimSpace(t.Notes) == "" {
		return styleMuted.Render(" No notes yet.")
	}
	if n.renderer == nil {
		r, err := glamour.NewTermRenderer(
			glamour.WithStandardStyle(n.glamStyle),
			glamour.WithWordWrap(max(n.rendW-4, 20)),
		)
		if err != nil {
			return t.Notes
		}
		n.renderer = r
	}
	out, err := n.renderer.Render(t.Notes)
	if err != nil {
		return t.Notes
	}
	out = strings.Trim(out, "\n")
	n.cache[t.ID] = out
	return out
}
