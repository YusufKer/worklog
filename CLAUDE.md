# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

worklog is a Go terminal todo app (Bubble Tea) where each todo is a Markdown file with YAML frontmatter, plus time tracking and an agent pane that drives an external CLI agent (Claude Code by default) to read/edit those files. User-facing behaviour (keys, config format, agent placeholders) is documented in README.md — keep it in sync when changing behaviour.

## Commands

```sh
go build ./...                     # build (binary `worklog` at repo root is gitignored)
go test ./...                      # all tests
go test ./internal/store -run TestCarryOver   # single test
go vet ./...
go run . -demo                     # run against a temp dir seeded with sample todos (see seedDemo in main.go)
go run . -dir /tmp/wl -config /tmp/wl.toml    # isolate from the real ~/worklog and ~/.config/worklog
go run . report -week last -csv -dir /tmp/wl  # CLI report (report.go)
```

There are no UI tests; `internal/ui` is verified by running the app. Avoid running against the user's real `~/worklog` data when testing.

## Architecture

`main.go` resolves the data dir (precedence: `-demo` > `-dir` > `$WORKLOG_DIR` > config `dir` > `~/worklog`), loads todos, and starts `ui.Model`. `DirOverridden` tells the settings screen the dir can't be changed in-app.

### internal/store — the files are the database
- `.trash/` holds deleted todos and timestamped conflict backups (`Backup`); `Restore` gives a todo a new ID if its ID is already in use.
- `LoadAll` skips unparseable files and returns them as `[]BadFile`; the UI shows a "skipped" count and never writes to them.
- One `.md` file per todo: `YYYY-MM-DD-<slug>.md`, frontmatter (`Todo` struct, yaml tags) + notes body. `Parse`/`Marshal` round-trip it; files without frontmatter still load.
- `Save` is atomic (temp file + rename). **Mutations go through `Store.Update(t, fn)`**, which re-reads from disk before applying `fn`, because the agent edits the same files concurrently. Don't write a stale in-memory `Todo` back with `Save` directly.
- `Edit` (rename/reschedule) renames the file to `date-slug.md` but keeps the `id`; UI state (agent conversations, editor) is keyed by ID, not path. Delete moves files to `.trash/`.
- `GroupByDate` builds the list's date groups; unfinished todos from earlier days are *displayed* under Today (`Group.Carried`) without changing their `date`.
- `timer.go`: time entries live in frontmatter `time:`; an open entry (no `end`) is the running timer. Only one runs at a time (`StartTimer` stops others). Totals split entries across midnight (`splitDays`). `Stale` drives the startup "forgotten timer" modal. Entries are identified by their `Start` (`PutTimeEntry`/`DeleteTimeEntry`), not by index, because `Update` re-reads the file.

- `report.go`: `BuildWeek` totals time per todo per day (via `ByDay`, so entries crossing midnight split correctly) and renders Markdown/CSV. Reports are saved under `reports/`; `LoadAll` only globs top-level `*.md`, so subfolders are safe.

### internal/agent — pluggable backends
- `Backend` interface (`Name`, `Resumable`, `Start`) returns a `Run` whose `Events` channel emits `Ev*` structs and always ends with exactly one `EvDone`. `launch()` handles process lifecycle, cancellation, and stderr capture for both backends.
- `Claude` runs `claude -p --output-format stream-json --include-partial-messages` in the data dir with a restricted tool list (`Tools`, no shell) and `--resume <session>`. `Parser` turns stream-json lines into events; `testdata/stream.jsonl` is a recorded fixture used by `stream_test.go` — update it if the parser changes to handle new event shapes.
- `Command` runs any CLI with `{prompt}`/`{message}`/`{notes_file}`/`{title}`/`{dir}` placeholder expansion and streams stdout as plain text (not resumable).

### internal/config
TOML at `~/.config/worklog/config.toml`. `Render` writes a commented file (not plain toml encoding); `Validate` is called before saving; `SaveRaw` backs the raw-TOML editor in settings. `ui.BackendFor` maps `config.Agent` to a backend.

### internal/ui — single root model
- `Model` (model.go) owns everything: list, notes, agent panes, focus, and one active `modal` (new todo, delete, stale timer, settings). `Update` routes keys: modal → notes editor → pane by focus. Multi-rune key messages are split into individual keypresses (fast key repeat).
- `listModel` keeps `all` (every todo) and `todos` (visible after search / hide-done). Anything that isn't about display — timers, stale checks, daily totals, finding a todo by ID — must use `all`.
- Agent runs are per-todo: `agentModel.convs[todoID]` holds each conversation and its in-flight `Run`; events arrive as `agentEventMsg` via `waitEvent`, which must be re-armed after each event until `EvDone`. Edit tool results and `EvDone` trigger `reloadKeepSelection()` so the notes pane reflects agent edits.
- Before sending a prompt, a dirty inline editor buffer is saved and editing stopped so the agent sees current notes and isn't overwritten. On `EvDone`, `finishRun` persists the Claude session ID to the todo's `session` frontmatter (cleared on `/clear` or "no conversation found").
- `systemPrompt()` in ui/agent.go is the context given to the agent (todo, notes path, "don't touch frontmatter").
- `applySettings` validates a new config (including loading a new data dir) before saving, then swaps backend/store live.
- `watch.go` polls the data dir every 2s (size + mtime of top-level `*.md`) and reloads on any change, including the app's own writes; a baseline is taken in `New` and on data-dir switches. `saveNotes` backs up the on-disk version to `.trash/` if it changed since editing started (`notesModel.base`).
- A 1s `timerTick` loop runs only while a timer is running (`ensureTick`).
