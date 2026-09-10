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
BASE="${GOTTO_HANDO_BASE_URL:-$BASE}"
TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
curl --fail --silent --show-error --location "$BASE/$ASSET" -o "$TMP/$ASSET"
curl --fail --silent --show-error --location "$BASE/SHA256SUMS" -o "$TMP/SHA256SUMS"
(cd "$TMP" && awk -v a="$ASSET" '$2 == a { n++; row=$0 } END { if (n != 1) exit 1; print row }' SHA256SUMS | shasum -a 256 -c -)
mkdir -p "$DEST"
TARGET="$DEST/gotto-hando"
chmod 0755 "$TMP/$ASSET"
STAGE="$(mktemp "$DEST/.gotto-hando.XXXXXX")"
trap 'rm -rf "$TMP" "$STAGE"' EXIT
cp "$TMP/$ASSET" "$STAGE"
chmod 0755 "$STAGE"
mv -f "$STAGE" "$TARGET"
echo "installed gotto-hando $VERSION at $TARGET"
echo "if a bridge is running, restart it manually with: gotto-hando --bridge" >&2
case ":${PATH:-}:" in *":$DEST:"*) ;; *) echo "warning: $DEST is not on PATH (PATH unchanged); add \"export PATH=\"\$HOME/.local/bin:\$PATH\"\" to ~/.zshrc or ~/.bashrc" >&2;; esac
