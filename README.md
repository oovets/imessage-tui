# iMessage TUI

Keyboard-first terminal client for iMessage, backed by a BlueBubbles-compatible server.

Module path:

```text
github.com/oovets/imessage-tui
```

## Features

- Real-time message updates over Socket.IO/WebSocket, with API polling as a reconciliation path
- Multi-pane chat layout with horizontal/vertical splits, up to 4 panes
- Per-pane focus, unread/new-message indicators, layout persistence, and message cache persistence
- Chat list with activity ordering, unread markers, timestamps, previews, search, and resizable width
- Chat delete and rename actions, with local alias fallback for unsupported server-side renames
- Optional timestamps, line numbers, sender labels, pane dividers, and chat previews
- Image attachment labels plus `/img #N`, click-to-open, and selected-row `Enter` open behavior
- YouTube, Spotify, Instagram, and news-site link previews through oEmbed, HTML metadata, or a configurable preview proxy
- Tapbacks render as compact emoji on the original message
- Optimistic outgoing messages with timeout reconciliation
- Mouse support for focus, chat-list resize, pane-divider resize, image row open, and message scrolling

## Requirements

- Go 1.24+
- A running BlueBubbles-compatible server
- Network access from this client to the BlueBubbles HTTP and WebSocket endpoints

## Configuration

Configuration is read from environment variables and `~/.config/imessage-tui/imessage.yaml`.
Environment variables override config file values.

| Config key | Environment variable | Default | Description |
|------------|----------------------|---------|-------------|
| `server_url` | `BB_SERVER_URL` | required | BlueBubbles server URL |
| `password` | `BB_PASSWORD` | required | BlueBubbles API password |
| `message_limit` | `BB_MESSAGE_LIMIT` | `50` | Messages fetched per chat |
| `chat_limit` | `BB_CHAT_LIMIT` | `50` | Chats fetched for the sidebar |
| `poll_interval_sec` | `BB_POLL_INTERVAL_SEC` | `10` | Periodic refresh interval; `0` disables polling |
| `enable_link_previews` | `BB_ENABLE_LINK_PREVIEWS` | `true` | Fetch supported link preview metadata |
| `max_previews_per_message` | `BB_MAX_PREVIEWS_PER_MESSAGE` | `2` | Link previews fetched per message |
| `preview_proxy_url` | `BB_PREVIEW_PROXY_URL` | empty | Optional JSON preview proxy endpoint |
| `oembed_endpoint` | `BB_OEMBED_ENDPOINT` | `https://noembed.com/embed` | oEmbed endpoint for link previews |

Example:

```yaml
server_url: "https://your-server:1234"
password: "your-api-password"
message_limit: 50
chat_limit: 50
poll_interval_sec: 10
enable_link_previews: true
max_previews_per_message: 2
```

Credentials prefer OS keyring storage when available and fall back to the config file.
Persisted UI state, layout state, message cache, and chat aliases live under `~/.config/imessage-tui/`.

## Build And Run

```bash
go test ./...
go build -o imessage-tui .
./imessage-tui
```

Runtime logs are written to:

```text
~/.imessage-tui.log
```

## Tests

```bash
go test ./...
```

Current test coverage focuses on:

- API request shape for sends and link-preview provider routing
- Message de-duplication across API/cache/WebSocket variants
- Message ordering, image-row lookup, tapback folding, and link-preview rendering
- Chat delete/rename state transitions and local alias persistence
- Split-pane layout, divider hit-testing, and resize behavior
- Chat-list preview toggling and timestamp formatting

Release checks:

```bash
gofmt -w $(git ls-files '*.go')
go test ./...
go build ./...
git diff --check
```

## Resource Profile

Last local measurement: Linux amd64, Go `1.24.2`.

| Operation | Command | Result |
|-----------|---------|--------|
| Test suite | `/usr/bin/time -f 'elapsed=%E user=%U sys=%S max_rss_kb=%M' go test ./...` | `elapsed=0:00.65`, `user=0.78`, `sys=0.24`, `max_rss_kb=34300` |
| Release build | `/usr/bin/time -f 'elapsed=%E user=%U sys=%S max_rss_kb=%M' go build -o /tmp/imessage-tui-release .` | `elapsed=0:01.03`, `user=1.20`, `sys=0.26`, `max_rss_kb=216960` |
| Binary size | `ls -lh /tmp/imessage-tui-release` | `14M` |

Runtime CPU and memory depend on chat/message limits, open panes, message cache size, link-preview fetches, and WebSocket activity. Measure a live session with:

```bash
/usr/bin/time -v ./imessage-tui
```

## Keybindings

### Navigation

