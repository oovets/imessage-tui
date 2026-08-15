[![Go](https://img.shields.io/badge/go-1.25%2B-00ADD8.svg)](https://go.dev/)
[![Bubble Tea](https://img.shields.io/badge/tui-bubble%20tea-ff69b4.svg)](https://github.com/charmbracelet/bubbletea)
[![Docs](https://img.shields.io/badge/docs-mkdocs--material-blue.svg)](https://stevoo.net/imessage-tui/)

keyboard-first terminal client for imessage and slack, in one chat list.
imessage is backed by a bluebubbles-compatible server; slack talks to slack directly.

```
== features ==

  real-time updates over socket.io/websocket, with api polling as a reconciliation path
  multi-pane chat layout with horizontal/vertical splits, up to 8 panes, per-pane focus
  unread msg indicators, layout + message-cache persistence chat list with activity
   ordering, unread markers, timestamps, previews, search, resizable width
  chat delete + rename, with local alias fallback for unsupported server-side renames
  optional timestamps, line numbers, sender labels, pane dividers, chat previews
  image attachment labels + /img #N, click-to-open, selected-row Enter open
  youtube / spotify / instagram / news-site link previews via oembed, html metadata,
   or a configurable preview proxy.
  tapbacks render as compact emoji on the original message
  sender names get a stable colour per person in group chats and slack channels,
   shown automatically wherever more than one person talks, toggleable per pane
  optimistic outgoing messages with timeout reconciliation
  mouse support: focus, chat-list resize, pane-divider resize, image open, scroll
  slack in the same list: channels, dms, group dms, threads, files, reactions,
   realtime over socket mode, several workspaces at once
```

```
== requirements ==

  go 1.25+
  a running bluebubbles-compatible server
  network access from this client to the bluebubbles http + websocket endpoints
```

```
== configuration ==

  read from env vars and ~/.config/imessage-tui/imessage.yaml; env overrides the file.
  credentials prefer the os keyring and fall back to the config file.
  ui/layout state, message cache, and chat aliases live under ~/.config/imessage-tui/

  server_url                BB_SERVER_URL               required  bluebubbles url
  password                  BB_PASSWORD                 required  api password
  message_limit             BB_MESSAGE_LIMIT            50        messages per chat
  chat_limit                BB_CHAT_LIMIT               50        chats in the sidebar
  poll_interval_sec         BB_POLL_INTERVAL_SEC        10        refresh; 0 disables
  enable_link_previews      BB_ENABLE_LINK_PREVIEWS     true      preview metadata
  max_previews_per_message  BB_MAX_PREVIEWS_PER_MESSAGE 2         previews per message
  preview_proxy_url         BB_PREVIEW_PROXY_URL        empty     optional json proxy
  oembed_endpoint           BB_OEMBED_ENDPOINT          noembed   oembed endpoint
  (env only)                BB_INSECURE_TLS             unset     skip tls verify (self-signed; insecure)
  (env only)                BB_SLACK_TOKEN              unset     slack user token, one workspace
  (env only)                BB_SLACK_APP_TOKEN          unset     slack app-level token, for realtime
  (env only)                BB_SLACK_WORKSPACE          slack     label for the env workspace
  theme                     BB_THEME                    auto      auto|light|dark terminal background

  theme: auto keeps a compromise palette — every colour is chosen to stay
  readable on light and dark alike, and message text carries no colour at all
  so it inherits the terminal's own foreground. pinning it to dark or light
  additionally tunes the palette for that background: on dark the accent blue,
  timestamps and dividers brighten, since they no longer have to survive a
  white background too. auto never picks the tuned palette — background
  detection is unreliable and a wrong guess paints unreadable text.

  on a black-background xterm, set theme: dark (or BB_THEME=dark) and make
  sure TERM is xterm-256color; a TERM without a colour suffix makes lipgloss
  strip every colour.
```

```yaml
# ~/.config/imessage-tui/imessage.yaml
server_url: "https://your-server:1234"
password: "your-api-password"
message_limit: 50
chat_limit: 50
poll_interval_sec: 10
enable_link_previews: true
max_previews_per_message: 2
```

```bash
# build and run -- runtime logs go to ~/.imessage-tui.log
go test ./...
go build -o imessage-tui .
./imessage-tui
```

```
== slack ==

  slack conversations appear in the same chat list as imessage. nothing is
  configured in imessage.yaml: slack is on when tokens are present, off when
  they are not, and the app starts either way. a workspace that fails to
  connect costs that workspace only.

  two tokens per workspace, because slack needs two:
    xoxp-…  user token, so the web api acts as you and sees what you see
    xapp-…  app-level token, which only opens the socket mode connection

  the fastest way in, for one workspace:

    export BB_SLACK_TOKEN=xoxp-…
    export BB_SLACK_APP_TOKEN=xapp-…
    export BB_SLACK_WORKSPACE=acme      # optional label

  for several workspaces, or to keep the tokens out of your shell profile,
  put them in the os keyring. an existing ~/.slack_config.json is imported
  automatically on first run -- delete that file afterwards, it holds both
  tokens in plaintext. the log line tells you when the import happened.

  what works: channels, dms, group dms, thread replies inline, files, emoji
  shortcodes, mentions resolved to names, reactions (the six tapback commands
  map onto slack emoji), sending, replying in a thread with /r #N, and
  realtime over socket mode.

  ordering: imessage chats first, then each workspace -- people and group
  chats before channels, alphabetically. slack's conversation list carries no
  last-activity timestamp, so recency ordering is not available without a
  history call per conversation. with more than one workspace connected, chat
  names are prefixed with the workspace so two "#general" stay apart.

  deleting or renaming a conversation is imessage-only; slack has no
  equivalent and the app says so rather than pretending.

  marking a conversation read needs a write scope on the user token
  (channels:write, groups:write, im:write, mpim:write). without it slack
  answers missing_scope, the app stops asking for that workspace, and
  everything else keeps working.
```

```
== keybindings ==

single-letter shortcuts (? q d r g G /) and the plain arrow keys only act when
you are not typing. with the message composer or chat search focused they edit
text instead — `?` writes a question mark, `←/→` move the cursor. shortcuts
that use ctrl/alt/shift work everywhere, including mid-message.

`tab`
  toggle focus between chat list and current window
`esc`
  return to the chat list
`← / →`
  move the cursor while writing; move between windows otherwise
`shift+← / shift+→`
  move between windows, also while writing
`ctrl+↑ / ctrl+↓`
  move to the window above or below
`↑ / ↓` or `k / j`
  move the cursor while writing; navigate chats or scroll messages otherwise
`g`
  jump to top of chat list
`G`
  jump to bottom of chat list
`enter`
  open selected chat or send from the input
`shift+enter`
  insert a newline in the input

== window management ==

`ctrl+f`
 split the focused window horizontally
`ctrl+g`
 split the focused window vertically
`ctrl+w`
 close the focused window

`ctrl+S`
  toggle chat list visibility
`ctrl+T`
  toggle message timestamps
`ctrl+N`
  toggle message line numbers
`ctrl+B`
  toggle sender names in the focused pane only
`alt+M`
  toggle sender names in every pane, and for new ones
`q` / `ctrl+C`
  quit
```

there is no persistent status bar. prompts you have to act on — delete/rename
confirmations, errors, toasts — appear on the bottom row while they are live and
disappear again; unread chats are marked in the chat list and with a ● on pane
headers.

```
== composer commands ==

typed into the message box and sent with enter, instead of the message:

  /r #N text     reply to message row N. on slack this posts into that
                 message's thread -- the only way to answer in a thread
                 rather than beside it. on imessage it becomes a quoted reply.
  /img #N        open the image on message row N
  /h /lol /tu    react to the latest message with ❤️ 😂 👍
  /te /!! /?     react with 👎 ‼️ ❓

  the #N numbering is the row number shown with ctrl+N. on slack the six
  reactions map onto :heart: :joy: :+1: :-1: :bangbang: :question:.
```

```
== emoticons ==

the composer rewrites text emoticons as emojis. an emoticon has to stand as its
own whitespace-delimited word, so "hej :)" converts and "http://x" does not.

  <3 </3 :) :-) =) :( ;) :| :* 8) :'( \o/   convert the moment you type them
  :D :P :p :O :o xD XD :/ :\ B) o/          convert on the next space, or on send

the second group waits for a word boundary so ":Down" and "xDrive" survive and
urls keep their "://". a rewrite is skipped while the cursor sits behind the end
of the text, so editing earlier in a draft never moves you to the end.

```
== chat management ==

delete uses the bluebubbles private api (DELETE /api/v1/chat/{guid}/delete) and clears
local cachet only after the server confirms: press d on a chat, then D to confirm,
Esc to cancel. rename uses the group-rename api when available; if rejected, the tui
 saves an alias in ~/.config/imessage-tui/chat_overrides.json and applies it on refresh.
```

```
== link previews ==

supported media urls render a compact preview line, e.g.

  [YouTube] video title      [Spotify] track/playlist title
  [Instagram] post/reel      [Aftonbladet] article title

hosts: youtube.com m.youtube.com youtu.be spotify.com open.spotify.com instagram.com
m.instagram.com aftonbladet.se expressen.se dn.se svd.se svt.se omni.se gp.se
sydsvenskan.se di.se. fetches are async (fallback label first, then metadata)
prefer html metadata so generic titles like "search" are ignored and refetched.
```

```
== bluebubbles setup ==

bluebubbles must run on a mac signed into icloud with messages enabled;
 grant full disk access, accessibility, and automation to the server app.

verify connectivity;
  curl -k "https://your-server/api/v1/server/info?password=YOUR_PASSWORD"
      use the same url + password as BB_SERVER_URL and BB_PASSWORD.
```

```text
== architecture ==

main.go    bubble tea program startup
api/       bluebubbles http client, contacts, attachments, link previews
config/    config loading, credentials, ui/layout/message-cache state
models/    chat, message, attachment, link-preview, websocket event types
tui/       bubble tea models, split layout, rendering, input, persistence
ws/        socket.io/websocket client with reconnect + overflow signals
```

```
== tests + release checks ==

go test ./...        # api shape, de-dup, ordering, tapbacks, layout, previews
gofmt -w $(git ls-files '*.go')
go build ./...
git diff --check
```

```
== troubleshooting ==
* tls errors: verify server url, certificate trust, and password.
  certs are verified by default; for a self-signed server set BB_INSECURE_TLS=1 (insecure).
* missing names: ensure contacts are available to bluebubbles.
* stale chats: verify websocket connectivity;
  polling reconciles open chats when enabled.
* build errors on modern stdlib packages: ensure go 1.24+ is first on PATH.
```
