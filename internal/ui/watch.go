package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const pollInterval = 2 * time.Second

// filesMsg reports the size and modification time of each todo file, so
// changes made outside the app (another editor, git, sync) are picked up.
type filesMsg struct {
	dir    string
	stamps map[string]string // file name -> size and mtime
}

// pollFiles checks the data directory after pollInterval.
func pollFiles(dir string) tea.Cmd {
	return tea.Tick(pollInterval, func(time.Time) tea.Msg {
		return filesMsg{dir: dir, stamps: fileStamps(dir)}
	})
}

func fileStamps(dir string) map[string]string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	stamps := map[string]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if info, err := e.Info(); err == nil {
			stamps[e.Name()] = fmt.Sprintf("%d %d", info.Size(), info.ModTime().UnixNano())
		}
	}
	return stamps
}

// changedFiles lists names added, removed or modified between two snapshots.
func changedFiles(old, cur map[string]string) []string {
	var names []string
	for name, s := range cur {
		if old[name] != s {
			names = append(names, name)
		}
	}
	for name := range old {
		if _, ok := cur[name]; !ok {
			names = append(names, name)
		}
	}
	return names
}

// handleFiles reloads when the directory changed since the last poll.
func (m Model) handleFiles(msg filesMsg) (tea.Model, tea.Cmd) {
	next := pollFiles(m.store.Dir)
	if msg.dir != m.store.Dir || msg.stamps == nil {
		return m, next // from before a data folder switch, or unreadable
	}
	if m.stamps == nil {
		m.stamps = msg.stamps
		return m, next
	}
	changed := changedFiles(m.stamps, msg.stamps)
	m.stamps = msg.stamps
	if len(changed) == 0 {
		return m, next
	}
	m.reloadKeepSelection()
	if m.notes.editing {
		if t := m.todoByID(m.notes.editID); t != nil {
			for _, name := range changed {
				if name == filepath.Base(t.Path) {
					m.setStatus("These notes changed on disk. Saving keeps your version; the other goes to .trash/", true)
				}
			}
		}
	}
	return m, tea.Batch(next, m.ensureTick())
}
