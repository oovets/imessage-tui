#!/usr/bin/env bash
# install.sh — full setup for BlueBubbles GUI
#
# What this script does:
#   1. Builds the GUI and preview-proxy binaries
#   2. Installs the preview-proxy systemd user service
#   3. Enables + starts the proxy service
#   4. Creates/updates the .desktop launcher file
#   5. Refreshes the desktop database so app launchers pick it up
#
# Safe to re-run — everything is idempotent.
#
# Usage:
#   ./scripts/install.sh
#   FYNE_SCALE=1.5 ./scripts/install.sh   # override HiDPI scale

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

# ── Configurable ──────────────────────────────────────────────────────────────
FYNE_SCALE="${FYNE_SCALE:-1.3}"
PREVIEW_PROXY_ADDR="${PREVIEW_PROXY_ADDR:-127.0.0.1:8090}"
# ──────────────────────────────────────────────────────────────────────────────

PROXY_URL="http://${PREVIEW_PROXY_ADDR}/preview"
SYSTEMD_USER_DIR="$HOME/.config/systemd/user"
SERVICE_NAME="bluebubbles-preview-proxy.service"
DESKTOP_DIR="$HOME/.local/share/applications"
DESKTOP_FILE="$DESKTOP_DIR/bluebubbles-gui.desktop"
ICON_DIR="$HOME/.local/share/icons/hicolor/scalable/apps"
ICON_FILE="$ICON_DIR/bluebubbles-gui.svg"

step() { echo; echo "── $* ──────────────────────────────────────────────"; }

# ── 1. Build ──────────────────────────────────────────────────────────────────
step "1/5  Building binaries"
echo "  → bluebubbles-gui"
go build -o "$ROOT_DIR/bluebubbles-gui" ./cmd/gui/
echo "  → bluebubbles-preview-proxy"
go build -o "$ROOT_DIR/bluebubbles-preview-proxy" ./cmd/preview-proxy/
echo "  ✓ GUI binaries built"

# ── 2. Systemd service ────────────────────────────────────────────────────────
step "2/5  Installing systemd user service"
mkdir -p "$SYSTEMD_USER_DIR"

# Rewrite the service file with the current repo path (handles moved repos)
sed \
  -e "s|%h/Code/bluebubbles-gui|$ROOT_DIR|g" \
  "$ROOT_DIR/systemd/$SERVICE_NAME" \
  > "$SYSTEMD_USER_DIR/$SERVICE_NAME"

echo "  ✓ Service written to $SYSTEMD_USER_DIR/$SERVICE_NAME"

# ── 3. Enable + start proxy ───────────────────────────────────────────────────
step "3/5  Enabling preview-proxy service"
systemctl --user daemon-reload
systemctl --user enable "$SERVICE_NAME"

if systemctl --user is-active --quiet "$SERVICE_NAME"; then
  echo "  ↺ Restarting running service..."
  systemctl --user restart "$SERVICE_NAME"
else
  echo "  ▶ Starting service..."
  systemctl --user start "$SERVICE_NAME"
fi

# Brief wait + health check
for _ in {1..20}; do
  if curl -fsS "http://${PREVIEW_PROXY_ADDR}/healthz" >/dev/null 2>&1; then
    break
  fi
  sleep 0.3
done

if curl -fsS "http://${PREVIEW_PROXY_ADDR}/healthz" >/dev/null 2>&1; then
  echo "  ✓ Preview proxy healthy at $PROXY_URL"
else
  echo "  ⚠ Preview proxy did not respond on $PREVIEW_PROXY_ADDR"
  echo "    Check: systemctl --user status $SERVICE_NAME"
  echo "    Logs:  journalctl --user -u $SERVICE_NAME -n 30"
fi

# ── 4. Desktop launcher ───────────────────────────────────────────────────────
step "4/5  Creating desktop launcher"
mkdir -p "$DESKTOP_DIR"
mkdir -p "$ICON_DIR"

cp "$ROOT_DIR/packaging/bluebubbles-gui.svg" "$ICON_FILE"

sed \
  -e "s|^Exec=.*$|Exec=env FYNE_SCALE=${FYNE_SCALE} BB_PREVIEW_PROXY_URL=${PROXY_URL} ${ROOT_DIR}/bluebubbles-gui|" \
  -e "s|^Icon=.*$|Icon=bluebubbles-gui|" \
  "$ROOT_DIR/packaging/bluebubbles-gui.desktop" \
  > "$DESKTOP_FILE"

echo "  ✓ Desktop file written to $DESKTOP_FILE"
echo "  ✓ Icon written to $ICON_FILE"

# ── 5. Refresh app launcher index ─────────────────────────────────────────────
step "5/5  Refreshing desktop database"
if command -v update-desktop-database &>/dev/null; then
  update-desktop-database "$DESKTOP_DIR"
  echo "  ✓ Desktop database updated"
else
  echo "  ⚠ update-desktop-database not found — skipping (Walker may need a restart)"
fi

# ── Done ──────────────────────────────────────────────────────────────────────
echo
echo "╔══════════════════════════════════════════════════════════════════╗"
echo "║  BlueBubbles is ready                                           ║"
echo "║                                                                  ║"
echo "║  Launch from Walker/Rofi/any launcher:  BlueBubbles             ║"
echo "║  Or run directly:                                                ║"
printf "║    %-62s║\n" "FYNE_SCALE=${FYNE_SCALE} BB_PREVIEW_PROXY_URL=${PROXY_URL} \\"
printf "║    %-62s║\n" "${ROOT_DIR}/bluebubbles-gui"
echo "║                                                                  ║"
echo "║  Preview proxy:  systemctl --user status $SERVICE_NAME  ║"
echo "║  Logs:           ~/.bluebubbles-gui.log                         ║"
echo "╚══════════════════════════════════════════════════════════════════╝"
