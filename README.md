# iMessage TUI

A keyboard-first terminal client for iMessage, powered by a BlueBubbles-compatible server.

- Real-time message updates over WebSocket
- Multi-pane conversation layout
- Fast, low-friction keyboard workflow
- Local UI and message cache persistence
- Lightweight footprint: ~18 MB RAM at runtime

Module path:

```text
github.com/oovets/imessage-tui
```

## Why This Project

iMessage TUI is built for engineers and power users who want native-feeling message throughput in a terminal environment. It focuses on reliability, predictable keybindings, and clean split-pane navigation instead of visual overhead.

## Feature Set

- Chat list with new-message indicators and activity-based ordering
- Real-time updates via Socket.IO/WebSocket
- Split windows (horizontal and vertical), up to 4 concurrent panes
- Per-pane chat focus and unread state handling
- Optional message timestamps, line numbers, and sender labels
- Attachment-aware message fetch and image open helper (`/img #<n>`)
- Debounced persistence for layout, UI state, and message cache

When the chat sidebar is hidden, new incoming messages are surfaced in the top status bar.

## Requirements

- Go 1.24+
- A running BlueBubbles-compatible server
- Network access to the server

## Configuration

You can configure the app through environment variables or config file.

### Environment variables

```bash
export BB_SERVER_URL="https://your-server:1234"
export BB_PASSWORD="your-api-password"
```

### Config file

```text
~/.config/imessage-tui/imessage.yaml
```

Example:

```yaml
server_url: "https://your-server:1234"
password: "your-api-password"
message_limit: 50
chat_limit: 50
```

### Credential behavior

- Preferred storage: OS keyring
- Fallback storage: config file
- `BB_SERVER_URL` and `BB_PASSWORD` override stored values

## Build

```bash
go build -o imessage-tui .
```

## Run

```bash
./imessage-tui
```

Logs are written to:

```text
~/.imessage-tui.log
```

## Keybindings

### Navigation

| Key | Action |
|-----|--------|
| `Tab` | Toggle focus between chat list and active window |
| `Escape` | Move focus to chat list |
| `Left` / `Right` | Move focus between windows |
| `Ctrl+Up` / `Ctrl+Down` | Move focus vertically between windows |
| `Up` / `Down` or `k` / `j` | Navigate chats or scroll messages |
| `g` | Jump to top of chat list |
| `G` | Jump to bottom of chat list |
| `Enter` | Open selected chat or send message |
| `Shift+Enter` | Insert newline in input |

### Window management

| Key | Action |
|-----|--------|
| `Ctrl+F` | Split focused window horizontally |
| `Ctrl+G` | Split focused window vertically |
| `Ctrl+W` | Close focused window |

### Display toggles

| Key | Action |
|-----|--------|
| `Ctrl+S` | Toggle chat list visibility |
| `Ctrl+T` | Toggle timestamps |
| `Ctrl+N` | Toggle line numbers |
| `Ctrl+B` | Toggle sender names |
| `Alt+M` | Alternative sender-name toggle |
| `Ctrl+M` | Sender-name toggle in terminals that distinguish it from Enter |
| `q` / `Ctrl+C` | Quit |

## Command Input

- `/img #<message-number>` opens the first image attachment from that rendered message row.

## Architecture

```text
main.go             Program startup and Bubble Tea runtime
api/                HTTP client for chats, messages, contacts, attachments
config/             Config loading, credential handling, persisted UI/layout state
models/             Domain models and WebSocket event envelope
tui/                Bubble Tea models, split layout, input and rendering
ws/                 Socket.IO WebSocket client with reconnect and overflow signaling
```

## Troubleshooting

### TLS/certificate issues

If your server uses a self-signed certificate, verify the endpoint is reachable and credentials are correct.

### Missing contacts or names

Ensure contacts are synced and available in your BlueBubbles environment.

### No chats or stale updates

- Verify server URL and password
- Verify WebSocket connectivity and firewall rules
- Restart the client to force full state reload

### Build fails with modern stdlib errors

If you see errors like `cannot load io/fs`, your shell is likely picking an old Go toolchain. Confirm:

```bash
which go
go version
go env GOROOT
```

Then ensure Go 1.24+ is first on `PATH`.
