// Package ui implements the worklog terminal interface.
package ui

import (
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"time"

	"github.com/YusufKer/worklog/internal/agent"
	"github.com/YusufKer/worklog/internal/config"
	"github.com/YusufKer/worklog/internal/store"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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

// Options configure the UI.
type Options struct {
	GlamStyle     string // "dark" or "light", detected before the program starts
	Config        config.Config
	ConfigPath    string
	DirOverridden bool            // the data dir came from -dir, $WORKLOG_DIR or -demo
	Skipped       []store.BadFile // files the initial load couldn't read
}

// Model is the root Bubble Tea model.
type Model struct {
	store         *store.Store
	backend       agent.Backend
	staleAfter    time.Duration
	cfg           config.Config
	cfgPath       string
	dirOverridden bool
	settings      settingsForm
	list          listModel
	notes         notesModel
	agent         agentModel
	focus         focus

	modal modalKind
	form  todoForm
	stale staleTimerForm
	times timeLog
	week  weekReport
	trash trashList
	kept  map[string]bool // stale timers the user chose to keep running

	stamps  map[string]string // last poll of the data directory
	skipped []store.BadFile   // todo files that couldn't be read
	ticking bool              // a timer tick loop is active
	status  string            // one-shot message shown in the footer
	isErr   bool

	width, height int
	listW         int
	notesH        int
	agentH        int
}

// New builds the UI.
func New(s *store.Store, todos []*store.Todo, o Options) Model {
	backend := BackendFor(o.Config.Agent)
	m := Model{
		store:         s,
		backend:       backend,
		staleAfter:    o.Config.StaleAfter(),
		cfg:           o.Config,
		cfgPath:       o.ConfigPath,
		dirOverridden: o.DirOverridden,
		notes:         newNotes(o.GlamStyle),
		list:          newList(),
		agent:         newAgent(o.GlamStyle, backend.Name(), isClaude(backend)),
		kept:          map[string]bool{},
	}
	m.list.setTodos(todos)
	m.syncSelection()
	m.setSkipped(o.Skipped)
	if s != nil {
		m.stamps = fileStamps(s.Dir) // baseline for spotting outside changes
	}
	m.ticking = m.runningTodo() != nil
	m.checkStale()
	return m
}

func isClaude(b agent.Backend) bool {
	_, ok := b.(agent.Claude)
	return ok
}

type timerTickMsg struct{}

func timerTick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return timerTickMsg{} })
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{pollFiles(m.store.Dir)}
	if m.ticking {
		cmds = append(cmds, timerTick())
	}
	return tea.Batch(cmds...)
}

// runningTodo returns the todo whose timer is running, if any.
func (m Model) runningTodo() *store.Todo {
	for _, t := range m.list.all {
		if t.Running() != nil {
			return t
		}
	}
	return nil
}

// ensureTick starts the once-a-second redraw loop while a timer runs.
func (m *Model) ensureTick() tea.Cmd {
	if m.ticking || m.runningTodo() == nil {
		return nil
	}
	m.ticking = true
	return timerTick()
}

// checkStale opens the forgotten-timer prompt for a timer that started on
// an earlier day or has run too long.
func (m *Model) checkStale() {
	if m.modal != modalNone {
		return
	}
	now := time.Now()
	for _, t := range m.list.all {
		if e := t.Running(); e != nil && e.Stale(now, m.staleAfter) && !m.kept[t.ID] {
			m.stale = newStaleForm(t, now)
			m.modal = modalStaleTimer
			return
		}
	}
}

