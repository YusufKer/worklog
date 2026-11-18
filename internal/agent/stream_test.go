package agent

import (
	"bufio"
	"os"
	"strings"
	"testing"
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
	args := strings.Join(Claude{Model: "sonnet"}.Args(Options{Message: "hi", Session: "abc", SystemPrompt: "sys"}), " ")
	for _, want := range []string{"-p hi", "--output-format stream-json", "--resume abc", "--append-system-prompt sys", "--tools " + Tools, "--model sonnet"} {
		if !strings.Contains(args, want) {
			t.Errorf("args missing %q: %s", want, args)
		}
	}

	// Resumed runs name the current notes file in the message; new ones
	// rely on the system prompt.
	resumed := Claude{}.Args(Options{Message: "hi", Session: "abc", NotesFile: "/d/2026-11-19-x.md"})
	if resumed[1] != "[Notes file: /d/2026-11-19-x.md]\n\nhi" {
		t.Errorf("resumed message = %q", resumed[1])
	}
	fresh := Claude{}.Args(Options{Message: "hi", NotesFile: "/d/2026-11-19-x.md"})
	if fresh[1] != "hi" {
		t.Errorf("new message = %q", fresh[1])
	}
}
