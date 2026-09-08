//go:build !darwin && !windows

package main

import (
	"errors"
	"io"

	"github.com/kang-sw/gotto-hando/internal/backend"
	"github.com/kang-sw/gotto-hando/internal/output"
)

// newLocalBackend has no real implementation outside darwin/windows yet
// (remote is a separate ticket - 260908-feat-remote-ssh). The error text
// matches the stub message dispatch.go printed before the darwin/windows
// tickets, so every other GOOS's abort is unchanged.
func newLocalBackend() (backend.Backend, error) {
	return nil, errors.New("platform backend not implemented, nothing ran")
}

// requestPerms is macOS-only (help.txt --request-perms :106-108): every
// other OS treats it as a usage error, abort E_VALIDATE / exit 2 before
// anything runs.
func requestPerms(opts parsedOptions, stdout, stderr io.Writer, abort func(output.ErrorCode, string) int) int {
	return abort(output.EValidate, "--request-perms is macOS only")
}
