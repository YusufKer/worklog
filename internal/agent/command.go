package agent

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// Command runs any CLI agent (codex, gemini, aider, a script…) and streams
// its stdout as plain text. Argv elements may contain placeholders:
//
//	{prompt}     the user's message with context about the todo and notes file
//	{message}    the user's message only
//	{notes_file} absolute path of the todo's notes file
//	{title}      the todo's title
//	{dir}        the notes directory
type Command struct {
	Label string
	Argv  []string
}

func (c Command) Name() string {
	if c.Label != "" {
		return c.Label
	}
	if len(c.Argv) > 0 {
		return c.Argv[0]
	}
	return "agent"
}

func (c Command) Resumable() bool { return false }

// Expand substitutes placeholders into the argv.
func (c Command) Expand(o Options) []string {
	prompt := o.Message
	if o.SystemPrompt != "" {
		prompt = o.SystemPrompt + "\n\nThe user's request:\n" + o.Message
	}
	r := strings.NewReplacer(
		"{prompt}", prompt,
		"{message}", o.Message,
		"{notes_file}", o.NotesFile,
		"{title}", o.Title,
		"{dir}", o.Dir,
	)
	out := make([]string, len(c.Argv))
	for i, a := range c.Argv {
		out[i] = r.Replace(a)
	}
	return out
}

func (c Command) Start(o Options) (*Run, error) {
	if len(c.Argv) == 0 {
		return nil, fmt.Errorf("no agent command configured")
	}
	argv := c.Expand(o)
	path, err := exec.LookPath(argv[0])
	if err != nil {
		return nil, fmt.Errorf("can't find agent command %q", argv[0])
	}
	return launch(path, argv[1:], o.Dir, false, func(stdout io.Reader, emit func(Event)) bool {
		emit(EvTextStart{})
		streamText(stdout, func(text string) { emit(EvText{Delta: text}) })
		return false
	})
}

// streamText reads r to EOF and emits its text with ANSI escapes stripped.
// A partial UTF-8 rune or an unterminated escape sequence at the end of a
// read is held back until the next read completes it, so neither leaks
// garbage into the text; whatever is still pending at EOF is flushed.
func streamText(r io.Reader, emit func(string)) {
	buf := make([]byte, 4096)
	var pending []byte
	flush := func(b []byte) {
		if text := ansi.Strip(string(b)); text != "" {
			emit(text)
		}
	}
	for {
		n, err := r.Read(buf)
		if n > 0 {
			chunk := append(pending, buf[:n]...)
			cut := validPrefix(chunk)
			cut = escPrefix(chunk[:cut])
			pending = append([]byte(nil), chunk[cut:]...)
			flush(chunk[:cut])
		}
		if err != nil {
			flush(pending)
			return
		}
	}
}

// validPrefix returns the length of b without a trailing partial rune.
func validPrefix(b []byte) int {
	for i := len(b); i > 0 && i > len(b)-utf8.UTFMax; i-- {
		if utf8.RuneStart(b[i-1]) {
			if !utf8.FullRune(b[i-1:]) {
				return i - 1
			}
			break
		}
	}
	return len(b)
}

// maxEsc is the longest escape sequence held back; anything longer is
// passed through so a stray ESC can't stall the stream.
const maxEsc = 1024

// escPrefix returns the length of b without a trailing unterminated escape
// sequence: a bare ESC, an ESC [ (CSI) sequence without its final byte
// (0x40–0x7E), an ESC ] / P / X / ^ / _ string without its BEL or ESC \
// terminator, or an ESC + intermediate (0x20–0x2F) sequence without its
// final byte (0x30–0x7E).
func escPrefix(b []byte) int {
	i := bytes.LastIndexByte(b, 0x1b)
	if i < 0 || len(b)-i > maxEsc {
		return len(b)
	}
	rest := b[i+1:]
	if len(rest) == 0 {
		return i // bare ESC
	}
	complete := func(from int, isFinal func(byte) bool) int {
		for j := from; j < len(rest); j++ {
			if isFinal(rest[j]) {
				return len(b)
			}
		}
		return i
	}
	switch c := rest[0]; {
	case c == '[':
		return complete(1, func(c byte) bool { return c >= 0x40 && c <= 0x7e })
	case c == ']' || c == 'P' || c == 'X' || c == '^' || c == '_':
		// String terminated by BEL or ST (ESC \). An ESC inside the string
		// would be the last ESC in b, so only BEL can show it is complete
		// here; ST is handled by the ESC \ case below.
		return complete(1, func(c byte) bool { return c == 0x07 })
	case c >= 0x20 && c <= 0x2f:
		return complete(1, func(c byte) bool { return c >= 0x30 && c <= 0x7e })
	}
	return len(b) // two-byte sequence such as ESC \ or ESC M
}
