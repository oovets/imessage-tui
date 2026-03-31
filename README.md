# BlueBubbles — iMessage Client

A real-time iMessage client for BlueBubbles with two frontends: a terminal UI (TUI) and a windowed GUI. Both share the same backend packages (`api/`, `ws/`, `models/`, `config/`) and produce separate binaries.

---

## Features

- Browse and read iMessage conversations with contact names
- Real-time delivery via WebSocket (Socket.IO) with auto-reconnect
- Unread indicators — chats with new messages are highlighted and moved to the top
- Split-window layout — view multiple conversations side by side or stacked
- Toggle chat list visibility
- Clickable links in messages (`http(s)`, `www.`, `mailto`, email addresses)
- Basic attachment/media rendering (inline image preview when URI is available)
- Link previews with title/metadata (toggleable in the GUI overflow menu)

---

## Prerequisites

- Go 1.24+
- BlueBubbles server running on macOS with iMessage synced
- Network access to your BlueBubbles server (HTTP/HTTPS)

---

## Configuration

Set environment variables or create a config file.

### Environment Variables

```bash
export BB_SERVER_URL="https://xxx.xxx.xxx.xxx:1234"
export BB_PASSWORD="your-api-password"
```

### Config File (optional)

`~/.config/bluebubbles-tui/bluebubbles.yaml`:

```yaml
server_url: "https://xxx.xxx.xxx.xxx:1234"
password: "your-api-password"
message_limit: 50
chat_limit: 50
enable_link_previews: true
max_previews_per_message: 2
# Optional backend proxy/oEmbed endpoints for stable social previews.
preview_proxy_url: ""
oembed_endpoint: "https://noembed.com/embed"
```

Optional environment variables:

```bash
export BB_ENABLE_LINK_PREVIEWS=true
export BB_MAX_PREVIEWS_PER_MESSAGE=2
export BB_PREVIEW_PROXY_URL=""
export BB_OEMBED_ENDPOINT="https://noembed.com/embed"
```

---

## Quick Install (recommended)

Run the install script once to build everything, install the preview proxy as a systemd user service, and create the desktop launcher:

```bash
./scripts/install.sh
```

Re-run any time after `git pull` to rebuild and update. Safe to run multiple times.

Optional env overrides:

```bash
FYNE_SCALE=1.5 ./scripts/install.sh        # override HiDPI scale (default 1.3)
PREVIEW_PROXY_ADDR=127.0.0.1:8091 ./scripts/install.sh
```

---

## Manual Build

```bash
go build -o bluebubbles-tui .            # terminal UI
go build -o bluebubbles-gui ./cmd/gui/  # windowed GUI
go build -o bluebubbles-preview-proxy ./cmd/preview-proxy/ # local preview proxy
```

### Preview Proxy (optional, recommended for IG/FB stability)

The install script handles this automatically. For manual setup:

```bash
# Build and start the proxy once
go build -o bluebubbles-preview-proxy ./cmd/preview-proxy/
./bluebubbles-preview-proxy &

# Point the GUI at it
export BB_PREVIEW_PROXY_URL="http://127.0.0.1:8090/preview"
```

Optional proxy env vars:

```bash
export PREVIEW_PROXY_ADDR="127.0.0.1:8090"
export PREVIEW_OEMBED_ENDPOINT="https://noembed.com/embed"
export PREVIEW_TIMEOUT_SEC=8
export PREVIEW_CACHE_TTL_SEC=21600
```

### Autostart With systemd (user)

`scripts/install.sh` handles this automatically. For manual setup:

```bash
mkdir -p ~/.config/systemd/user
# The service file uses %h (home dir) — copy as-is, no edits needed
cp systemd/bluebubbles-preview-proxy.service ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now bluebubbles-preview-proxy.service
systemctl --user status bluebubbles-preview-proxy.service
```

---

## TUI

### Usage

```bash
./bluebubbles-tui
```

Logs to `~/.bluebubbles-tui.log`.

### Keyboard Shortcuts

#### Navigation

| Key | Action |
|-----|--------|
| `Tab` | Toggle focus between chat list and current window |
| `Escape` | Return to chat list |
| `← / →` | Move between windows |
| `Ctrl+↑ / Ctrl+↓` | Move to window above/below |
| `↑ / ↓` or `k / j` | Navigate chat list / scroll messages |
| `g` (chat list) | Jump to top |
| `G` (chat list) | Jump to bottom |
| `Enter` (chat list) | Open chat in focused window |
| `Enter` (input) | Send message |
| `Shift+Enter` (input) | New line |

#### Split Windows

| Key | Action |
|-----|--------|
| `Ctrl+F` | Split focused window horizontally |
| `Ctrl+G` | Split focused window vertically |
| `Ctrl+W` | Close focused window |

Up to 4 windows can be open at once.

#### Toggles

| Key | Action |
|-----|--------|
| `Ctrl+S` | Toggle chat list visibility |
| `Ctrl+T` | Toggle message timestamps |
| `q` / `Ctrl+C` | Quit |

---

### Usage

```bash
./bluebubbles-gui
```

Logs to `~/.bluebubbles-gui.log`.

### Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `Ctrl+H` | Split focused pane side by side |
| `Ctrl+J` | Split focused pane top/bottom |
| `Ctrl+W` | Close focused pane |
| `Ctrl+S` | Toggle chat list visibility |
| `Ctrl+N` | New window |
| `Ctrl+O` | Move focused pane to a new window |

Up to 8 panes can be open at once. Click in a pane's input field to focus it, then select a chat from the list to load it into that pane.

Reply to a specific message by clicking the `↩` button on that message. A reply preview appears above the input; send to post a native iMessage reply (threaded), or click `X` on the preview to cancel.

