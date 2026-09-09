//go:build darwin

package darwin

import "os"

// IsRemoteSession reports whether this process is running remotely
// (started by ssh) per help-remote.txt SESSION BRIDGE's detection rule and
// help-macos.txt SESSION BRIDGE FOR SSH (REMOTE MAC): SSH_CONNECTION or
// SSH_TTY set in the environment. Unlike windows's IsRemoteSession, macOS
// has no console-session-id/WinSta0 analogue to also check - help-remote.txt
// SESSION BRIDGE "Detection" (:109-117) confirms the extra session-id/
// WinSta0 checks are windows-only, so the env-var check alone is the full
// detection rule here.
//
// This is a routing convenience for cmd/gotto-hando's `local` dispatch
// (shouldForwardToBridge, 260908-feat-remote-ssh Phase 2), not the security
// boundary: a process that scrubs these env vars and runs in-process simply
// fails Preflight (no TCC grant for a non-GUI process => E_SESSION)
// instead.
func IsRemoteSession() bool {
	return os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_TTY") != ""
}
