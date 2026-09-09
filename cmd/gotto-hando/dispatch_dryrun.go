//go:build dryrun

package main

import (
	"io"

	"github.com/kang-sw/gotto-hando/internal/backend"
	"github.com/kang-sw/gotto-hando/internal/backend/dryrun"
	"github.com/kang-sw/gotto-hando/internal/ir"
	"github.com/kang-sw/gotto-hando/internal/output"
)

// lastDryrunBackend records the most recently constructed dryrun.Backend
// so dryrun-tagged tests can inspect its Calls log (e.g. to confirm a
// held key was released) after a run - a test-only hook, gated by the
// same `dryrun` build tag as the rest of this file, so it never ships
// (the CLI itself never reads this variable; only *_test.go files under
// `//go:build dryrun` do, review T3).
var lastDryrunBackend *dryrun.Backend

// dryrunCaptureImage is a small, fixed, deterministic RGBA image (4
// bytes/pixel, tightly packed) returned by every dryrun `cap` - the
// zero-valued backend.Image a bare `&dryrun.Backend{}` would otherwise
// return has W=H=0, which internal/engine's own encodePNG rejects as
// malformed, making `cap` always fail through this harness. A real
// (if trivial) image lets the ssh-wrapper's capture-relay path
// (internal/remote.Relay.processCapture, the base64-frame-over-the-wire
// -> local .png write) be exercised end to end (review T1).
func dryrunCaptureImage() backend.Image {
	const w, h = 4, 3
	pix := make([]byte, w*h*4)
	for i := range pix {
		// A deterministic, non-uniform byte pattern (not just all-0x00
		// or all-0xFF) so a corrupted transfer/decode is more likely to
		// change the encoded PNG bytes than to coincidentally match.
		pix[i] = byte(i * 7 % 256)
	}
	return backend.Image{W: w, H: h, Scale: 1, Pixels: pix}
}

// newLocalBackend, under the dryrun build tag, stands in for "the remote
// copy of gotto-hando" in 260908-feat-remote-ssh Phase 1's fake-ssh
// integration test harness (dryrun.Backend's own doc comment records this
// exception): a binary built with `-tags dryrun` (never selectable in a
// release build - main_test.go's normal build carries no such tag) binds
// `local` to the recording/canned dryrun.Backend instead of a real OS
// backend, so the harness drives the real CLI/engine/remote wiring without
// linking real backend/syscall code.
func newLocalBackend() (backend.Backend, error) {
	be := &dryrun.Backend{CaptureResult: dryrunCaptureImage()}
	lastDryrunBackend = be
	return be, nil
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
