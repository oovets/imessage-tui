# Slack in imessage-tui

Slack conversations in the same chat list as iMessage, in the terminal. Built
against the working Slack implementation in the desktop app (`~/Code/imessage`),
which is where most of the design decisions below come from — they were already
paid for there.

## What the desktop app does

| Layer | Where | What |
|---|---|---|
| Client | `crates/slack-core/` (1 593 lines of Rust) | Web API + Socket Mode, updates on a `tokio` broadcast channel. Its doc comment says it was *extracted from the `slack_rust` terminal client* — this code started life in a TUI |
| Host | `src-tauri/src/slack.rs` | One client per workspace, tokens in the keychain, `sl_*` commands, one event stream to the webview |
| Frontend | `src/lib/source.ts`, `src/lib/accounts.ts`, `src/slack/adapters.ts` | Unified inbox: a chat's backend is **derived from its GUID prefix**, never stored |

Endpoints in use: `users.conversations`, `conversations.history` /
`.replies` / `.members` / `.leave`, `chat.postMessage`, `reactions.add`,
`users.info`, `bots.info`, `auth.test`, `apps.connections.open`.

Auth is two tokens per workspace: `xoxp-` (Web API, acting as the user) and
`xapp-` (app-level, opens Socket Mode).

## The one idea worth copying verbatim

From `src/lib/source.ts`:

> The source is *derived* from the GUID rather than stored as a field on Chat so
> the two can never drift, and so chats restored from the persisted cache need
> no migration.

GUID shapes:

```
<bluebubbles guid>                 iMessage (unprefixed, the default)
sl:<workspace>:<channel>           Slack chat
sl:<workspace>:<channel>:<ts>      Slack message
```

This is close to free here: nothing in this codebase parses a GUID. `grep` for
`strings.Split(…GUID`, `HasPrefix(…GUID` returns nothing — GUIDs are opaque
strings through the chat list, the dedupe, the message cache and the persister.

## Why a Go port rather than the Rust crate as a sidecar

Running `slack-core` as a subprocess would save porting ~1 500 lines, but it
puts `cargo` in the build and an IPC layer under a program whose whole
distribution story is one static binary. `github.com/slack-go/slack` (v0.29.0)
covers every endpoint above and ships `socketmode`. The Rust crate stays useful
as a spec: it already solved the user/bot name cache, the rate-limit behaviour
and multi-byte-safe log truncation, and those solutions port directly.

## Architecture

### The provider seam

Backends sit behind `provider.Provider`. The interface is small on purpose —
everything every backend can do — and anything a backend *might* not do is an
optional interface it either implements or doesn't:

```go
type Provider interface {
	ID() string
	Chats(limit int) ([]models.Chat, error)
	Messages(chatGUID string, limit int) ([]models.Message, error)
	Send(chatGUID, text, replyToGUID, echoGUID string) error
	React(chatGUID, messageGUID, reaction string) error
	MarkRead(chatGUID string) error
}

type ChatEditor interface { … }      // iMessage renames/deletes; Slack does not
type AttachmentStore interface { … } // fetch bytes for an attachment
type LinkPreviewer interface { … }   // iMessage proxies previews via the server
type Stream interface { … }          // realtime feed
```

A `Registry` routes by GUID prefix; `Registry.For(guid)` is the only place that
maps a conversation to a backend.

### Normalized events

`tui/app.go` used to parse BlueBubbles WebSocket JSON inline. Providers now
publish `provider.Event{Kind, ChatGUID, Message}` and keep their own wire format
to themselves. Making the Slack provider emit BlueBubbles-shaped JSON would have
worked and would have been a lie with a long tail.

### Slack specifics

- **Adapters**: `sl:<ws>:<channel>` for chats, `+:<ts>` for messages. Slack's
  `ts` is `"1700000000.123456"` — seconds with microseconds — so it converts to
  epoch millis, it does not parse as a date. Channels display as `#name`; DMs
  and group DMs keep their name.
- **mrkdwn → plain text**: a port of `src/slack/mrkdwn.ts` (154 lines).
  Slack does not send what a human typed: `<@U123>`, `<#C1|general>`,
  `<https://x.com|label>`, and HTML-escaped `& < >`. Rendering it raw shows the
  brackets *and* breaks link previews, because the URL extractor reads
  `https://x.com|label` as one token. `:shortcode:` → emoji comes nearly free:
  `github.com/enescakir/emoji` is already a dependency for the composer's
  autocomplete and shares Slack's naming.
- **Credentials**: `github.com/zalando/go-keyring` is already a dependency.
  Import `~/.slack_config.json` once, as the desktop app does, then delete it —
  it holds both tokens in plaintext today.

## What is built

All of it. `provider/slack/` is the backend; `config/slack.go` holds the
credentials; `main.go` registers one provider per workspace.

| Capability | Where | Notes |
|---|---|---|
| Conversations | `slack.go` `Chats` | `users.conversations`, paginated, bot DMs filtered out |
| History | `slack.go` `Messages` | `conversations.history`, oldest-first, mrkdwn converted |
| App messages | `content.go` | Block Kit blocks and legacy attachments drawn as text |
| Threads | `slack.go` `threadReplies` | `conversations.replies`, inline, marked `↳` |
| Reading a thread | `slack.go` `Thread`, `threadMessages`; `tui/app.go` `/t #N` | The thread as a conversation of its own, in a pane beside the channel |
| Sending | `slack.go` `Send` | `chat.postMessage`, threaded when given a message GUID or sent to a thread |
| Replying in a thread | `tui/app.go` `/r #N` | Posts under the root, also when row N is itself a reply |
| Reactions | `slack.go` `React` | The six tapbacks map onto Slack emoji names |
| Files | `slack.go` `DownloadAttachment` | Fetched with the token, never handed to the system opener |
| Read state | `slack.go` `MarkRead` | `conversations.mark` up to the newest ts this client has seen |
| Realtime | `stream.go` | Socket Mode: new, edited; deletions reconcile on the next refresh |
| Several workspaces | `main.go` `connectSlack` | One provider each, names prefixed when more than one is connected |

