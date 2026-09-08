//go:build darwin && !dryrun

package main

import (
	"fmt"
	"io"

	"github.com/kang-sw/gotto-hando/internal/backend"
	"github.com/kang-sw/gotto-hando/internal/backend/darwin"
	"github.com/kang-sw/gotto-hando/internal/ir"
	"github.com/kang-sw/gotto-hando/internal/output"
)

// newLocalBackend constructs the real darwin backend (dispatch.go's
// dest=="local" path). See dispatch_other.go for every other GOOS, which
// has no backend yet (windows/remote are separate tickets).
func newLocalBackend() (backend.Backend, error) {
	return darwin.New()
}

// requestPerms implements `local --request-perms` on darwin (help.txt
// --request-perms :91-108, help-macos.txt GRANTING PERMISSIONS): a process
// in the GUI session calls AXIsProcessTrustedWithOptions(prompt=true) and
// CGRequestScreenCaptureAccess() so macOS shows its prompts, then prints
// one perms= line and exits 0 when both are ok, else 4. A non-local dest
// would be the bridge's job (260908-feat-remote-ssh, not wired here).
func requestPerms(opts parsedOptions, stdout, stderr io.Writer, abort func(output.ErrorCode, string) int) int {
	if opts.Dest != "local" {
		return abort(output.EValidate, "remote destinations not implemented")
	}
	acc, scr, err := darwin.RequestPerms()
	if err != nil {
		return abort(output.EValidate, err.Error())
	}
	fmt.Fprintf(stdout, "perms=accessibility:%s,screen:%s\n", okMissing(acc), okMissing(scr))
	if acc && scr {
		return output.ExitOK
	}
	return output.ExitPreflight
}

// runBridge is macOS's session-bridge entry point (dispatch.go's
// opts.Bridge branch). The macOS unix-socket bridge is Phase 1/2 (ticket
// Out of Scope: "The macOS unix-socket bridge ... (Phase 1/2)"); today's
// abort - message, code, exit - is reproduced byte-for-byte via the same
// abort closure dispatch.go already builds, so this is not an observable
// regression from the inline abort this replaced (dispatch.go:89-91
// before this ticket).
func runBridge(opts parsedOptions, stdout, stderr io.Writer, abort func(output.ErrorCode, string) int) int {
	return abort(output.EValidate, "session bridge not implemented")
}

// shouldForwardToBridge is always false outside windows - the named-pipe
// bridge and its forwarding path are a windows-only concern this ticket
// (260908-feat-remote-ssh Phase 0); darwin's dest=="local" path is
// completely unaffected (dispatch.go's shouldForwardToBridge branch is a
// no-op here).
func shouldForwardToBridge(seq *ir.Sequence) bool {
	return false
}

// forwardToBridge is unreachable on darwin (shouldForwardToBridge is
// always false); it exists only so dispatch.go compiles on every GOOS.
func forwardToBridge(opts parsedOptions, seq *ir.Sequence, stdout, stderr io.Writer, abort func(output.ErrorCode, string) int) int {
	return abort(output.EValidate, "session bridge not implemented")
}

func okMissing(ok bool) string {
	if ok {
		return "ok"
	}
	return "missing"
}