func (m *Model) toggleTimer() tea.Cmd {
	t := m.list.selected()
	if t == nil {
		return nil
	}
	now := time.Now()
	if e := t.Running(); e != nil {
		if err := m.store.StopTimer(t, now); err != nil {
			m.setStatus("Couldn't stop timer: "+err.Error(), true)
			return nil
		}
		m.setStatus(fmt.Sprintf("Stopped · %s on “%s”", store.FormatDuration(now.Sub(e.Start)), t.Title), false)
	} else {
		if err := m.store.StartTimer(t, m.list.all, now); err != nil {
			m.setStatus("Couldn't start timer: "+err.Error(), true)
			return nil
		}
		m.setStatus("Timer started on “"+t.Title+"”", false)
	}
	m.reload(t.ID)
	return m.ensureTick()
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		m.syncSelection()
		return m, nil

	case agentEventMsg:
		if msg.closed {
			return m, nil
		}
		if m.agent.handle(msg) {
			m.reloadKeepSelection()
		}
		if done, ok := msg.ev.(agent.EvDone); ok {
			m.finishRun(msg.todoID, done)
			return m, nil
		}
		if run := m.agent.conv(msg.todoID).run; run != nil {
			return m, waitEvent(msg.todoID, run.Events)
		}
		return m, nil

	case filesMsg:
		return m.handleFiles(msg)

	case timerTickMsg:
		if m.runningTodo() == nil {
			m.ticking = false
			return m, nil
		}
		return m, timerTick()

	case spinner.TickMsg:
		if !m.agent.anyRunning() {
			return m, nil
		}
		var cmd tea.Cmd
		m.agent.spin, cmd = m.agent.spin.Update(msg)
		return m, cmd

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			if m.notes.dirty() {
				m.saveNotes() // don't lose edits on quit
			}
			m.agent.cancelAll()
			return m, tea.Quit
		}
		// Fast typing or key repeat can arrive as one multi-rune message
		// ("jjj"); handle each rune as its own keypress. Text inputs need
		// this too: their key bindings match the whole message, so typing
		// "end" quickly would otherwise move the cursor instead.
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
		m.status = ""
		if m.modal != modalNone {
			return m.updateModal(msg)
		}
		if m.notes.editing {
			return m.updateEditor(msg)
		}
		if m.list.typing {
			return m.updateFilter(msg)
		}
		switch msg.String() {
		case "q":
			if m.focus != focusAgent {
				m.agent.cancelAll()
				return m, tea.Quit
			}
		case "tab":
			return m, m.setFocus((m.focus + 1) % numFocus)
		case "shift+tab":
			return m, m.setFocus((m.focus + numFocus - 1) % numFocus)
		}
		switch m.focus {
		case focusList:
			return m.updateList(msg)
		case focusNotes:
			return m.updateNotes(msg)
		case focusAgent:
			return m.updateAgent(msg)
		}
	}
	if m.modal == modalTodo {
		return m, m.form.update(msg)
	}
	if m.modal == modalStaleTimer {
		var cmd tea.Cmd
		m.stale.input, cmd = m.stale.input.Update(msg)
		return m, cmd
	}
	if m.modal == modalSettings {
		return m, m.settings.update(msg)
	}
	if m.modal == modalTime && m.times.editing {
		var cmd tea.Cmd
		*m.times.input(), cmd = m.times.input().Update(msg)
		return m, cmd
	}
	if m.notes.editing {
		var cmd tea.Cmd
		m.notes.ta, cmd = m.notes.ta.Update(msg)
		return m, cmd
	}
	if m.list.typing {
		var cmd tea.Cmd
		m.list.filter, cmd = m.list.filter.Update(msg)
		return m, cmd
	}
	if m.focus == focusAgent {
		var cmd tea.Cmd
		m.agent.input, cmd = m.agent.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

// updateFilter handles keys while typing in the list's filter bar. The list
// narrows as you type; up/down move through the matches.
func (m Model) updateFilter(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.list.clearQuery()
	case "enter", "tab":
		m.list.typing = false
		m.list.filter.Blur()
		m.list.scroll()
		if m.list.query == "" {
			m.list.filter.SetValue("")
		}
	case "up", "ctrl+p":
		m.list.move(-1)
	case "down", "ctrl+n":
		m.list.move(1)
	default:
		var cmd tea.Cmd
		m.list.filter, cmd = m.list.filter.Update(msg)
		m.list.setQuery(m.list.filter.Value())
		m.syncSelection()
		return m, cmd
	}
	m.syncSelection()
	return m, nil
}

// setFocus moves focus between panes, focusing the agent prompt input
// when the agent pane is entered.
func (m *Model) setFocus(f focus) tea.Cmd {
	m.focus = f
	if f == focusAgent {
		return m.agent.input.Focus()
	}
	m.agent.input.Blur()
	return nil
}

// syncSelection points the notes and agent panes at the selected todo.
func (m *Model) syncSelection() {
	t := m.list.selected()
	m.notes.show(t)
	if t != nil {
		m.agent.show(t.ID)
	} else {
		m.agent.show("")
	}
}

