#!/usr/bin/env bash
# build-appimage.sh - package BlueBubbles GUI as an AppImage
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

APP_NAME="BlueBubbles"
APP_ID="bluebubbles-gui"
APPDIR="$ROOT_DIR/dist/${APP_NAME}.AppDir"
OUTPUT_DIR="$ROOT_DIR/dist"
FYNE_SCALE="${FYNE_SCALE:-1.3}"
PREVIEW_PROXY_URL="${BB_PREVIEW_PROXY_URL:-http://127.0.0.1:8090/preview}"

if ! command -v appimagetool >/dev/null 2>&1; then
  echo "appimagetool is required."
  echo "Download from: https://github.com/AppImage/AppImageKit/releases"
  exit 1
fi

echo "==> Building GUI binary"
go build -o "$ROOT_DIR/bluebubbles-gui" ./cmd/gui/

echo "==> Preparing AppDir"
rm -rf "$APPDIR"
mkdir -p "$APPDIR/usr/bin" "$APPDIR/usr/share/applications" "$APPDIR/usr/share/icons/hicolor/scalable/apps"

cp "$ROOT_DIR/bluebubbles-gui" "$APPDIR/usr/bin/bluebubbles-gui"
cp "$ROOT_DIR/packaging/bluebubbles-gui.svg" "$APPDIR/usr/share/icons/hicolor/scalable/apps/bluebubbles-gui.svg"
cp "$ROOT_DIR/packaging/bluebubbles-gui.desktop" "$APPDIR/usr/share/applications/bluebubbles-gui.desktop"

cat > "$APPDIR/AppRun" <<EOF
#!/usr/bin/env bash
set -euo pipefail
HERE=\"\$(dirname \"\$(readlink -f \"\$0\")\")\"
export FYNE_SCALE=${FYNE_SCALE}
export BB_PREVIEW_PROXY_URL=${PREVIEW_PROXY_URL}
exec \"\$HERE/usr/bin/bluebubbles-gui\" \"\$@\"
EOF
chmod +x "$APPDIR/AppRun"

ln -sf "usr/share/applications/bluebubbles-gui.desktop" "$APPDIR/bluebubbles-gui.desktop"
ln -sf "usr/share/icons/hicolor/scalable/apps/bluebubbles-gui.svg" "$APPDIR/bluebubbles-gui.svg"

mkdir -p "$OUTPUT_DIR"

echo "==> Building AppImage"
appimagetool "$APPDIR" "$OUTPUT_DIR/${APP_NAME}-x86_64.AppImage"

echo
echo "Done: $OUTPUT_DIR/${APP_NAME}-x86_64.AppImage"
