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
go run .                        # needs one network: BB_SERVER_URL + BB_PASSWORD, a Slack token, or a Telegram login
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
| `main.go` | Config load, API ping, terminal-background resolution, backend wiring, Bubble Tea startup |
| `emojiset/` | The one shortcode table, generated from the dataset Slack uses; both the composer and the Slack decoder read it |
| `provider/` | The seam between the UI and the chat backends. `provider/imessage/` adapts `api/` + `ws/` to it; `provider/slack/` and `provider/telegram/` are the Slack and Telegram backends |
| `commands.go` | The `telegram login/logout/accounts` subcommands, which run instead of the TUI |
| `termqr/` | QR codes as half-block text, for logins that a phone scans |
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

**Pair `Width` with `MaxWidth`, and `Height` with `MaxHeight`, on anything that
becomes a pane.** The `Width`/`Height` setters only pad; they do not clip.
`lipgloss.JoinHorizontal` sizes a block to its widest line, so one over-wide
line widens the whole pane and drags the divider — and every pane right of it —
sideways. The vertical version is worse: with eight panes on a short terminal,
unclipped panes rendered an 87-row frame into a 24-row window, which scrolls the
alt screen and eats the top rows. `TestEightPanesStayInsideTheTerminal` pins
both, including the empty-pane placeholder, which is the block that overflows
first.

**Re-arm a style after an embedded ANSI reset.** A pre-styled fragment inside a
line (the muted timestamp, a `[Spotify]` link badge) ends with `\x1b[0m`, which
drops the remainder of that line back to the terminal default. Wrap line content
in `reopenAfterResets` (in `tui/messages.go`) before rendering it with an
enclosing style. Symptom when you forget: the color is correct on wrapped
continuation lines, which carry no prefix, and wrong on the first line.

**Sender names answer per chat, not per app.** By default they show only in
group chats: a chat whose participants say it is a group (iMessage), or a pane
that has seen more than one distinct incoming sender (Slack channels, Telegram
groups, whose chats carry no participants). `alt+m` turns them on in one-to-one
chats too. `ctrl+b` pins the focused chat either way; the choice is kept per
chat GUID in `chat_overrides.json` (`WindowManager.senderNameChoices`), applied
by `WindowManager.ShowChat` whenever the chat is put in a pane, and reaches
every pane showing that chat. A toggle that lands back on what the chat would
show anyway deletes the choice rather than storing it, so the saved choices are
exactly the exceptions and everything else keeps following `alt+m`. Put a chat
in a pane with `ShowChat`, not `ChatWindow.SetChat`, or its choice is skipped.

**Sender colours are derived, not assigned.** A name's colour is an FNV hash of
the lowercased name into `NickPalette`, so the same person keeps the same colour
across panes, restarts and arrival orders — anything driven by map iteration or
first-seen order would make the colour mean nothing. Colouring only starts once
a pane has seen two distinct senders, and the message that crosses that line
forces a full re-render, because the incremental append path never redraws the
names already on screen.

**The palette never branches on background *detection*.** The default values in
`tui/styles.go` are chosen to stay readable on white and black alike, and
incoming message text deliberately carries no foreground so it inherits the
terminal's own. `lipgloss.HasDarkBackground()` queries the terminal over stdin
and is wrong often enough to be unusable — multiplexers answer for the terminal
and some emulators never reply. It is resolved once in `main.go` before Bubble
Tea claims stdin.

An *explicit* `BB_THEME=dark`/`light` is different: it is a fact, not a guess,
so `tui.ApplyTheme` swaps in values tuned for that background (brighter accent,
muted and divider on dark). Auto stays on the compromise palette. A new style
that bakes a `Color*` value in at construction time must be added to
`rebuildStyles`, or a pinned theme will not reach it — `TestDarkThemeBrightensWholeView`
catches that.

**Tabs park the layout they are not showing.** `WindowManager`'s `root`,
`windows`, `nextID` and `focusedWindow` are always the *active* tab's; a
background tab's sit in its `paneTab` (`tui/tabs.go`) until it comes back.
So `AllWindows`, `WindowAt`, `SplitWindow` and the rest stay single-tab and
need no tab argument, while anything that answers per chat — new messages,
renames, display toggles — must use `WindowsShowingChat` /
`WindowsInAllTabs`, which span every tab. The periodic refresh deliberately
polls only the active tab (Tier 3 / FLOOD_WAIT); `afterTabChange` catches a
tab up when it returns. Tab keys are alt keys, never `alt+[`: that is the
head of a torn escape sequence and `swallowEscapeSequenceTail` eats it. The
bar exists only with two or more tabs, and takes its row from the panes, so
mouse rows in the pane area are offset by `tabBarRows()`.

**Never write to stdout.** The TUI owns it. `main.go` redirects `log` to
`~/.imessage-tui.log`; use `log.Printf` for debugging, never `fmt.Println`.

## Backends

The TUI knows conversations by GUID and nothing else. Which backend owns a GUID
is **derived from its prefix** (`provider.Registry`), never stored on the chat,
so the two cannot drift and cached chats need no migration. iMessage GUIDs
arrive from BlueBubbles unprefixed and are the default route. Nothing outside
`provider/` may parse a GUID.

`provider.Provider` holds only what every backend can do. Anything a backend
might not do — renaming a chat, resolving a link preview, handing back
attachment bytes — is a separate interface it either implements or doesn't, and
callers type-assert. Adding a method to the core interface to then return
"unsupported" is the thing this design exists to avoid.

