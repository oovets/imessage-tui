# Slack GUI (bluebubbles-tui)

Slack-klienten är nu separerad under `slack/`:

- `slack/api` — Slack API-klient (channels, messages, threads, replies, media)
- `slack/gui` — Fyne-baserad GUI med split panes och sticky focus
- `cmd/slack-gui` — binär entrypoint

## Krav

- Go 1.24+
- En Slack bot-token med scopes för kanaler, historik, postning och users-listning

## Konfiguration

Slack GUI kan använda samma credentials-filformat som `slack_rust` (`slack_config.json` med `workspaces` + `active_workspace`).

Prioritet för token:

1. `SLACK_BOT_TOKEN`
2. `SLACK_TOKEN`
3. `slack_config.json` (sökvägar nedan)

```bash
export SLACK_BOT_TOKEN="xoxb-..."
# Valfritt:
export SLACK_API_BASE_URL="https://slack.com/api"
# Om config-filen ligger på annan plats:
export SLACK_CONFIG_PATH="/path/to/slack_config.json"
```

Automatiskt sökta config-paths:

- `~/.config/slack_rust/slack_config.json`
- `~/.config/slack/slack_config.json`
- `~/Code/slack_rust/config/slack_config.json`
- `<repo>/config/slack_config.json`
- `<repo>/../slack_rust/config/slack_config.json`

## Build + Run

```bash
go build -o slack-gui ./cmd/slack-gui/
./slack-gui
```

Logg skrivs till `~/.slack-gui.log`.

## Funktioner

- Kanal-lista + filter
- Split panes (horisontell/vertikal)
- Sticky focus per pane
- Threads (`🧵`)
- Replies (`↩`) med reply-target i input
- Media/file-rader och bildförhandsvisning
- Persistens av fönsterstorlek, compact mode, channel-list och split-layout

## Shortcuts

| Key | Action |
|-----|--------|
| `Ctrl+H` | Split focused pane side by side |
| `Ctrl+J` | Split focused pane top/bottom |
| `Ctrl+W` | Close focused pane |
| `Ctrl+S` | Toggle channel list |
| `Ctrl+N` | New window |
