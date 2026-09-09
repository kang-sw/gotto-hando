//go:build darwin

package darwin

import (
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// withTempHome points $HOME at a fresh temp directory for the duration of
// the test, so bridgeSocketPath's $HOME/Library/Application Support/
// gotto-hando/bridge.sock resolves under an isolated, throwaway location
// instead of this dev machine's real one.
//
// Deliberately NOT t.TempDir(): that nests under os.TempDir() plus the
// test's own name (e.g. /var/folders/.../T/TestFoo.../001), which is
// already close to macOS's sockaddr_un limit (sun_path[104]) before even
// appending "/Library/Application Support/gotto-hando/bridge.sock" - the
// combined path reliably overflows it ("connect: invalid argument"/"bind:
// invalid argument"). A short, directly-under-/tmp directory keeps the
// full socket path well under the limit, matching a real $HOME's actual
// length (e.g. /Users/name) far better than a nested test temp dir would.
func withTempHome(t *testing.T) string {
	t.Helper()
	home, err := os.MkdirTemp("/tmp", "gh-bridge-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	t.Setenv("HOME", home)
	return home
}

// (a) ListenBridge -> DialBridge -> write/read round trip over a real
// unix-domain socket.
func TestBridgeSocketRoundTrip(t *testing.T) {
	withTempHome(t)

	ln, err := ListenBridge()
	if err != nil {
		t.Fatalf("ListenBridge: %v", err)
	}
	defer ln.Close()

	acceptedCh := make(chan io.ReadWriteCloser, 1)
	acceptErrCh := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			acceptErrCh <- err
			return
		}
		acceptedCh <- conn
	}()

	client, err := DialBridge()
	if err != nil {
		t.Fatalf("DialBridge: %v", err)
	}
	defer client.Close()

	var server io.ReadWriteCloser
	select {
	case server = <-acceptedCh:
	case err := <-acceptErrCh:
		t.Fatalf("Accept: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("Accept did not return after DialBridge connected")
	}
	defer server.Close()

	const msg = "hello-bridge-socket\n"
	if _, err := client.Write([]byte(msg)); err != nil {
		t.Fatalf("client.Write: %v", err)
	}
	buf := make([]byte, len(msg))
	if _, err := io.ReadFull(server, buf); err != nil {
		t.Fatalf("server read: %v", err)
	}
	if string(buf) != msg {
		t.Fatalf("server read %q, want %q", buf, msg)
	}
}

// (b) a second ListenBridge while the first is still live fails with
// ErrBridgeAlreadyRunning (help-macos.txt SESSION BRIDGE FOR SSH (REMOTE
// MAC): "A second bridge for the same user exits 2").
func TestBridgeSocketSecondListenFails(t *testing.T) {
	withTempHome(t)

	ln, err := ListenBridge()
	if err != nil {
		t.Fatalf("first ListenBridge: %v", err)
	}
	defer ln.Close()

	_, err = ListenBridge()
	if err != ErrBridgeAlreadyRunning {
		t.Fatalf("second ListenBridge error = %v, want ErrBridgeAlreadyRunning", err)
	}
}

// (c) a leftover socket file with nothing listening (e.g. a bridge that
// crashed without cleaning up) must not block a fresh ListenBridge - the
// stale-socket-file recovery path (ECONNREFUSED/ENOENT on the dial probe
// means safe to remove and proceed).
func TestBridgeSocketStaleFileRecovers(t *testing.T) {
	home := withTempHome(t)

	path, err := bridgeSocketPath()
	if err != nil {
		t.Fatalf("bridgeSocketPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// A UnixListener bound then immediately closed (without removing the
	// file) leaves exactly the "stale socket file, nothing listening"
	// condition this test targets - simpler and more realistic than
	// fabricating an arbitrary regular file at the same path.
	addr, err := net.ResolveUnixAddr("unix", path)
	if err != nil {
		t.Fatalf("ResolveUnixAddr: %v", err)
	}
	staleLn, err := net.ListenUnix("unix", addr)
	if err != nil {
		t.Fatalf("bind stale listener: %v", err)
	}
	// net.UnixListener unlinks its socket file on Close() by default - the
	// opposite of what a real crashed bridge (e.g. killed -9, no deferred
	// Close ever runs) leaves behind. Disable that so the file survives
	// Close(), reproducing the actual stale-file condition this test
	// targets.
	staleLn.SetUnlinkOnClose(false)
	staleLn.Close()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stale socket file missing before test: %v", err)
	}

	ln, err := ListenBridge()
	if err != nil {
		t.Fatalf("ListenBridge over a stale socket file: %v", err)
	}
	defer ln.Close()

	if _, err := os.Stat(filepath.Join(home, "Library", "Application Support", "gotto-hando")); err != nil {
		t.Fatalf("bridge directory missing: %v", err)
	}
}
