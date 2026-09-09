//go:build darwin

// bridge_socket.go implements the session bridge's unix-socket endpoint
// (help-macos.txt SESSION BRIDGE FOR SSH (REMOTE MAC): $HOME/Library/
// Application Support/gotto-hando/bridge.sock, file 0600, directory 0700;
// help-remote.txt SESSION BRIDGE; 260908-feat-remote-ssh Phase 2). Stdlib
// net/os/path/filepath/syscall only - no new dependency. Mirrors internal/
// backend/windows/bridge_pipe.go's shape with the unix-socket-specific
// simplifications the ticket calls out: no per-user name is needed (the
// socket path is already under $HOME), and no WaitNamedPipe-style
// busy-retry loop in DialBridge (a unix SOCK_STREAM listener's kernel
// accept backlog already queues a concurrent connect() while Handle is
// busy).
package darwin

import (
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// ErrBridgeAlreadyRunning is returned by ListenBridge when a bridge for
// this user is already running (a live socket answers a dial probe). The
// caller (dispatch_darwin.go's runBridge) surfaces this the same way
// windows's ListenBridge does ("a second bridge for the same user exits
// 2", help-macos.txt SESSION BRIDGE FOR SSH (REMOTE MAC)).
var ErrBridgeAlreadyRunning = errors.New("a gotto-hando bridge for this user is already running")

// ErrListenerClosed is returned by Accept after Close.
var ErrListenerClosed = errors.New("bridge socket listener closed")

// bridgeSocketPath resolves $HOME/Library/Application Support/gotto-hando/
// bridge.sock (help-macos.txt SESSION BRIDGE FOR SSH (REMOTE MAC)).
func bridgeSocketPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "Application Support", "gotto-hando", "bridge.sock"), nil
}

// SocketListener is the bridge's unix-socket server endpoint.
type SocketListener struct {
	ln   *net.UnixListener
	path string
}

// ListenBridge creates the current-user unix socket $HOME/Library/
// Application Support/gotto-hando/bridge.sock (directory 0700, socket file
// 0600). A second instance for the same user fails with
// ErrBridgeAlreadyRunning.
func ListenBridge() (*SocketListener, error) {
	path, err := bridgeSocketPath()
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	// MkdirAll's mode is subject to umask, so an explicit chmod is needed
	// to guarantee 0700 even when the directory already existed with
	// looser permission bits.
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, err
	}

	if conn, dialErr := net.DialTimeout("unix", path, 200*time.Millisecond); dialErr == nil {
		conn.Close()
		return nil, ErrBridgeAlreadyRunning
	} else if !isStaleSocketErr(dialErr) {
		return nil, dialErr
	}
	// A stale socket file (nothing listening) or no file at all: safe to
	// remove and proceed.
	_ = os.Remove(path)

	addr, err := net.ResolveUnixAddr("unix", path)
	if err != nil {
		return nil, err
	}
	ln, err := net.ListenUnix("unix", addr)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		ln.Close()
		return nil, err
	}
	return &SocketListener{ln: ln, path: path}, nil
}

// isStaleSocketErr reports whether err is the dial-probe outcome of a
// stale (nothing listening) or absent socket file - ECONNREFUSED or
// ENOENT, per the ticket's ListenBridge detection rule - as opposed to a
// genuine error (e.g. a permission problem) that ListenBridge should
// surface instead of silently removing the path.
func isStaleSocketErr(err error) bool {
	return errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ENOENT) || errors.Is(err, os.ErrNotExist)
}

// Accept blocks until a client connects (or Close unblocks it), returning
// the connection as an io.ReadWriteCloser.
func (l *SocketListener) Accept() (io.ReadWriteCloser, error) {
	conn, err := l.ln.Accept()
	if err != nil {
		if errors.Is(err, net.ErrClosed) {
			return nil, ErrListenerClosed
		}
		return nil, err
	}
	return conn, nil
}

// Close stops the listener (unblocking a pending Accept) and best-effort
// removes the socket file - clean Ctrl-C shutdown (help.txt --bridge "Exit
// 0 on Ctrl-C").
func (l *SocketListener) Close() error {
	err := l.ln.Close()
	_ = os.Remove(l.path)
	return err
}

// DialBridge connects to the bridge unix socket as a client - the default
// implementation behind cmd/gotto-hando's local forwarder's dialBridge
// seam (dispatch_darwin.go's forwardToBridge/requestPerms). A dial failure
// is the caller's "no bridge reachable" signal; unlike windows's
// single-instance named pipe, a unix SOCK_STREAM listener's kernel accept
// backlog already queues a concurrent connect() while Handle is busy, so
// no WaitNamedPipe-style busy-retry loop is needed here (ticket Out of
// Scope).
func DialBridge() (io.ReadWriteCloser, error) {
	path, err := bridgeSocketPath()
	if err != nil {
		return nil, err
	}
	return net.Dial("unix", path)
}
