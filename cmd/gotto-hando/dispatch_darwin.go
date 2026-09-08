//go:build darwin

package main

import (
	"github.com/kang-sw/gotto-hando/internal/backend"
	"github.com/kang-sw/gotto-hando/internal/backend/darwin"
)

// newLocalBackend constructs the real darwin backend (dispatch.go's
// dest=="local" path). See dispatch_other.go for every other GOOS, which
// has no backend yet (windows/remote are separate tickets).
func newLocalBackend() (backend.Backend, error) {
	return darwin.New()
}
