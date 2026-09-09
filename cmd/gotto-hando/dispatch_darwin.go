//go:build darwin && !dryrun

package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/kang-sw/gotto-hando/internal/backend"
	"github.com/kang-sw/gotto-hando/internal/backend/darwin"
	"github.com/kang-sw/gotto-hando/internal/bridge"
	"github.com/kang-sw/gotto-hando/internal/ir"
	"github.com/kang-sw/gotto-hando/internal/output"
	"github.com/kang-sw/gotto-hando/internal/remote"
	"github.com/kang-sw/gotto-hando/internal/syntax"
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
// the perms result and exits 0 when both are ok, else 4. dispatch.go only
// ever calls this with opts.Dest == "local" - a non-local dest routes to
// cmd/gotto-hando/remote.go's runRemoteRequestPerms before requestPerms is
// ever reached (260908-feat-remote-ssh Phase 1).
//
// When this process is itself ssh-started (darwin.IsRemoteSession), it has
// no GUI session of its own to raise the prompts in, so it forwards the
// request-perms call to `gotto-hando --bridge` instead
// (help-macos.txt SESSION BRIDGE FOR SSH (REMOTE MAC): "gotto-hando <dest>
// --request-perms ... calls AXIsProcessTrustedWithOptions/
// CGRequestScreenCaptureAccess in its own GUI session, so the system
// prompts appear there") - mirroring forwardToBridge's dial/response
// handling but for the request_perms wire message instead of a run.
func requestPerms(opts parsedOptions, stdout, stderr io.Writer, abort func(output.ErrorCode, string) int) int {
	if !darwin.IsRemoteSession() {
		acc, scr, err := darwin.RequestPerms()
		if err != nil {
			return abort(output.EValidate, err.Error())
		}
		return writePermsResult(opts, stdout, acc, scr)
	}

	conn, err := dialBridge()
	if err != nil {
		return abort(output.ESession, "start `gotto-hando --bridge` in the logged-on GUI session")
	}
	defer conn.Close()

	if _, err := conn.Write(append(bridge.EncodeRequestPerms(), '\n')); err != nil {
		return abort(output.EConnect, "bridge connection failed: "+err.Error())
	}

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 4096), 16*1024*1024)

	// First line is always the bridge's own start object (internal/bridge/
	// session.go's Handle writes it unconditionally) - discard it, this
	// forwarder builds its own start/perms output via writePermsResult
	// instead (mirrors forwardToBridge's identical discard).
	if !scanner.Scan() {
		return abort(output.EConnect, "bridge connection closed before responding")
	}

	if !scanner.Scan() {
		return abort(output.EConnect, "bridge connection closed before responding")
	}
	line := append([]byte(nil), scanner.Bytes()...)
	if code, msg, isAbort := decodeAbortEvent(line); isAbort {
		return abort(code, msg)
	}
	if p, ok := remote.DecodePerms(line); ok {
		return writePermsResult(opts, stdout, p.Accessibility == "ok", p.Screen == "ok")
	}
	return abort(output.EConnect, "bridge connection failed: unexpected response")
}

// runBridge is macOS's session-bridge entry point (dispatch.go's
// opts.Bridge branch, 260908-feat-remote-ssh Phase 2): listen on the
// current-user unix socket, serve one connection at a time via
// bridge.Session, until interrupted (Ctrl-C) or killed. Mirrors
// dispatch_windows.go's runBridge exactly, modulo the transport
// (darwin.ListenBridge's unix socket vs. winbackend.ListenBridge's named
// pipe).
func runBridge(opts parsedOptions, stdout, stderr io.Writer, abort func(output.ErrorCode, string) int) int {
	listener, err := darwin.ListenBridge()
	if err != nil {
		if errors.Is(err, darwin.ErrBridgeAlreadyRunning) {
			return abort(output.EValidate, "a gotto-hando bridge for this user is already running")
		}
		return abort(output.EValidate, err.Error())
	}
	defer listener.Close()

	logLine, closeLog := openBridgeLog(stderr)
	defer closeLog()

	be, err := darwin.NewBridge()
	if err != nil {
		logLine("startup error: " + err.Error())
		return abort(output.EValidate, err.Error())
	}
	sess := &bridge.Session{Backend: be, Log: logLine, RequestPerms: darwin.RequestPerms}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	defer signal.Stop(sig)
	go func() {
		<-sig
		listener.Close()
	}()

	logLine("bridge started")
	for {
		conn, err := listener.Accept()
		if err != nil {
			if errors.Is(err, darwin.ErrListenerClosed) {
				logLine("bridge stopped (interrupted)")
				return output.ExitOK
			}
			logLine("accept error: " + err.Error())
			continue
		}
		sess.Handle(context.Background(), conn)
	}
}

// openBridgeLog opens (creating parent directories as needed)
// ~/Library/Logs/gotto-hando/bridge.log for append and returns a
// one-line-per-run/error sink writing "<RFC3339 timestamp> <line>\n" to
// it, plus a closer. If the log cannot be opened (no $HOME, permission
// error), logging falls back to stderr rather than aborting the bridge
// over a logging failure - the bridge's own operation does not depend on
// the log. Identical shape to dispatch_windows.go's openBridgeLog, modulo
// the log path (%LOCALAPPDATA% there, $HOME here).
func openBridgeLog(stderr io.Writer) (logLine func(string), closeFn func()) {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(stderr, "warning: $HOME not set, bridge.log disabled")
		return func(string) {}, func() {}
	}
	path := filepath.Join(home, "Library", "Logs", "gotto-hando", "bridge.log")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		fmt.Fprintf(stderr, "warning: bridge.log disabled: %v\n", err)
		return func(string) {}, func() {}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		fmt.Fprintf(stderr, "warning: bridge.log disabled: %v\n", err)
		return func(string) {}, func() {}
	}
	return func(line string) {
			fmt.Fprintf(f, "%s %s\n", time.Now().UTC().Format(time.RFC3339), line)
		}, func() {
			_ = f.Close()
		}
}

