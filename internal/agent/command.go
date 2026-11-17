package agent

import (
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
		buf := make([]byte, 4096)
		var pending []byte // incomplete UTF-8 sequence carried between reads
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				chunk := append(pending, buf[:n]...)
				cut := validPrefix(chunk)
				pending = append([]byte(nil), chunk[cut:]...)
				if text := ansi.Strip(string(chunk[:cut])); text != "" {
					emit(EvText{Delta: text})
				}
			}
			if err != nil {
				return false
			}
		}
	})
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
