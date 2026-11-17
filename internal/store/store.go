// Package store persists todos as Markdown files with YAML frontmatter.
package store

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const DateLayout = "2006-01-02"

// Todo is a single work item. Its notes are the Markdown body of the file.
type Todo struct {
	ID      string      `yaml:"id"`
	Title   string      `yaml:"title"`
	Date    string      `yaml:"date"`
	Done    bool        `yaml:"done"`
	Created time.Time   `yaml:"created,omitempty"`
	Session string      `yaml:"session,omitempty"`
	Time    []TimeEntry `yaml:"time,omitempty"`

	Notes string `yaml:"-"`
	Path  string `yaml:"-"`
}

// Store reads and writes todos in a single directory.
type Store struct {
	Dir string
}

func New(dir string) (*Store, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
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

// Create makes a new todo for the given date and writes it to disk.
func (s *Store) Create(title, date string) (*Todo, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, fmt.Errorf("title is required")
	}
	if _, err := time.Parse(DateLayout, date); err != nil {
		return nil, fmt.Errorf("date must be YYYY-MM-DD")
	}
	slug := Slugify(title)
	t := &Todo{
		ID:      slug + "-" + randHex(3),
		Title:   title,
		Date:    date,
		Created: time.Now().Truncate(time.Second),
		Path:    s.uniquePath(date + "-" + slug),
	}
	if err := s.Save(t); err != nil {
		return nil, err
	}
	return t, nil
}

func (s *Store) uniquePath(base string) string {
	p := filepath.Join(s.Dir, base+".md")
	for i := 2; fileExists(p); i++ {
		p = filepath.Join(s.Dir, fmt.Sprintf("%s-%d.md", base, i))
	}
	return p
}

// Save writes the todo atomically (temp file + rename) to t.Path.
func (s *Store) Save(t *Todo) error {
	data, err := Marshal(t)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(t.Path), ".tmp-*.md")
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
	return os.Rename(tmp.Name(), t.Path)
}

// Update re-reads the todo from disk, applies fn, and saves it, so changes
// made to the file elsewhere (e.g. by the agent) are not clobbered.
func (s *Store) Update(t *Todo, fn func(*Todo)) (*Todo, error) {
	fresh, err := Load(t.Path)
	if err != nil {
		return nil, err
	}
	fn(fresh)
	if err := s.Save(fresh); err != nil {
		return nil, err
	}
	return fresh, nil
}

// Delete moves the todo's file into a .trash directory.
func (s *Store) Delete(t *Todo) error {
	trash := filepath.Join(s.Dir, ".trash")
	if err := os.MkdirAll(trash, 0o755); err != nil {
		return err
	}
	dest := filepath.Join(trash, filepath.Base(t.Path))
	if fileExists(dest) {
		dest = filepath.Join(trash, fmt.Sprintf("%s-%d.md",
			strings.TrimSuffix(filepath.Base(t.Path), ".md"), time.Now().Unix()))
	}
	return os.Rename(t.Path, dest)
}

// Marshal renders a todo as frontmatter followed by its notes.
func Marshal(t *Todo) ([]byte, error) {
	fm, err := yaml.Marshal(t)
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	b.WriteString("---\n")
	b.Write(fm)
	b.WriteString("---\n")
	if t.Notes != "" {
		b.WriteString("\n")
		b.WriteString(t.Notes)
	}
	return b.Bytes(), nil
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// Slugify turns a title into a short filename-safe slug.
func Slugify(title string) string {
	slug := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(title), "-"), "-")
	if len(slug) > 40 {
		slug = strings.TrimRight(slug[:40], "-")
	}
	if slug == "" {
		slug = "todo"
	}
	return slug
}

func randHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// Group is a set of todos shown under one date header.
type Group struct {
	Label string
	Date  string
	Todos []*Todo
}

// Carried reports whether t is shown in this group although it is dated
// earlier (an unfinished todo carried over to today).
func (g Group) Carried(t *Todo) bool { return t.Date != g.Date }

// GroupByDate groups already-sorted todos by date, newest first, labelling
// each group relative to now (Today, Yesterday, or a short date).
// Unfinished todos from earlier days are carried over into Today, after
// today's own todos; their stored date is left unchanged.
func GroupByDate(todos []*Todo, now time.Time) []Group {
	today := now.Format(DateLayout)
	var future, current, past []*Todo
	for _, t := range todos {
		switch {
		case t.Date > today:
			future = append(future, t)
		case t.Date == today:
			current = append(current, t)
		case !t.Done:
			current = append(current, t) // carried over
		default:
			past = append(past, t)
		}
	}

	var groups []Group
	add := func(date string, t *Todo) {
		if len(groups) == 0 || groups[len(groups)-1].Date != date {
			groups = append(groups, Group{Label: DateLabel(date, now), Date: date})
		}
		g := &groups[len(groups)-1]
		g.Todos = append(g.Todos, t)
	}
	for _, t := range future {
		add(t.Date, t)
	}
	for _, t := range current {
		add(today, t)
	}
	for _, t := range past {
		add(t.Date, t)
	}
	return groups
}

// Flatten returns the todos in display order.
func Flatten(groups []Group) []*Todo {
	var out []*Todo
	for _, g := range groups {
		out = append(out, g.Todos...)
	}
	return out
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
