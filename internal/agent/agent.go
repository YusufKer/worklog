// Package agent runs the AI agent behind the agent pane and turns its output
// into simple events for the UI. Claude Code is the full-featured default;
// Command runs any other CLI agent with plain-text output.
package agent

import (
	"context"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Backend starts agent runs.
type Backend interface {
	// Name labels the agent in the UI.
	Name() string
	// Resumable reports whether runs report sessions that can be resumed.
	Resumable() bool
	Start(o Options) (*Run, error)
}

// Event is one of the Ev* types below.
type Event any

type (
	// EvSession reports a session ID, used to resume later.
	EvSession struct{ ID string }
	// EvTextStart marks the start of a new block of assistant text.
	EvTextStart struct{}
	// EvText is a streamed chunk of assistant text.
	EvText struct{ Delta string }
	// EvTool reports a tool call, e.g. Name "Edit", Detail "notes.md".
	EvTool struct{ ID, Name, Detail string }
	// EvToolResult reports a tool call finishing.
	EvToolResult struct {
		ID, Name string
		IsError  bool
		Message  string // set for errors
	}
	// EvDone is always the last event of a run.
	EvDone struct {
		IsError bool
		Message string  // error message, if any
		CostUSD float64 // 0 when unknown
		Elapsed time.Duration
	}
)

// Options configure a single run.
type Options struct {
	Dir          string // working directory (the notes directory)
	Message      string // what the user typed
	SystemPrompt string // context: which todo and notes file
	NotesFile    string
	Title        string
	Session      string // resume this session if set (resumable backends only)
}

// Run is an in-flight agent invocation.
type Run struct {
	Events <-chan Event
	cancel context.CancelFunc
}

// Cancel stops the run; an EvDone is still delivered.
func (r *Run) Cancel() { r.cancel() }

// launch starts the process and calls read with its stdout. read returns
// true if it already emitted an EvDone; otherwise one is synthesised from
// the exit status. If requireDone is set, a clean exit without EvDone is an
// error.
func launch(path string, args []string, dir string, requireDone bool,
	read func(stdout io.Reader, emit func(Event)) bool) (*Run, error) {

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = dir
	cmd.WaitDelay = 2 * time.Second
	stderr := &tailBuffer{max: 4096}
	cmd.Stderr = stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}

	events := make(chan Event, 64)
	start := time.Now()
	emit := func(ev Event) {
		if d, ok := ev.(EvDone); ok {
			d.Elapsed = time.Since(start)
			ev = d
		}
		events <- ev
	}
	go func() {
		defer close(events)
		done := read(stdout, emit)
		waitErr := cmd.Wait()
		if done {
			return
		}
		d := EvDone{}
		switch {
		case ctx.Err() != nil:
			d.IsError, d.Message = true, "Stopped."
		case waitErr != nil:
			d.IsError = true
			d.Message = strings.TrimSpace(stderr.String())
			if d.Message == "" {
				d.Message = waitErr.Error()
			}
		case requireDone:
			d.IsError, d.Message = true, "the agent exited without a result"
		}
		emit(d)
	}()
	return &Run{Events: events, cancel: cancel}, nil
}

// tailBuffer keeps the last max bytes written to it.
type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
	max int
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if over := len(t.buf) - t.max; over > 0 {
		t.buf = t.buf[over:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}
