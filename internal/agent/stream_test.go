package agent

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// testdata/stream.jsonl is a real (trimmed) capture of
// `claude -p ... --output-format stream-json --verbose --include-partial-messages`
// where the agent was asked to append a line to note.md.
func TestParseFixture(t *testing.T) {
	f, err := os.Open("testdata/stream.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	p := &Parser{}
	var events []Event
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		events = append(events, p.Parse(sc.Bytes())...)
	}

	var (
		session string
		text    strings.Builder
		tools   []string
		edited  bool
		denied  int
		done    *EvDone
	)
	for _, ev := range events {
		switch e := ev.(type) {
		case EvSession:
			session = e.ID
		case EvText:
			text.WriteString(e.Delta)
		case EvTool:
			tools = append(tools, e.Name+" "+e.Detail)
		case EvToolResult:
			if e.IsError {
				denied++
			} else if IsEdit(e.Name) {
				edited = true
			}
		case EvDone:
			done = &e
		}
	}

	if session != "00000000-0000-4000-8000-000000000001" {
		t.Errorf("session = %q", session)
	}
	if got := text.String(); got != "I added \"- hello from agent\" to the end of `note.md`." {
		t.Errorf("text = %q", got)
	}
	want := []string{"Read note.md", "Edit note.md"}
	if len(tools) < 2 || tools[len(tools)-2] != want[0] || tools[len(tools)-1] != want[1] {
		t.Errorf("tools = %v", tools)
	}
	if !edited {
		t.Error("expected a successful Edit result")
	}
	if denied != 2 {
		t.Errorf("denied = %d, want 2", denied)
	}
	if done == nil || done.IsError || done.CostUSD <= 0 {
		t.Errorf("done = %+v", done)
	}
	if !p.Done {
		t.Error("parser should be Done after result")
	}
}

func TestParseErrorResult(t *testing.T) {
	p := &Parser{}
	evs := p.Parse([]byte(`{"type":"result","subtype":"error_during_execution","is_error":true,"errors":["No conversation found"]}`))
	if len(evs) != 1 {
		t.Fatalf("got %d events", len(evs))
	}
	d := evs[0].(EvDone)
	if !d.IsError || d.Message != "No conversation found" {
		t.Errorf("done = %+v", d)
	}
}

func TestArgs(t *testing.T) {
	argv := Claude{Model: "sonnet"}.Args(Options{Message: "hi", Session: "abc", SystemPrompt: "sys"})
	args := strings.Join(argv, " ")
	for _, want := range []string{"-p --", "--output-format stream-json", "--resume abc", "--append-system-prompt sys", "--tools " + Tools, "--model sonnet"} {
		if !strings.Contains(args, want) {
			t.Errorf("args missing %q: %s", want, args)
		}
	}
	// -p is a boolean flag; the prompt is positional and must come after
	// "--" so a message like "- buy milk" or "--help" isn't read as an option.
	last := func(argv []string) string {
		if len(argv) < 2 || argv[len(argv)-2] != "--" {
			t.Fatalf("message should follow --: %q", argv)
		}
		return argv[len(argv)-1]
	}
	if got := last(Claude{}.Args(Options{Message: "--help"})); got != "--help" {
		t.Errorf("message = %q", got)
	}

	// Resumed runs name the current notes file in the message; new ones
	// rely on the system prompt.
	resumed := Claude{}.Args(Options{Message: "hi", Session: "abc", NotesFile: "/d/2026-11-19-x.md"})
	if got := last(resumed); got != "[Notes file: /d/2026-11-19-x.md]\n\nhi" {
		t.Errorf("resumed message = %q", got)
	}
	fresh := Claude{}.Args(Options{Message: "hi", NotesFile: "/d/2026-11-19-x.md"})
	if got := last(fresh); got != "hi" {
		t.Errorf("new message = %q", got)
	}
}

// fakeClaude writes a stand-in claude script and returns a backend using it.
func fakeClaude(t *testing.T, script string) Claude {
	t.Helper()
	p := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	return Claude{Bin: p}
}

func TestClaudeFakeRun(t *testing.T) {
	// The prompt is the last argument; echo it back in the result line.
	c := fakeClaude(t, `for a; do p="$a"; done
printf '{"type":"system","subtype":"init","session_id":"s1"}\n'
printf '{"type":"result","subtype":"success","result":"%s","total_cost_usd":0.01}\n' "$p"
`)
	run, err := c.Start(Options{Dir: t.TempDir(), Message: "- buy milk"})
	if err != nil {
		t.Fatal(err)
	}
	var session string
	var done EvDone
	for ev := range run.Events {
		switch e := ev.(type) {
		case EvSession:
			session = e.ID
		case EvDone:
			done = e
		}
	}
	if session != "s1" || done.IsError || done.CostUSD != 0.01 {
		t.Errorf("session = %q, done = %+v", session, done)
	}
}

// A line longer than the scanner allows used to stop reading stdout, which
// left the child blocked on a full pipe and the run hanging forever.
func TestClaudeLongLineDoesNotHang(t *testing.T) {
	c := fakeClaude(t, `printf '{"type":"system","subtype":"init","session_id":"s1"}\n'
head -c `+strconv.Itoa(maxLine+1024*1024)+` /dev/zero | tr '\0' x
printf '\n{"type":"result","subtype":"success","result":"ok"}\n'
`)
	run, err := c.Start(Options{Dir: t.TempDir(), Message: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	type outcome struct {
		done  EvDone
		count int
	}
	res := make(chan outcome, 1)
	go func() {
		var o outcome
		for ev := range run.Events {
			if d, ok := ev.(EvDone); ok {
				o.done = d
				o.count++
			}
		}
		res <- o
	}()
	select {
	case o := <-res:
		if o.count != 1 {
			t.Errorf("got %d EvDone, want 1", o.count)
		}
		if !o.done.IsError || !strings.Contains(o.done.Message, "too long") {
			t.Errorf("done = %+v, want a 'line too long' error", o.done)
		}
	case <-time.After(20 * time.Second):
		run.Cancel()
		t.Fatal("run hung after an over-long output line")
	}
}
