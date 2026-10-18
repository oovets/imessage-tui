# Signal in imessage-tui

Signal conversations in the same chat list as the other networks. Unlike Slack
and Telegram there is no earlier implementation to port, and Signal differs from
every backend so far in one way that shapes everything else: **the server keeps
no history.**

## The one fact that decides the design

BlueBubbles, Slack and Telegram all answer "give me the last fifty messages of
this chat". Signal cannot: its servers hold a message only until each of the
recipient's devices has fetched it, then delete it. A linked device sees what
arrives after it was linked, and nothing before.

Signal does now offer to transfer recent history when linking a new Desktop or
iPad (["A Synchronized Start for Linked
Devices"](https://signal.org/blog/a-synchronized-start-for-linked-devices/)),
but that is a feature of the official clients; nothing in signal-cli's release
notes says it can receive one.

So the Signal provider needs **its own durable message store**, and
`Messages()` reads from it. The existing `config/message_cache.json` is not
that: it is a cache, replaced wholesale on refresh, and a backend that loses a
message there loses it for good.

## Options

| Route | For | Against |
|---|---|---|
| **`signal-cli jsonRpc`, child process** (chosen) | Mature and maintained ([AsamK/signal-cli](https://github.com/AsamK/signal-cli)). JSON-RPC over stdin/stdout, so there is no port, no socket and no daemon to keep alive. GPLv3, but a separate program speaking a protocol is not linking. | A second program to install: Java 25, or the GraalVM native build, which the project calls experimental. Not in Arch's official repos (AUR, or the release tarball). |
| signal-cli-rest-api in Docker on a server | The BlueBubbles pattern: one always-on box, the TUI connects over HTTP + websocket from anywhere | Docker, a server, and the same history problem — the store would have to live on the server |
| mautrix-signal's `signalmeow` (Go) | In process | AGPL-3.0, cgo against Rust `libsignal`, a Rust toolchain in the build, and an internal package of a bridge rather than a library with a stable API |
| presage (Rust) | A real library | The sidecar argument in `SLACK.md`, again |

## Architecture

### The process

The provider starts `signal-cli -a <account> -o json jsonRpc` when the app
starts and owns it for the app's lifetime:

- stdin carries requests, stdout carries responses and `receive` notifications,
  one JSON object per line.
- stderr goes to `~/.imessage-tui.log`, never to the terminal.
- If the process dies, it is restarted with backoff and the stream fires
  `Reconnected`, which makes the UI resync — the same contract as a dropped
  websocket.
- `--receive-mode on-start`: everything queued for this device while the app
  was closed arrives as notifications right after start, and goes into the
  store before anything else reads it.

Messages that arrive while the TUI is closed wait on Signal's server for this
device. Signal unlinks a device that stays away too long — about a month,
going by Signal's support pages; check the current number before relying on
it.

### Linking

```
imessage-tui signal link      # Signal on the phone → Settings → Linked devices → +
imessage-tui signal unlink    # forget the account locally
```

`signal-cli link -n imessage-tui` prints an `sgnl://linkdevice?…` URI and waits;
the subcommand draws it as a QR in the terminal (the renderer Telegram's QR
login uses) and records the account once linking finishes. signal-cli keeps
its keys under `$XDG_DATA_HOME/signal-cli/`; this app stores only which account
to start it with.

### GUIDs

```
sig:<account>:<conversation>                         chat
sig:<account>:<conversation>:<author>:<timestamp>    message
```

- `<account>` is a slug of the phone number.
- `<conversation>` is `u<ACI>` for a person (the account UUID, which survives a
  number change) or `g<group id>` (base64; it never contains a colon).
- A Signal message has no id of its own. It is addressed by **author plus sent
  timestamp** — that is what a reaction, a quote, an edit and a delete all
  point at — so both are in the GUID.

### Store

SQLite through `modernc.org/sqlite`: pure Go, so the binary stays static and
cgo stays out. It costs a few MB of binary. The queries are exactly the ones a
database is for — "last N messages of this chat", "the message by this author
at this timestamp", "every chat by last activity" — and edits, reactions and
deletes are updates to rows that already exist. An append-only JSON log would
work too, and would turn each of those into a replay.

One database per account under `~/.config/imessage-tui/signal/`, mode 0600. It
holds message text in plaintext on disk, which is what signal-cli's own data
directory and Signal Desktop's database also amount to without full-disk
encryption.

### Mapping

| Provider method | Signal |
|---|---|
| `Chats` | From the store: conversations by last activity, unread counts kept locally. Names from `listContacts` / `listGroups` |
| `Messages` | From the store, never from the network |
| `Send` | `send` (`recipient` or `groupId`, `message`, `quoteTimestamp` + `quoteAuthor` for a reply). Written to the store with the timestamp signal-cli returns, because a message this device sent never comes back to it |
| `React` | `sendReaction` (`emoji`, `targetAuthor`, `targetTimestamp`). The six tapbacks map to themselves: Signal takes any emoji |
| `MarkRead` | `sendReceipt` (`type: read`) for the unread messages, then the local counter. Whether signal-cli can also tell the phone that they were read is open |
| `DownloadAttachment` | Received attachments are already on disk (signal-cli downloads them); `getAttachment` by id |
| `Stream` | The `receive` notifications, after the store has applied them |

What comes in the `receive` envelope, and what it becomes:

| Envelope | Store | Event |
|---|---|---|
| `dataMessage` | new row | `EventNewMessage` |
| `syncMessage.sentMessage` — sent from the phone | new row, `IsFromMe` | `EventNewMessage` |
| `dataMessage.reaction` | reaction on the target row | `EventUpdatedMessage` |
| `editMessage` | target row's text | `EventUpdatedMessage` |
| `dataMessage.remoteDelete` | target row removed | none — the next refresh drops it, as with Slack deletions |
| `receiptMessage`, `typingMessage` | nothing for now | none |

## Prerequisites shared with Telegram

- iMessage must be optional first (see `TELEGRAM.md`, plan step 1).
- The terminal QR renderer.

## Open questions

- Read sync to the phone (above).
- Group names and members change by group update messages; the first version
  re-reads `listGroups` on start and when an unknown group id appears.
- Stories and calls are ignored.
- Attachments sent from this client (`send` with `attachment`) need a file
  picker the UI does not have; receiving comes first.
