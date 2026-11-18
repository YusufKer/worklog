package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/YusufKer/worklog/internal/agent"
	"github.com/YusufKer/worklog/internal/store"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/x/ansi"
)

type entryKind int

const (
	entUser entryKind = iota
	entText
	entTool
	entError
	entInfo
)

type entry struct {
	kind  entryKind
	text  string
	final bool // text is complete and can be rendered as Markdown

	rendered  string
	renderedW int
}

// conversation is the transcript and in-flight run for one todo.
type conversation struct {
	entries []entry
	run     *agent.Run
	session string // session ID reported by the current run
	resumed bool   // the current run resumed an earlier session
}

func (c *conversation) add(kind entryKind, text string) {
	c.finalize()
	c.entries = append(c.entries, entry{kind: kind, text: text})
}

// finalize marks streamed text as complete.
func (c *conversation) finalize() {
	if n := len(c.entries); n > 0 && c.entries[n-1].kind == entText {
		c.entries[n-1].final = true
	}
}

// agentEventMsg carries one event from a run into Bubble Tea.
type agentEventMsg struct {
	todoID string
	ev     agent.Event
	closed bool
}

func waitEvent(todoID string, ch <-chan agent.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		return agentEventMsg{todoID: todoID, ev: ev, closed: !ok}
	}
}

// agentModel is the AI pane: a transcript per todo plus a prompt input.
type agentModel struct {
	convs     map[string]*conversation
	dir       string // data directory, for saved chat history
	shownID   string
	vp        viewport.Model
	input     textinput.Model
	spin      spinner.Model
	glamStyle string
	renderer  *glamour.TermRenderer
	width     int
	name      string // agent name for prompts
	markdown  bool   // render finished replies as Markdown
}

func newAgent(glamStyle, name string, markdown bool) agentModel {
	in := textinput.New()
	in.Prompt = "› "
	in.PromptStyle = styleSelected
	in.Placeholder = "Ask " + name + " to research or edit these notes…"
	in.PlaceholderStyle = styleMuted

	sp := spinner.New(spinner.WithSpinner(spinner.Dot), spinner.WithStyle(styleSelected))
	return agentModel{
		convs:     map[string]*conversation{},
		vp:        viewport.New(0, 0),
		input:     in,
		spin:      sp,
		glamStyle: glamStyle,
		name:      name,
		markdown:  markdown,
	}
}

// setBackend updates labels after the agent is changed in settings.
func (a *agentModel) setBackend(name string, markdown bool) {
	a.name, a.markdown = name, markdown
	a.input.Placeholder = "Ask " + name + " to research or edit these notes…"
	a.refresh()
}

func (a *agentModel) conv(id string) *conversation {
	c, ok := a.convs[id]
	if !ok {
		c = &conversation{entries: loadHistory(a.dir, id)}
		a.convs[id] = c
	}
	return c
}

// save writes a todo's transcript to disk.
func (a *agentModel) save(id string) error {
	return saveHistory(a.dir, id, a.conv(id).entries)
}

func (a *agentModel) running(id string) bool {
	c, ok := a.convs[id]
	return ok && c.run != nil
}

func (a *agentModel) anyRunning() bool {
	for _, c := range a.convs {
		if c.run != nil {
			return true
		}
	}
	return false
}

func (a *agentModel) cancelAll() {
	for _, c := range a.convs {
		if c.run != nil {
			c.run.Cancel()
		}
	}
}

func (a *agentModel) setSize(w, h int) {
	if w != a.width {
		a.renderer = nil
	}
	a.width = w
	a.vp.Width = w
	a.vp.Height = max(h-1, 1) // last line is the prompt
	a.input.Width = max(w-4, 10)
	a.refresh()
}

// show switches the transcript to the given todo.
func (a *agentModel) show(id string) {
	if id == a.shownID {
		return
	}
	if id != "" {
		a.conv(id) // load saved history
	}
	a.shownID = id
	a.refresh()
	a.vp.GotoBottom()
}

// refresh re-renders the shown transcript, following the bottom if the
// user hasn't scrolled up.
func (a *agentModel) refresh() {
	follow := a.vp.AtBottom()
	a.vp.SetContent(a.render())
	if follow {
		a.vp.GotoBottom()
	}
}