func (m Model) updateAgent(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	t := m.list.selected()
	switch msg.String() {
	case "esc":
		if t != nil && m.agent.running(t.ID) {
			m.agent.conv(t.ID).run.Cancel()
			return m, nil
		}
		return m, m.setFocus(focusList)
	case "enter":
		return m.sendPrompt()
	case "up", "down", "pgup", "pgdown", "ctrl+u", "ctrl+d":
		var cmd tea.Cmd
		m.agent.vp, cmd = m.agent.vp.Update(msg)
		return m, cmd
	}
	var cmd tea.Cmd
	m.agent.input, cmd = m.agent.input.Update(msg)
	return m, cmd
}

// sendPrompt starts a Claude run for the selected todo.
func (m Model) sendPrompt() (tea.Model, tea.Cmd) {
	t := m.list.selected()
	text := strings.TrimSpace(m.agent.input.Value())
	if t == nil || text == "" || m.agent.running(t.ID) {
		return m, nil
	}
	m.agent.input.SetValue("")
	c := m.agent.conv(t.ID)

	if text == "/clear" {
		c.entries = nil
		if t.Session != "" {
			if _, err := m.store.Update(t, func(t *store.Todo) { t.Session = "" }); err != nil {
				m.setStatus("Couldn't clear session: "+err.Error(), true)
			}
			m.reloadKeepSelection()
		}
		m.agent.refresh()
		m.setStatus("Started a new conversation for this todo", false)
		return m, nil
	}

	// Make sure Claude sees the latest notes, and that we don't overwrite
	// its edits from a stale editor buffer.
	if m.notes.editing && m.notes.editID == t.ID {
		if m.notes.dirty() && !m.saveNotes() {
			return m, nil
		}
		m.notes.stopEdit()
	}

	c.add(entUser, text)
	resume := ""
	if m.backend.Resumable() {
		resume = t.Session
	}
	run, err := m.backend.Start(agent.Options{
		Dir:          m.store.Dir,
		Message:      text,
		SystemPrompt: systemPrompt(t),
		NotesFile:    t.Path,
		Title:        t.Title,
		Session:      resume,
	})
	if err != nil {
		c.add(entError, capitalize(err.Error()))
		m.agent.refresh()
		return m, nil
	}
	c.run, c.session, c.resumed = run, "", resume != ""
	m.agent.refresh()
	m.agent.vp.GotoBottom()
	return m, tea.Batch(waitEvent(t.ID, run.Events), m.agent.spin.Tick)
}

// finishRun stores the session ID so the next prompt continues the same
// conversation, or forgets it if it could not be resumed.
func (m *Model) finishRun(todoID string, done agent.EvDone) {
	t := m.todoByID(todoID)
	if t == nil {
		return
	}
	c := m.agent.conv(todoID)
	session := c.session
	if done.IsError && c.resumed && strings.Contains(strings.ToLower(done.Message), "no conversation found") {
		session = ""
		c.add(entInfo, "Previous conversation not found — the next message starts a new one.")
		m.agent.refresh()
	}
	if !m.backend.Resumable() || session == t.Session || (session == "" && !c.resumed) {
		return
	}
	if _, err := m.store.Update(t, func(t *store.Todo) { t.Session = session }); err != nil {
		m.setStatus("Couldn't save agent session: "+err.Error(), true)
		return
	}
	m.reloadKeepSelection()
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
	case "a", "i":
		return m, m.setFocus(focusAgent)
	case "e":
		return m.startEdit()
	case "t":
		return m, m.toggleTimer()
	case ",":
		return m, m.openSettings()
	case "n":
		m.modal = modalTodo
		m.form = newForm()
		return m, textinput.Blink
	case "?":
		m.modal = modalKeys
	case "T":
		m.openTimeLog()
	case "W":
		m.modal, m.week = modalWeek, newWeekReport(time.Now())
	case "u":
		m.openTrash()
	case "/":
		m.list.typing = true
		m.list.filter.CursorEnd()
		m.list.scroll()
		return m, m.list.filter.Focus()
	case "esc":
		if m.list.filtered() {
			m.list.clearFilter()
		}
	case ".":
		m.list.hideDone = !m.list.hideDone
		m.list.apply()
		if m.list.hideDone {
			m.setStatus("Hiding done todos", false)
		} else {
			m.setStatus("Showing done todos", false)
		}
	case "r":
		if t := m.list.selected(); t != nil {
			if m.agent.running(t.ID) {
				m.setStatus(m.backend.Name()+" is working on this todo — stop it first (esc in the agent pane)", true)
			} else {
				m.modal = modalTodo
				m.form = editForm(t)
				return m, textinput.Blink
			}
		}
	case "x", " ", "space":
		m.toggleDone()
	case "d":
		if t := m.list.selected(); t != nil {
			if m.agent.running(t.ID) {
				m.setStatus(m.backend.Name()+" is working on this todo — stop it first (esc in the agent pane)", true)
			} else {
				m.modal = modalDelete
			}
		}
	}
	m.syncSelection()
	return m, nil
}

