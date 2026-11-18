# worklog

A lightweight terminal todo app with Markdown notes, time tracking, and an AI
agent that can research and edit your notes.

```
╭─ Todos ─────────────────╮╭─ Notes: Fix checkout bug · today 40m · total 1h 20m ─╮
│ Today · 3h 05m          ││  Customers see a double charge when they refresh…    │
│ ▶ ○ Fix checkou… 1h 20m ││                                                      │
│   ○ Write inven… ⏱ 0:42 ││  ## Ideas                                            │
│   ○ Review se… ↩ Nov 16 ││  • Idempotency keys on the payment intent            │
│                         │╰──────────────────────────────────────────────────────╯
│ Yesterday · 6h 30m      │╭─ Agent · Claude ─────────────────────────────────────╮
│   ✓ Deploy API … 2h 10m ││ › add a next-steps section to my notes               │
│                         ││   ↳ Edit 2026-11-17-fix-checkout-bug.md              │
╰─────────────────────────╯╰──────────────────────────────────────────────────────╯
 j/k move · n new · x done · d delete · e edit · t timer · a ask · tab focus · q quit
```

## Install

Requires Go 1.24+ (Go downloads it automatically if you have an older
version) and, for the agent pane, [Claude Code](https://claude.com/claude-code)
or another CLI agent.

```sh
go install github.com/YusufKer/worklog@latest   # installs ~/go/bin/worklog
# or from a clone:  go install .
worklog             # uses ~/worklog/
worklog -demo       # try it on sample data in a temp directory
```

## Layout

- **Todos** (left): grouped by date, newest first. Unfinished todos from
  earlier days are carried over into **Today** under a *Carried over*
  divider (`↩ Nov 14`); their date in the file doesn't change, and `J`/`K`
  only reorder todos within their own day. Each row shows the todo's
  tracked time or its live timer; each date header shows the hours worked
  that day.
- **Notes** (top right, 67%): the selected todo's Markdown notes.
- **Agent** (bottom right, 33%): chat with your agent about the selected todo.
  Each todo has its own conversation, kept in `.chats/` in the data folder so
  it's still there after a restart (`/clear` deletes it).

## Keys

| Where | Key | Action |
|---|---|---|
| everywhere | `tab` / `shift+tab` | switch pane |
| | `ctrl+c` | quit (saves unsaved notes) |
| list | `j`/`k`, `↑`/`↓`, `g`/`G` | move, top, bottom |
| | `J`/`K` | move the todo down/up within its day (new todos start at the top) |
| | `n` | new todo (title + date, default today) |
| | `r` | rename or reschedule (renames the file to match) |
| | `/` | search titles and notes (`enter` keeps the filter, `esc` clears) |
| | `.` | hide/show done todos |
| | `esc` | clear search and show done todos |
| | `x` / `space` | toggle done (also stops its timer) |
| | `d` | delete (moved to `.trash/`) |
| | `u` | trash: restore deleted todos and conflict backups |
| | `enter` / `l` | go to notes |
| | `q` | quit |
| list, notes | `e` | edit notes inline |
| | `E` | open the todo's file in `$VISUAL` or `$EDITOR` (e.g. `vim`, `code --wait`) |
| | `,` | settings |
| | `?` | show all keys |
| | `t` | start/stop the timer (one at a time) |
| | `T` | time entries: add (`a`), edit (`enter`), delete (`d`) |
| | `W` | week report: `h`/`l` change week, `c` copy as Markdown, `s` save CSV |
| | `a` / `i` | ask the agent |
| notes | `j`/`k`, `g`/`G` | scroll |
| | `esc` / `h` | back to list |
| editing | `esc` | save and stop editing |
| | `ctrl+s` | save |
| | `ctrl+f`/`ctrl+b`, `pgup`/`pgdn` | move a screenful down/up (some terminals keep `fn+↑`/`fn+↓` for their own scrollback) |
| | `home`/`end` (`fn+←`/`fn+→` on a Mac) | jump to the top/bottom of the note |
| | `ctrl+a`/`ctrl+e` | start/end of the line |
| agent | `enter` | send |
| | `esc` | stop a running agent, otherwise back to list |
| | `↑`/`↓`, `pgup`/`pgdn` | scroll the conversation |
| | `/clear` | start a new conversation for this todo |

Date fields take `YYYY-MM-DD` or a shortcut: `today`, `tomorrow`,
`yesterday`, `+N` / `-N` days from today.

In the time entries editor, Start takes `HH:MM` or `YYYY-MM-DD HH:MM`; End
also takes a duration like `1h30m`. Leave End empty to keep a running timer
running. A todo's entries can't overlap or be in the future.

The week report shows time per todo and day (Monday–Sunday). `s` saves it
as CSV in decimal hours to `reports/2026-W47.csv` in the data folder; `c`
copies it as a Markdown table.

From the command line, `worklog add` creates a todo without opening the
app and prints its file. `-date` takes the same shortcuts as the app;
`-notes` sets the notes, or reads them from stdin with `-notes -`:

```sh
worklog add Call the bank
worklog add -date tomorrow "Review search PR"
pbpaste | worklog add -notes - Call supplier
```

`worklog report` prints a week as Markdown, or CSV
with `-csv`. `-week` takes `this` (default), `last`, `-N` weeks ago, an ISO
week like `2026-W47`, or any date in the week. `-dir` and `-config` work as
they do for the app:

```sh
worklog report -week last -csv > timesheet.csv
```

If a timer has been running since an earlier day, or longer than
`stale_timer_hours`, worklog asks on startup whether to stop it at a given
time, stop it now, keep it, or discard it.

## Files

Each todo is a Markdown file in the data directory, e.g.
`~/worklog/2026-11-17-fix-checkout-bug.md`:

```markdown
---
id: fix-checkout-bug-3fa9c1
title: Fix checkout bug
date: "2026-11-17"
done: false
created: 2026-11-17T09:12:00+02:00
session: 257a9a5f-…            # agent conversation to resume
order: 2                       # position within its day, set by J/K
time:
    - start: 2026-11-17T09:12:00+02:00
      end: 2026-11-17T10:32:00+02:00
---

# Fix checkout bug
Your notes…
```

You can edit, grep or version these files with anything: worklog checks the
folder every couple of seconds and picks up changes. If notes you're editing
in worklog change on disk, saving keeps your version and puts the other in
`.trash/`. Deleted todos are moved to `.trash/` too; if a todo is deleted
while you're editing its notes, the text goes to `.trash/` as well, so `u`
can bring it back. The inline editor keeps tabs on lines you don't change
and refuses notes over 10,000 lines (use another editor for those). A file
worklog can't parse (e.g. broken frontmatter) is skipped with a warning and
left untouched until you fix it.

## Configuration

Press `,` for the settings screen: pick the agent and model, set the data
folder and stale-timer threshold, or press `ctrl+e` to edit the raw TOML.
Changes are validated, saved, and applied immediately (switching the data
folder loads that folder's todos).

Settings live in `~/.config/worklog/config.toml` (or `-config path`):

```toml
# Where todos live. Overridden by -dir and $WORKLOG_DIR.
dir = "~/worklog"

# Ask about timers running longer than this.
stale_timer_hours = 4

[agent]
# Claude Code (default): streams replies, shows tool calls and cost, and
# remembers each todo's conversation.
type = "claude"
model = "sonnet"        # or "haiku" (cheapest), "opus"; empty = your Claude Code default
# claude_path = "/opt/homebrew/bin/claude"   # default: $WORKLOG_CLAUDE or claude on PATH
```

### The Claude agent

The agent pane runs your installed `claude` CLI headlessly
(`claude -p --output-format stream-json`) in the data directory, so it uses
your Claude Code login (or `ANTHROPIC_API_KEY`), settings, hooks and MCP
servers. Its built-in tools are limited to Read, Edit, Write, Glob, Grep,
WebSearch and WebFetch — no shell. It is told which todo you're on and which
file holds its notes, and edits that file directly; the notes pane refreshes
as it does.

Each message costs roughly $0.20 with Opus and $0.02 with Haiku; the cost of
each run is shown in the pane.

### Bring your own agent

Any CLI agent can drive the pane. Its output is streamed as plain text and
the notes are reloaded when it finishes:

```toml
[agent]
type = "command"
name = "codex"                                  # shown in the pane title
command = ["codex", "exec", "{prompt}"]
```

Placeholders, usable anywhere in an argument:

| Placeholder | Value |
|---|---|
| `{prompt}` | your message, prefixed with context: the todo and its notes file |
| `{message}` | your message only |
| `{notes_file}` | absolute path of the todo's notes file |
| `{title}` | the todo's title |
| `{dir}` | the data directory (also the working directory) |

More examples:

```toml
command = ["gemini", "-p", "{prompt}"]
command = ["aider", "--yes", "--message", "{message}", "{notes_file}"]
command = ["/path/to/my-agent.sh", "{notes_file}", "{message}"]
```

Command agents don't get tool-call lines, cost, or remembered conversations
(each message is a fresh run).

## Development

```sh
go test ./...
go run . -demo
```

Code layout: `internal/store` (files, grouping, time tracking),
`internal/agent` (Claude Code and command backends), `internal/config`,
`internal/ui` (Bubble Tea panes).

## License

MIT
