// Package store persists todos as Markdown files with YAML frontmatter.
package store

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math"
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

// BadFile is a todo file that couldn't be read or parsed.
type BadFile struct {
	Name string // file name within the directory
	Err  error
}

func (b BadFile) String() string { return b.Name + ": " + b.Err.Error() }

// LoadAll reads every .md file in the directory, sorted newest date first.
// Files that can't be read or parsed are skipped and returned as bad, so
// one broken file doesn't hide the rest. Hidden files (such as the .tmp-*
// files Save writes before renaming) are not todos and are ignored.
func (s *Store) LoadAll() (todos []*Todo, bad []BadFile, err error) {
	paths, err := filepath.Glob(filepath.Join(s.Dir, "*.md"))
	if err != nil {
		return nil, nil, err
	}
	todos = make([]*Todo, 0, len(paths))
	for _, p := range paths {
		if strings.HasPrefix(filepath.Base(p), ".") {
			continue
		}
		t, err := Load(p)
		if err != nil {
			bad = append(bad, BadFile{Name: filepath.Base(p), Err: err})
			continue
		}
		todos = append(todos, t)
	}
	Sort(todos)
	return todos, bad, nil
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
// treated as notes only, as are files whose leading "---" block is not YAML
// (e.g. notes opening with a horizontal rule). A block that looks like
// frontmatter but can't be parsed is an error, so a broken todo is reported
// rather than silently loaded as notes.
func Parse(data []byte) (*Todo, error) {
	t := &Todo{}
	text := string(bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n")))
	text = strings.TrimPrefix(text, "\uFEFF")
	if !strings.HasPrefix(text, "---\n") {
		t.Notes = text
		return t, nil
	}
	// Search from the opening line's newline so an empty block ("---\n---")
	// is found too.
	rest := text[len("---"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		if !looksLikeYAML(rest) {
			t.Notes = text
			return t, nil
		}
		return nil, fmt.Errorf("unterminated frontmatter")
	}
	fm := strings.TrimPrefix(rest[:end], "\n")
	if err := yaml.Unmarshal([]byte(fm), t); err != nil {
		if !looksLikeYAML(fm) {
			return &Todo{Notes: text}, nil
		}
		return nil, fmt.Errorf("frontmatter: %w", err)
	}
	body := rest[end+len("\n---"):]
	body = strings.TrimPrefix(body, "\n")
	t.Notes = strings.TrimPrefix(body, "\n")
	return t, nil
}

var yamlKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*:(\s|$)`)

// looksLikeYAML reports whether a "---" block was meant as frontmatter: its
// first non-blank line is a "key:" pair. Anything else (prose, a heading)
// is Markdown following a horizontal rule.
func looksLikeYAML(block string) bool {
	for _, line := range strings.Split(block, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		return yamlKey.MatchString(line)
	}
	return true // empty frontmatter is fine
}

// Create makes a new todo for the given date and writes it to disk.
func (s *Store) Create(title, date string) (*Todo, error) {
	title, err := validate(title, date)
	if err != nil {
		return nil, err
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

// Edit changes a todo's title and date, renaming its file to match. The ID
// is kept, so the agent conversation and session stay attached.
func (s *Store) Edit(t *Todo, title, date string) (*Todo, error) {
	title, err := validate(title, date)
	if err != nil {
		return nil, err
	}
	fresh, err := s.Update(t, func(x *Todo) {
		x.Title = title
		x.Date = date
	})
	if err != nil {
		return nil, err
	}
	base := date + "-" + Slugify(title)
	if hasBase(filepath.Base(fresh.Path), base) {
		return fresh, nil
	}
	path := s.uniquePath(base)
	if err := os.Rename(fresh.Path, path); err != nil {
		return nil, err
	}
	fresh.Path = path
	return fresh, nil
}

func validate(title, date string) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return "", fmt.Errorf("title is required")
	}
	if _, err := time.Parse(DateLayout, date); err != nil {
		return "", fmt.Errorf("date must be YYYY-MM-DD")
	}
	return title, nil
}

// hasBase reports whether name is base.md or a uniquePath variant of it.
func hasBase(name, base string) bool {
	if name == base+".md" {
		return true
	}
	rest, ok := strings.CutPrefix(name, base+"-")
	if !ok {
		return false
	}
	n, ok := strings.CutSuffix(rest, ".md")
	return ok && n != "" && strings.Trim(n, "0123456789") == ""
}

func (s *Store) uniquePath(base string) string { return uniqueFile(s.Dir, base) }

// uniqueFile returns dir/base.md, or the first dir/base-N.md (N >= 2) that
// doesn't exist yet.
func uniqueFile(dir, base string) string {
	p := filepath.Join(dir, base+".md")
	for i := 2; fileExists(p); i++ {
		p = filepath.Join(dir, fmt.Sprintf("%s-%d.md", base, i))
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
	// Never overwrite an earlier deletion of the same name.
	dest := uniqueFile(trash, strings.TrimSuffix(filepath.Base(t.Path), ".md"))
	if err := os.Rename(t.Path, dest); err != nil {
		return err
	}
	// Record when it was deleted, for listing the trash.
	now := time.Now()
	os.Chtimes(dest, now, now)
	return nil
}

// Backup copies the todo's file into .trash/ with a timestamp, for keeping a
// version that is about to be overwritten. It returns the copy's path.
func (s *Store) Backup(t *Todo) (string, error) {
	data, err := os.ReadFile(t.Path)
	if err != nil {
		return "", err
	}
	trash := filepath.Join(s.Dir, ".trash")
	if err := os.MkdirAll(trash, 0o755); err != nil {
		return "", err
	}
	name := strings.TrimSuffix(filepath.Base(t.Path), ".md")
	// Two backups within a second get -2, -3, … rather than overwriting.
	dest := uniqueFile(trash, name+"."+time.Now().Format("20060102-150405"))
	return dest, os.WriteFile(dest, data, 0o644)
}

// Trashed is a file in .trash/: a deleted todo or a backup made by saving
// over notes that changed on disk.
type Trashed struct {
	Name     string
	Todo     *Todo // nil if the file can't be parsed
	Modified time.Time
	Backup   bool // a conflict backup rather than a deleted todo
}

var backupSuffix = regexp.MustCompile(`\.\d{8}-\d{6}(-\d+)?\.md$`)

// Trash lists .trash/, most recently modified first.
func (s *Store) Trash() ([]Trashed, error) {
	paths, err := filepath.Glob(filepath.Join(s.Dir, ".trash", "*.md"))
	if err != nil {
		return nil, err
	}
	var out []Trashed
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		t, _ := Load(p)
		out = append(out, Trashed{
			Name:     filepath.Base(p),
			Todo:     t,
			Modified: info.ModTime(),
			Backup:   backupSuffix.MatchString(filepath.Base(p)),
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Modified.After(out[j].Modified) })
	return out, nil
}

// Restore moves a file from .trash/ back into the directory, named after
// its date and title. If its ID is already used by one of existing (e.g. a
// backup of a todo that still exists), it gets a new ID, no agent session
// and no time entries, so the two don't share a conversation and the time
// (possibly a running timer) isn't counted twice.
func (s *Store) Restore(name string, existing []*Todo) (*Todo, error) {
	src := filepath.Join(s.Dir, ".trash", filepath.Base(name))
	t, err := Load(src)
	if err != nil {
		return nil, err
	}
	for _, e := range existing {
		if e.ID == t.ID {
			t.ID = Slugify(t.Title) + "-" + randHex(3)
			t.Session = ""
			t.Time = nil
			break
		}
	}
	if _, err := time.Parse(DateLayout, t.Date); err != nil {
		t.Date = time.Now().Format(DateLayout)
	}
	t.Path = s.uniquePath(t.Date + "-" + Slugify(t.Title))
	if err := s.Save(t); err != nil {
		return nil, err
	}
	if err := os.Remove(src); err != nil {
		return nil, err
	}
	return t, nil
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
	// Count days between noons, rounded: a DST change makes a day 23 or 25
	// hours long, which would otherwise be truncated to the wrong day.
	noon := func(y int, m time.Month, d int) time.Time {
		return time.Date(y, m, d, 12, 0, 0, 0, now.Location())
	}
	diff := noon(now.Date()).Sub(noon(d.Date()))
	switch days := int(math.Round(diff.Hours() / 24)); {
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
