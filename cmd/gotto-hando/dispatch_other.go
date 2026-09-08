//go:build !darwin && !windows

package main

import (
	"errors"

	"github.com/kang-sw/gotto-hando/internal/backend"
)

// newLocalBackend has no real implementation outside darwin/windows yet
// (remote is a separate ticket - 260908-feat-remote-ssh). The error text
// matches the stub message dispatch.go printed before the darwin/windows
// tickets, so every other GOOS's abort is unchanged.
func newLocalBackend() (backend.Backend, error) {
	return nil, errors.New("platform backend not implemented, nothing ran")
}
