package main

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/kang-sw/gotto-hando/internal/engine"
	"github.com/kang-sw/gotto-hando/internal/output"
)

// bridge_relay.go holds the bridge-forwarding wire-decode/relay helpers
// shared by every platform that has a local->bridge forwarding path
// (currently windows's dispatch_windows.go and darwin's
// dispatch_darwin.go, 260908-feat-remote-ssh Phase 0/2). It carries no
// build tag - it imports nothing platform-specific - so both files' own
// forwardToBridge/requestPerms implementations can call these symbols
// directly instead of duplicating them per GOOS.

// decodeAbortEvent reports whether line is a bridge "abort" event
// (internal/output.WriteAbortEvent's jsonl shape) and, if so, its code/msg.
func decodeAbortEvent(line []byte) (code output.ErrorCode, msg string, ok bool) {
	var v struct {
		Event string `json:"event"`
		Code  string `json:"code"`
		Msg   string `json:"msg"`
	}
	if json.Unmarshal(line, &v) != nil || v.Event != "abort" {
		return "", "", false
	}
	return output.ErrorCode(v.Code), v.Msg, true
}

// wireDone is the bridge "done" event's decoded shape (internal/output.
// WriteDone's jsonl fields).
type wireDone struct {
	Event        string `json:"event"`
	OK           int    `json:"ok"`
	Err          int    `json:"err"`
	Skip         int    `json:"skip"`
	ElapsedMS    int64  `json:"elapsed_ms"`
	HeldReleased int    `json:"held_released"`
	State        string `json:"state"`
}

func decodeDoneEvent(line []byte) (wireDone, bool) {
	var d wireDone
	if json.Unmarshal(line, &d) != nil || d.Event != "done" {
		return wireDone{}, false
	}
	return d, true
}

// resultStatus returns a valid bridge result status for the forwarder's
// post-start connection-loss summary. It intentionally ignores malformed or
// non-result frames, which relayResult preserves under the existing contract.
func resultStatus(line []byte) string {
	var event struct {
		Status string `json:"status"`
	}
	if json.Unmarshal(line, &event) != nil {
		return ""
	}
	switch event.Status {
	case "ok", "err", "skip", "warn":
		return event.Status
	default:
		return ""
	}
}

// relayResult prints one bridge per-line result event to stdout in
// whichever form the caller asked for. --jsonl forwards the line
// byte-for-byte (the bridge's own writer, internal/output.WriteResult, is
// the exact function the local run path already uses, so the wire shape
// is already identical); plain mode decodes it and rebuilds Detail/Extra
// via engine.ResultDetailFromJSON.
func relayResult(opts parsedOptions, stdout io.Writer, line []byte) error {
	if opts.JSONL {
		_, err := stdout.Write(append(append([]byte(nil), line...), '\n'))
		return err
	}
	var w struct {
		Line   int    `json:"line"`
		Status string `json:"status"`
		Cmd    string `json:"cmd"`
		Src    string `json:"src"`
		Code   string `json:"code"`
		Msg    string `json:"msg"`
	}
	if err := json.Unmarshal(line, &w); err != nil {
		return nil
	}
	res := output.Result{Line: w.Line, Status: w.Status, Cmd: w.Cmd, Src: w.Src, ErrMsg: w.Msg, ErrCode: output.ErrorCode(w.Code)}
	if w.Status != "err" {
		var fields map[string]json.RawMessage
		if json.Unmarshal(line, &fields) == nil {
			if detail, extra, alwaysShow, ok := engine.ResultDetailFromJSON(w.Cmd, fields); ok {
				res.Detail, res.Extra, res.AlwaysShow = detail, extra, alwaysShow
			}
		}
	}
	return output.WriteResult(stdout, false, opts.Quiet, res)
}

// relayDone prints the bridge's "done" event to stdout - verbatim in
// --jsonl mode, reformatted via output.WriteDone in plain mode.
func relayDone(opts parsedOptions, stdout io.Writer, line []byte, d wireDone) error {
	if opts.JSONL {
		_, err := stdout.Write(append(append([]byte(nil), line...), '\n'))
		return err
	}
	return output.WriteDone(stdout, false, output.Done{
		OK: d.OK, Err: d.Err, Skip: d.Skip, ElapsedMS: d.ElapsedMS,
		HeldReleased: d.HeldReleased, StateUnknown: d.State == "unknown",
	})
}

// writePermsResult prints --request-perms' terminal output and returns its
// exit code (help.txt --request-perms :91-108, JSONL "--request-perms"
// :617-619): plain mode writes the existing "perms=accessibility:...,
// screen:...\n" line; --jsonl mode writes the start object followed by the
// perms object (output.WriteStart + output.WritePermsEvent), matching what
// a bridge-forwarded run's own JSONL stream already carries. Shared by
// dispatch_darwin.go's direct (darwin.RequestPerms) and bridge-forwarded
// (over the unix socket) requestPerms paths so both stay byte-identical.
func writePermsResult(opts parsedOptions, stdout io.Writer, accessibility, screen bool) int {
	if opts.JSONL {
		outDir := output.DefaultOutDir(opts.Out, opts.Dest, newRunID())
		_ = output.WriteStart(stdout, true, opts.Dest, outDir)
		_ = output.WritePermsEvent(stdout, accessibility, screen)
	} else {
		fmt.Fprintf(stdout, "perms=accessibility:%s,screen:%s\n", okMissing(accessibility), okMissing(screen))
	}
	if accessibility && screen {
		return output.ExitOK
	}
	return output.ExitPreflight
}

func okMissing(ok bool) string {
	if ok {
		return "ok"
	}
	return "missing"
}
