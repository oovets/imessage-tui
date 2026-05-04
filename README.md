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

<details>
<summary><strong>Installing a BlueBubbles server</strong></summary>

The BlueBubbles server is a macOS app that bridges iMessage to an HTTP/WebSocket
API. It must run on a Mac that is signed into iCloud and has Messages working.

**Host requirements**

- A Mac (mini, MacBook, iMac) running macOS 11 Big Sur or newer
- Signed into iCloud, with iMessage enabled and at least one conversation visible
- Always-on power, network, and "Prevent automatic sleeping" enabled
- Full Disk Access granted to the BlueBubbles server app (so it can read the
  Messages SQLite database)
- Accessibility and Automation permissions granted (so it can send messages)

**Install steps**

1. On the host Mac, download the latest server build from
   [bluebubbles.app/server](https://bluebubbles.app/server) (or the
   [GitHub releases](https://github.com/BlueBubblesApp/bluebubbles-server/releases)).
2. Move `BlueBubbles.app` into `/Applications` and launch it.
3. Approve the macOS permission prompts:
   - System Settings → Privacy & Security → **Full Disk Access** → enable BlueBubbles
   - **Accessibility** → enable BlueBubbles
   - **Automation** → allow BlueBubbles to control Messages and System Events
4. In the server UI, set a strong **server password** — this is the API
   password the client will use.
5. Choose a port (default `1234`) and start the server.
6. Optional: enable "Launch on startup" and disable App Nap so the server
   survives reboots and stays awake.

**Exposing the server**

For LAN-only use, the local URL (`http://<mac-ip>:1234`) is enough. For remote
access, pick one of:

- **Cloudflare Tunnel** (recommended): the server's built-in proxy can publish
  a `*.trycloudflare.com` URL, or you can attach a named tunnel.
- **ngrok**: also supported from the server UI under Connection settings.
- **Manual port-forward + dynamic DNS**: forward the chosen port on your router
  and point a hostname at your home IP. Use TLS in front of it.

**Verify the server is up**

```bash
curl -k "https://your-server/api/v1/server/info?password=YOUR_PASSWORD"
```

A JSON payload with server metadata confirms the API is reachable. Use that
same URL and password as `BB_SERVER_URL` / `BB_PASSWORD` for this client.

**Common pitfalls**

- Messages app must have launched at least once and synced an iMessage chat.
- iCloud "Messages in iCloud" should be on, otherwise old history is missing.
- If macOS upgrades reset permissions, re-grant Full Disk Access and restart
  the server.
- Self-signed certificates work but the client must trust them — prefer a real
  TLS cert via Cloudflare Tunnel for remote use.

</details>

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
