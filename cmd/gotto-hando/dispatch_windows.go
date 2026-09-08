//go:build windows

package main

import (
	"github.com/kang-sw/gotto-hando/internal/backend"
	winbackend "github.com/kang-sw/gotto-hando/internal/backend/windows"
)

// newLocalBackend constructs the real windows backend (dispatch.go's
// dest=="local" path). See dispatch_darwin.go for the darwin equivalent
// and dispatch_other.go for every remaining GOOS (remote is a separate
// ticket).
func newLocalBackend() (backend.Backend, error) {
	return winbackend.New()
}