func (m Model) updateNotes(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "h", "left":
		m.focus = focusList
		return m, nil
	case "a", "i":
		return m, m.setFocus(focusAgent)
	case "e":
		return m.startEdit()
	case "t":
		return m, m.toggleTimer()
	case ",":
		return m, m.openSettings()
	case "?":
		m.modal = modalKeys
		return m, nil
	case "T":
		m.openTimeLog()
		return m, nil
	case "W":
		m.modal, m.week = modalWeek, newWeekReport(time.Now())
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

func (m Model) startEdit() (tea.Model, tea.Cmd) {
	t := m.list.selected()
	if t == nil {
		return m, nil
	}
	if m.agent.running(t.ID) {
		m.setStatus(m.backend.Name()+" is working on these notes — wait for it, or press esc in the agent pane to stop", true)
		return m, nil
	}
	m.setFocus(focusNotes)
	return m, m.notes.startEdit(t)
}

func (m Model) updateEditor(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		if m.notes.dirty() && !m.saveNotes() {
			return m, nil // keep editing so nothing is lost
		}
		m.notes.stopEdit()
		return m, nil
	case "ctrl+s":
		if m.saveNotes() {
			m.setStatus("Saved", false)
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.notes.ta, cmd = m.notes.ta.Update(msg)
	return m, cmd
}

// saveNotes writes the editor contents to the todo being edited.
func (m *Model) saveNotes() bool {
	var target *store.Todo
	for _, t := range m.list.all {
		if t.ID == m.notes.editID {
			target = t
		}
	}
	if target == nil {
		m.setStatus("Couldn't save: todo no longer exists", true)
		return false
	}
	// If the notes changed on disk since editing started, keep that version
	// in .trash/ rather than silently overwriting it.
	backup := ""
	if disk, err := store.Load(target.Path); err == nil && m.notes.changedOnDisk(disk.Notes) {
		if backup, err = m.store.Backup(disk); err != nil {
			m.setStatus("Couldn't back up the version on disk, so didn't save: "+err.Error(), true)
			return false
		}
	}
	notes := m.notes.value()
	if _, err := m.store.Update(target, func(t *store.Todo) { t.Notes = notes }); err != nil {
		m.setStatus("Couldn't save: "+err.Error(), true)
		return false
	}
	m.notes.markSaved()
	m.reload(target.ID)
	if backup != "" {
		m.setStatus("Saved. The notes had changed on disk; that version is in "+shortenHome(backup), false)
	}
	return true
}

func (m *Model) openSettings() tea.Cmd {
	m.settings = newSettings(m.cfg, m.cfgPath, m.dirOverridden)
	m.modal = modalSettings
	return m.settings.focusCurrent()
}

func (m Model) updateSettings(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := &m.settings
	if f.raw {
		switch msg.String() {
		case "esc":
			f.raw, f.err = false, ""
			f.ta.Blur()
			return m, f.focusCurrent()
		case "ctrl+s":
			data := []byte(f.ta.Value() + "\n")
			cfg, err := config.Parse(data)
			if err == nil {
				err = m.applySettings(cfg, func() error { return config.SaveRaw(m.cfgPath, data) })
			}
			if err != nil {
				f.err = capitalize(err.Error())
				return m, nil
			}
			m.modal = modalNone
			m.setStatus("Settings saved", false)
			return m, nil
		}
		return m, f.update(msg)
	}

	switch msg.String() {
	case "esc":
		m.modal = modalNone
		return m, nil
	case "enter":
		cfg, err := f.config()
		if err == nil {
			err = m.applySettings(cfg, func() error { return config.Save(m.cfgPath, cfg) })
		}
		if err != nil {
			f.err = capitalize(err.Error())
			return m, nil
		}
		m.modal = modalNone
		m.setStatus("Settings saved", false)
		return m, nil
	case "ctrl+e":
		return m, f.startRaw(m.height - 16)
	case "up", "shift+tab":
		f.err = ""
		return m, f.move(-1)
	case "down", "tab":
		f.err = ""
		return m, f.move(1)
	case "left", "right":
		delta := 1
		if msg.String() == "left" {
			delta = -1
		}
		if f.cycle(delta) {
			f.err = ""
			return m, nil
		}
	}
	f.err = ""
	return m, f.update(msg)
}

// applySettings switches to the new settings. save is called once the new
// settings are known to work, so a failed switch leaves the file untouched.
func (m *Model) applySettings(cfg config.Config, save func() error) error {
	var newStore *store.Store
	var todos []*store.Todo
	var skipped []store.BadFile
	if !m.dirOverridden {
		dir := cfg.Dir
		if dir == "" {
			dir = "~/worklog"
		}
		dir, err := filepath.Abs(config.ExpandHome(dir))
		if err != nil {
			return err
		}
		if dir != m.store.Dir {
			if m.agent.anyRunning() {
				return errors.New("an agent is still running — stop it before changing the data folder")
			}
			if newStore, err = store.New(dir); err != nil {
				return err
			}
			if todos, skipped, err = newStore.LoadAll(); err != nil {
				return err
			}
		}
	}
	if err := save(); err != nil {
		return err
	}

	m.cfg = cfg
	m.staleAfter = cfg.StaleAfter()
	m.backend = BackendFor(cfg.Agent)
	m.agent.setBackend(m.backend.Name(), isClaude(m.backend))
	if newStore != nil {
		m.store = newStore
		m.stamps = fileStamps(newStore.Dir)
		m.agent.convs = map[string]*conversation{}
		m.kept = map[string]bool{}
		m.list.cursor, m.list.offset = 0, 0
		m.list.setTodos(todos)
		m.notes.invalidate()
		m.syncSelection()
		m.setSkipped(skipped)
	}
	return nil
}

func (m Model) updateModal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.modal {
	case modalSettings:
		return m.updateSettings(msg)
	case modalStaleTimer:
		t, now := m.stale.todo, time.Now()
		var err error
		switch msg.String() {
		case "enter":
			var at time.Time
			if at, err = m.stale.stopTime(now); err != nil {
				m.stale.err = err.Error()
				return m, nil
			}
			err = m.store.StopTimer(t, at)
		case "n":
			err = m.store.StopTimer(t, now)
		case "d":
			err = m.store.DiscardTimer(t)
		case "k", "esc":
			m.kept[t.ID] = true
		default:
			m.stale.err = ""
			var cmd tea.Cmd
			m.stale.input, cmd = m.stale.input.Update(msg)
			return m, cmd
		}
		if err != nil {
			m.stale.err = capitalize(err.Error())
			return m, nil
		}
		m.modal = modalNone
		m.reloadKeepSelection()
		m.checkStale()
		return m, nil

	case modalKeys:
		m.modal = modalNone // any key closes it
		return m, nil

	case modalTime:
		return m.updateTimeLog(msg)

	case modalWeek:
		return m.updateWeek(msg)

	case modalTrash:
		return m.updateTrash(msg)

	case modalDelete:
		switch msg.String() {
		case "y", "Y", "enter":
			m.modal = modalNone
			m.deleteSelected()
		case "n", "N", "esc", "q":
			m.modal = modalNone
		}
		return m, nil

	case modalTodo:
		switch msg.String() {
		case "esc":
			m.modal = modalNone
			return m, nil
		case "tab", "shift+tab", "up", "down":
			m.form.switchField()
			return m, nil
		case "enter":
			title, date := m.form.title.Value(), resolveDate(m.form.date.Value(), time.Now())
			var t *store.Todo
			var err error
			if m.form.edit != nil {
				t, err = m.store.Edit(m.form.edit, title, date)
			} else if t, err = m.store.Create(title, date); err == nil {
				m.list.clearFilter() // so the new todo is visible
			}
			if err != nil {
				m.form.err = capitalize(err.Error())
				return m, nil
			}
			m.modal = modalNone
			m.reload(t.ID)
			if m.form.edit != nil {
				m.setStatus("Saved “"+t.Title+"” ("+store.DateLabel(t.Date, time.Now())+")", false)
			} else {
				m.setStatus("Created “"+t.Title+"”", false)
			}
			return m, nil
		}
		m.form.err = ""
		return m, m.form.update(msg)
	}
	return m, nil
}

func (m Model) todoByID(id string) *store.Todo {
	for _, t := range m.list.all {
		if t.ID == id {
			return t
		}
	}
	return nil
}

func (m *Model) openTimeLog() {
	if t := m.list.selected(); t != nil {
		m.modal = modalTime
		m.times = newTimeLog(t)
	}
}

// updateTimeLog handles the time entries modal: a list of entries, a form
// for adding or editing one, and a delete confirmation.
func (m Model) updateTimeLog(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f, now := &m.times, time.Now()
	var err error
	switch {
	case f.editing:
		switch msg.String() {
		case "esc":
			f.editing = false
			return m, nil
		case "tab", "shift+tab", "up", "down":
			f.switchField()
			return m, nil
		case "enter":
			var e store.TimeEntry
			if e, err = f.entry(now); err == nil {
				err = m.store.PutTimeEntry(f.todo, f.old, e, now)
			}
			if err != nil {
				f.err = capitalize(err.Error())
				return m, nil
			}
			f.editing = false
		default:
			f.err = ""
			var cmd tea.Cmd
			*f.input(), cmd = f.input().Update(msg)
			return m, cmd
		}

	case f.deleting:
		switch msg.String() {
		case "y", "Y", "enter":
			f.deleting = false
			if e, ok := f.selected(); ok {
				err = m.store.DeleteTimeEntry(f.todo, e.Start)
			}
		case "n", "N", "esc", "q":
			f.deleting = false
			return m, nil
		default:
			return m, nil
		}

	default:
		f.err = ""
		switch msg.String() {
		case "esc", "q", "T":
			m.modal = modalNone
		case "j", "down":
			f.move(1)
		case "k", "up":
			f.move(-1)
		case "a", "n":
			f.add(now)
			return m, textinput.Blink
		case "enter", "e":
			if f.edit() {
				return m, textinput.Blink
			}
		case "d", "x":
			_, f.deleting = f.selected()
		}
		return m, nil
	}

	if err != nil {
		f.err = capitalize(err.Error())
	}
	m.reloadKeepSelection()
	if t := m.todoByID(f.todo.ID); t != nil {
		f.setTodo(t)
	}
	m.checkStale()
	return m, m.ensureTick()
}

// weekSize is the most room the week report may use.
func (m Model) weekSize() (w, h int) {
	return m.width - 4, m.height - 3
}

func (m Model) updateWeek(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	now := time.Now()
	week := store.BuildWeek(m.list.all, m.week.start, now)
	_, h := m.weekSize()
	switch msg.String() {
	case "esc", "q", "W":
		m.modal = modalNone
	case "h", "left", "[":
		m.week.shift(-1)
	case "l", "right", "]":
		m.week.shift(1)
	case "t":
		m.week = newWeekReport(now)
	case "j", "down":
		m.week.scroll(1, len(week.Rows), visibleRows(h))
	case "k", "up":
		m.week.scroll(-1, len(week.Rows), visibleRows(h))
	case "c":
		if err := copyMarkdown(week); err != nil {
			m.setStatus("Couldn't copy: "+err.Error(), true)
		} else {
			m.setStatus("Copied "+week.Name()+" as Markdown", false)
		}
	case "s":
		if path, err := saveCSV(m.store.Dir, week); err != nil {
			m.setStatus("Couldn't save: "+err.Error(), true)
		} else {
			m.setStatus("Saved "+shortenHome(path), false)
		}
	}
	return m, nil
}

func (m *Model) openTrash() {
	items, err := m.store.Trash()
	if err != nil {
		m.setStatus("Couldn't read the trash: "+err.Error(), true)
		return
	}
	m.modal, m.trash = modalTrash, trashList{items: items}
}

func (m Model) updateTrash(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := &m.trash
	f.err = ""
	switch msg.String() {
	case "esc", "q", "u":
		m.modal = modalNone
	case "j", "down":
		f.move(1)
	case "k", "up":
		f.move(-1)
	case "enter", "r":
		x, ok := f.selected()
		if !ok {
			break
		}
		t, err := m.store.Restore(x.Name, m.list.all)
		if err != nil {
			f.err = "Couldn't restore: " + err.Error()
			break
		}
		m.modal = modalNone
		m.list.clearFilter() // so the restored todo is visible
		m.reload(t.ID)
		m.setStatus("Restored “"+t.Title+"”", false)
	}
	return m, nil
}

func (m *Model) toggleDone() {
	t := m.list.selected()
	if t == nil {
		return
	}
	now := time.Now()
	if _, err := m.store.Update(t, func(t *store.Todo) {
		t.Done = !t.Done
		if e := t.Running(); t.Done && e != nil {
			e.End = &now // finishing a todo stops its timer
		}
	}); err != nil {
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
	m.setStatus("Deleted “"+t.Title+"” — u to restore", false)
}

// reload re-reads all todos from disk, keeping the cursor on selectID when
// given, or on the same position otherwise.
func (m *Model) reload(selectID string) {
	todos, skipped, err := m.store.LoadAll()
	if err != nil {
		m.setStatus("Couldn't load todos: "+err.Error(), true)
		return
	}
	m.setSkipped(skipped)
	m.list.setTodos(todos)
	if selectID != "" {
		m.list.selectID(selectID)
	}
	if m.modal == modalTime {
		if t := m.todoByID(m.times.todo.ID); t != nil {
			m.times.setTodo(t)
		}
	}
	m.notes.invalidate()
	m.syncSelection()
}

// reloadKeepSelection reloads from disk without moving the cursor.
func (m *Model) reloadKeepSelection() {
	id := ""
	if t := m.list.selected(); t != nil {
		id = t.ID
	}
	m.reload(id)
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// setSkipped records unreadable files, warning when the set changes.
func (m *Model) setSkipped(bad []store.BadFile) {
	same := len(bad) == len(m.skipped)
	for i := 0; same && i < len(bad); i++ {
		same = bad[i].String() == m.skipped[i].String()
	}
	m.skipped = bad
	switch {
	case same:
	case len(bad) == 1:
		m.setStatus("Skipped "+bad[0].String(), true)
	case len(bad) > 1:
		names := make([]string, len(bad))
		for i, b := range bad {
			names[i] = b.Name
		}
		m.setStatus(fmt.Sprintf("Skipped %d unreadable files: %s", len(bad), strings.Join(names, ", ")), true)
	}
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
	m.agent.setSize(m.width-m.listW-2, m.agentH-2)
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

	listTitle := "Todos"
	if m.list.filtered() {
		listTitle += fmt.Sprintf(" · %d/%d", len(m.list.todos), len(m.list.all))
	}
	if len(m.skipped) > 0 {
		listTitle += fmt.Sprintf(" · %d skipped", len(m.skipped))
	}
	list := renderPane(listTitle, m.list.view(m.listW-2, m.focus == focusList),
		m.listW, bodyH, m.focus == focusList)

	notesTitle := "Notes"
	if t := m.list.selected(); t != nil {
		notesTitle = "Notes: " + t.Title + timeSummary(t, time.Now())
		if m.notes.editing {
			notesTitle = "Editing: " + t.Title
			if m.notes.dirty() {
				notesTitle += " •"
			}
		}
	}
	notes := renderPane(notesTitle, m.notes.view(), rightW, m.notesH, m.focus == focusNotes)
	agentTitle := "Agent · " + m.backend.Name()
	if t := m.list.selected(); t != nil && m.agent.running(t.ID) {
		agentTitle += " · working"
	}
	agent := renderPane(agentTitle, m.agent.view(rightW-2, m.agentH-2, m.focus == focusAgent),
		rightW, m.agentH, m.focus == focusAgent)

	body := lipgloss.JoinHorizontal(lipgloss.Top, list, lipgloss.JoinVertical(lipgloss.Left, notes, agent))
	switch m.modal {
	case modalTodo:
		body = overlay(body, m.form.view(), m.width, bodyH)
	case modalDelete:
		body = overlay(body, deleteView(m.list.selected()), m.width, bodyH)
	case modalKeys:
		body = overlay(body, keysView(), m.width, bodyH)
	case modalTime:
		body = overlay(body, m.times.view(time.Now()), m.width, bodyH)
	case modalTrash:
		body = overlay(body, m.trash.view(time.Now()), m.width, bodyH)
	case modalWeek:
		now := time.Now()
		w, h := m.weekSize()
		body = overlay(body, m.week.view(store.BuildWeek(m.list.all, m.week.start, now), w, h, now), m.width, bodyH)
	case modalStaleTimer:
		body = overlay(body, m.stale.view(time.Now()), m.width, bodyH)
	case modalSettings:
		body = overlay(body, m.settings.view(), m.width, bodyH)
	}
	return lipgloss.JoinVertical(lipgloss.Left, body, m.footer())
}

// footer shows status or key hints on the left, and the running timer on
// the right so it is visible from any pane.
func (m Model) footer() string {
	left := m.footerLeft()
	t := m.runningTodo()
	if t == nil {
		return left
	}
	right := styleSelected.Render("⏱ "+store.FormatClock(time.Since(t.Running().Start))) +
		styleMuted.Render(" "+ansi.Truncate(t.Title, 24, "…")) + " "
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		left = ansi.Truncate(left, max(m.width-lipgloss.Width(right)-1, 0), "…")
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

// timeSummary describes a todo's tracked time for the notes title.
func timeSummary(t *store.Todo, now time.Time) string {
	var parts []string
	if today := t.ByDay(now)[now.Format(store.DateLayout)]; today >= time.Minute {
		parts = append(parts, "today "+store.FormatDuration(today))
	}
	if total := t.Total(now); total >= time.Minute {
		parts = append(parts, "total "+store.FormatDuration(total))
	}
	if !t.Done && t.Date < now.Format(store.DateLayout) {
		parts = append(parts, "from "+shortDate(t.Date))
	}
	if len(parts) == 0 {
		return ""
	}
	return " · " + strings.Join(parts, " · ")
}

func (m Model) footerLeft() string {
	switch {
	case m.status != "" && m.isErr:
		return " " + styleError.Render(m.status)
	case m.status != "":
		return " " + styleDoneIcon.Render(m.status)
	case m.modal == modalTodo:
		action := "create"
		if m.form.edit != nil {
			action = "save"
		}
		return renderHelp([]hint{{"tab", "switch field"}, {"enter", action}, {"esc", "cancel"}})
	case m.modal == modalKeys:
		return renderHelp([]hint{{"any key", "close"}})
	case m.modal == modalTime && m.times.editing:
		return renderHelp([]hint{{"tab", "switch field"}, {"enter", "save"}, {"esc", "cancel"}})
	case m.modal == modalTime && m.times.deleting:
		return renderHelp([]hint{{"y", "delete"}, {"n/esc", "cancel"}})
	case m.modal == modalTrash:
		return renderHelp([]hint{{"j/k", "move"}, {"enter", "restore"}, {"esc", "close"}})
	case m.modal == modalWeek:
		return renderHelp([]hint{{"h/l", "prev/next week"}, {"t", "this week"}, {"j/k", "scroll"}, {"c", "copy Markdown"}, {"s", "save CSV"}, {"esc", "close"}})
	case m.modal == modalTime:
		return renderHelp([]hint{{"j/k", "move"}, {"a", "add"}, {"enter/e", "edit"}, {"d", "delete"}, {"esc", "close"}})
	case m.modal == modalDelete:
		return renderHelp([]hint{{"y", "delete"}, {"n/esc", "cancel"}})
	case m.modal == modalSettings && m.settings.raw:
		return renderHelp([]hint{{"ctrl+s", "save"}, {"esc", "back to form"}})
	case m.modal == modalSettings:
		return renderHelp([]hint{{"↑/↓", "field"}, {"←/→", "change"}, {"enter", "save"}, {"ctrl+e", "edit raw TOML"}, {"esc", "cancel"}})
	case m.modal == modalStaleTimer:
		return renderHelp([]hint{{"enter", "stop at time"}, {"n", "stop now"}, {"k", "keep running"}, {"d", "discard"}})
	case m.list.typing:
		return renderHelp([]hint{{"enter", "keep filter"}, {"↑/↓", "move"}, {"esc", "clear"}})
	case m.notes.editing:
		return renderHelp([]hint{{"esc", "save & close"}, {"ctrl+s", "save"}, {"ctrl+c", "save & quit"}})
	}
	return renderHelp(helpFor(m.focus))
}
