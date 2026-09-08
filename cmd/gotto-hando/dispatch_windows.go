//go:build windows

package main

import (
	"io"

	"github.com/kang-sw/gotto-hando/internal/backend"
	winbackend "github.com/kang-sw/gotto-hando/internal/backend/windows"
	"github.com/kang-sw/gotto-hando/internal/output"
)

// newLocalBackend constructs the real windows backend (dispatch.go's
// dest=="local" path). See dispatch_darwin.go for the darwin equivalent
// and dispatch_other.go for every remaining GOOS (remote is a separate
// ticket).
func newLocalBackend() (backend.Backend, error) {
	return winbackend.New()
}

// requestPerms is macOS-only (help.txt --request-perms :106-108): on
// Windows it is a usage error, abort E_VALIDATE / exit 2 before anything
// runs ("--request-perms is macOS only").
func requestPerms(opts parsedOptions, stdout, stderr io.Writer, abort func(output.ErrorCode, string) int) int {
	return abort(output.EValidate, "--request-perms is macOS only")
}
