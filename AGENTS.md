# AGENTS.md

Keyboard-first iMessage client for the terminal, in Go, on top of Bubble Tea /
Bubbles / Lipgloss. It talks to a BlueBubbles-compatible server over HTTP plus a
socket.io/websocket feed.

This file covers what is easy to get wrong here. The README covers features and
configuration; don't duplicate it.

## Commands

```bash
go build ./...                  # compile everything
go test ./...                   # full suite, ~1s
go test ./tui -run TestName -v  # one test
gofmt -l .                      # must print nothing
go vet ./...
golangci-lint run ./...         # advisory; the tree is not clean yet
go run .                        # needs BB_SERVER_URL + BB_PASSWORD
```

`gofmt`, `go vet` and `go test ./...` must all pass before a change is done.
`golangci-lint` currently reports pre-existing findings — don't treat its output
as a regression unless your change added to it.

Docs: the root `*.md` files are the source of truth. `scripts/docs.sh` stages
them into `docs/` for MkDocs, and `docs/*.md` is gitignored — never edit files
there, edit the root ones.

## Layout

| Path | What lives there |
|---|---|
| `main.go` | Config load, API ping, terminal-background resolution, Bubble Tea startup |
| `api/` | BlueBubbles HTTP client, link-preview fetching |
| `ws/` | Socket.io/websocket client for live events |
| `models/` | Wire types: `Chat`, `Message`, `Attachment`, `Handle` |
| `config/` | YAML + env config, keyring credentials, UI/layout state, message cache |
| `tui/` | Everything on screen. `app.go` is the root model; `windowmanager.go` splits panes; `messages.go`, `simplelist.go`, `input.go` are the three main views; `styles.go` is the palette |

## Rendering rules

These are the ones that have actually caused bugs.

**Measure width in columns, never in runes.** An emoji is one rune but two
columns; `👍🏽` and `👨‍👩‍👧‍👦` are 2–7 runes drawn as one two-column glyph. Use
`lipgloss.Width` to measure and `truncateToWidth` (in `tui/timefmt.go`) to cut —
it cuts on grapheme boundaries via `ansi.Truncate`. Slicing `[]rune` both
overshoots the column budget and can split a cluster, leaving a stray modifier
that shifts the rest of the line.

**Pair `Width` with `MaxWidth` on anything that becomes a pane.** `Width` only
pads short lines; it does not clip long ones. `lipgloss.JoinHorizontal` sizes a
block to its widest line, so a single over-wide line widens the whole pane and
drags the divider column — and every pane right of it — sideways.

**Re-arm a style after an embedded ANSI reset.** A pre-styled fragment inside a
line (the muted timestamp, a `[Spotify]` link badge) ends with `\x1b[0m`, which
drops the remainder of that line back to the terminal default. Wrap line content
in `reopenAfterResets` (in `tui/messages.go`) before rendering it with an
enclosing style. Symptom when you forget: the color is correct on wrapped
continuation lines, which carry no prefix, and wrong on the first line.

**The palette does not branch on terminal background.** Every value in
`tui/styles.go` is chosen to stay readable on white and black alike, and
incoming message text deliberately carries no foreground so it inherits the
terminal's own. `lipgloss.HasDarkBackground()` queries the terminal over stdin
and is wrong often enough to be unusable — multiplexers answer for the terminal
and some emulators never reply. It is resolved once in `main.go` before Bubble
Tea claims stdin, and `BB_THEME` can pin it. Do not add background-conditional
colors.

**Never write to stdout.** The TUI owns it. `main.go` redirects `log` to
`~/.imessage-tui.log`; use `log.Printf` for debugging, never `fmt.Println`.

## Messages

Messages arrive from three places — the REST backfill, the websocket feed, and
optimistic local echo — so the same message shows up more than once. Dedupe in
`tui/message_dedupe.go` matches on GUID first, then on a fingerprint of
timestamp, text, handle, tapback association and attachment keys. If you add a
field that distinguishes two otherwise identical messages, it belongs in
`messageFingerprint`.

Outgoing messages render optimistically with `Pending` set and reconcile when
the server confirms or the timeout fires. Rendering is incremental —
`renderMessageBlock` appends to the line-index slices (`lineMessages`,
`lineLinks`) that mouse clicks and `/img #N` resolve against, so a new render
path must keep one entry per *visual* line, including blank and separator lines.

## Tests

Table-driven where there is more than one case. Renderer tests must force a
color profile, since `go test` has no TTY and Lipgloss otherwise strips all
color:

```go
old := lipgloss.ColorProfile()
lipgloss.SetColorProfile(termenv.ANSI256)
t.Cleanup(func() { lipgloss.SetColorProfile(old) })
```

Assert on `stripANSI(view)` for text content, and on the escape sequences
themselves only when color *is* the thing under test. `stripANSI` lives in
`tui/message_dedupe_test.go`.

When you fix a rendering bug, verify the new test fails against the old
behaviour before you call it done — a width or color test that passes both ways
is asserting nothing.

## Security

Credentials come from the OS keyring first (`config/credentials.go`), falling
back to the config file. Never log the password or put it in an error string.
`BB_INSECURE_TLS` disables certificate verification and exists only for
self-signed dev servers; it must stay opt-in and env-only.

## Housekeeping

The built binary `/imessage-tui` is gitignored — don't commit it. Commits are
lowercase, imperative, and explain *why* in the body when the reason isn't
obvious from the diff.