Realtime updates cross the seam as `provider.Event`, already normalized.
A backend's wire format stops inside its own package. One backend, one
`provider.Stream`; `provider.Merge` fans them into the single feed the UI reads.

Slack lives in `provider/slack/` — one `Provider` per workspace, registered
under `sl:<workspace>:`. Two things there are load-bearing and easy to undo by
accident: names come from one `users.list` rather than a `users.info` per
sender, and threads are cached by reply count so the refresh timer does not
re-expand them. Both exist because `conversations.history` and
`conversations.replies` are Tier 3, about fifty calls a minute. For the same
reason the stream publishes a reply under its thread's own GUID only for
threads opened with `/t` (`Provider.watched`), never for every thread. A
message's body is assembled in `content.go` from its text, Block Kit blocks and legacy
attachments, as mrkdwn, and `TextToPlain` runs over each part before the
attachments' `Accents` offsets are taken — it changes lengths. Every typed
message also carries a `rich_text` block saying what its text says, so the
text wins unless an app sent a layout; drop that rule and every message shows
twice. `SLACK.md` has the rest.

Telegram lives in `provider/telegram/` — one `Provider` per logged-in account,
registered under `tg:<user id>:`, on `gotd/td`. Three things there are easy to
break. gotd's `Client.Run` *is* the connection, so it runs for the provider's
lifetime in its own goroutine and every call goes through it while it runs.
Users and channels need an `access_hash` that only arrives as a side effect of
other responses: everything carrying users or chats goes through the peer
table's `apply`, and a "min" object's hash must never replace a real one.
`Messages` serves a per-chat history cache only while the realtime feed is
up, and any update for a chat drops its entry — the UI's refresh timer would
otherwise turn into a FLOOD_WAIT. Read optional TL fields directly
(`msg.Media != nil`), not through the generated `Get…` accessors: those check
flag bits that only decoding sets, so they lie about objects built in tests.
`TELEGRAM.md` has the rest; `SIGNAL.md` is the plan for Signal.

Logins run as subcommands (`commands.go`) before Bubble Tea starts, which is
the one place the program may print to stdout.

**One emoji table, both directions.** `emojiset` merges the generated Slack set
with `enescakir/emoji`, and both the composer's autocomplete and the Slack
mrkdwn decoder read it. Two tables would mean receiving glyphs you cannot type,
or typing names that render as `:like_this:` at the other end. Regenerate with
`scripts/generate-emoji-map.py`; never edit `slack_generated.go` by hand.

## Terminal input

Bubble Tea's input reader assumes a read shorter than its buffer ends on an
event boundary (`readAnsiInputs`: `canHaveMoreData := numBytes == len(buf)`). A
terminal does not promise that. When the app is busy the kernel can hand over
half a sequence, and the remainder is then parsed as ordinary runes and typed
into whichever composer has focus — `[1;6C` from a modified arrow key,
`<35;15;33M` from a mouse report. `swallowEscapeSequenceTail` in `tui/app.go`
drops those leftovers.

`InputModel.Update` also refuses one key outright: bubbles v1.0.0
`textarea.wordLeft` loops `characterLeft` until it reaches a non-space
character, but `characterLeft` stops at column 0 — so alt+b or alt+left with
nothing but spaces to the left spins forever and locks the app with a pegged
core. `wordBackwardWouldHang` checks for a word to move to first. Remove the
guard only alongside a bubbles version that fixes the loop.

Which is why the program runs in **cell** mouse motion, not all motion: nothing
reacts to hover, and all-motion emits a sequence per movement, flooding the
reader that tears them. If a feature ever needs hover, weigh it against that.

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
`lineLinks`, `lineLinkSpans`) that mouse clicks and `/img #N` resolve against,
through `appendLineIndex`, so a new render path must keep one entry per
*visual* line, including blank and separator lines.

A URL in message text opens only where it is drawn: `lineLinkSpans` holds its
columns on each visual line, found after wrapping (`wrappedLinkSpans` in
`tui/links.go`), so a long URL that breaks across rows is clickable on each
piece. A preview line instead opens its link from anywhere on the row
(`lineLinks`). Anything that changes what precedes the text on a line —
padding, a marker, a prefix — moves those columns; `TestClickOpensTheURLUnderThePointerOnly`
clicks every cell of a rendered pane and catches it. Only http(s) links are
ever handed to the system opener (`isOpenableLink`, checked again in
`openURLCmd`): message text is written by other people.

A message with `Format: models.FormatMrkdwn` keeps its marks in `Text`, which
is what echo matching and the dedupe compare; `tui/mrkdwn.go` takes them out
at render time and styles word by word, because each wrapped row is drawn on
its own and a style spanning a break would be lost on the next row. Anywhere
text is shown without styles reads `displayText(msg)`. `Accents` split the
text into segments drawn behind a coloured bar, which takes columns like any
other prefix.

## Tests

`tui/main_test.go` points `HOME` at a temporary directory for the whole
package: ordinary key presses and clicks save layout, UI state and chat
overrides under `~/.config/imessage-tui`, and before it a test run wrote over
the real files of whoever ran it. A test that reads back what it saved, or must
start from a first run, sets its own `t.Setenv("HOME", t.TempDir())`.

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
