package ui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/YusufKer/worklog/internal/config"
	tea "github.com/charmbracelet/bubbletea"
)

// editorDoneMsg arrives when the external editor opened by E exits.
type editorDoneMsg struct{ err error }

// openExternal opens the selected todo's file in $VISUAL or $EDITOR,
// suspending the UI until the editor exits.
func (m Model) openExternal() (tea.Model, tea.Cmd) {
	t := m.list.selected()
	if t == nil {
		return m, nil
	}
	if m.agent.running(t.ID) {
		m.setStatus(m.backend.Name()+" is working on this todo — stop it first (esc in the agent pane)", true)
		return m, nil
	}
	args, err := editorCommand()
	if err != nil {
		m.setStatus(err.Error(), true)
		return m, nil
	}
	c := exec.Command(args[0], append(args[1:], t.Path)...)
	c.Dir = m.store.Dir
	return m, tea.ExecProcess(c, func(err error) tea.Msg { return editorDoneMsg{err} })
}

// editorCommand parses $VISUAL, then $EDITOR, as a command line (so
// "code --wait" works).
func editorCommand() ([]string, error) {
	for _, v := range []string{"VISUAL", "EDITOR"} {
		s := os.Getenv(v)
		if s == "" {
			continue
		}
		args, err := config.SplitArgs(s)
		if err != nil {
			return nil, fmt.Errorf("$%s: %v", v, err)
		}
		if len(args) == 0 {
			continue
		}
		return args, nil
	}
	return nil, errors.New("set $EDITOR (or $VISUAL) to open todos in an editor")
}
