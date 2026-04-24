# iMessage TUI

iMessage TUI is a terminal iMessage client for a BlueBubbles-compatible server.

Module path:

```text
github.com/oovets/imessage-tui
```

This repository is standalone. It contains its own API client, config loading, models, and WebSocket code.

## What The App Does

The TUI connects to your server, loads chats and messages over HTTP, and stays updated through a live WebSocket connection.

It is optimized for keyboard-driven messaging:

- browse chats in the list view
- open conversations in split windows
- follow live message activity
- send messages directly from the terminal
- keep multiple conversations visible at once

## Features

- Browse and read iMessage conversations with contact names
- Real-time updates via WebSocket
- Unread indicators and chat reordering on new activity
- Split-window conversation layout
- Clickable links in messages
- Keyboard-driven navigation and messaging

## Prerequisites

- Go 1.24+
- A running compatible server on macOS
- Network access to the server

## Configuration

Set environment variables or create a config file.

### Environment Variables

```bash
export BB_SERVER_URL="https://your-server:1234"
export BB_PASSWORD="your-api-password"
```

### Config File

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

### Credential Storage

- Preferred: password is stored in the OS keyring
- Fallback: password is stored in the config file
- `BB_SERVER_URL` and `BB_PASSWORD` override stored config

## Build

```bash
go version
go build -o imessage-tui .
```

If `go version` reports an older distro-provided toolchain such as Go 1.13, the build will fail with errors like `cannot load io/fs`. Install Go 1.24+ and ensure that newer binary is first on your `PATH`.

## Run

```bash
./imessage-tui
```

Logs are written to `~/.imessage-tui.log`.

## Keyboard Shortcuts

### Navigation

| Key | Action |
|-----|--------|
| `Tab` | Toggle focus between chat list and current window |
| `Escape` | Return to the chat list |
| `← / →` | Move between windows |
| `Ctrl+↑ / Ctrl+↓` | Move to the window above or below |
| `↑ / ↓` or `k / j` | Navigate chats or scroll messages |
| `g` | Jump to top of chat list |
| `G` | Jump to bottom of chat list |
| `Enter` | Open selected chat or send from the input |
| `Shift+Enter` | Insert a newline in the input |

### Window Management

| Key | Action |
|-----|--------|
| `Ctrl+F` | Split the focused window horizontally |
| `Ctrl+G` | Split the focused window vertically |
| `Ctrl+W` | Close the focused window |

Up to 4 windows can be open at once.

### Toggles

| Key | Action |
|-----|--------|
| `Ctrl+S` | Toggle chat list visibility |
| `Ctrl+T` | Toggle message timestamps |
| `Ctrl+N` | Toggle message line numbers |
| `Ctrl+B` | Toggle sender names (show text only when off) |
| `Alt+M` | Toggle sender names (alternative binding) |
| `q` / `Ctrl+C` | Quit |

## Architecture

```text
main.go             entry point
api/                REST client
config/             config loading and credential storage
models/             chat and message data structures
ws/                 WebSocket client for real-time updates
tui/                Bubble Tea UI
```

## Troubleshooting

### Connection fails with "certificate signed by unknown authority"

Some server setups use self-signed HTTPS certificates. This client is designed to work in that environment.

### Contact names are missing

Make sure contacts are available in the server itself.

### No chats appear

Verify that the server has synced your iMessages and that your credentials are correct.

### Messages do not update in real time

Check WebSocket connectivity and firewall rules between the client and the server.

### Build fails with `cannot load io/fs`

This project depends on modern Go standard library packages and language features. That error usually means your shell is picking up an old Go toolchain.

Verify the active binary:

```bash
which go
go version
go env GOROOT
```

If those point to an older install such as `/usr/lib/go-1.13`, install Go 1.24+ and update your `PATH` so the newer binary is used for builds.
