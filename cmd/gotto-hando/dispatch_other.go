//go:build !darwin

package main

import (
	"errors"

	"github.com/kang-sw/gotto-hando/internal/backend"
)

// newLocalBackend has no real implementation outside darwin yet (windows
// is a separate ticket - 260907-feat-windows-backend). The error text
// matches the stub message dispatch.go printed before this ticket, so the
// non-darwin abort is unchanged.
func newLocalBackend() (backend.Backend, error) {
	return nil, errors.New("platform backend not implemented, nothing ran")
}
