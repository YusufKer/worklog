// Package store persists todos as Markdown files with YAML frontmatter.
package store

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const DateLayout = "2006-01-02"

// Todo is a single work item. Its notes are the Markdown body of the file.
type Todo struct {
	ID      string    `yaml:"id"`
	Title   string    `yaml:"title"`
	Date    string    `yaml:"date"`
	Done    bool      `yaml:"done"`
	Created time.Time `yaml:"created"`
	Session string    `yaml:"session,omitempty"`

	Notes string `yaml:"-"`
	Path  string `yaml:"-"`
}

// Store reads and writes todos in a single directory.
type Store struct {
	Dir string
}

func New(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{Dir: dir}, nil
}

// LoadAll reads every .md file in the directory, sorted newest date first.
func (s *Store) LoadAll() ([]*Todo, error) {
	paths, err := filepath.Glob(filepath.Join(s.Dir, "*.md"))
	if err != nil {
		return nil, err
	}
	todos := make([]*Todo, 0, len(paths))
	for _, p := range paths {
		t, err := Load(p)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(p), err)
		}
		todos = append(todos, t)
	}
	Sort(todos)
	return todos, nil
}

// Sort orders todos by date (newest first), then creation time (newest first).
func Sort(todos []*Todo) {
	sort.SliceStable(todos, func(i, j int) bool {
		if todos[i].Date != todos[j].Date {
			return todos[i].Date > todos[j].Date
		}
		return todos[i].Created.After(todos[j].Created)
	})
}

// Load parses a single todo file.
func Load(path string) (*Todo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	t, err := Parse(data)
	if err != nil {
		return nil, err
	}
	t.Path = path
	if t.Title == "" {
		t.Title = strings.TrimSuffix(filepath.Base(path), ".md")
	}
	if t.ID == "" {
		t.ID = strings.TrimSuffix(filepath.Base(path), ".md")
	}
	if t.Date == "" {
		if info, err := os.Stat(path); err == nil {
			t.Date = info.ModTime().Format(DateLayout)
		}
	}
	return t, nil
}

// Parse splits frontmatter from the body. Files without frontmatter are
// treated as notes only.
func Parse(data []byte) (*Todo, error) {
	t := &Todo{}
	text := string(bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n")))
	if !strings.HasPrefix(text, "---\n") {
		t.Notes = text
		return t, nil
	}
	rest := text[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return nil, fmt.Errorf("unterminated frontmatter")
	}
	if err := yaml.Unmarshal([]byte(rest[:end]), t); err != nil {
		return nil, fmt.Errorf("frontmatter: %w", err)
	}
	body := rest[end+len("\n---"):]
	body = strings.TrimPrefix(body, "\n")
	t.Notes = strings.TrimPrefix(body, "\n")
	return t, nil
}

// Group is a set of todos sharing a date.
type Group struct {
	Label string
	Date  string
	Todos []*Todo
}

// GroupByDate groups already-sorted todos by their date, labelling each group
// relative to now (Today, Yesterday, or a short date).
func GroupByDate(todos []*Todo, now time.Time) []Group {
	var groups []Group
	for _, t := range todos {
		if len(groups) == 0 || groups[len(groups)-1].Date != t.Date {
			groups = append(groups, Group{Label: DateLabel(t.Date, now), Date: t.Date})
		}
		g := &groups[len(groups)-1]
		g.Todos = append(g.Todos, t)
	}
	return groups
}

// DateLabel renders a YYYY-MM-DD date relative to now.
func DateLabel(date string, now time.Time) string {
	d, err := time.ParseInLocation(DateLayout, date, now.Location())
	if err != nil {
		return date
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	switch days := int(today.Sub(d).Hours() / 24); {
	case days == 0:
		return "Today"
	case days == 1:
		return "Yesterday"
	case days == -1:
		return "Tomorrow"
	}
	if d.Year() == now.Year() {
		return d.Format("Mon Jan 2")
	}
	return d.Format("Mon Jan 2 2006")
}
