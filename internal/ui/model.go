// Package ui implements the worklog terminal interface.
package ui

import (
	"math"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/yusuf/worklog/internal/store"
)

type focus int

const (
	focusList focus = iota
	focusNotes
	focusAgent
	numFocus
)

const (
	minWidth  = 60
	minHeight = 12
)

// Model is the root Bubble Tea model.
type Model struct {
	store *store.Store
	list  listModel
	notes notesModel
	agent agentModel
	focus focus

	modal  modalKind
	form   newTodoForm
	status string // one-shot message shown in the footer
	isErr  bool

	width, height int
	listW         int
	notesH        int
	agentH        int
}

// New builds the UI. glamStyle is "dark" or "light", detected before the
// program starts so it does not race with Bubble Tea's input handling.
func New(s *store.Store, todos []*store.Todo, glamStyle string) Model {
	m := Model{store: s, notes: newNotes(glamStyle)}
	m.list.setTodos(todos)
	return m
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		m.notes.show(m.list.selected())
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		m.status = ""
		if m.modal != modalNone {
			return m.updateModal(msg)
		}
		// Fast key repeat can arrive as one multi-rune message ("jjj");
		// handle each rune as its own keypress.
		if msg.Type == tea.KeyRunes && len(msg.Runes) > 1 && !msg.Paste {
			var model tea.Model = m
			var cmds []tea.Cmd
			for _, r := range msg.Runes {
				var cmd tea.Cmd
				model, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
				cmds = append(cmds, cmd)
			}
			return model, tea.Batch(cmds...)
		}
		switch msg.String() {
		case "q":
			return m, tea.Quit
		case "tab":
			m.focus = (m.focus + 1) % numFocus
			return m, nil
		case "shift+tab":
			m.focus = (m.focus + numFocus - 1) % numFocus
			return m, nil
		}
		switch m.focus {
		case focusList:
			return m.updateList(msg)
		case focusNotes:
			return m.updateNotes(msg)
		case focusAgent:
			if msg.String() == "esc" {
				m.focus = focusList
			}
		}
	}
	if m.modal == modalNew {
		return m, m.form.update(msg)
	}
	return m, nil
}

func (m Model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		m.list.move(1)
	case "k", "up":
		m.list.move(-1)
	case "g", "home":
		m.list.top()
	case "G", "end":
		m.list.bottom()
	case "enter", "l", "right":
		m.focus = focusNotes
	case "n":
		m.modal = modalNew
		m.form = newForm()
		return m, textinput.Blink
	case "x", " ", "space":
		m.toggleDone()
	case "d":
		if m.list.selected() != nil {
			m.modal = modalDelete
		}
	}
	m.notes.show(m.list.selected())
	return m, nil
}

func (m Model) updateNotes(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "h", "left":
		m.focus = focusList
		return m, nil
	case "g", "home":
		m.notes.vp.GotoTop()
		return m, nil
	case "G", "end":
		m.notes.vp.GotoBottom()
		return m, nil
	}
	var cmd tea.Cmd
	m.notes.vp, cmd = m.notes.vp.Update(msg)
	return m, cmd
}

func (m Model) updateModal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.modal {
	case modalDelete:
		switch msg.String() {
		case "y", "Y", "enter":
			m.modal = modalNone
			m.deleteSelected()
		case "n", "N", "esc", "q":
			m.modal = modalNone
		}
		return m, nil

	case modalNew:
		switch msg.String() {
		case "esc":
			m.modal = modalNone
			return m, nil
		case "tab", "shift+tab", "up", "down":
			m.form.switchField()
			return m, nil
		case "enter":
			t, err := m.store.Create(m.form.title.Value(), strings.TrimSpace(m.form.date.Value()))
			if err != nil {
				m.form.err = capitalize(err.Error())
				return m, nil
			}
			m.modal = modalNone
			m.reload(t.ID)
			m.setStatus("Created “"+t.Title+"”", false)
			return m, nil
		}
		m.form.err = ""
		return m, m.form.update(msg)
	}
	return m, nil
}

func (m *Model) toggleDone() {
	t := m.list.selected()
	if t == nil {
		return
	}
	if _, err := m.store.Update(t, func(t *store.Todo) { t.Done = !t.Done }); err != nil {
		m.setStatus("Couldn't update: "+err.Error(), true)
		return
	}
	m.reload(t.ID)
}

func (m *Model) deleteSelected() {
	t := m.list.selected()
	if t == nil {
		return
	}
	if err := m.store.Delete(t); err != nil {
		m.setStatus("Couldn't delete: "+err.Error(), true)
		return
	}
	m.reload("")
	m.setStatus("Deleted “"+t.Title+"” (moved to .trash/)", false)
}

// reload re-reads all todos from disk, keeping the cursor on selectID when
// given, or on the same position otherwise.
func (m *Model) reload(selectID string) {
	todos, err := m.store.LoadAll()
	if err != nil {
		m.setStatus("Couldn't load todos: "+err.Error(), true)
		return
	}
	m.list.setTodos(todos)
	if selectID != "" {
		m.list.selectID(selectID)
	}
	m.notes.invalidate()
	m.notes.show(m.list.selected())
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func (m *Model) setStatus(s string, isErr bool) {
	m.status, m.isErr = s, isErr
}

// layout splits the window: a narrow list on the left, notes (67%) over
// the agent pane (33%) on the right, and a one-line help footer.
func (m *Model) layout() {
	bodyH := m.height - 1
	m.listW = clamp(m.width/4, 24, 34)
	m.notesH = int(math.Round(float64(bodyH) * 0.67))
	m.agentH = bodyH - m.notesH
	m.list.setHeight(bodyH - 2)
	m.notes.setSize(m.width-m.listW-2, m.notesH-2)
}

func (m Model) View() string {
	if m.width == 0 {
		return ""
	}
	if m.width < minWidth || m.height < minHeight {
		return styleMuted.Render("Terminal too small — resize to at least 60×12.")
	}
	bodyH := m.height - 1
	rightW := m.width - m.listW

	list := renderPane("Todos", m.list.view(m.listW-2, m.focus == focusList),
		m.listW, bodyH, m.focus == focusList)

	notesTitle := "Notes"
	if t := m.list.selected(); t != nil {
		notesTitle = "Notes: " + t.Title
	}
	notes := renderPane(notesTitle, m.notes.vp.View(), rightW, m.notesH, m.focus == focusNotes)
	agent := renderPane("Agent", m.agent.view(rightW-2, m.agentH-2, m.focus == focusAgent),
		rightW, m.agentH, m.focus == focusAgent)

	body := lipgloss.JoinHorizontal(lipgloss.Top, list, lipgloss.JoinVertical(lipgloss.Left, notes, agent))
	switch m.modal {
	case modalNew:
		body = overlay(body, m.form.view(), m.width, bodyH)
	case modalDelete:
		body = overlay(body, deleteView(m.list.selected()), m.width, bodyH)
	}
	return lipgloss.JoinVertical(lipgloss.Left, body, m.footer())
}

func (m Model) footer() string {
	switch {
	case m.status != "" && m.isErr:
		return " " + styleError.Render(m.status)
	case m.status != "":
		return " " + styleDoneIcon.Render(m.status)
	case m.modal == modalNew:
		return renderHelp([]hint{{"tab", "switch field"}, {"enter", "create"}, {"esc", "cancel"}})
	case m.modal == modalDelete:
		return renderHelp([]hint{{"y", "delete"}, {"n/esc", "cancel"}})
	}
	return renderHelp(helpFor(m.focus))
}
