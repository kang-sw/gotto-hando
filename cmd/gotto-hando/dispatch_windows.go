//go:build windows

package main

import (
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
	"github.com/kang-sw/gotto-hando/internal/output"
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
