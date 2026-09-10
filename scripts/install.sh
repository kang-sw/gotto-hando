#!/usr/bin/env bash
set -euo pipefail

REPO="${GOTTO_HANDO_REPO:-kang-sw/gotto-hando}"
VERSION="${1:-latest}"
USER_HOME="${HOME:?HOME is required}"
DEST="${GOTTO_HANDO_INSTALL_DIR:-$USER_HOME/.local/bin}"
if [[ "$VERSION" == latest ]]; then
  VERSION="$(curl --fail --silent --show-error "https://api.github.com/repos/$REPO/releases/latest" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"v\{0,1\}\([^"]*\)".*/\1/p' | head -n1)"
fi
VERSION="${VERSION#v}"
[[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]] || { echo "invalid release version: $VERSION" >&2; exit 2; }
case "$(uname -s):$(uname -m)" in
  Darwin:arm64) ASSET=gotto-hando-darwin-arm64;;
  Darwin:x86_64) ASSET=gotto-hando-darwin-amd64;;
  *) echo "unsupported platform (macOS arm64/amd64 required)" >&2; exit 2;;
esac
BASE="https://github.com/$REPO/releases/download/v$VERSION"
TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
curl --fail --silent --show-error --location "$BASE/$ASSET" -o "$TMP/$ASSET"
curl --fail --silent --show-error --location "$BASE/SHA256SUMS" -o "$TMP/SHA256SUMS"
(cd "$TMP" && grep "  $ASSET$" SHA256SUMS | shasum -a 256 -c -)
mkdir -p "$DEST"
TARGET="$DEST/gotto-hando"
chmod 0755 "$TMP/$ASSET"
mv -f "$TMP/$ASSET" "$TARGET"
echo "installed gotto-hando $VERSION at $TARGET"
echo "if a bridge is running, restart it manually with: gotto-hando --bridge" >&2
case ":${PATH:-}:" in *":$DEST:"*) ;; *) echo "warning: $DEST is not on PATH; add it in your shell profile (PATH unchanged)" >&2;; esac
