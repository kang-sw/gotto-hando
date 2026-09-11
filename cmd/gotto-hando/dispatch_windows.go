//go:build windows && !dryrun

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
	winbackend "github.com/kang-sw/gotto-hando/internal/backend/windows"
	"github.com/kang-sw/gotto-hando/internal/bridge"
	"github.com/kang-sw/gotto-hando/internal/ir"
	"github.com/kang-sw/gotto-hando/internal/output"
	"github.com/kang-sw/gotto-hando/internal/syntax"
)

// newLocalBackend constructs the real windows backend (dispatch.go's
// dest=="local" path). See dispatch_darwin.go for the darwin equivalent
// and dispatch_other.go for every remaining GOOS (remote is a separate
// ticket).
func newLocalBackend() (backend.Backend, error) {
	return winbackend.New()
}

// requestPerms is macOS-only (help.txt --request-perms :106-108): on
// Windows it is a usage error, abort E_VALIDATE / exit 2 before anything
// runs ("--request-perms is macOS only").
func requestPerms(opts parsedOptions, stdout, stderr io.Writer, abort func(output.ErrorCode, string) int) int {
	return abort(output.EValidate, "--request-perms is macOS only")
}

// runBridge runs the session bridge in the foreground until interrupted
// (Ctrl-C) or killed (help.txt --bridge :83-89, help-remote.txt SESSION
// BRIDGE). It never emits JSONL/plain result output of its own - only the
// "already running" failure path goes through the normal abort()
// machinery, matching how every other not-yet-implemented option in this
// file already reports its usage error.
func runBridge(opts parsedOptions, stdout, stderr io.Writer, abort func(output.ErrorCode, string) int) int {
	listener, err := winbackend.ListenBridge()
	if err != nil {
		if errors.Is(err, winbackend.ErrBridgeAlreadyRunning) {
			return abort(output.EValidate, "a gotto-hando bridge for this user is already running")
		}
		return abort(output.EValidate, err.Error())
	}
	defer listener.Close()

	logLine, closeLog := openBridgeLog(stderr)
	defer closeLog()

	be, err := winbackend.NewBridge()
	if err != nil {
		logLine("startup error: " + err.Error())
		return abort(output.EValidate, err.Error())
	}
	sess := &bridge.Session{Backend: be, Log: logLine}

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
			if errors.Is(err, winbackend.ErrListenerClosed) {
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
// %LOCALAPPDATA%\gotto-hando\bridge.log for append and returns a
// one-line-per-run/error sink writing "<RFC3339 timestamp> <line>\n" to
// it, plus a closer. If the log cannot be opened (missing
// %LOCALAPPDATA%, permission error), logging falls back to stderr rather
// than aborting the bridge over a logging failure - the bridge's own
// operation does not depend on the log.
func openBridgeLog(stderr io.Writer) (logLine func(string), closeFn func()) {
	dir := os.Getenv("LOCALAPPDATA")
	if dir == "" {
		fmt.Fprintln(stderr, "warning: %LOCALAPPDATA% not set, bridge.log disabled")
		return func(string) {}, func() {}
	}
	path := filepath.Join(dir, "gotto-hando", "bridge.log")
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

// shouldForwardToBridge is windows's half of dispatch.go's per-GOOS
// forwarding seam (help-remote.txt SESSION BRIDGE "Detection"): every run
// from a non-console session (ssh, RDP-without-console, a service) goes to
// `gotto-hando --bridge`. Query-only runs must take this path too: qinfo,
// qdisp and qmouse describe the desktop that later GUI operations use, not
// the ssh-side Session 0 process. Session exemptions remain a Preflight
// policy only; they must not decide transport routing.
func shouldForwardToBridge(_ *ir.Sequence) bool {
	return winbackend.IsRemoteSession()
}

// dialBridge is a seam over winbackend.DialBridge so tests can substitute
// a net.Pipe() end driven by an in-process bridge.Session instead of a
// real named pipe.
var dialBridge = winbackend.DialBridge

// forwardToBridge implements the windows-only local->bridge forwarding
// path (dispatch.go's shouldForwardToBridge branch): dial the already-
// running `gotto-hando --bridge` listener for this user, forward seq plus
// a run envelope built from opts, and relay the bridge's JSONL response
// stream back to stdout in whichever form (plain/--jsonl) the caller
// asked for - reusing engine.ResultDetailFromJSON to reconstruct plain
// Detail/Extra text from the JSONL-only wire (help-remote.txt SESSION
// BRIDGE).
func forwardToBridge(opts parsedOptions, seq *ir.Sequence, stdout, stderr io.Writer, abort func(output.ErrorCode, string) int) int {
	username, err := winbackend.CurrentUsername()
	if err != nil {
		return abort(output.ESession, "start `gotto-hando --bridge` in the logged-on GUI session")
	}
	conn, err := dialBridge(winbackend.PipeName(username))
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
	startedAt := time.Now()
	ok, errN, skip := 0, 0, 0
	stateUnknownDone := func() int {
		_ = output.WriteDone(stdout, opts.JSONL, output.Done{
			OK: ok, Err: errN, Skip: skip, ElapsedMS: time.Since(startedAt).Milliseconds(), StateUnknown: true,
		})
		return output.ExitStateUnknown
	}

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
				return stateUnknownDone()
			}
			if done.Err > 0 {
				return output.ExitRuntimeFailure
			}
			return output.ExitOK
		}
		if err := relayResult(opts, stdout, line); err != nil {
			return stateUnknownDone()
		}
		resultOK, resultErr, resultSkip := resultCounts(line)
		ok += resultOK
		errN += resultErr
		skip += resultSkip
		if !scanner.Scan() {
			return stateUnknownDone()
		}
		line = append([]byte(nil), scanner.Bytes()...)
	}
}

// decodeAbortEvent/wireDone/decodeDoneEvent/relayResult/relayDone moved to
// bridge_relay.go (260908-feat-remote-ssh Phase 2): darwin's own
// forwardToBridge/requestPerms now need the identical wire-decode/relay
// logic, so these are shared from a build-tag-free file instead of
// duplicated per GOOS.