func (a *agentModel) render() string {
	c, ok := a.convs[a.shownID]
	if !ok || len(c.entries) == 0 {
		return styleMuted.Render(" Ask " + a.name + " to research something or edit these notes,\n" +
			" e.g. “add a next-steps checklist” or “research Stripe idempotency keys”.\n" +
			" Each todo keeps its own conversation. Type /clear to start over.")
	}
	w := max(a.width-2, 10)
	var out []string
	for i := range c.entries {
		e := &c.entries[i]
		switch e.kind {
		case entUser:
			if i > 0 {
				out = append(out, "")
			}
			out = append(out, styleSelected.Render(" › ")+ansi.Wrap(e.text, w-2, ""))
		case entText:
			out = append(out, a.renderText(e, w))
		case entTool:
			out = append(out, styleMuted.Render(ansi.Truncate("   ↳ "+e.text, w+1, "…")))
		case entError:
			out = append(out, styleError.Render(" "+ansi.Wrap(e.text, w, "")))
		case entInfo:
			out = append(out, styleMuted.Render(" "+e.text))
		}
	}
	return strings.Join(out, "\n")
}

// renderText shows streaming text as wrapped plain text, and completed
// text as rendered Markdown.
func (a *agentModel) renderText(e *entry, w int) string {
	text := strings.TrimSpace(e.text)
	if !e.final || text == "" || !a.markdown {
		return " " + strings.ReplaceAll(ansi.Wrap(text, w, ""), "\n", "\n ")
	}
	if e.rendered != "" && e.renderedW == w {
		return e.rendered
	}
	if a.renderer == nil {
		r, err := glamour.NewTermRenderer(
			glamour.WithStandardStyle(a.glamStyle),
			glamour.WithWordWrap(w-2),
		)
		if err != nil {
			return " " + ansi.Wrap(text, w, "")
		}
		a.renderer = r
	}
	out, err := a.renderer.Render(text)
	if err != nil {
		return " " + ansi.Wrap(text, w, "")
	}
	e.rendered, e.renderedW = strings.Trim(out, "\n"), w
	return e.rendered
}

func (a agentModel) view(width, height int, focused bool) string {
	var prompt string
	switch {
	case a.running(a.shownID):
		prompt = " " + a.spin.View() + styleMuted.Render(" "+a.name+" is working… esc to stop")
	case focused:
		prompt = " " + a.input.View()
	default:
		prompt = styleMuted.Render(" › press a to ask " + a.name)
	}
	return a.vp.View() + "\n" + prompt
}

// handle applies an event from a run to its conversation. It reports whether
// the notes on disk may have changed.
func (a *agentModel) handle(msg agentEventMsg) (notesChanged bool) {
	c := a.conv(msg.todoID)
	switch ev := msg.ev.(type) {
	case agent.EvSession:
		c.session = ev.ID
	case agent.EvTextStart:
		c.add(entText, "")
	case agent.EvText:
		if n := len(c.entries); n > 0 && c.entries[n-1].kind == entText && !c.entries[n-1].final {
			c.entries[n-1].text += ev.Delta
		} else {
			c.add(entText, ev.Delta)
		}
	case agent.EvTool:
		label := ev.Name
		if ev.Detail != "" {
			label += " " + ev.Detail
		}
		c.add(entTool, label)
	case agent.EvToolResult:
		if ev.IsError {
			c.add(entTool, ev.Name+" failed: "+firstLine(ev.Message))
		} else if agent.IsEdit(ev.Name) {
			notesChanged = true
		}
	case agent.EvDone:
		c.finalize()
		c.run = nil
		if ev.IsError {
			c.add(entError, ev.Message)
		} else {
			info := "done in " + ev.Elapsed.Round(time.Second).String()
			if ev.CostUSD > 0 {
				info += fmt.Sprintf(" · $%.2f", ev.CostUSD)
			}
			c.add(entInfo, info)
		}
		notesChanged = true
	}
	if msg.todoID == a.shownID {
		a.refresh()
	}
	return notesChanged
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}

// systemPrompt tells Claude where it is and which file holds the notes.
func systemPrompt(t *store.Todo) string {
	status := "open"
	if t.Done {
		status = "done"
	}
	return fmt.Sprintf(`You are the assistant inside Worklog, the user's terminal todo app.
The user is looking at the todo %q (date %s, %s).
Its notes are the Markdown file: %s

- When asked to add to, change, tidy or summarise the notes, edit that file directly with Edit or Write.
- The file starts with YAML frontmatter between --- lines. The app manages it: never change it.
- When asked to research, use WebSearch/WebFetch. Answer in the chat; only put findings in the notes if the user asks.
- The chat pane is small: keep replies short and skimmable, and mention briefly what you changed in the notes.
- Other todos are the other .md files in the same directory; read them if useful, but only edit them if asked.`,
		t.Title, t.Date, status, t.Path)
}