| Key | Action |
|-----|--------|
| `Tab` | Toggle focus between chat list and active pane |
| `Esc` | Focus chat list, or close help overlay |
| `Left` / `Right` | Move pane focus horizontally; `Left` from leftmost pane focuses chat list |
| `Ctrl+Up` / `Ctrl+Down` | Move pane focus vertically |
| `Up` / `Down` or `k` / `j` | Navigate chat list |
| `g` / `G` | Jump to top/bottom of chat list; `G` in a pane jumps messages to bottom |
| `Enter` | Open selected chat, send message, or open selected image row when input is empty |
| `Shift+Enter` | Insert newline in input |

### Chat List

| Key | Action |
|-----|--------|
| `/` | Filter chats; `Esc` clears filter |
| `d` / `D` | Start delete for selected chat / confirm destructive delete |
| `r` | Rename selected chat |
| `Ctrl+D` / `Ctrl+R` | Delete / rename the chat in the active pane |
| `Ctrl+Left` / `Ctrl+Right` | Resize chat list |
| `Ctrl+P` | Toggle chat previews |
| Mouse drag near right edge | Resize chat list |

### Messages

| Key | Action |
|-----|--------|
| `PgUp` / `PgDn` | Scroll message history |
| `End` / `G` | Jump to newest messages |
| `/img #N` | Open first image attachment from rendered message row `N` |
| Click image row | Open image attachment |

### Panes

| Key | Action |
|-----|--------|
| `Ctrl+F` | Split focused pane horizontally |
| `Ctrl+G` | Split focused pane vertically |
| `Ctrl+W` | Close focused pane |
| `Ctrl+Shift+Left` / `Ctrl+Shift+Right` | Adjust focused split ratio |
| Mouse drag pane divider | Resize split |

### Display

| Key | Action |
|-----|--------|
| `Ctrl+S` | Toggle chat list visibility |
| `Ctrl+T` | Toggle message timestamps |
| `Ctrl+N` | Toggle line numbers |
| `Ctrl+B` / `Alt+M` / `Ctrl+M` | Toggle sender labels |
| `Ctrl+E` | Toggle pane dividers |
| `?` | Toggle help overlay |
| `q` / `Ctrl+C` | Quit |

The status bar shows a connection dot: green when connected, red when disconnected or reconnecting.
When the chat list is hidden, new incoming messages are summarized in the status bar.

## Chat Management

Chat deletion uses the BlueBubbles Private API (`DELETE /api/v1/chat/{guid}/delete`) and removes local cache/layout state only after the server confirms success. Press `d` on a selected chat, then `D` to confirm, or `Esc` to cancel.

Chat rename uses the BlueBubbles group rename API when available. If the server rejects rename, the TUI saves a local alias in `~/.config/imessage-tui/chat_overrides.json` and applies it on future chat refreshes.

## Link Previews

Messages containing supported media URLs render a compact preview line:

```text
[YouTube] Video title
[Spotify] Track or playlist title
[Instagram] Post or reel title
[Aftonbladet] Article title
```

Supported hosts:

- `youtube.com`
- `m.youtube.com`
- `youtu.be`
- `spotify.com`
- `open.spotify.com`
- `instagram.com`
- `m.instagram.com`
- `aftonbladet.se`
- `expressen.se`
- `dn.se`
- `svd.se`
- `svt.se`
- `omni.se`
- `gp.se`
- `sydsvenskan.se`
- `di.se`

Preview fetches are asynchronous. The message first renders a fallback label, then updates when metadata arrives. News sites prefer HTML metadata so generic titles like `search` are ignored and refetched.

## BlueBubbles Setup

BlueBubbles must run on a Mac signed into iCloud with Messages enabled.
Grant Full Disk Access, Accessibility, and Automation permissions to the BlueBubbles server app.
Verify connectivity with:

```bash
curl -k "https://your-server/api/v1/server/info?password=YOUR_PASSWORD"
```

Use the same URL and password as `BB_SERVER_URL` and `BB_PASSWORD`.

## Architecture

```text
main.go             Bubble Tea program startup
api/                BlueBubbles HTTP client, contacts, attachments, link previews
config/             Config loading, credential handling, UI/layout/message cache state
models/             Chat, message, attachment, link-preview, WebSocket event types
tui/                Bubble Tea models, split layout, rendering, input, persistence
ws/                 Socket.IO/WebSocket client with reconnect and overflow signals
```

## Troubleshooting

- TLS errors: verify server URL, certificate trust, and password.
- Missing names: ensure contacts are available to BlueBubbles.
- Stale chats: verify WebSocket connectivity; polling reconciles open chats when enabled.
- Build errors involving modern stdlib packages: ensure Go 1.24+ is first on `PATH`.
