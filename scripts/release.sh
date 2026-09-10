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

build_one() {
  local goos="$1" goarch="$2" suffix="$3"
  local name="gotto-hando-${suffix}"
  [[ "$goos" == windows ]] && name+=".exe"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -trimpath -o "$OUT_DIR/$name" ./cmd/gotto-hando
  local got
  got="$(sed -n '1s/^gotto-hando \([^ ]*\).*/\1/p' assets/help.txt)"
  [[ "$got" == "$VERSION" ]] || { echo "assets/help.txt reports $got, expected $VERSION" >&2; exit 1; }
}

build_one darwin arm64 darwin-arm64
build_one darwin amd64 darwin-amd64
build_one windows amd64 windows-amd64

(cd "$OUT_DIR" && shasum -a 256 gotto-hando-* > SHA256SUMS)
cp scripts/install.sh scripts/install.ps1 "$OUT_DIR/"
{
  echo "version=$VERSION"
  echo "commit=$(git rev-parse HEAD)"
  echo "go=$(go version)"
  cat "$OUT_DIR/SHA256SUMS"
} > "$OUT_DIR/RELEASE-MANIFEST"
echo "packaged $VERSION in $OUT_DIR"
