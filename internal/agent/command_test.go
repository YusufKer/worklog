package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func collect(t *testing.T, run *Run) (text string, done EvDone) {
	t.Helper()
	var b strings.Builder
	for ev := range run.Events {
		switch e := ev.(type) {
		case EvText:
			b.WriteString(e.Delta)
		case EvDone:
			done = e
		}
	}
	return b.String(), done
}

func TestCommandExpand(t *testing.T) {
	c := Command{Argv: []string{"agent", "--file={notes_file}", "{prompt}", "{message}", "{title}"}}
	got := c.Expand(Options{Message: "hi", SystemPrompt: "ctx", NotesFile: "/n.md", Title: "T"})
	want := []string{"agent", "--file=/n.md", "ctx\n\nThe user's request:\nhi", "hi", "T"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("arg %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestCommandStreamsAndEdits(t *testing.T) {
	dir := t.TempDir()
	notes := filepath.Join(dir, "n.md")
	os.WriteFile(notes, []byte("# Notes\n"), 0o644)

	// A stand-in agent: prints coloured text and appends to the notes file.
	c := Command{Argv: []string{"sh", "-c", `printf '\033[32mhello\033[0m {message}\n'; echo '- added' >> "$1"`, "sh", "{notes_file}"}}
	run, err := c.Start(Options{Dir: dir, Message: "world", NotesFile: notes})
	if err != nil {
		t.Fatal(err)
	}
	text, done := collect(t, run)
	if text != "hello world\n" {
		t.Errorf("text = %q (ANSI should be stripped)", text)
	}
	if done.IsError {
		t.Errorf("done = %+v", done)
	}
	if b, _ := os.ReadFile(notes); string(b) != "# Notes\n- added\n" {
		t.Errorf("notes = %q", b)
	}
}

func TestCommandFailure(t *testing.T) {
	c := Command{Argv: []string{"sh", "-c", "echo boom >&2; exit 3"}}
	run, err := c.Start(Options{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	_, done := collect(t, run)
	if !done.IsError || done.Message != "boom" {
		t.Errorf("done = %+v", done)
	}
	if _, err := (Command{Argv: []string{"definitely-not-a-real-agent"}}).Start(Options{}); err == nil {
		t.Error("expected error for missing command")
	}
}

func TestValidPrefix(t *testing.T) {
	b := []byte("hé")            // h, 0xC3, 0xA9
	if validPrefix(b[:2]) != 1 { // cut mid-rune
		t.Error("should hold back partial rune")
	}
	if validPrefix(b) != 3 {
		t.Error("complete string should pass through")
	}
}
