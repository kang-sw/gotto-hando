//go:build windows

// bridge_pipe.go implements the session bridge's named-pipe endpoint
// (help-remote.txt SESSION BRIDGE: `\\.\pipe\gotto-hando-<username>`, ACL:
// current user; 260908-feat-remote-ssh Phase 0). It calls
// golang.org/x/sys/windows directly (CreateNamedPipe/ConnectNamedPipe/
// DisconnectNamedPipe/CreateFile/SecurityDescriptorFromString), the same
// convention backend.go/session.go already use for APIs that package
// already wraps - no ffi.go addition, no new dependency
// (github.com/Microsoft/go-winio is deliberately not used).
package windows

import (
	"errors"
	"fmt"
	"io"
	"os/user"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// pipeBufSize is CreateNamedPipe's suggested in/out buffer size. The
// bridge's protocol frames one JSON document per read/write (internal/
// bridge's newline-delimited framing), so this only needs to comfortably
// hold one line, not size any hard limit - the OS grows the pipe's
// internal buffer as needed for a single ReadFile/WriteFile call.
const pipeBufSize = 64 * 1024

// ErrBridgeAlreadyRunning is returned by ListenBridge when a bridge for
// this user is already running: CreateNamedPipe with
// FILE_FLAG_FIRST_PIPE_INSTANCE fails ERROR_ACCESS_DENIED/ERROR_PIPE_BUSY
// against an existing single-instance pipe of the same name. The caller
// (dispatch_windows.go's runBridge) surfaces this as "a second instance for
// the same user exits 2" (help.txt --bridge :83-89).
var ErrBridgeAlreadyRunning = errors.New("a gotto-hando bridge for this user is already running")

// ErrListenerClosed is returned by Accept after Close.
var ErrListenerClosed = errors.New("bridge pipe listener closed")

// PipeName builds the current-user-scoped pipe name (help-remote.txt
// SESSION BRIDGE :118-121). Exported so cmd/gotto-hando's local forwarder
// (dispatch_windows.go's forwardToBridge) can build the same name to dial.
func PipeName(username string) string {
	return `\\.\pipe\gotto-hando-` + username
}

// CurrentUsername resolves this process's username, stripping a
// `DOMAIN\` prefix os/user.Current() may include - the pipe name and its
// ACL are per-user, not per-domain-account-string. Exported for the same
// reason as PipeName: both the bridge listener and the local forwarder need
// the identical value.
func CurrentUsername() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", err
	}
	name := u.Username
	if i := strings.LastIndex(name, `\`); i >= 0 {
		name = name[i+1:]
	}
	return name, nil
}

// currentUserSDDL builds a SDDL string granting GENERIC_ALL to this
// process's own user SID and nothing else - "ACL: current user"
// (help-remote.txt SESSION BRIDGE :118-121). D:P marks the DACL protected
// (no inherited ACEs loosen it).
func currentUserSDDL() (string, error) {
	tok := windows.GetCurrentProcessToken()
	tu, err := tok.GetTokenUser()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("D:P(A;;GA;;;%s)", tu.User.Sid.String()), nil
}

// PipeListener is the bridge's named-pipe server endpoint: one pipe
// instance (maxInstances=1, per help.txt --bridge "if another bridge of
// this user is already running"), accepted and reused across connections
// (Accept/Close below).
type PipeListener struct {
	handle windows.Handle

	mu     sync.Mutex
	closed bool
}

// ListenBridge creates the current-user-ACL'd named pipe
// `\\.\pipe\gotto-hando-<username>`. A second instance for the same user
// fails with ErrBridgeAlreadyRunning.
func ListenBridge() (*PipeListener, error) {
	username, err := CurrentUsername()
	if err != nil {
		return nil, fmt.Errorf("bridge: resolve username: %w", err)
	}
	sddl, err := currentUserSDDL()
	if err != nil {
		return nil, fmt.Errorf("bridge: build ACL: %w", err)
	}
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return nil, fmt.Errorf("bridge: parse ACL: %w", err)
	}
	sa := &windows.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
		SecurityDescriptor: sd,
	}
	namePtr, err := windows.UTF16PtrFromString(PipeName(username))
	if err != nil {
		return nil, fmt.Errorf("bridge: pipe name: %w", err)
	}

	h, err := windows.CreateNamedPipe(
		namePtr,
		windows.PIPE_ACCESS_DUPLEX|windows.FILE_FLAG_FIRST_PIPE_INSTANCE,
		windows.PIPE_TYPE_BYTE|windows.PIPE_READMODE_BYTE|windows.PIPE_WAIT,
		1, // maxInstances: one run at a time, one bridge per user.
		pipeBufSize, pipeBufSize,
		0, // default timeout (50ms)
		sa,
	)
	if err != nil {
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_PIPE_BUSY) {
			return nil, ErrBridgeAlreadyRunning
		}
		return nil, fmt.Errorf("bridge: CreateNamedPipe: %w", err)
	}
	return &PipeListener{handle: h}, nil
}

// Accept blocks until a client connects (or Close unblocks it), then
// returns the pipe as an io.ReadWriteCloser. The same handle backs every
// Accept() call in sequence (a named pipe with maxInstances=1 is a single
// instance reused via DisconnectNamedPipe + ConnectNamedPipe again, the
// normal idiom - not recreated per connection).
func (l *PipeListener) Accept() (io.ReadWriteCloser, error) {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil, ErrListenerClosed
	}
	handle := l.handle
	l.mu.Unlock()

	err := windows.ConnectNamedPipe(handle, nil)
	if err != nil && !errors.Is(err, windows.ERROR_PIPE_CONNECTED) {
		l.mu.Lock()
		closed := l.closed
		l.mu.Unlock()
		if closed {
			return nil, ErrListenerClosed
		}
		return nil, fmt.Errorf("bridge: ConnectNamedPipe: %w", err)
	}
	return &pipeConn{handle: handle}, nil
}

// Close unblocks a pending Accept (via CancelIoEx, which cancels
// outstanding synchronous I/O issued against the handle from any thread in
// this process) and releases the pipe - clean Ctrl-C shutdown
// (help.txt --bridge "Exit 0 on Ctrl-C").
func (l *PipeListener) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	_ = windows.CancelIoEx(l.handle, nil)
	return windows.CloseHandle(l.handle)
}

// pipeConn wraps a server-side named-pipe instance. Close disconnects the
// client (DisconnectNamedPipe) without destroying the pipe instance itself
// - the handle is reused by the PipeListener's next Accept() call.
type pipeConn struct {
	handle windows.Handle
}

func (c *pipeConn) Read(p []byte) (int, error)  { return pipeRead(c.handle, p) }
func (c *pipeConn) Write(p []byte) (int, error) { return pipeWrite(c.handle, p) }
func (c *pipeConn) Close() error                { return windows.DisconnectNamedPipe(c.handle) }

// clientPipeConn wraps a client-side dial (DialBridge). Unlike pipeConn,
// Close destroys the handle outright (CloseHandle) - a client dial has no
// pipe instance to reuse.
type clientPipeConn struct {
	handle windows.Handle
}

func (c *clientPipeConn) Read(p []byte) (int, error)  { return pipeRead(c.handle, p) }
func (c *clientPipeConn) Write(p []byte) (int, error) { return pipeWrite(c.handle, p) }
func (c *clientPipeConn) Close() error                { return windows.CloseHandle(c.handle) }

func pipeRead(h windows.Handle, p []byte) (int, error) {
	var n uint32
	err := windows.ReadFile(h, p, &n, nil)
	if err != nil {
		if errors.Is(err, windows.ERROR_BROKEN_PIPE) {
			return int(n), io.EOF
		}
		return int(n), err
	}
	if n == 0 {
		return 0, io.EOF
	}
	return int(n), nil
}

func pipeWrite(h windows.Handle, p []byte) (int, error) {
	var n uint32
	err := windows.WriteFile(h, p, &n, nil)
	return int(n), err
}

// nmpwaitWaitForever is WaitNamedPipe's nTimeOut sentinel for an indefinite
// wait (winbase.h NMPWAIT_WAIT_FOREVER). A bridge run is bounded, so
// blocking until an instance frees up (rather than a bounded retry budget)
// matches help-remote.txt SESSION BRIDGE's "a second caller waits until the
// first run has finished".
const nmpwaitWaitForever = 0xFFFFFFFF

// DialBridge connects to the named pipe (PipeName's return value) as a
// client - the default implementation behind cmd/gotto-hando's local
// forwarder's dialBridge seam (dispatch_windows.go's forwardToBridge).
//
// The pipe is a single instance (ListenBridge's maxInstances=1): while the
// bridge is mid-Handle for another caller, CreateFile fails
// ERROR_PIPE_BUSY - that means the bridge IS running, just occupied, so it
// is not surfaced as an error here. Instead this blocks on WaitNamedPipe
// until an instance frees up and retries CreateFile (a racing third caller
// can re-BUSY it, hence the loop), giving the "one run at a time: a second
// caller waits" contract (help-remote.txt SESSION BRIDGE) instead of the
// wrong "bridge not running" hint. Any other CreateFile error (no pipe at
// all - ERROR_FILE_NOT_FOUND and friends) is a genuine "no bridge running"
// and is returned immediately: the caller maps that to abort E_SESSION
// with the "start `gotto-hando --bridge` ..." hint (help-windows.txt
// :72-73).
func DialBridge(name string) (io.ReadWriteCloser, error) {
	namePtr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	for {
		h, err := windows.CreateFile(
			namePtr,
			windows.GENERIC_READ|windows.GENERIC_WRITE,
			0,   // no sharing: this is a duplex byte-mode pipe, one client
			nil, // default security, not inherited
			windows.OPEN_EXISTING,
			0,
			0,
		)
		if err == nil {
			return &clientPipeConn{handle: h}, nil
		}
		if !errors.Is(err, windows.ERROR_PIPE_BUSY) {
			return nil, err
		}
		if err := waitNamedPipe(namePtr); err != nil {
			return nil, fmt.Errorf("bridge: WaitNamedPipe: %w", err)
		}
	}
}

// waitNamedPipe blocks until an instance of the pipe named by namePtr is
// available to connect to, or returns an error (e.g. the pipe was removed
// while waiting). procWaitNamedPipeW.Call follows the standard BOOL-Win32
// convention: a zero return means failure, and the accompanying error
// (from GetLastError) is meaningful only in that case.
func waitNamedPipe(namePtr *uint16) error {
	r1, _, e1 := procWaitNamedPipeW.Call(uintptr(unsafe.Pointer(namePtr)), uintptr(nmpwaitWaitForever))
	if r1 == 0 {
		return e1
	}
	return nil
}
