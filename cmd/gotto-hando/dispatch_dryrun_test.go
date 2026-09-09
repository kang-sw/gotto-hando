//go:build dryrun

package main

import (
	"io"
	"strings"
	"testing"
)

// This file only compiles into the `-tags dryrun` build (the same tag
// 260908-feat-remote-ssh Phase 1's fake-ssh integration harness uses for
// its "remote" binary, main_test.go's TestMain) - it drives run() directly,
// in-process, against dryrun.Backend (via lastDryrunBackend,
// dispatch_dryrun.go) so a held key's release can be asserted from its
// recorded Calls log, which is impossible across the subprocess boundary
// the CLI-level tests in remote_test.go use. Run explicitly via:
//
//	go test -tags dryrun ./cmd/gotto-hando -run TestHeldKeyReleasedOnStdoutWriteFailure

// failAfterWriter fails every Write call after the Nth, simulating the
// ssh-spawned remote's own stdout pipe breaking mid-run (SIGPIPE/EPIPE on
// the far side) - the scenario dispatch.go's local --jsonl path must
// react to by cancelling the run (review T3).
type failAfterWriter struct {
	n    int
	seen int
}

func (w *failAfterWriter) Write(p []byte) (int, error) {
	w.seen++
	if w.seen > w.n {
		return 0, io.ErrClosedPipe
	}
	return len(p), nil
}

// TestHeldKeyReleasedOnStdoutWriteFailure asserts that when the local
// --jsonl path's OnResult write to stdout starts failing (the ssh-spawned
// remote's own channel dying mid-run), the engine both stops running
// further ops AND still releases the already-held key through its normal
// end-of-run releaseAll (ticket Phase 1 verification: "the remote process,
// on stdout EOF mid-run, releases held keys through the dry-run backend").
func TestHeldKeyReleasedOnStdoutWriteFailure(t *testing.T) {
	lastDryrunBackend = nil

	// kd[]a holds 'a' with nothing to release it; four qinfo lines follow
	// so there is something left to (wrongly) keep running if
	// cancellation does not happen. write #1 is the JSONL "start" line;
	// allowing only that one through means the very first OnResult write
	// (kd's own result) is what fails and triggers cancel().
	args := []string{"--jsonl", "local", "kd[]a", "qinfo", "qinfo", "qinfo", "qinfo"}
	out := &failAfterWriter{n: 1}
	_ = run(args, strings.NewReader(""), out, io.Discard)

	if lastDryrunBackend == nil {
		t.Fatal("newLocalBackend was never called")
	}
	calls := lastDryrunBackend.Calls

	releasedA := false
	for _, c := range calls {
		if c == "KeyUp a" {
			releasedA = true
		}
	}
	if !releasedA {
		t.Errorf("Backend.Calls = %v, want a \"KeyUp a\" release", calls)
	}

	// Cancellation must stop the engine before any of the four qinfo
	// lines execute: Info() records exactly "Info" (internal/backend/
	// dryrun/dryrun.go), so zero Info calls is the evidence the run
	// stopped promptly instead of blindly finishing every remaining op
	// on the (simulated) dead connection.
	infoCalls := 0
	for _, c := range calls {
		if c == "Info" {
			infoCalls++
		}
	}
	if infoCalls != 0 {
		t.Errorf("Info calls = %d, want 0 (the run should have been cancelled before any qinfo executed); calls=%v", infoCalls, calls)
	}
}

// TestHeldKeyReleasedOnCleanRun is the control case: with no write
// failure, a held key with no matching release still gets released by the
// engine's normal end-of-run releaseAll, and every op runs (four Info
// calls). This isolates what the cancellation actually changes (stopping
// early), rather than the release itself, which run.go's own comment
// notes happens unconditionally either way.
func TestHeldKeyReleasedOnCleanRun(t *testing.T) {
	lastDryrunBackend = nil

	args := []string{"--jsonl", "local", "kd[]a", "qinfo", "qinfo", "qinfo", "qinfo"}
	var out strings.Builder
	code := run(args, strings.NewReader(""), &out, io.Discard)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stdout=%q)", code, out.String())
	}

	if lastDryrunBackend == nil {
		t.Fatal("newLocalBackend was never called")
	}
	calls := lastDryrunBackend.Calls

	releasedA := false
	infoCalls := 0
	for _, c := range calls {
		if c == "KeyUp a" {
			releasedA = true
		}
		if c == "Info" {
			infoCalls++
		}
	}
	if !releasedA {
		t.Errorf("Backend.Calls = %v, want a \"KeyUp a\" release", calls)
	}
	if infoCalls != 4 {
		t.Errorf("Info calls = %d, want 4 (a clean run executes every op)", infoCalls)
	}
}
