#!/usr/bin/env bash
set -euo pipefail

VERSION="${1:-}"
OUT_DIR="${2:-dist}"
if [[ -z "$VERSION" || "$VERSION" == -* ]]; then
  echo "usage: $0 VERSION [OUTPUT_DIR]" >&2
  exit 2
fi
if [[ "$VERSION" == v* ]]; then VERSION="${VERSION#v}"; fi
if [[ ! "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]]; then
  echo "invalid SemVer: $VERSION" >&2
  exit 2
fi

mkdir -p "$OUT_DIR"
rm -f "$OUT_DIR"/gotto-hando-* "$OUT_DIR"/SHA256SUMS "$OUT_DIR"/RELEASE-MANIFEST
rm -f "$OUT_DIR"/install.sh "$OUT_DIR"/install.ps1
SOURCE_VERSION="$(sed -n '1s/^gotto-hando \([^ ]*\).*/\1/p' assets/help.txt)"
[[ "$SOURCE_VERSION" == "$VERSION" ]] || { echo "assets/help.txt reports $SOURCE_VERSION, expected $VERSION" >&2; exit 1; }

build_one() {
  local goos="$1" goarch="$2" suffix="$3"
  local name="gotto-hando-${suffix}"
  [[ "$goos" == windows ]] && name+=".exe"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -trimpath -o "$OUT_DIR/$name" ./cmd/gotto-hando
}

build_one darwin arm64 darwin-arm64
build_one darwin amd64 darwin-amd64
build_one windows amd64 windows-amd64

# Execute the artifact matching this host; the other targets are cross-builds.
case "$(uname -s):$(uname -m)" in
  Darwin:arm64) NATIVE_ASSET=gotto-hando-darwin-arm64;;
  Darwin:x86_64) NATIVE_ASSET=gotto-hando-darwin-amd64;;
  *) NATIVE_ASSET="";;
esac
if [[ -n "$NATIVE_ASSET" ]]; then
  ACTUAL_VERSION="$("$OUT_DIR/$NATIVE_ASSET" --version)"
  [[ "$ACTUAL_VERSION" == "$VERSION" ]] || { echo "$NATIVE_ASSET reports $ACTUAL_VERSION, expected $VERSION" >&2; exit 1; }
fi

cp scripts/install.sh scripts/install.ps1 "$OUT_DIR/"
(cd "$OUT_DIR" && shasum -a 256 gotto-hando-* install.sh install.ps1 > SHA256SUMS)
{
  echo "version=$VERSION"
  echo "commit=$(git rev-parse HEAD)"
  echo "go=$(go version)"
  cat "$OUT_DIR/SHA256SUMS"
} > "$OUT_DIR/RELEASE-MANIFEST"
echo "packaged $VERSION in $OUT_DIR"
