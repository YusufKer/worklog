package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Chat transcripts are kept per todo in <dir>/.chats/<id>.json so the agent
// pane shows earlier messages after a restart. The agent's own memory of
// the conversation is the session ID in the todo's frontmatter.

const historyMax = 200 // entries kept per todo

type savedEntry struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

var kindNames = map[entryKind]string{
	entUser: "user", entText: "text", entTool: "tool", entError: "error", entInfo: "info",
}

// historyPath returns where a todo's chat is kept, or "" if the ID isn't
// safe to use as a file name (IDs can be edited by hand).
func historyPath(dir, id string) string {
	if dir == "" || id == "" || id == "." || id == ".." || strings.ContainsAny(id, `/\`) {
		return ""
	}
	return filepath.Join(dir, ".chats", id+".json")
}

func loadHistory(dir, id string) []entry {
	path := historyPath(dir, id)
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var saved []savedEntry
	if json.Unmarshal(data, &saved) != nil {
		return nil
	}
	kinds := map[string]entryKind{}
	for k, name := range kindNames {
		kinds[name] = k
	}
	var entries []entry
	for _, s := range saved {
		if k, ok := kinds[s.Kind]; ok {
			entries = append(entries, entry{kind: k, text: s.Text, final: true})
		}
	}
	return entries
}

// saveHistory writes the last historyMax entries atomically.
func saveHistory(dir, id string, entries []entry) error {
	path := historyPath(dir, id)
	if path == "" {
		return nil
	}
	entries = entries[max(len(entries)-historyMax, 0):]
	saved := make([]savedEntry, 0, len(entries))
	for _, e := range entries {
		saved = append(saved, savedEntry{Kind: kindNames[e.kind], Text: e.text})
	}
	data, err := json.MarshalIndent(saved, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func deleteHistory(dir, id string) error {
	path := historyPath(dir, id)
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
