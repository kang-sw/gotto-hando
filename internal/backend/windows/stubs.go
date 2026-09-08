//go:build windows

package windows

import (
	"context"
	"errors"

	"github.com/kang-sw/gotto-hando/internal/backend"
	"github.com/kang-sw/gotto-hando/internal/ir"
)

// errPhase1 marks the Backend methods for commands outside the Phase 1
// goal list (win/qwin/cap are Phase 2; exec/open real behavior is Phase 3
// - ticket Out of Scope). They exist only so *Backend satisfies
// backend.Backend; none is reachable through Phase 1's Preflight-gated
// command set (verbatim structure of darwin's stubs.go).
var errPhase1 = errors.New("not implemented in Phase 1")

func (b *Backend) Windows(ctx context.Context, sel ir.Selector) ([]backend.Window, error) {
	return nil, errPhase1
}

func (b *Backend) Focus(ctx context.Context, w backend.Window) error {
	return errPhase1
}

func (b *Backend) Capture(ctx context.Context, req backend.CaptureReq) (backend.Image, error) {
	return backend.Image{}, errPhase1
}

func (b *Backend) Exec(ctx context.Context, req backend.ExecReq) (backend.ExecResult, error) {
	return backend.ExecResult{}, errPhase1
}

func (b *Backend) Open(ctx context.Context, target string) error {
	return errPhase1
}
