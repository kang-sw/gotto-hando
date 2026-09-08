#!/usr/bin/env bash
# Sign the local gotto-hando binary with the stable self-signed
# "gotto-hando-dev" identity so a TCC grant survives rebuilds
# (help-macos.txt STABLE SIGNING IDENTITY). Create the identity once in
# Keychain Access (Certificate Assistant > Create a Certificate, Name
# "gotto-hando-dev", Self Signed Root, Code Signing); then run this after
# every build. Matters only where the binary itself is the responsible
# process (a LaunchAgent bridge); terminal use inherits the terminal app's
# grant and needs no signing.
set -euo pipefail

BIN="${1:-./gotto-hando}"

codesign -s "gotto-hando-dev" --force "$BIN"
codesign -dv "$BIN"   # shows Authority=gotto-hando-dev
