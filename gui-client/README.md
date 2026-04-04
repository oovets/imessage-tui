# BlueBubbles GUI

BlueBubbles GUI is a desktop iMessage client for the BlueBubbles server, built with Fyne.

Module path:

```text
github.com/oovets/bluebubbles-gui
```

This repository is standalone. It contains its own API client, config loading, models, and WebSocket code.

## What The App Does

The GUI connects to your BlueBubbles server, loads your chat list and recent messages over HTTP, and keeps the UI updated through a live WebSocket connection.

It is designed around panes:

- the chat list lives on the left
- the focused pane shows one conversation
- panes can be split horizontally or vertically
- chats can be moved into new windows
- replies and reactions are sent directly from the conversation view

If credentials are missing, the GUI opens a first-run setup wizard and saves the server URL and password for later launches.

## Features

- Browse and read iMessage conversations with contact names
- Real-time updates via WebSocket with reconnect behavior
- Unread indicators and chat bumping on new activity
- Multi-pane conversation layout
- New-window and detached-pane workflows
- Clickable links in messages
- Inline attachment and image rendering when available
- Optional URL previews with title, description, site name, and image
- Native replies from the message view
- Appearance controls for theme, font family, font size, compact mode, and bold text

## Prerequisites

- Go 1.24+
- A running BlueBubbles server on macOS
- Network access to the BlueBubbles server

## Configuration

Environment variables:

```bash
export BB_SERVER_URL="https://your-server:1234"
export BB_PASSWORD="your-api-password"
```

Optional preview settings:

```bash
export BB_ENABLE_LINK_PREVIEWS=true
export BB_MAX_PREVIEWS_PER_MESSAGE=2
export BB_PREVIEW_PROXY_URL=""
export BB_OEMBED_ENDPOINT="https://noembed.com/embed"
```

Config file location:

```text
~/.config/bluebubbles-tui/bluebubbles.yaml
```

Preferred password storage is the OS keyring. If keyring storage is unavailable, the password falls back to the config file.

## Build

```bash
go build -o bluebubbles-gui ./cmd/gui
go build -o bluebubbles-preview-proxy ./cmd/preview-proxy
```

## Install

The install script builds the GUI binaries, installs the preview-proxy systemd user service, and creates the desktop launcher:

```bash
./scripts/install.sh
```

Optional overrides:

```bash
FYNE_SCALE=1.5 ./scripts/install.sh
PREVIEW_PROXY_ADDR=127.0.0.1:8091 ./scripts/install.sh
```

## Run

```bash
./bluebubbles-gui
```

Logs are written to `~/.bluebubbles-gui.log`.

### Startup Flags

```bash
./bluebubbles-gui --chat-guid <chat-guid>
./bluebubbles-gui --detached-pane
```

- `--chat-guid` opens a specific chat in the focused pane at startup
- `--detached-pane` starts a stripped-down single-pane window without the chat list or restored split layout

## Preview Proxy

The preview proxy is optional but recommended for more stable previews from sites that do not behave well with direct fetches.

Build and run manually:

```bash
go build -o bluebubbles-preview-proxy ./cmd/preview-proxy
./bluebubbles-preview-proxy &
export BB_PREVIEW_PROXY_URL="http://127.0.0.1:8090/preview"
```

Optional proxy environment variables:

```bash
export PREVIEW_PROXY_ADDR="127.0.0.1:8090"
export PREVIEW_OEMBED_ENDPOINT="https://noembed.com/embed"
export PREVIEW_TIMEOUT_SEC=8
export PREVIEW_CACHE_TTL_SEC=21600
```

For local development:

```bash
./scripts/start-gui-with-preview-proxy.sh
```

## AppImage

```bash
./scripts/build-appimage.sh
```

Output:

```text
dist/BlueBubbles-x86_64.AppImage
```

Requirements:

- `appimagetool` in `PATH`
- Linux x86_64 build host

## Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `Ctrl+H` | Split focused pane side by side |
| `Ctrl+J` | Split focused pane top/bottom |
| `Ctrl+W` | Close focused pane |
| `Ctrl+S` | Toggle chat list visibility |
| `Ctrl+N` | Open a new window |
| `Ctrl+O` | Move the focused pane into a new window |

Up to 8 panes can be open at once.

## Message Actions

- Click a chat in the list to load it into the focused pane
- Click the reply action on a message to send a native iMessage reply
- A reply preview appears above the input until you send or cancel it
- Link previews are rendered below messages when enabled
- Reactions can be sent directly from the pane UI

## GUI Menu

The top-right overflow menu exposes the main window actions and appearance controls.

| Item | Action |
|------|--------|
| `Settings...` | Open the full settings dialog |
| `New Window` | Open another GUI window |
| `Move Focused Pane to New Window` | Detach the focused pane into its own window |
| `A+ Larger` | Increase font size |
| `A- Smaller` | Decrease font size |
| `Toggle Bold` | Toggle bold weight for all text |
| `Font ->` | Choose from discovered font families |
| `Enable/Disable Compact Mode` | Toggle tighter spacing |
| `Switch to Light/Dark Mode` | Toggle theme |
| `Disable/Enable Previews` | Toggle URL preview fetching |
| `Max Previews: 1 / 2` | Limit preview cards per message |

Changes apply immediately without restarting.

## Settings Dialog

The settings dialog is organized into four tabs:

- `Appearance`: theme, font family, font size, compact mode, bold text
- `Behavior`: chat list visibility and window actions
- `Previews`: enable previews and set max previews per message
- `Connection`: inspect current server settings and clear the saved password

## Fonts

The font picker combines known system fonts with bundled fallback families.

Bundled families:

- `Lato`
- `Inter`
- `Noto Sans`
- `JetBrains Mono`

`FYNE_SCALE=1.3` or similar is recommended on higher-resolution displays because it improves perceived glyph rendering in Fyne without changing the application logic.

## Architecture

```text
cmd/gui/main.go        entry point: load config, ping server, create GUI app
cmd/preview-proxy      local preview proxy for link metadata
api/                   BlueBubbles REST client
config/                config loading and credential storage
models/                chat and message data structures
ws/                    WebSocket client for real-time updates
gui/                   Fyne UI
```

## Troubleshooting

### Connection fails with "certificate signed by unknown authority"

BlueBubbles commonly uses self-signed HTTPS certificates. The client is designed to work in that setup.

### Contact names are missing

Make sure contacts are present in the BlueBubbles server itself. If the server only has phone numbers, the GUI will too.

### No chats appear

Verify the BlueBubbles server has synced iMessages and that your credentials are correct.

### Messages do not update in real time

Check WebSocket connectivity and firewall rules between the GUI machine and the BlueBubbles server.

### Fonts look jagged

Run the GUI with a higher internal scale, for example:

```bash
FYNE_SCALE=1.3 ./bluebubbles-gui
```
