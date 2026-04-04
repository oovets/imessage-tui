#!/usr/bin/env bash
set -euo pipefail

# Start BlueBubbles GUI with local preview proxy.
# Usage:
#   scripts/start-gui-with-preview-proxy.sh
#   PREVIEW_PROXY_ADDR=127.0.0.1:8091 scripts/start-gui-with-preview-proxy.sh

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

PREVIEW_PROXY_ADDR="${PREVIEW_PROXY_ADDR:-127.0.0.1:8090}"
PREVIEW_OEMBED_ENDPOINT="${PREVIEW_OEMBED_ENDPOINT:-https://noembed.com/embed}"
PREVIEW_TIMEOUT_SEC="${PREVIEW_TIMEOUT_SEC:-8}"
PREVIEW_CACHE_TTL_SEC="${PREVIEW_CACHE_TTL_SEC:-21600}"
BB_ENABLE_LINK_PREVIEWS="${BB_ENABLE_LINK_PREVIEWS:-true}"
BB_MAX_PREVIEWS_PER_MESSAGE="${BB_MAX_PREVIEWS_PER_MESSAGE:-2}"

PROXY_URL="http://${PREVIEW_PROXY_ADDR}/preview"

echo "[1/4] Building preview proxy..."
go build -o bluebubbles-preview-proxy ./cmd/preview-proxy/

echo "[2/4] Building GUI..."
go build -o bluebubbles-gui ./cmd/gui/

echo "[3/4] Starting preview proxy on ${PREVIEW_PROXY_ADDR}..."
PREVIEW_PROXY_ADDR="$PREVIEW_PROXY_ADDR" \
PREVIEW_OEMBED_ENDPOINT="$PREVIEW_OEMBED_ENDPOINT" \
PREVIEW_TIMEOUT_SEC="$PREVIEW_TIMEOUT_SEC" \
PREVIEW_CACHE_TTL_SEC="$PREVIEW_CACHE_TTL_SEC" \
"$ROOT_DIR/bluebubbles-preview-proxy" >/tmp/bluebubbles-preview-proxy.log 2>&1 &
PROXY_PID=$!

cleanup() {
  if kill -0 "$PROXY_PID" >/dev/null 2>&1; then
    kill "$PROXY_PID" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT INT TERM

for _ in {1..30}; do
  if curl -fsS "http://${PREVIEW_PROXY_ADDR}/healthz" >/dev/null 2>&1; then
    break
  fi
  sleep 0.2
done

if ! curl -fsS "http://${PREVIEW_PROXY_ADDR}/healthz" >/dev/null 2>&1; then
  echo "Preview proxy did not become healthy. Check /tmp/bluebubbles-preview-proxy.log"
  exit 1
fi

echo "[4/4] Launching GUI with preview proxy: ${PROXY_URL}"
BB_ENABLE_LINK_PREVIEWS="$BB_ENABLE_LINK_PREVIEWS" \
BB_MAX_PREVIEWS_PER_MESSAGE="$BB_MAX_PREVIEWS_PER_MESSAGE" \
BB_PREVIEW_PROXY_URL="$PROXY_URL" \
"$ROOT_DIR/bluebubbles-gui"