// shouldForwardToBridge is darwin's half of dispatch.go's per-GOOS
// forwarding seam (help-remote.txt SESSION BRIDGE "Detection",
// 260908-feat-remote-ssh Phase 2): an ssh-started process running a
// sequence that actually needs the interactive GUI session forwards to
// `gotto-hando --bridge` instead of touching the real local backend
// directly - darwin.IsRemoteSession alone is not enough, since an
// all-exempt sequence (e.g. a lone qinfo/qclip) never needs the bridge at
// all (darwin.RequiresSession mirrors backend/darwin/preflight.go's own
// session gate).
func shouldForwardToBridge(seq *ir.Sequence) bool {
	return darwin.IsRemoteSession() && darwin.RequiresSession(seq)
}

// dialBridge is a seam over darwin.DialBridge so tests can substitute a
// net.Pipe() end driven by an in-process bridge.Session instead of a real
// unix socket. Unlike windows's DialBridge, darwin's takes no name
// argument (the socket path is already per-user under $HOME) - the two
// files never compile together, so the differing seam function type is
// fine.
var dialBridge = darwin.DialBridge

// forwardToBridge implements darwin's local->bridge forwarding path
// (dispatch.go's shouldForwardToBridge branch, 260908-feat-remote-ssh
// Phase 2): dial the already-running `gotto-hando --bridge` listener for
// this user, forward seq plus a run envelope built from opts, and relay
// the bridge's JSONL response stream back to stdout in whichever form
// (plain/--jsonl) the caller asked for. Mirrors dispatch_windows.go's
// forwardToBridge exactly, modulo the transport (no username/pipe-name
// resolution needed here).
func forwardToBridge(opts parsedOptions, seq *ir.Sequence, stdout, stderr io.Writer, abort func(output.ErrorCode, string) int) int {
	conn, err := dialBridge()
	if err != nil {
		return abort(output.ESession, "start `gotto-hando --bridge` in the logged-on GUI session")
	}
	defer conn.Close()

	deadlineMS := 0
	if opts.Timeout != "" {
		if ms, ok := syntax.ParseDurationMS(opts.Timeout); ok {
			deadlineMS = ms
		}
	}
	req, err := bridge.EncodeRequest(seq, bridge.RunEnvelope{
		DeadlineMS: deadlineMS,
		KeepGoing:  opts.KeepGoing,
		Quiet:      opts.Quiet,
		CapOnError: opts.CapOnError,
	})
	if err != nil {
		return abort(output.EValidate, err.Error())
	}
	if _, err := conn.Write(append(req, '\n')); err != nil {
		return abort(output.EConnect, "bridge connection failed: "+err.Error())
	}

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 4096), 16*1024*1024)

	// First line is always the bridge's own start object
	// (internal/bridge/session.go's Handle writes it unconditionally,
	// before checking anything else) - discard it, this forwarder prints
	// its own start object instead (using local-only dest/out values).
	if !scanner.Scan() {
		return abort(output.EConnect, "bridge connection closed before responding")
	}

	// The next line decides the outcome: an immediate "abort" (e.g. IR
	// schema mismatch) means no local start is ever printed, matching the
	// local run's own plain-mode "no start on the abort path" contract
	// (output.WriteAbort's doc comment) - so route it through the same
	// abort() closure dispatch.go already built. Anything else is the
	// first content event, after which this forwarder commits to its own
	// start object.
	if !scanner.Scan() {
		return abort(output.EConnect, "bridge connection closed before responding")
	}
	line := append([]byte(nil), scanner.Bytes()...)
	if code, msg, isAbort := decodeAbortEvent(line); isAbort {
		return abort(code, msg)
	}

	outDir := output.DefaultOutDir(opts.Out, opts.Dest, newRunID())
	_ = output.WriteStart(stdout, opts.JSONL, opts.Dest, outDir)

	for {
		if code, msg, isAbort := decodeAbortEvent(line); isAbort {
			// Defensive only: the protocol never sends "abort" after
			// "start" (Handle returns immediately on its own abort path),
			// but if it ever did, WriteAbortEvent (not abort()) avoids
			// double-printing the start object we already committed to.
			_ = output.WriteAbortEvent(stdout, opts.JSONL, code, msg)
			return output.AbortExit(code)
		}
		if done, ok := decodeDoneEvent(line); ok {
			if err := relayDone(opts, stdout, line, done); err != nil {
				return abort(output.EConnect, "bridge connection failed: "+err.Error())
			}
			if done.Err > 0 {
				return output.ExitRuntimeFailure
			}
			return output.ExitOK
		}
		if err := relayResult(opts, stdout, line); err != nil {
			return abort(output.EConnect, "bridge connection failed: "+err.Error())
		}
		if !scanner.Scan() {
			return abort(output.EConnect, "bridge connection closed mid-run")
		}
		line = append([]byte(nil), scanner.Bytes()...)
	}
}
