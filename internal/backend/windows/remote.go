//go:build windows

package windows

import "os"

// IsRemoteSession reports whether this process is running remotely
// (started by ssh, or otherwise outside the interactive console session)
// per help-remote.txt SESSION BRIDGE's detection rule and
// help-windows.txt SESSION BRIDGE FOR SSH: SSH_CONNECTION/SSH_TTY set (any
// platform), OR this process's session id != WTSGetActiveConsoleSessionId,
// OR the input desktop (WinSta0) is not accessible.
//
// This is a routing convenience for cmd/gotto-hando's `local` dispatch
// (shouldForwardToBridge, 260908-feat-remote-ssh Phase 0), not the
// security boundary: a process that scrubs those env vars and runs
// in-process simply fails Preflight (Session 0 => E_SESSION) instead.
//
// Deliberately NOT the same predicate as sessionState() == "locked": a
// LOCKED LOCAL session must stay locally-preflighted (Preflight reports
// E_SESSION itself), not be routed to the bridge. IsRemoteSession only
// asks "is this process itself remote", reusing session.go's session-id/
// WinSta0 primitives but not its "Default"/"Winlogon" desktop-name branch,
// which only matters once already known to be the console session.
func IsRemoteSession() bool {
	if os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_TTY") != "" {
		return true
	}
	return isRemoteSessionID()
}
