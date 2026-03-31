# BlueBubbles GUI - QA and Release Checklist

## First-run setup

- Start GUI with no `BB_SERVER_URL`, no `BB_PASSWORD`, and no existing config.
- Verify first-run wizard opens automatically.
- Enter invalid URL and confirm validation error is shown.
- Enter valid server URL + wrong password and confirm connection error is shown.
- Enter valid credentials, test connection, save, and confirm main GUI opens.
- Restart app and confirm wizard is skipped.

## Credential storage

- Verify password is loaded from keyring when available.
- Verify fallback to `~/.config/bluebubbles-tui/bluebubbles.yaml` works when keyring is unavailable.
- Use Settings -> Connection -> Clear saved password.
- Restart app and confirm wizard opens again.

## Core chat flow

- Select a chat and confirm loading state appears briefly.
- Confirm messages render and scroll to bottom.
- Send a message and verify optimistic update appears immediately.
- Confirm transient `Message queued` feedback appears in the input box.
- Simulate send failure and confirm optimistic message is removed and `Send failed` appears.
- Reply to a message and confirm reply chip appears and can be cancelled.

## Layout and polish

- Confirm no visible scrollbars are rendered in message view or chat list.
- Confirm pane separators are not visible by default.
- Confirm floating input box is indented, padded, and does not cover the last message.
- Confirm dark and light mode both remain readable.
- Confirm compact mode still works.
- Confirm font family and font size changes apply immediately.

## Multi-pane behavior

- Split horizontally and vertically.
- Close focused pane and confirm focus moves correctly.
- Move focused pane to a new window.
- Restart GUI and confirm pane layout restores.

## Packaging

- Run `./scripts/install.sh` and verify launcher + icon are installed.
- Launch from desktop launcher and confirm GUI starts with preview proxy env configured.
- Run `./scripts/build-appimage.sh` on a machine with `appimagetool` installed.
- Launch the AppImage and confirm first-run wizard still appears when credentials are missing.

## Release gate

- Run `go test ./...`.
- Run `go vet ./...`.
- Run `bash -n scripts/install.sh`.
- Run `bash -n scripts/build-appimage.sh`.
- Run `go build -o bluebubbles-gui ./cmd/gui/`.
- Smoke-test one real server connection before release.
