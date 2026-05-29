# imessage-tui

[![Go](https://img.shields.io/badge/go-1.24%2B-00ADD8.svg)](https://go.dev/)
[![Bubble Tea](https://img.shields.io/badge/tui-bubble%20tea-ff69b4.svg)](https://github.com/charmbracelet/bubbletea)
[![Docs](https://img.shields.io/badge/docs-mkdocs--material-blue.svg)](https://stevoo.net/imessage-tui/)

keyboard-first terminal client for imessage, backed by a bluebubbles-compatible server.
module path: github.com/oovets/imessage-tui

```
== features ==

- real-time updates over socket.io/websocket, with api polling as a reconciliation path
- multi-pane chat layout with horizontal/vertical splits, up to 4 panes
- per-pane focus, unread/new-message indicators, layout + message-cache persistence
- chat list with activity ordering, unread markers, timestamps, previews, search,
  resizable width
- chat delete + rename, with local alias fallback for unsupported server-side renames
- optional timestamps, line numbers, sender labels, pane dividers, chat previews
- image attachment labels + /img #N, click-to-open, selected-row Enter open
- youtube / spotify / instagram / news-site link previews via oembed, html metadata,
  or a configurable preview proxy
- tapbacks render as compact emoji on the original message
- optimistic outgoing messages with timeout reconciliation
- mouse support: focus, chat-list resize, pane-divider resize, image open, scroll
```

```
== requirements ==

- go 1.24+
- a running bluebubbles-compatible server
- network access from this client to the bluebubbles http + websocket endpoints
```

```
== configuration ==

read from env vars and ~/.config/imessage-tui/imessage.yaml; env overrides the file.
credentials prefer the os keyring and fall back to the config file. ui/layout state,
message cache, and chat aliases live under ~/.config/imessage-tui/.

  server_url                BB_SERVER_URL               required  bluebubbles url
  password                  BB_PASSWORD                 required  api password
  message_limit             BB_MESSAGE_LIMIT            50        messages per chat
  chat_limit                BB_CHAT_LIMIT              50        chats in the sidebar
  poll_interval_sec         BB_POLL_INTERVAL_SEC       10        refresh; 0 disables
  enable_link_previews      BB_ENABLE_LINK_PREVIEWS    true      fetch preview metadata
  max_previews_per_message  BB_MAX_PREVIEWS_PER_MESSAGE 2        previews per message
  preview_proxy_url         BB_PREVIEW_PROXY_URL       empty     optional json proxy
  oembed_endpoint           BB_OEMBED_ENDPOINT         noembed   oembed endpoint
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

navigation   Tab focus chat-list<->pane · Esc focus list / close help ·
             Left/Right pane focus (Left from leftmost -> list) · Ctrl+Up/Down pane
             focus vertically · Up/Down or k/j list · g/G top/bottom (G in pane ->
             newest) · Enter open chat / send / open image row · Shift+Enter newline
chat list    / filter (Esc clears) · d then D delete · r rename · Ctrl+D/Ctrl+R
             delete/rename pane chat · Ctrl+Left/Right resize · Ctrl+P previews ·
             mouse-drag right edge to resize
messages     PgUp/PgDn scroll · End/G newest · /img #N open image from row N ·
             /h /lol /tu /te react heart/laugh/up/down · /!! /? emphasize/question ·
             click image row to open  (reactions via /api/v1/message/react)
panes        Ctrl+F split horizontal · Ctrl+G split vertical · Ctrl+W close ·
             Ctrl+Shift+Left/Right adjust ratio · mouse-drag divider to resize
display      Ctrl+S chat list · Ctrl+T timestamps · Ctrl+N line numbers ·
             Ctrl+B/Alt+M/Ctrl+M sender labels · Ctrl+E pane dividers · ? help ·
             q / Ctrl+C quit
```

status bar shows a connection dot (green connected, red disconnected/reconnecting); with
the chat list hidden, new incoming messages are summarised there.

```
== chat management ==

delete uses the bluebubbles private api (DELETE /api/v1/chat/{guid}/delete) and clears
local cache/layout only after the server confirms: press d on a chat, then D to confirm,
Esc to cancel. rename uses the group-rename api when available; if rejected, the tui saves
a local alias in ~/.config/imessage-tui/chat_overrides.json and applies it on refresh.
```

```
== link previews ==

supported media urls render a compact preview line, e.g.
  [YouTube] video title      [Spotify] track/playlist title
  [Instagram] post/reel       [Aftonbladet] article title
hosts: youtube.com m.youtube.com youtu.be spotify.com open.spotify.com instagram.com
m.instagram.com aftonbladet.se expressen.se dn.se svd.se svt.se omni.se gp.se
sydsvenskan.se di.se. fetches are async (fallback label first, then metadata); news sites
prefer html metadata so generic titles like "search" are ignored and refetched.
```

```
== bluebubbles setup ==

bluebubbles must run on a mac signed into icloud with messages enabled; grant full disk
access, accessibility, and automation to the server app. verify connectivity:
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

- tls errors: verify server url, certificate trust, and password.
- missing names: ensure contacts are available to bluebubbles.
- stale chats: verify websocket connectivity; polling reconciles open chats when enabled.
- build errors on modern stdlib packages: ensure go 1.24+ is first on PATH.
```
