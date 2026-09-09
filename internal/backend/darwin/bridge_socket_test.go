//go:build darwin

package darwin

import (
	"bufio"
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kang-sw/gotto-hando/internal/backend/dryrun"
	"github.com/kang-sw/gotto-hando/internal/bridge"
	"github.com/kang-sw/gotto-hando/internal/ir"
	"github.com/kang-sw/gotto-hando/internal/syntax"
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

	// Guard the two os.Chmod calls in ListenBridge against a umask or
	// pre-existing-looser-directory regression that would expose the
	// socket to other local users (help-macos.txt SESSION BRIDGE FOR SSH
	// (REMOTE MAC): "socket file 0600, directory 0700"). Empirically
	// confirmed on macOS: os.Stat on a chmod(0600) unix-socket inode
	// reports Mode() "Srw-------" / Perm() 0600, same as a regular file -
	// no surprising OS-reported bits for the socket type here.
	path, err := bridgeSocketPath()
	if err != nil {
		t.Fatalf("bridgeSocketPath: %v", err)
	}
	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat bridge dir: %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Fatalf("bridge dir perm = %o, want 0700", perm)
	}
	sockInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat bridge socket: %v", err)
	}
	if perm := sockInfo.Mode().Perm(); perm != 0o600 {
		t.Fatalf("bridge socket perm = %o, want 0600", perm)
	}

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

// (d) caller-disconnect -> held-key-release, exercised over a REAL darwin
// unix socket (ListenBridge/DialBridge), not the GOOS-agnostic net.Pipe()
// harness internal/bridge/session_test.go:TestHandleWriteFailureStopsRunAndReleasesHeldKey
// already covers. The release mechanism itself (OnResult write-failure ->
// cancel() -> engine's unconditional releaseAll) is transport-agnostic, but
// this closes the plan's Codebase Findings gap that specifically called for
// a real-socket exercise of it. kd[]shift holds a key that only end-of-run
// releaseAll frees; k[]a/k[]b are plain taps (press+release, never held) so
// the only way "KeyUp shift" appears is via the cancel-triggered
// early-stop's releaseAll, not via k[]a/k[]b's own execution. After the
// disconnected run finishes, a second DialBridge/Accept pair must still
// succeed - the listener itself must remain healthy after one caller drops
// mid-run.
func TestBridgeSocketDisconnectReleasesHeldKey(t *testing.T) {
	withTempHome(t)

	ln, err := ListenBridge()
	if err != nil {
		t.Fatalf("ListenBridge: %v", err)
	}
	defer ln.Close()

	seq, diags := syntax.Parse([]string{"kd[]shift", "k[]a", "k[]b"}, ir.Defaults{})
	if len(diags) != 0 {
		t.Fatalf("parse diags: %+v", diags)
	}
	if d := ir.Validate(seq, 3); len(d) != 0 {
		t.Fatalf("validate diags: %+v", d)
	}
	body, err := bridge.EncodeRequest(seq, bridge.RunEnvelope{})
	if err != nil {
		t.Fatalf("EncodeRequest: %v", err)
	}

	be := &dryrun.Backend{}
	sess := &bridge.Session{Backend: be}

	serverDone := make(chan struct{})
	acceptedCh := make(chan io.ReadWriteCloser, 1)
	acceptErrCh := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			acceptErrCh <- err
			return
		}
		acceptedCh <- conn
		go func() {
			sess.Handle(context.Background(), conn)
			close(serverDone)
		}()
	}()

	client, err := DialBridge()
	if err != nil {
		t.Fatalf("DialBridge: %v", err)
	}
	select {
	case <-acceptedCh:
	case err := <-acceptErrCh:
		t.Fatalf("Accept: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("Accept did not return after DialBridge connected")
	}

	go func() {
		if _, err := client.Write(append(body, '\n')); err != nil {
			// Expected once the read side below closes the connection
			// mid-run: a soft log, not a failure.
			t.Logf("client write: %v", err)
		}
	}()

	// Read exactly through the "start" event and the first (kd[]shift)
	// result, then close the client end - mirrors
	// TestHandleWriteFailureStopsRunAndReleasesHeldKey's timing, now over
	// the real socket.
	sc := bufio.NewScanner(client)
	for i := 0; i < 2; i++ {
		if !sc.Scan() {
			t.Fatalf("scan %d: %v", i, sc.Err())
		}
	}
	client.Close()

	select {
	case <-serverDone:
	case <-time.After(5 * time.Second):
		t.Fatal("Handle did not return after the caller disconnected")
	}

	found := false
	for _, c := range be.Calls {
		if c == "KeyUp shift" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("be.Calls = %v, want a KeyUp shift release after the caller disconnected mid-run", be.Calls)
	}

	// The bridge listener must still accept a subsequent caller after the
	// first one dropped mid-run.
	acceptedCh2 := make(chan io.ReadWriteCloser, 1)
	acceptErrCh2 := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			acceptErrCh2 <- err
			return
		}
		acceptedCh2 <- conn
	}()
	client2, err := DialBridge()
	if err != nil {
		t.Fatalf("second DialBridge: %v", err)
	}
	defer client2.Close()
	select {
	case conn := <-acceptedCh2:
		conn.Close()
	case err := <-acceptErrCh2:
		t.Fatalf("second Accept: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("second Accept did not return after the second DialBridge connected")
	}
}
