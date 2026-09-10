#!/usr/bin/env bash
set -euo pipefail
if [[ "$(go env GOOS)" != windows ]]; then
  exec go vet ./...
fi
# Native clipboard HGLOBAL handles necessarily cross uintptr -> unsafe.Pointer.
# Keep every analyzer elsewhere, and only exempt unsafeptr in this FFI package.
packages=()
while IFS= read -r package; do
  [[ "$package" == github.com/kang-sw/gotto-hando/internal/backend/windows ]] || packages+=("$package")
done < <(go list ./...)
go vet "${packages[@]}"
go vet -unsafeptr=false ./internal/backend/windows
