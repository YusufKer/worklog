// Package agent runs Claude Code headlessly (claude -p) and turns its
// stream-json output into simple events for the UI.
package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Tools the agent may use. Bash is deliberately excluded: the agent works on
// notes and the web, not the machine.
const Tools = "Read,Edit,Write,Glob,Grep,WebSearch,WebFetch"

// Event is one of the Ev* types below.
type Event any

type (
	// EvSession reports the Claude session ID, used to resume later.
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
		Message string // error message, if any
		CostUSD float64
		Elapsed time.Duration
	}
)

// Options configure a single run.
type Options struct {
	Bin          string // path to claude; defaults to $WORKLOG_CLAUDE or "claude"
	Dir          string // working directory (the notes directory)
	Prompt       string
	SystemPrompt string // appended to Claude Code's system prompt
	Session      string // resume this session if set
}

// Run is an in-flight agent invocation.
type Run struct {
	Events <-chan Event
	cancel context.CancelFunc
}

// Cancel stops the run; an EvDone is still delivered.
func (r *Run) Cancel() { r.cancel() }

// Args builds the claude command-line arguments.
func Args(o Options) []string {
	args := []string{
		"-p", o.Prompt,
		"--output-format", "stream-json",
		"--verbose",
		"--include-partial-messages",
		"--permission-mode", "acceptEdits",
		"--tools", Tools,
		"--allowedTools", Tools,
	}
	if o.SystemPrompt != "" {
		args = append(args, "--append-system-prompt", o.SystemPrompt)
	}
	if o.Session != "" {
		args = append(args, "--resume", o.Session)
	}
	return args
}

// Start launches claude and streams its events until it exits.
func Start(o Options) (*Run, error) {
	bin := o.Bin
	if bin == "" {
		bin = os.Getenv("WORKLOG_CLAUDE")
	}
	if bin == "" {
		bin = "claude"
	}
	path, err := exec.LookPath(bin)
	if err != nil {
		return nil, fmt.Errorf("can't find the claude CLI (%s): install Claude Code or set WORKLOG_CLAUDE", bin)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, path, Args(o)...)
	cmd.Dir = o.Dir
	cmd.Stdin = nil
	cmd.WaitDelay = 2 * time.Second
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
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
	go func() {
		defer close(events)
		p := &Parser{}
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 64*1024), 32*1024*1024)
		for sc.Scan() {
			for _, ev := range p.Parse(sc.Bytes()) {
				if d, ok := ev.(EvDone); ok {
					d.Elapsed = time.Since(start)
					ev = d
				}
				events <- ev
			}
		}
		waitErr := cmd.Wait()
		if p.Done {
			return
		}
		done := EvDone{IsError: true, Elapsed: time.Since(start)}
		switch {
		case ctx.Err() != nil:
			done.Message = "Stopped."
		case waitErr != nil:
			done.Message = strings.TrimSpace(stderr.String())
			if done.Message == "" {
				done.Message = waitErr.Error()
			}
		default:
			done.Message = "claude exited without a result"
		}
		events <- done
	}()

	return &Run{Events: events, cancel: cancel}, nil
}

// Parser converts stream-json lines into events. It tracks tool names by ID
// so results can be attributed.
type Parser struct {
	Done  bool
	tools map[string]string
}

type rawEvent struct {
	Type      string          `json:"type"`
	Subtype   string          `json:"subtype"`
	SessionID string          `json:"session_id"`
	Event     json.RawMessage `json:"event"`
	Message   struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
	// result
	IsError bool     `json:"is_error"`
	Result  string   `json:"result"`
	Cost    float64  `json:"total_cost_usd"`
	Errors  []string `json:"errors"`
}

type streamEvent struct {
	Type         string `json:"type"`
	ContentBlock struct {
		Type string `json:"type"`
	} `json:"content_block"`
	Delta struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"delta"`
}

type contentBlock struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	IsError   bool            `json:"is_error"`
	Content   json.RawMessage `json:"content"`
}

// Parse handles one line of output. Unknown or malformed lines are ignored.
func (p *Parser) Parse(line []byte) []Event {
	if p.tools == nil {
		p.tools = map[string]string{}
	}

	var e rawEvent
	if err := json.Unmarshal(line, &e); err != nil {
		return nil
	}
	switch e.Type {
	case "system":
		if e.Subtype == "init" && e.SessionID != "" {
			return []Event{EvSession{ID: e.SessionID}}
		}

	case "stream_event":
		var se streamEvent
		if json.Unmarshal(e.Event, &se) != nil {
			return nil
		}
		switch {
		case se.Type == "content_block_start" && se.ContentBlock.Type == "text":
			return []Event{EvTextStart{}}
		case se.Type == "content_block_delta" && se.Delta.Type == "text_delta" && se.Delta.Text != "":
			return []Event{EvText{Delta: se.Delta.Text}}
		}

	case "assistant":
		var out []Event
		for _, b := range blocks(e.Message.Content) {
			if b.Type == "tool_use" {
				p.tools[b.ID] = b.Name
				out = append(out, EvTool{ID: b.ID, Name: b.Name, Detail: toolDetail(b.Name, b.Input)})
			}
		}
		return out

	case "user":
		var out []Event
		for _, b := range blocks(e.Message.Content) {
			if b.Type == "tool_result" {
				r := EvToolResult{ID: b.ToolUseID, Name: p.tools[b.ToolUseID], IsError: b.IsError}
				if b.IsError {
					r.Message = textOf(b.Content)
				}
				out = append(out, r)
			}
		}
		return out

	case "result":
		p.Done = true
		d := EvDone{IsError: e.IsError || e.Subtype != "success", CostUSD: e.Cost}
		if d.IsError {
			d.Message = e.Result
			if d.Message == "" && len(e.Errors) > 0 {
				d.Message = strings.Join(e.Errors, "; ")
			}
			if d.Message == "" {
				d.Message = e.Subtype
			}
		}
		return []Event{d}
	}
	return nil
}

func blocks(raw json.RawMessage) []contentBlock {
	var bs []contentBlock
	if json.Unmarshal(raw, &bs) != nil {
		return nil
	}
	return bs
}

// textOf extracts text from a tool_result content, which is either a string
// or a list of {type:text,text} blocks.
func textOf(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) == nil {
		var texts []string
		for _, p := range parts {
			texts = append(texts, p.Text)
		}
		return strings.Join(texts, "\n")
	}
	return ""
}

// toolDetail summarises a tool call's input for display.
func toolDetail(name string, input json.RawMessage) string {
	var in map[string]any
	if json.Unmarshal(input, &in) != nil {
		return ""
	}
	str := func(k string) string { s, _ := in[k].(string); return s }
	switch name {
	case "Read", "Edit", "Write", "MultiEdit":
		return filepath.Base(str("file_path"))
	case "WebSearch":
		return str("query")
	case "WebFetch":
		return str("url")
	case "Glob", "Grep":
		return str("pattern")
	}
	for _, k := range []string{"description", "query", "path", "url"} {
		if s := str(k); s != "" {
			return s
		}
	}
	return ""
}

// IsEdit reports whether a tool modifies files.
func IsEdit(name string) bool {
	switch name {
	case "Edit", "Write", "MultiEdit", "NotebookEdit":
		return true
	}
	return false
}
