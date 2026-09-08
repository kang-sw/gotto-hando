//go:build windows

package windows

import (
	"context"
	"errors"

	"github.com/kang-sw/gotto-hando/internal/backend"
)

// errPhase1 marks the Backend methods for commands outside the Phase 1/2
// goal list (exec/open real behavior is Phase 2 Slice B). They exist only
// so *Backend satisfies backend.Backend; neither is reachable through
// Phase 1's Preflight-gated command set (verbatim structure of darwin's
// stubs.go).
var errPhase1 = errors.New("not implemented in Phase 1")

func (b *Backend) Exec(ctx context.Context, req backend.ExecReq) (backend.ExecResult, error) {
	return backend.ExecResult{}, errPhase1
}

func (b *Backend) Open(ctx context.Context, target string) error {
	return errPhase1
}
