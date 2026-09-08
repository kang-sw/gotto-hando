//go:build darwin

package darwin

import (
	"context"
	"errors"

	"github.com/kang-sw/gotto-hando/internal/backend"
)

// errNotThisPhase marks the Backend methods for commands outside this
// phase's goal list: exec/open real behavior is Phase 3 (ticket Out of
// Scope). They exist only so *Backend satisfies backend.Backend; neither is
// reachable through the Preflight-gated command set this phase wires, so a
// line using one fails at run time as err, which is acceptable. (win/qwin/
// cap are now implemented in windows.go / capture.go and no longer stubbed.)
var errNotThisPhase = errors.New("not implemented in this phase")

func (b *Backend) Exec(ctx context.Context, req backend.ExecReq) (backend.ExecResult, error) {
	return backend.ExecResult{}, errNotThisPhase
}

func (b *Backend) Open(ctx context.Context, target string) error {
	return errNotThisPhase
}
