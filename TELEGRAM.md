# Telegram in imessage-tui

Telegram conversations in the same chat list as iMessage and Slack. Same
starting point as Slack: there is already working Telegram code to learn from,
twice over —

| Where | What |
|---|---|
| `~/Code/imessage/crates/telegram-api/` | The desktop app's MTProto boundary: login (code, 2FA, QR), a keychain-backed session, ops, wire → domain mapping |
| `~/Code/imessage/crates/telegram-core/` | Accounts, sync engine, event bus |
| `~/Code/telegram_rust/` | A standalone Telegram TUI |

Both are Rust on [grammers](https://github.com/Lonami/grammers). They are the
spec, not a dependency: the same argument as in `SLACK.md` applies — a Rust
sidecar puts `cargo` and an IPC layer under a program whose distribution story
is one static binary.

## Library

[`github.com/gotd/td`](https://pkg.go.dev/github.com/gotd/td) — pure Go,
MIT, actively released (v0.162.0 at the time of writing). It has everything the
Rust code relies on:

| Need | gotd |
|---|---|
| Raw API | `tg.Client` — every TL method, generated |
| Session persistence | `session.Storage` (`LoadSession` / `StoreSession`) — two methods, so the keyring backs it |
| QR login | `telegram/auth/qrlogin` (`auth.exportLoginToken`, `Token.URL()`) |
| Code + 2FA login | `telegram/auth` `Flow` + `UserAuthenticator` |
| Realtime with gap recovery | `telegram/updates` `Manager` around a `tg.UpdateDispatcher` |
| Connection state | `Options.OnConnectionState` (connecting / ready / disconnected) |
| Sending with random ids | `telegram/message` `Sender` |
| File download | `telegram/downloader` |

The module also carries voice-call code (pion/webrtc) in its `go.mod`. Go only
links the packages that are imported, so that never reaches the binary — but
`go.sum` gets longer.

## Credentials

Telegram needs two kinds of secret:

- **`api_id` / `api_hash`** identify the *application*. Each user gets their own
  at <https://my.telegram.org> → API development tools. The desktop app reads
  them from `TG_API_ID` / `TG_API_HASH`; this app accepts the same names, and
  `BB_TELEGRAM_API_ID` / `BB_TELEGRAM_API_HASH` like the rest of its env vars.
- **The session** is the logged-in authorization: an auth key per data centre.
  It is as good as the account, so it lives in the OS keyring next to the
  BlueBubbles password and the Slack tokens, never in a file. One keyring entry
  (`telegram.accounts`) holds every account, as `slack.workspaces` does, so the
  keyring prompts once.

`api_id` / `api_hash` are stored with the account at login, so a normal start
needs no environment at all.

## Login

Login is interactive — a code from the Telegram app or SMS, maybe a 2FA
password, or a QR scan — and the TUI owns the terminal. So it is a subcommand
that runs *before* Bubble Tea and may use stdin/stdout freely:

```
imessage-tui telegram login           # QR: Telegram → Settings → Devices → Link Desktop Device
imessage-tui telegram login --phone   # code sent to the Telegram app / SMS instead
imessage-tui telegram logout          # log out the session and forget it
```

The QR is drawn in the terminal with half-block characters, which needs no
image protocol and works over ssh. A 2FA password, when the account has one,
is asked for after either flow. Signal's linking (`SIGNAL.md`) needs the same QR
renderer, so it is shared.

## Architecture

One `Provider` per logged-in account, registered under `tg:<account>:`, where
`<account>` is the account's numeric Telegram user id — it does not change
when the phone number or name does.

### Client lifecycle

gotd's `Client.Run(ctx, f)` is the connection: it dials, keeps the session,
reconnects, and every API call must happen while `f` is running. So the
provider starts `Run` once in a goroutine, `f` signals ready and then blocks
until shutdown, and every `Provider` method calls `client.API()` from whichever
goroutine the UI runs it on. `New` waits for ready with a timeout and checks
`auth.Status`: a revoked session costs that account only, never the app.

### GUIDs

```
tg:<account>:<peer>                 chat
tg:<account>:<peer>:<message id>    message
tgfile:<account>:<peer>:<message id> attachment
```

`<peer>` is a kind letter and the id: `u<id>` user, `g<id>` basic group,
`c<id>` channel or supergroup. The kind cannot be left out: the three id
spaces overlap, and the API wants a different input type for each
(`InputPeerUser`, `InputPeerChat`, `InputPeerChannel`).

Message ids are per-chat for channels and per-account for everything else; in
both cases chat plus id is unique, which is all the GUID needs.

### Access hashes

Users and channels cannot be addressed by id alone; the API also wants the
`access_hash` the server handed out with the object. Every response that
carries users or chats (`getDialogs`, `getHistory`, every update's entities)
feeds a per-account peer table, and an `InputPeer` is built from it. A peer that
has never been seen — a GUID restored from the message cache before the chat
list has loaded — triggers one dialogs reload before giving up.

The table is in memory for now. If restored panes turn out to race the chat
list in practice, it moves to disk; the hashes are account-specific but not
secret.

### Mapping

| Provider method | Telegram |
|---|---|
| `Chats` | `messages.getDialogs` — comes ordered by last activity and with unread counts and the top message, which Slack cannot offer |
| `Messages` | `messages.getHistory`, newest first from the server, reversed |
| `Send` | `messages.sendMessage`, with `reply_to` when replying |
| `React` | `messages.sendReaction` |
| `MarkRead` | `messages.readHistory`, or `channels.readHistory` for channels and supergroups |
| `DownloadAttachment` | `upload.getFile` via the downloader, on a message re-fetched first |
| `Stream` | `updates.Manager` → `UpdateNewMessage`, `UpdateNewChannelMessage`, `UpdateEditMessage`, `UpdateEditChannelMessage`, `UpdateMessageReactions` |

Names are DisplayName on the chat (user's first + last name, group title), no
participant list: a group's members are another call per group, and the UI
already turns sender names on by itself once a pane shows two senders.

Reactions fold into `ReactionCounts` like Slack's do. The UI's six tapbacks map
onto Telegram's standard reactions: love `❤`, laugh `😁`, like `👍`, dislike
`👎`, emphasize `🔥`, question `🤔`. `‼️` and `❓` are not among Telegram's
reactions, and a chat can restrict the set further, in which case the server
says `REACTION_INVALID` and the error reaches the user.

Attachment file references expire, which is why a download re-fetches its
message first rather than trusting the reference captured at render time.

### Rate limits

Telegram answers too many calls with `FLOOD_WAIT_<seconds>`. The UI's refresh
timer re-reads every open pane every `poll_interval_sec`, and a reconnect
resyncs every cached chat — fine against BlueBubbles on the LAN, a flood risk
against Telegram. So `Messages` serves from a per-chat history cache while the
realtime feed is up and nothing has arrived for that chat; an event for the
chat, or the feed dropping, invalidates it. The cache is what the feed keeps
honest, not a TTL guess.

## Plan

1. **iMessage optional.** `main.go` refuses to start without BlueBubbles; it
   must start with any one backend configured, the way a dead Slack token
   already does not stop the app.
2. `config/telegram.go` — accounts in the keyring, `api_id`/`api_hash` from env.
3. `imessage-tui telegram login | logout` with the shared terminal QR.
4. `provider/telegram/` — GUIDs, peer table, `Chats`, `Messages`, `Send`,
   `React`, `MarkRead`.
5. Realtime stream + the history cache.
6. Attachments.
7. Edits and deletions, replies shown as quotes.

## Known limits (expected)

- Secret chats are device-bound end-to-end encrypted chats; they do not exist
  for a second client and will not appear.
- Custom-emoji reactions (Premium) have no Unicode form and show as `★`.
- Formatting entities (bold, links with labels) arrive as offsets beside the
  text; the renderer draws plain text, so labels without their URL are lost
  until entities are mapped.
- Service messages (joins, pins, title changes) are skipped.
