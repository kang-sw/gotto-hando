//go:build darwin

package darwin

import "testing"

// (a) neither SSH_CONNECTION nor SSH_TTY set -> not a remote session.
func TestIsRemoteSessionFalseWithoutEnvVars(t *testing.T) {
	t.Setenv("SSH_CONNECTION", "")
	t.Setenv("SSH_TTY", "")
	if IsRemoteSession() {
		t.Error("IsRemoteSession() = true, want false with neither SSH_CONNECTION nor SSH_TTY set")
	}
}

// (b) SSH_CONNECTION set -> remote session.
func TestIsRemoteSessionTrueWithSSHConnection(t *testing.T) {
	t.Setenv("SSH_CONNECTION", "10.0.0.1 22 10.0.0.2 22")
	t.Setenv("SSH_TTY", "")
	if !IsRemoteSession() {
		t.Error("IsRemoteSession() = false, want true with SSH_CONNECTION set")
	}
}

// (c) SSH_TTY set -> remote session, even without SSH_CONNECTION.
func TestIsRemoteSessionTrueWithSSHTTY(t *testing.T) {
	t.Setenv("SSH_CONNECTION", "")
	t.Setenv("SSH_TTY", "/dev/ttys001")
	if !IsRemoteSession() {
		t.Error("IsRemoteSession() = false, want true with SSH_TTY set")
	}
}
