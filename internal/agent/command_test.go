package agent

import (
	"io"
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

// chunkReader hands out r in fixed-size reads, like a pipe would.
type chunkReader struct {
	r    io.Reader
	size int
}

func (c chunkReader) Read(p []byte) (int, error) {
	if len(p) > c.size {
		p = p[:c.size]
	}
	return c.r.Read(p)
}

// An escape sequence split across two reads used to leak its tail ("31m")
// into the text, because ansi.Strip ran on each read separately.
func TestStreamTextHoldsBackSplitEscape(t *testing.T) {
	body := strings.Repeat("a", 4094) + "\x1b[31mred\x1b[0m"
	for _, size := range []int{4096, 4095, 4097, 1, 7} {
		var got strings.Builder
		streamText(chunkReader{strings.NewReader(body), size}, func(s string) { got.WriteString(s) })
		if want := strings.Repeat("a", 4094) + "red"; got.String() != want {
			t.Errorf("chunk size %d: got %q (len %d), want %d a's + \"red\"", size, got.String(), got.Len(), 4094)
		}
	}

	// Pending bytes are flushed at EOF, including a lone ESC.
	var got strings.Builder
	streamText(chunkReader{strings.NewReader("hi\x1b"), 1}, func(s string) { got.WriteString(s) })
	if got.String() != "hi" {
		t.Errorf("EOF flush: got %q", got.String())
	}
}

func TestCommandSplitEscape(t *testing.T) {
	// printf writes 4094 bytes then a colour sequence, so a 4096-byte read
	// ends with "ESC [" and the next one starts with "31m".
	c := Command{Argv: []string{"sh", "-c", `head -c 4094 /dev/zero | tr '\0' a; printf '\033[31mred\033[0m\n'`}}
	run, err := c.Start(Options{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	text, done := collect(t, run)
	if want := strings.Repeat("a", 4094) + "red\n"; text != want {
		t.Errorf("text = %q...%q (len %d), want %d a's + \"red\\n\"", text[:min(10, len(text))], text[max(0, len(text)-12):], len(text), 4094)
	}
	if done.IsError {
		t.Errorf("done = %+v", done)
	}
}

func TestEscPrefix(t *testing.T) {
	cases := map[string]int{
		"plain":                             5,
		"ab\x1b":                            2, // bare ESC
		"ab\x1b[":                           2, // CSI without final byte
		"ab\x1b[31":                         2,
		"ab\x1b[31m":                        7, // complete CSI
		"ab\x1b[31mcd":                      9,
		"ab\x1b]0;title":                    2,    // OSC without terminator
		"ab\x1b]0;t\x07":                    8,    // OSC + BEL
		"ab\x1b]0;t\x1b\\":                  9,    // OSC + ST: last ESC is the ST
		"ab\x1b(":                           2,    // nF without final
		"ab\x1b(B":                          5,    // charset select
		"ab\x1bM":                           4,    // two-byte Fe
		"\x1b[" + strings.Repeat("1;", 600): 1202, // too long to be real; pass through
	}
	for in, want := range cases {
		if got := escPrefix([]byte(in)); got != want {
			t.Errorf("escPrefix(%q) = %d, want %d", in, got, want)
		}
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
