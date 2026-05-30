[![Go](https://img.shields.io/badge/go-1.24%2B-00ADD8.svg)](https://go.dev/)
[![Bubble Tea](https://img.shields.io/badge/tui-bubble%20tea-ff69b4.svg)](https://github.com/charmbracelet/bubbletea)
[![Docs](https://img.shields.io/badge/docs-mkdocs--material-blue.svg)](https://stevoo.net/imessage-tui/)

keyboard-first terminal client for imessage, backed by a bluebubbles-compatible server.

```
== features ==

  real-time updates over socket.io/websocket, with api polling as a reconciliation path
  multi-pane chat layout with horizontal/vertical splits, up to 4 panes per-pane focus
  unread msg indicators, layout + message-cache persistence chat list with activity
   ordering, unread markers, timestamps, previews, search, resizable width
  chat delete + rename, with local alias fallback for unsupported server-side renames
  optional timestamps, line numbers, sender labels, pane dividers, chat previews
  image attachment labels + /img #N, click-to-open, selected-row Enter open
  youtube / spotify / instagram / news-site link previews via oembed, html metadata,
   or a configurable preview proxy.
  tapbacks render as compact emoji on the original message
  optimistic outgoing messages with timeout reconciliation
  mouse support: focus, chat-list resize, pane-divider resize, image open, scroll
```

```
== requirements ==

  go 1.24+
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
== keybindings ==

`tab`
  toggle focus between chat list and current window
`esc`
  return to the chat list
`← / →`
  move between windows
`ctrl+↑ / ctrl+↓`
  move to the window above or below
`↑ / ↓` or `k / j`
  navigate chats or scroll messages
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
  toggle sender names (show text only when off)
`alt+M`
  toggle sender names (alternative binding)
`q` / `ctrl+C`
  quit
```

status bar shows a connection dot (green connected, red disconnected/reconnecting); with
the chat list hidden, new incoming messages are summarised there.

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
