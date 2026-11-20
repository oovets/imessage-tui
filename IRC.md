# IRC in imessage-tui

IRC channels and private conversations in the same chat list as iMessage,
Slack and Telegram, built the way those are: one `Provider` per network
behind the `provider` seam, its own GUID prefix, credentials in the keyring,
and nothing IRC-shaped above `provider/irc/`.

## The one fact that decides the design

IRC keeps no history. A plain server sends what is said while the client is
connected and nothing else: there is no "load the last fifty messages", no
list of conversations, no read state. Everything the UI asks a backend for —
`Chats`, `Messages`, `MarkRead` — has to be answered from somewhere else.

That somewhere is a bouncer with IRCv3: [soju](https://soju.im) or
[Ergo](https://ergo.chat) keep history and hand it out through
`draft/chathistory`, list conversations through `CHATHISTORY TARGETS`, and
share read state through `draft/read-marker`. So the backend is built for a
bouncer and degrades on a plain server instead of the other way round:

| | Behind a bouncer | On a plain server |
|---|---|---|
| History | `CHATHISTORY LATEST`, once per conversation per connection | What arrived since the program started |
| Gap after a drop | Filled on the next read | Lost |
| Private conversations in the list | From `CHATHISTORY TARGETS` | Only those that spoke since startup |
| Read state | Shared with the user's other clients | This run only |
| Reactions, replies | `+draft/react`, `+draft/reply` | Not available (no `message-tags`) |

## Library

[`github.com/ergochat/irc-go`](https://pkg.go.dev/github.com/ergochat/irc-go)
(`ircevent`, `ircmsg`, `ircfmt`) — pure Go, MIT, from the Ergo authors. It
negotiates IRCv3 capabilities, does SASL PLAIN, collects batches, reconnects
by itself, and parses formatting codes. It is small: the binary barely moves.

Two things about it matter here. It logs to stdout unless given a logger, and
the TUI owns stdout, so `New` hands it one on `log.Writer()` — the file
`main.go` redirected the log to. And its callbacks run on the connection's read
loop, which also answers PINGs: a callback that blocks gets the connection
dropped, so nothing in `handlers.go` waits on anything.

## Architecture

### GUIDs

| Thing | GUID |
|---|---|
| Conversation | `irc:<network>:<target>` |
| Message | `irc:<network>:<target>:<msgid>` |

The network is a slug of its name, fixed when it is added. The target is a
channel or, for a private conversation, the other person's nick, **casefolded**
by the server's `CASEMAPPING`: `#Go` and `#go` are one channel to the server,
and `[` and `{` are the same letter under rfc1459. Neither a channel name nor
a nick can contain a colon (RFC 2812), so `:` separates safely up to the
target; the msgid is the server's token, may contain anything but a space, and
is always the last part.

A message the server gave no msgid gets one made from its time, sender and
text, marked with a leading `~`. Made from what it says, the same line seen
twice gets the same id. It cannot be replied to or reacted to: the server has
nothing to look it up by.

### The connection is the stream

As with Telegram, the realtime feed is not a second connection. `New` connects,
waits for registration and starts the library's reconnect loop; `Stream` only
translates. Every registration — the first and each after a reconnect — joins
the configured channels and, from the second one on, marks every
conversation's history as needing a reload and signals `Reconnected`, which
makes the UI re-read what it has open.

### The log

`chatLog` (`history.go`) is each conversation's messages as this client knows
them, up to a thousand. `Messages` reads it and nothing else, after asking the
server for history once per connection — the UI's refresh timer re-reads every
open pane every few seconds, and none of that reaches the server. Live lines
are added as they arrive; a history batch is merged in by GUID and published to
no one, since it is not news.

### Chats

IRC has no list of conversations, so `Chats` puts one together: the
configured channels, the channels the connection has JOINed (a bouncer replays
them right after registration, so the list waits `settleDelay` for them to
land), `CHATHISTORY TARGETS` from the last thirty days, and whatever has spoken
since startup. A channel that was parted or kicked from drops out of the list
but keeps its log, for a pane that still shows it.

Chats carry no participants. A channel's member list would become its name —
`models.Chat.GetDisplayName` names a group by its members — and the pane
already decides "group" from seeing more than one sender, as it does for Slack
channels and Telegram groups.

### Formatting

mIRC control codes never reach the pane: they measure differently in different
terminals, and a BEL would ring for every message it was pasted into. Bold,
italic, strikethrough and monospace become the mrkdwn the renderer already
draws for Slack (`FormatMrkdwn`); colour, underline and reverse have no mark
and are dropped, keeping their text. A message without formatting stays plain,
so an asterisk someone typed stays an asterisk. `/me` arrives as
`* nick waves`; other CTCP (VERSION, PING) is machines talking and is dropped.

### Sending

IRC carries one line per message and about 400 bytes per line. `Send` splits on
newlines and cuts long lines at a space (or a rune boundary), then paces them:
four at once, then one every two seconds, which is what servers tolerate
before an "Excess Flood" disconnect.

The UI shows what was typed as one optimistic message and matches the server's
copy against it by text. With `echo-message`, the server's copy of a
multi-line message comes back as several lines, which would never match, so
the echoes are held until all have arrived and published as one message with
the typed text (`outgoing` in `handlers.go`). When history later brings the
lines back separately, the ids of all but the first are remembered and
skipped. Without `echo-message` the server never confirms anything; `Send`
publishes the message itself under the UI's own echo GUID, which reconciles
the optimistic copy by identity.

### Reactions and read state

A reaction is a `TAGMSG` with `+draft/react` naming the message by
`+draft/reply`. Received, it is counted onto that message in the log, once per
reaction msgid so that history does not count it again; no event is published,
because a pane ignores an update for a message it already shows, and the next
refresh reads the log — the route Telegram's reactions take too. The six
tapbacks map to ❤️ 😂 👍 👎 ‼️ ❓.

`MarkRead` sends `MARKREAD <target> timestamp=…` when the server has
`draft/read-marker`, and the `MARKREAD` lines the server sends — on join, and
when another client reads — set the marker that unread counts are taken from.
Without it, nothing is counted at startup, and the UI's own unread markers do
the work while it runs.

## Credentials

`config/irc.go` keeps every network in one keyring entry, `irc.networks`:
server, TLS, nick, SASL account, password, channels. The password is a secret
and never leaves the keyring except to the server. With a SASL account the
password is the SASL password; without one it is sent as the server password
(`PASS`), which is how soju is often logged into (`user/network:password`).

A bouncer that takes one connection per network — IRCCloud's, a ZNC with a
user per network — is several networks here, one `irc add` each. `irc add`
offers the server, TLS and nick of the network added last as its answers, so
the second and later ones need only a name and a password. IRCCloud's
password is `bnc:…` per network; written `bnc@<client>:…`, it names this
client, and IRCCloud replays what that client missed when it connects. That
replay arrives as ordinary live lines with `server-time`, so it lands in the
log and the panes like anything said while connected; IRCCloud documents no
`chathistory`, so older history is not there to ask for.

`BB_IRC_SERVER` and `BB_IRC_NICK` (plus `BB_IRC_TLS`, `BB_IRC_NETWORK`,
`BB_IRC_LOGIN`, `BB_IRC_PASSWORD`, `BB_IRC_CHANNELS`) set one network from the
environment, used instead of the stored list, the way `BB_SLACK_TOKEN` is.
TLS is on unless turned off: a password should not cross the network in the
clear by default.

## What is built

| Capability | Where | Notes |
|---|---|---|
| Networks | `config/irc.go`, `commands.go` | `irc add` / `remove` / `networks`, each add suggesting the last one's server and nick; env for one network |
| Connection | `irc.go` `New`, `main.go` `connectIRC` | Networks connect in parallel; a failing one is reported, never fatal |
| GUIDs, casefolding | `guid.go` | rfc1459, rfc1459-strict, ascii |
| Chats | `irc.go` `Chats` | Configured + joined channels, TARGETS, recent speakers |
| History | `irc.go` `Messages`, `history.go` | Once per conversation per connection |
| Receiving | `handlers.go` | PRIVMSG, NOTICE, ACTION, server-time, msgid, replies |
| Sending, replies | `irc.go` `Send` | Split, paced, echoes folded back into one |
| Reactions | `irc.go` `React`, `handlers.go` `onTagmsg` | Needs `message-tags` |
| Read state | `irc.go` `MarkRead`, `handlers.go` `onMarkRead` | Shared with `draft/read-marker` |
| Several networks | `main.go` `connectIRC` | Names and the network label carry the network name |

### Nicks and highlights

The UI draws chats on a `provider.NickNetwork` the IRC way (`tui/nicks.go`),
and this package is one: `SelfNick` hands it the nick the server knows the
user by now, which can differ from the one configured after a collision.
Every line names its nick in the nick's colour from the first line on, in a
wider palette than other chats; a nick that has spoken in the pane takes the
same colour where someone names it; and the user's nick is highlighted where
someone else writes it — the line's sender too — and notifies through a mute,
like a reply to them. Nicks are matched as whole words, ignoring case, never
inside a URL.

### Filters and ignores

`/filter` and `/ignore` work as in WeeChat, and they live in the UI
(`tui/filters.go`), not here: a filtered line is hidden, not dropped, so
alt+= can bring it back, and the log and the UI's cache keep it either way.
What this package gives them is the buffer name — `irc.<network>.<target>`,
the target casefolded as in the GUID — through `provider.BufferNamer`, so the
UI can scope a filter to a network or a channel without reading a GUID. An
ignore matches the nick alone: messages here carry no user@host.

## Known limits

- A private conversation that starts while the program runs is not added to
  the chat list until the next start: the list is loaded once, for every
  backend. Its messages are kept, and show once the chat is open.
- No joining or parting from the client yet; the configured channels and the
  bouncer's are what there is.
- A nick change starts a new private conversation: the conversation is keyed
  by the nick.
- Colour is dropped, not drawn.
- No file transfer (DCC), and links are links: there is nothing to download
  through the backend.
