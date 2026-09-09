package bridge

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/kang-sw/gotto-hando/internal/backend"
	"github.com/kang-sw/gotto-hando/internal/engine"
	"github.com/kang-sw/gotto-hando/internal/ir"
	"github.com/kang-sw/gotto-hando/internal/output"
)

// Session serves bridge connections one at a time against Backend
// (help-remote.txt SESSION BRIDGE: "One run at a time: a second caller
// waits until the first run has finished"). Backend is injected - the real
// windows.NewBridge() result in production, a *dryrun.Backend in tests -
// and Log is the one-line-per-run/error sink (production: append to
// bridge.log; tests: capture to a slice). Neither field may be mutated
// after the first Handle call.
type Session struct {
	Backend backend.Backend
	// Log receives one line per run or per protocol error - no sequence
	// text, no image data (help-remote.txt SESSION BRIDGE "Log:" clause).
	// nil is a no-op.
	Log func(string)
	// RequestPerms implements --request-perms over the bridge
	// (help-remote.txt SESSION BRIDGE :122-129, help-macos.txt GRANTING
	// PERMISSIONS PROCEDURE step 2): called in the bridge's own GUI session
	// so the system permission prompts appear there. nil-safe like Log - a
	// bridge with no RequestPerms wired (there is currently no windows
	// equivalent; its bridge never receives this message) answers a
	// request_perms request with an abort instead of panicking.
	RequestPerms func() (accessibility, screen bool, err error)

	mu sync.Mutex
}

// Handle serves one bridge connection end-to-end: read the framed request,
// run it through Backend via engine.Run, stream the same JSONL Backend
// would print on stdout with --inline-captures, and close conn. Handle
// holds Session's lock for its entire duration, so a second concurrent
// Handle call on another connection blocks until this one returns - the
// single-run-at-a-time contract.
func (s *Session) Handle(ctx context.Context, conn io.ReadWriteCloser) {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer conn.Close()

	// maxRequestBytes bounds the request read so a malformed same-user
	// caller (or a caller stuck writing without ever sending '\n') cannot
	// grow bridge memory unboundedly; it mirrors the 16 MiB cap
	// cmd/gotto-hando/dispatch_windows.go's forwarder already applies to
	// the bridge's own response stream. A request that hits this cap
	// without a trailing '\n' fails decodeRequest below like any other
	// malformed request - the caller sees "malformed request: ...".
	const maxRequestBytes = 16 * 1024 * 1024
	line, err := bufio.NewReader(io.LimitReader(conn, maxRequestBytes)).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		// A same-machine, same-user caller sending nothing at all is not
		// expected in normal operation; best-effort report and log, no
		// JSONL machinery (there is no request to have gotten "v" from).
		s.logf("read request: %v", err)
		return
	}

	seq, env, reqPerms, err := decodeRequest(line)
	if err != nil {
		s.logf("decode request: %v", err)
		fmt.Fprintf(conn, "malformed request: %v\n", err)
		return
	}

	// dest/out are locally-meaningless placeholders here: the forwarder
	// prints its OWN start object using local-only dest/out values and
	// never trusts these fields (cmd/gotto-hando's forwardToBridge).
	if err := output.WriteStart(conn, true, "", ""); err != nil {
		s.logf("write start: %v", err)
		return
	}

	if seq.V != ir.SchemaVersion {
		msg := "IR schema version mismatch"
		_ = output.WriteAbortEvent(conn, true, output.EConnect, msg)
		s.logf("abort v=%d (want %d): %s", seq.V, ir.SchemaVersion, msg)
		return
	}

	if reqPerms {
		s.handleRequestPerms(conn)
		return
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	opt := engine.RunOptions{
		KeepGoing:  env.KeepGoing,
		CapOnError: env.CapOnError,
		// InlineCaptures is unconditionally true here: the bridge writes
		// nothing else to disk (help-remote.txt SESSION BRIDGE :135), so a
		// cap op must always come back as inline base64 data over the wire
		// rather than a file written into the bridge process's own working
		// directory. Without this, internal/engine/capture.go's
		// !st.inlineCaptures branch calls WriteCaptureFile with
		// CapturePath("", ...) - a stray on-disk write this fixes.
		InlineCaptures: true,
		// jsonl is always true and quiet always false here: JSONL is never
		// quiet-filtered (help.txt JSONL section) and this connection only
		// ever speaks JSONL. A write failure means the caller disconnected
		// (broken pipe); cancel runCtx so the engine's ctx.Err() check
		// (internal/engine/run.go) stops the rest of the run and still
		// releases held keys via its normal end-of-run releaseAll.
		OnResult: func(r output.Result) {
			if err := output.WriteResult(conn, true, false, r); err != nil {
				cancel()
			}
		},
	}
	sum := engine.Run(runCtx, s.Backend, seq, opt)

	if sum.Aborted {
		_ = output.WriteAbortEvent(conn, true, sum.AbortCode, sum.AbortMsg)
		s.logf("run aborted: code=%s msg=%q", sum.AbortCode, sum.AbortMsg)
		return
	}
	if err := output.WriteDone(conn, true, sum.Done); err != nil {
		// The caller is already gone; nothing more to send.
		s.logf("write done: %v (caller likely disconnected)", err)
		return
	}
	s.logf("run done: ok=%d err=%d skip=%d held_released=%d",
		sum.Done.OK, sum.Done.Err, sum.Done.Skip, sum.Done.HeldReleased)
}

// handleRequestPerms serves a --request-perms request (help-remote.txt
// SESSION BRIDGE :122-129): calls s.RequestPerms in the bridge's own GUI
// session (so the system permission prompts appear there) and writes the
// perms object, or an abort when the callback errors or is not wired.
// conn's "start" object has already been written by Handle before this is
// called.
func (s *Session) handleRequestPerms(conn io.ReadWriteCloser) {
	if s.RequestPerms == nil {
		msg := "request_perms not supported by this bridge"
		_ = output.WriteAbortEvent(conn, true, output.EValidate, msg)
		s.logf("abort request_perms: %s", msg)
		return
	}
	acc, scr, err := s.RequestPerms()
	if err != nil {
		_ = output.WriteAbortEvent(conn, true, output.EValidate, err.Error())
		s.logf("request_perms error: %v", err)
		return
	}
	if err := output.WritePermsEvent(conn, acc, scr); err != nil {
		s.logf("write perms: %v (caller likely disconnected)", err)
		return
	}
	s.logf("request_perms done: accessibility=%v screen=%v", acc, scr)
}

func (s *Session) logf(format string, args ...any) {
	if s.Log != nil {
		s.Log(fmt.Sprintf(format, args...))
	}
}