### Decisions worth knowing

**Rate limits shape two designs.** `conversations.history` is Tier 3 — roughly
fifty calls a minute — and the UI reconciles every open pane on a timer. So
threads are cached by reply count (`cachedThread`): the count arrives free with
the history, and an unchanged thread costs no call at all. Names come from one
`users.list` rather than a `users.info` per sender, which would otherwise be
thirty calls for one busy channel.

**A thread is a conversation, addressed by its root.** Inline, replies sort by
time among everything else, so a channel with several live threads cannot be
read as threads at all. `/t #N` asks the provider (`provider.Threader`) for the
thread's conversation and opens it in a pane like any other chat. Its GUID is
the root message's, `sl:<workspace>:<channel>:<root ts>`, so the same thread is
reached from the root or from any reply, and a restored layout gets it back
from the GUID alone — the chat list never holds threads. `Messages`, `Send` and
`MarkRead` all take it: one `conversations.replies` call per load, replies
without the `↳` marker, posts under the root, and no read marking, since the
Web API has none for threads.

Two costs keep that bounded. The refresh timer loads a thread pane like any
other, and the reply-count cache cannot help there — the count comes with the
channel's history, which a thread pane never fetches — so each open thread pane
is one Tier 3 call per poll interval. And realtime replies reach a thread's
GUID only for threads that have been loaded that way (`watched`): publishing
every live reply under its thread too would make each one a chat the UI
caches, persists and resyncs after every reconnect.

**Deletions are not applied.** There is no "remove one message" path in the UI,
and adding one for a single backend would mean a delete iMessage can never
send. The periodic refresh replaces a pane's history wholesale, so a deleted
message disappears within one poll interval.

**App messages are read from their blocks and attachments.** Apps rarely
put what they have to say in `text`. An Alertmanager webhook leaves it empty
and sends a legacy attachment — title, body, colour — so reading the text
alone drew a row with a name and nothing after it. Linear sends a one-line
summary as text and the issue itself in Block Kit. `content.go` assembles the
body the way Slack's client stacks it: the blocks when the app sent a layout,
otherwise the text, then each attachment (pretext, author, title with its
link, text, fields, blocks, image, URL buttons, footer), falling back to the
attachment's `fallback` only when nothing else is there. It builds mrkdwn
rather than display text, so one `TextToPlain` pass resolves mentions, links,
entities and emoji in all of it the same way; literal text — `plain_text`
objects, rich-text runs — is escaped first so it comes through as written.

Every message typed in Slack's composer also carries a `rich_text` block with
what its text says. So text wins whenever the blocks are only `rich_text`,
which keeps typed messages exactly as `TextToPlain` has always shown them;
`rich_text` is rendered only when the text is empty. Section fields of the
form `*Label*\nvalue` are joined onto one line.

Socket Mode needs the same care as files did: slackevents keeps a message's
blocks but drops its attachments, so they are read back off the raw envelope.

**Formatting marks stay in the text and are drawn as styles.** Every Slack
message goes out as `models.FormatMrkdwn`, with `*bold*`, `_italic_`,
`~strike~`, `` `code` ``, ` ``` ` blocks and `> ` quotes still in `Text`. The
text is what was sent: the optimistic copy of a send is matched against it,
and the dedupe compares it. The renderer (`tui/mrkdwn.go`) takes the marks out
and styles the words, with Slack's boundary rules — a mark opens where a word
starts and closes where one ends, so `snake_case_names` and `2*3*4` stay as
written — and never inside a URL, where a dropped underscore would open a
different page. The chat list's preview line and `/open` read the text with
the marks taken out.

**Attachments keep their colour bar.** `models.Message.Accents` marks the
stretches of `Text` that came from attachments, as byte offsets with the
attachment's colour (`good`, `warning` and `danger` become Slack's hex; no
colour is the neutral grey). The pane draws `▎` in that colour at the start of
every row those lines take, so a firing alert reads red and a resolved one
green at a glance. Offsets are taken after `TextToPlain`, part by part,
because it changes lengths; the renderer ignores accents that do not fit the
text rather than cutting it in the wrong places.

**Attachment URLs never reach the system opener.** Slack's `url_private` is a
login page without the workspace token, so it rides on
`models.Attachment.SourceURL` and the provider fetches the bytes. `URL` stays
empty, which is what the opener checks.

## Known limits

- No last-activity ordering for Slack. `users.conversations` returns no
  timestamp, and getting one means a history call per conversation.
- Unread counts come from the conversation list only; Slack has no per-message
  read receipt to mirror iMessage's.
- Custom workspace emoji (`:aspace-logo:`) have no Unicode character and stay
  as shortcodes.
- An app's buttons, menus and inputs only work inside Slack. A button with a
  URL is shown as that link; the rest are left out. Tables and link-unfurl
  cards (including shared messages) are not drawn.
- Socket Mode only delivers events for channels the app is subscribed to.
- The optimistic echo reconciles on text, so sending a message whose text Slack
  rewrites (a bare `:shortcode:` the composer did not already expand) shows a
  duplicate row until the next refresh.
