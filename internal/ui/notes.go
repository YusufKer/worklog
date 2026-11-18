package ui

import (
	"strings"

	"github.com/YusufKer/worklog/internal/store"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

// notesModel renders the selected todo's Markdown notes in a scrollable
// viewport, and switches to a textarea for inline editing.
type notesModel struct {
	vp        viewport.Model
	glamStyle string // "dark" or "light"

	renderer *glamour.TermRenderer
	rendW    int
	cache    map[string]string // todo ID -> rendered notes at rendW
	shownID  string

	// Scroll position to restore when the same todo is re-shown after an
	// invalidate (e.g. after saving), instead of jumping to the top.
	keepID     string
	keepOffset int

	editing  bool
	editID   string
	ta       textarea.Model
	original string // textarea value when editing started or last saved
	base     string // notes on disk at that point, without trailing newlines
}

func newNotes(glamStyle string) notesModel {
	ta := textarea.New()
	ta.Prompt = ""
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.MaxHeight = 0
	ta.Placeholder = "Write notes in Markdown…"
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.FocusedStyle.Placeholder = styleMuted
	ta.BlurredStyle.Placeholder = styleMuted

	return notesModel{vp: viewport.New(0, 0), ta: ta, glamStyle: glamStyle, cache: map[string]string{}}
}

func (n *notesModel) setSize(w, h int) {
	n.vp.Width, n.vp.Height = w, h
	n.ta.SetWidth(w)
	n.ta.SetHeight(h)
	if w != n.rendW {
		n.rendW = w
		n.renderer = nil
		n.invalidate()
	}
}

// invalidate drops cached renders so the next show re-reads notes.
func (n *notesModel) invalidate() {
	n.cache = map[string]string{}
	n.keepID, n.keepOffset = n.shownID, n.vp.YOffset
	n.shownID = ""
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
	if t.ID == n.keepID {
		n.vp.SetYOffset(n.keepOffset)
	} else {
		n.vp.GotoTop()
	}
	n.keepID = ""
}

func (n *notesModel) render(t *store.Todo) string {
	if s, ok := n.cache[t.ID]; ok {
		return s
	}
	if strings.TrimSpace(t.Notes) == "" {
		return styleMuted.Render(" No notes yet. Press e to write some.")
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

// startEdit loads the todo's raw Markdown into the textarea.
func (n *notesModel) startEdit(t *store.Todo) tea.Cmd {
	n.editing = true
	n.editID = t.ID
	n.original = strings.TrimRight(t.Notes, "\n")
	n.base = n.original
	n.ta.SetValue(n.original)
	cmd := n.ta.Focus()
	// SetValue leaves the cursor at the end; jump to the start of the text.
	n.ta, _ = n.ta.Update(tea.KeyMsg{Type: tea.KeyCtrlHome})
	return cmd
}

func (n *notesModel) stopEdit() {
	n.editing = false
	n.editID = ""
	n.ta.Blur()
}

// markSaved records the current text as the saved baseline.
func (n *notesModel) markSaved() {
	n.original = n.ta.Value()
	n.base = strings.TrimRight(n.value(), "\n")
}

// changedOnDisk reports whether notes read from disk differ from the version
// this edit started from (or last saved).
func (n *notesModel) changedOnDisk(disk string) bool {
	return strings.TrimRight(disk, "\n") != n.base
}

func (n *notesModel) dirty() bool { return n.editing && n.ta.Value() != n.original }

// value returns the edited notes, normalised to end with a newline.
func (n *notesModel) value() string {
	v := strings.TrimRight(n.ta.Value(), "\n")
	if v == "" {
		return ""
	}
	return v + "\n"
}

func (n *notesModel) view() string {
	if n.editing {
		return n.ta.View()
	}
	return n.vp.View()
}
