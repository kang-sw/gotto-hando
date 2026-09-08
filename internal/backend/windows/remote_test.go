//go:build windows

package windows

import "testing"

// TestIsRemoteSessionSSHConnectionEnvVar covers IsRemoteSession's env-var
// branch (help-remote.txt SESSION BRIDGE: "SSH_CONNECTION or SSH_TTY is set
// in its environment (any platform)"); the WTS/session-id/WinSta0 branch
// needs a real session and is left to the over-ssh acceptance pass
// (260908-feat-remote-ssh Phase 0 plan, Verification Plan).
func TestIsRemoteSessionSSHConnectionEnvVar(t *testing.T) {
	t.Setenv("SSH_CONNECTION", "10.0.0.1 1234 10.0.0.2 22")
	t.Setenv("SSH_TTY", "")
	if !IsRemoteSession() {
		t.Fatal("IsRemoteSession() = false, want true when SSH_CONNECTION is set")
	}
}

// TestIsRemoteSessionSSHTTYEnvVar is SSH_TTY's counterpart.
func TestIsRemoteSessionSSHTTYEnvVar(t *testing.T) {
	t.Setenv("SSH_CONNECTION", "")
	t.Setenv("SSH_TTY", "/dev/pts/0")
	if !IsRemoteSession() {
		t.Fatal("IsRemoteSession() = false, want true when SSH_TTY is set")
	}
}
