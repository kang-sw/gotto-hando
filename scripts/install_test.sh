#!/usr/bin/env bash
set -euo pipefail
ROOT="$(mktemp -d)"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
INSTALL_DIR="$ROOT/install dir/.local/bin"
SERVER=""
cleanup() {
  status=$?
  if [[ "$status" != 0 ]]; then
    echo "installer fixture failed (exit $status); last installer output:" >&2
    [[ ! -f "$ROOT/output" ]] || cat "$ROOT/output" >&2
    [[ ! -f "$ROOT/server.log" ]] || cat "$ROOT/server.log" >&2
  fi
  if [[ -n "$SERVER" ]]; then kill "$SERVER" 2>/dev/null || true; wait "$SERVER" 2>/dev/null || true; fi
  rm -rf "$ROOT"
}
trap 'echo "installer fixture failed at line $LINENO: $BASH_COMMAND" >&2' ERR
trap cleanup EXIT
case "$(uname -m)" in arm64) ASSET=gotto-hando-darwin-arm64;; x86_64) ASSET=gotto-hando-darwin-amd64;; *) exit 2;; esac
RELEASES="$ROOT/github/kang-sw/gotto-hando/releases/download"
API="$ROOT/api/repos/kang-sw/gotto-hando/releases"
mkdir -p "$RELEASES/v0.1.0" "$RELEASES/v0.1.1" "$API" "$ROOT/shims"
printf 'test binary 0.1.0\n' > "$RELEASES/v0.1.0/$ASSET"
printf 'test binary 0.1.1\n' > "$RELEASES/v0.1.1/$ASSET"
for version in 0.1.0 0.1.1; do (cd "$RELEASES/v$version" && shasum -a 256 "$ASSET" > SHA256SUMS); done
printf '{"tag_name":"v0.1.1"}\n' > "$API/latest"
python3 "$SCRIPT_DIR/testdata/install_server.py" "$ROOT" "$ROOT/port" >"$ROOT/server.log" 2>&1 &
SERVER=$!
for _ in $(seq 1 100); do [[ -s "$ROOT/port" ]] && break; kill -0 "$SERVER"; sleep 0.1; done
[[ -s "$ROOT/port" ]] || { cat "$ROOT/server.log"; exit 1; }
export GOTTO_TEST_ORIGIN="http://127.0.0.1:$(cat "$ROOT/port")"
export GOTTO_TEST_CURL="$(command -v curl)" GOTTO_TEST_REQUESTS="$ROOT/requests"
export GOTTO_TEST_MV="$(command -v mv)" GOTTO_TEST_STAGE_CHECK="$ROOT/stage-checked"
cat > "$ROOT/shims/curl" <<'CURL'
#!/usr/bin/env bash
set -euo pipefail
args=()
for arg in "$@"; do
  case "$arg" in
    https://api.github.com/*) printf '%s\n' "$arg" >> "$GOTTO_TEST_REQUESTS"; arg="$GOTTO_TEST_ORIGIN/api/${arg#https://api.github.com/}";;
    https://github.com/*) printf '%s\n' "$arg" >> "$GOTTO_TEST_REQUESTS"; arg="$GOTTO_TEST_ORIGIN/github/${arg#https://github.com/}";;
  esac
  args+=("$arg")
done
exec "$GOTTO_TEST_CURL" "${args[@]}"
CURL
cat > "$ROOT/shims/mv" <<'MV'
#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == -f && "${2##*/}" == .gotto-hando.* ]]; then
  [[ -x "$2" && "$(stat -f '%Lp' "$2")" == 755 ]]
  [[ "$(dirname "$2")" == "$(dirname "$3")" ]]
  printf 'checked\n' >> "$GOTTO_TEST_STAGE_CHECK"
fi
exec "$GOTTO_TEST_MV" "$@"
MV
chmod +x "$ROOT/shims/curl" "$ROOT/shims/mv"
export PATH="$ROOT/shims:$PATH" GOTTO_HANDO_INSTALL_DIR="$INSTALL_DIR"
unset GOTTO_HANDO_BASE_URL GOTTO_HANDO_REPO
PATH_BEFORE="$PATH"
TARGET="$INSTALL_DIR/gotto-hando"
run_install() { "$SCRIPT_DIR/install.sh" "$@" >"$ROOT/output" 2>&1 || return $?; [[ "$PATH" == "$PATH_BEFORE" ]]; }
check_target() { cmp "$1" "$TARGET"; [[ -x "$TARGET" ]]; [[ "$(stat -f '%Lp' "$TARGET")" == 755 ]]; }
expect_failure() {
  if run_install 0.1.1; then echo "unexpected installation success" >&2; exit 1; fi
  check_target "$ROOT/old"
  [[ "$PATH" == "$PATH_BEFORE" ]]
  [[ -z "$(find "$INSTALL_DIR" -name '.gotto-hando.*' -print)" ]]
}
run_install v0.1.0
check_target "$RELEASES/v0.1.0/$ASSET"
[[ -s "$GOTTO_TEST_STAGE_CHECK" ]]
grep -F 'PATH unchanged' "$ROOT/output" >/dev/null
grep -F "$(printf 'export PATH=%q:' "$INSTALL_DIR")" "$ROOT/output" >/dev/null
run_install 0.1.1
check_target "$RELEASES/v0.1.1/$ASSET"
cp "$TARGET" "$ROOT/old"
for mode in default latest; do
  : > "$GOTTO_TEST_REQUESTS"
  if [[ "$mode" == default ]]; then run_install; else run_install latest; fi
  [[ "$(grep -c '/releases/latest$' "$GOTTO_TEST_REQUESTS")" == 1 ]]
  [[ "$(grep -c "/releases/download/v0.1.1/$ASSET$" "$GOTTO_TEST_REQUESTS")" == 1 ]]
  [[ "$(grep -c '/releases/download/v0.1.1/SHA256SUMS$' "$GOTTO_TEST_REQUESTS")" == 1 ]]
  check_target "$ROOT/old"
done
CURRENT="$RELEASES/v0.1.1"
cp "$CURRENT/SHA256SUMS" "$ROOT/manifest"
printf 'tampered\n' > "$CURRENT/$ASSET"; expect_failure; cp "$ROOT/old" "$CURRENT/$ASSET"
cat "$ROOT/manifest" "$ROOT/manifest" > "$CURRENT/SHA256SUMS"; expect_failure
printf '%064d  other-asset\n' 0 > "$CURRENT/SHA256SUMS"; expect_failure
rm "$CURRENT/SHA256SUMS"; expect_failure; cp "$ROOT/manifest" "$CURRENT/SHA256SUMS"
mv "$CURRENT/$ASSET" "$ROOT/asset"; expect_failure; mv "$ROOT/asset" "$CURRENT/$ASSET"
printf '%s' "$ASSET" > "$ROOT/truncate"; expect_failure; rm "$ROOT/truncate"
rm "$TARGET"; mkdir "$TARGET"; printf 'keep' > "$TARGET/sentinel"
if run_install 0.1.1; then echo 'target directory was accepted' >&2; exit 1; fi
[[ "$(cat "$TARGET/sentinel")" == keep ]]
[[ "$(find "$TARGET" -type f | wc -l | tr -d ' ')" == 1 ]]
echo 'macOS installer fixtures passed: install/update/latest/checksum/duplicate/missing/interrupted/directory/PATH/mode'
