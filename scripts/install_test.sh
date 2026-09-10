#!/usr/bin/env bash
set -euo pipefail
ROOT="$(mktemp -d)"
ASSET_DIR="$ROOT/v0.1.0"
INSTALL_DIR="$ROOT/install/.local/bin"
mkdir -p "$ASSET_DIR"
printf 'test binary 0.1.0\n' > "$ASSET_DIR/gotto-hando-darwin-arm64"
(cd "$ASSET_DIR" && shasum -a 256 gotto-hando-darwin-arm64 > SHA256SUMS)
PORT_FILE="$ROOT/port"
python3 -c 'import http.server, socketserver, pathlib, sys, os; d=sys.argv[1]; p=socketserver.TCPServer(("127.0.0.1",0),http.server.SimpleHTTPRequestHandler); pathlib.Path(sys.argv[2]).write_text(str(p.server_address[1])); os.chdir(d); p.serve_forever()' "$ROOT" "$PORT_FILE" &
SERVER=$!
trap 'kill "$SERVER" 2>/dev/null || true' EXIT
for _ in $(seq 1 50); do [[ -s "$PORT_FILE" ]] && break; sleep 0.1; done
BASE="http://127.0.0.1:$(cat "$PORT_FILE")/v0.1.0"
GOTTO_HANDO_BASE_URL="$BASE" GOTTO_HANDO_INSTALL_DIR="$INSTALL_DIR" scripts/install.sh 0.1.0 >/dev/null
cmp "$ASSET_DIR/gotto-hando-darwin-arm64" "$INSTALL_DIR/gotto-hando"
test -x "$INSTALL_DIR/gotto-hando"
printf 'bad\n' > "$ASSET_DIR/gotto-hando-darwin-arm64"
if GOTTO_HANDO_BASE_URL="$BASE" GOTTO_HANDO_INSTALL_DIR="$INSTALL_DIR" scripts/install.sh 0.1.0 >/dev/null 2>&1; then exit 1; fi
test "$(cat "$INSTALL_DIR/gotto-hando")" = "test binary 0.1.0"
echo "installer fixture tests passed"