`FYNE_SCALE=1.3` renders at 130% internal resolution. This is the recommended workaround for Fyne's grayscale font anti-aliasing on 1440p displays — it makes glyphs noticeably smoother without changing the perceived window size much.

### Fonts

The GUI font picker shows installed families from known system paths, with graceful fallback to Fyne's built-in sans-serif.

Included families in this app:

- `Lato`
- `Inter`
- `Noto Sans`
- `JetBrains Mono Nerd Font`
- `Geist`

Install on Arch (examples):

```bash
sudo pacman -S ttf-lato
sudo pacman -S ttf-jetbrains-mono-nerd
# Geist via AUR (pick one):
yay -S ttf-geist
# or:
yay -S otf-geist
fc-cache -f
```

### GUI Menu

Appearance and window actions live under the small overflow button in the top-right corner:

| Item | Action |
|------|--------|
| New Window | Open another GUI window |
| Move Focused Pane to New Window | Detach the focused pane into its own window |
| A+ Larger | Increase font size (max 20) |
| A- Smaller | Decrease font size (min 6) |
| Toggle Bold | Toggle bold weight for all text |
| Font → | Submenu listing installed font families |
| Hide/Show Pane Separators | Toggle split-pane dividers |
| Enable/Disable Compact Mode | Toggle compact spacing |
| Switch to Light/Dark Mode | Toggle between dark and light theme |
| Disable/Enable Previews | Toggle URL preview fetching |
| Max Previews: 1 / 2 | Limit preview cards per message for performance |

Changes apply instantly without restarting.

### GUI Architecture

```
cmd/gui/main.go      — Entry point (config → ping → wsClient → gui.NewApp().Run())
gui/app.go           — App struct, layout wiring, overflow menu, WebSocket event loop
gui/chatlist.go      — Left-side chat list (widget.List)
gui/messages.go      — Message thread (VScroll + VBox of labels)
gui/input.go         — Input area (Entry + Send button)
gui/pane.go          — Single chat pane (messages + input)
gui/panemanager.go   — Binary tree of panes, split/close logic
gui/theme.go         — Mutable theme: dark/light, font size, font family, bold
gui/util.go          — stripEmojis(), formatMessageTime(), truncateString()
```

### Fyne Themes

Fyne's theming system is interface-based. Any type that implements `fyne.Theme` can be passed to `fyneApp.Settings().SetTheme()`. The interface has four methods:

```go
type Theme interface {
    Color(name ThemeColorName, variant ThemeVariant) color.Color
    Font(style TextStyle) Resource
    Icon(name ThemeIconName) Resource
    Size(name ThemeSizeName) float32
}
```

Rather than embedding a base theme, `compactTheme` implements all four methods explicitly and delegates to a `base()` method that returns either `theme.DarkTheme()` or `theme.LightTheme()` depending on the `dark` field. This makes live switching between dark and light straightforward — just flip the field and call `Settings().SetTheme()` again.

```go
type compactTheme struct {
    dark        bool
    fontSize    float32
    boldAll     bool
    compactMode bool
    fonts       map[string]fontSet
    curFamily   string
}

func (t *compactTheme) base() fyne.Theme {
    if t.dark { return theme.DarkTheme() }
    return theme.LightTheme()
}
```

Calling `Settings().SetTheme()` with a mutated instance of the same struct broadcasts a theme-change event to all widgets, causing an immediate full re-render.

**`Font(style fyne.TextStyle) fyne.Resource`** — called every time Fyne renders text. The `boldAll` flag forces `style.Bold = true` before the font lookup, making every widget use its bold variant regardless of what style it requested.

**`Size(name fyne.ThemeSizeName) float32`** — controls spacing and text size. Key names:

| Constant | Affects |
|---|---|
| `theme.SizeNamePadding` | Outer padding around widgets |
| `theme.SizeNameInnerPadding` | Inner padding (e.g. inside buttons) |
| `theme.SizeNameText` | Body text size |
| `theme.SizeNameSubHeadingText` | Sub-heading text size |
| `theme.SizeNameScrollBar` | Scroll bar width (set to 0 to hide) |
| `theme.SizeNameScrollBarSmall` | Scroll bar width when inactive (set to 0 to hide) |

This app sets both scroll bar sizes to 0 — scrollbars are invisible but scroll still works via mouse wheel and touchpad.

---

## Architecture (shared backend)

| Package | Purpose |
|---|---|
| `models/types.go` | Data structures (Chat, Message, Handle) |
| `api/client.go` | REST API client for BlueBubbles server |
| `ws/client.go` | WebSocket client for real-time updates (Socket.IO) |
| `config/config.go` | Configuration loading |
| `tui/` | Bubble Tea terminal UI |
| `gui/` | Fyne windowed UI |

---

## Troubleshooting

### Connection fails with "certificate signed by unknown authority"
BlueBubbles uses self-signed HTTPS certificates. The client skips TLS verification — this is expected.

### Seeing phone numbers instead of contact names
Ensure contacts are synced in your BlueBubbles server. Check the web interface at your server URL. If contacts aren't there, names won't appear.

### No chats showing
Verify your BlueBubbles server has synced iMessages. Check credentials and restart BlueBubbles if needed.

### Message sending fails
Make sure a chat is selected and the input box is focused. Check the log file for API errors.

### Messages not updating in real-time
WebSocket connection may have failed. Check network/firewall rules between the client and BlueBubbles server.

### GUI fonts look jagged
Use `FYNE_SCALE=1.3` (or higher) when launching. Fyne uses grayscale anti-aliasing only — sub-pixel rendering (ClearType) is not available. A higher scale renders glyphs at higher internal resolution, which reduces jaggedness at the cost of slightly larger UI elements.
