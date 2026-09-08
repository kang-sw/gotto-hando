//go:build dryrun

package main

import (
	"io"

	"github.com/kang-sw/gotto-hando/internal/backend"
	"github.com/kang-sw/gotto-hando/internal/backend/dryrun"
	"github.com/kang-sw/gotto-hando/internal/ir"
	"github.com/kang-sw/gotto-hando/internal/output"
)

// newLocalBackend, under the dryrun build tag, stands in for "the remote
// copy of gotto-hando" in 260908-feat-remote-ssh Phase 1's fake-ssh
// integration test harness (dryrun.Backend's own doc comment records this
// exception): a binary built with `-tags dryrun` (never selectable in a
// release build - main_test.go's normal build carries no such tag) binds
// `local` to the recording/canned dryrun.Backend instead of a real OS
// backend, so the harness drives the real CLI/engine/remote wiring without
// linking real backend/syscall code.
func newLocalBackend() (backend.Backend, error) {
	return &dryrun.Backend{}, nil
}

// requestPerms mirrors dispatch_other.go's non-macOS stub: --request-perms
// is a usage error, abort E_VALIDATE / exit 2 before anything runs. This is
// what makes the harness's "--request-perms forwarded to a fake windows
// remote -> start, abort E_VALIDATE" scenario reproducible without a real
// macOS/Windows backend.
func requestPerms(opts parsedOptions, stdout, stderr io.Writer, abort func(output.ErrorCode, string) int) int {
	return abort(output.EValidate, "--request-perms is macOS only")
}

// runBridge mirrors dispatch_other.go's stub: the dryrun harness never
// exercises the session bridge (Phase 2, out of scope for 260908-feat-
// remote-ssh Phase 1).
func runBridge(opts parsedOptions, stdout, stderr io.Writer, abort func(output.ErrorCode, string) int) int {
	return abort(output.EValidate, "session bridge not implemented")
}

// shouldForwardToBridge mirrors dispatch_other.go: always false, so the
// dryrun "remote" always runs directly against dryrun.Backend instead of
// forwarding - dry-run bridge routing is Phase 2, out of scope here.
func shouldForwardToBridge(seq *ir.Sequence) bool {
	return false
}

// forwardToBridge is unreachable (shouldForwardToBridge is always false);
// it exists only so dispatch.go compiles under the dryrun tag.
func forwardToBridge(opts parsedOptions, seq *ir.Sequence, stdout, stderr io.Writer, abort func(output.ErrorCode, string) int) int {
	return abort(output.EValidate, "session bridge not implemented")
}
