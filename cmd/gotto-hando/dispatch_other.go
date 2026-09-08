//go:build !darwin && !windows

package main

import (
	"errors"
	"io"

	"github.com/kang-sw/gotto-hando/internal/backend"
	"github.com/kang-sw/gotto-hando/internal/ir"
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

// runBridge has no listener outside windows yet (dispatch.go's
// opts.Bridge branch; the windows named-pipe bridge is 260908-feat-remote-
// ssh Phase 0). Today's abort - message, code, exit - is reproduced
// byte-for-byte via the same abort closure dispatch.go already builds, so
// this is not an observable regression from the inline abort this
// replaced (dispatch.go:89-91 before this ticket).
func runBridge(opts parsedOptions, stdout, stderr io.Writer, abort func(output.ErrorCode, string) int) int {
	return abort(output.EValidate, "session bridge not implemented")
}

// shouldForwardToBridge is always false outside windows - see
// dispatch_darwin.go's identical function for the rationale.
func shouldForwardToBridge(seq *ir.Sequence) bool {
	return false
}

// forwardToBridge is unreachable here (shouldForwardToBridge is always
// false); it exists only so dispatch.go compiles on every GOOS.
func forwardToBridge(opts parsedOptions, seq *ir.Sequence, stdout, stderr io.Writer, abort func(output.ErrorCode, string) int) int {
	return abort(output.EValidate, "session bridge not implemented")
}
