package agent

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Tools the Claude agent may use. Bash is deliberately excluded: the agent
// works on notes and the web, not the machine.
const Tools = "Read,Edit,Write,Glob,Grep,WebSearch,WebFetch"

// Claude runs Claude Code headlessly (claude -p) and parses its stream-json
// output into rich events: streamed text, tool calls, cost and sessions.
type Claude struct {
	Bin   string // path to claude; defaults to $WORKLOG_CLAUDE or "claude"
	Model string // optional model alias or ID, e.g. "sonnet"
}

func (c Claude) Name() string    { return "Claude" }
func (c Claude) Resumable() bool { return true }

// Args builds the claude command-line arguments.
func (c Claude) Args(o Options) []string {
	msg := o.Message
	if o.Session != "" && o.NotesFile != "" {
		// A resumed session remembers the notes path from earlier turns, but
		// the file is renamed when the todo is renamed or rescheduled. Put the
		// current path in the newest turn so it wins over the history.
		msg = "[Notes file: " + o.NotesFile + "]\n\n" + msg
	}
	args := []string{
		"-p", msg,
		"--output-format", "stream-json",
		"--verbose",
		"--include-partial-messages",
		"--permission-mode", "acceptEdits",
		"--tools", Tools,
		"--allowedTools", Tools,
	}
	if c.Model != "" {
		args = append(args, "--model", c.Model)
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
func (c Claude) Start(o Options) (*Run, error) {
	bin := c.Bin
	if bin == "" {
		bin = os.Getenv("WORKLOG_CLAUDE")
	}
	if bin == "" {
		bin = "claude"
	}
	path, err := exec.LookPath(bin)
	if err != nil {
		return nil, fmt.Errorf("can't find the claude CLI (%s): install Claude Code or set agent.claude_path", bin)
	}
	return launch(path, c.Args(o), o.Dir, true, func(stdout io.Reader, emit func(Event)) bool {
		p := &Parser{}
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 64*1024), 32*1024*1024)
		for sc.Scan() {
			for _, ev := range p.Parse(sc.Bytes()) {
				emit(ev)
			}
		}
		return p.Done
	})
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
