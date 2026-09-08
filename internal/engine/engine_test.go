package engine_test

import (
	"context"
	"errors"
	"testing"

	"github.com/kang-sw/gotto-hando/internal/backend"
	"github.com/kang-sw/gotto-hando/internal/backend/dryrun"
	"github.com/kang-sw/gotto-hando/internal/engine"
	"github.com/kang-sw/gotto-hando/internal/ir"
	"github.com/kang-sw/gotto-hando/internal/output"
	"github.com/kang-sw/gotto-hando/internal/syntax"
)

var defaults = ir.Defaults{DelayMS: 0, TextIntervalMS: 0, KeyGapMS: 0}

func parse(t *testing.T, lines ...string) *ir.Sequence {
	t.Helper()
	seq, diags := syntax.Parse(lines, defaults)
	if len(diags) != 0 {
		t.Fatalf("parse diags: %+v", diags)
	}
	if d := ir.Validate(seq, len(lines)); len(d) != 0 {
		t.Fatalf("validate diags: %+v", d)
	}
	return seq
}

func statuses(sum engine.Summary) []string {
	var s []string
	for _, r := range sum.Results {
		s = append(s, r.Status)
	}
	return s
}

// TestFailFast: a failed line stops the run; later lines are skip; exit 1
// (help.txt:519-520).
func TestFailFast(t *testing.T) {
	seq := parse(t, "k[]a", "k[]b", "k[]c")
	be := &dryrun.Backend{FailOn: func(call string) error {
		if call == "KeyDown b" {
			return errors.New("injected")
		}
		return nil
	}}
	sum := engine.Run(context.Background(), be, seq, engine.RunOptions{})

	if got := statuses(sum); len(got) != 3 || got[0] != "ok" || got[1] != "err" || got[2] != "skip" {
		t.Fatalf("statuses = %v, want [ok err skip]", got)
	}
	if sum.OK != 1 || sum.Err != 1 || sum.Skip != 1 {
		t.Fatalf("counts ok=%d err=%d skip=%d, want 1/1/1", sum.OK, sum.Err, sum.Skip)
	}
	if sum.Exit != 1 {
		t.Fatalf("exit = %d, want 1", sum.Exit)
	}
	// The line after the failure never reached the backend.
	for _, c := range be.Calls {
		if c == "KeyDown c" {
			t.Fatalf("line 3 should not have run; calls=%v", be.Calls)
		}
	}
}

// TestKeepGoing: -k continues after a failure; final exit is still 1
// (help.txt:521-523).
func TestKeepGoing(t *testing.T) {
	seq := parse(t, "k[]a", "k[]b", "k[]c")
	be := &dryrun.Backend{FailOn: func(call string) error {
		if call == "KeyDown b" {
			return errors.New("injected")
		}
		return nil
	}}
	sum := engine.Run(context.Background(), be, seq, engine.RunOptions{KeepGoing: true})

	if got := statuses(sum); len(got) != 3 || got[0] != "ok" || got[1] != "err" || got[2] != "ok" {
		t.Fatalf("statuses = %v, want [ok err ok]", got)
	}
	if sum.OK != 2 || sum.Err != 1 || sum.Skip != 0 {
		t.Fatalf("counts ok=%d err=%d skip=%d, want 2/1/0", sum.OK, sum.Err, sum.Skip)
	}
	if sum.Exit != 1 {
		t.Fatalf("exit = %d, want 1", sum.Exit)
	}
}

// TestHeldKeyReverseRelease: keys held by kd are released in reverse order
// at run end, counted in held_released and reported as warn (help.txt:314,
// :515-516, :566).
func TestHeldKeyReverseRelease(t *testing.T) {
	seq := parse(t, "kd[]shift", "kd[]ctrl")
	be := &dryrun.Backend{}
	sum := engine.Run(context.Background(), be, seq, engine.RunOptions{})

	if sum.HeldReleased != 2 || sum.Done.HeldReleased != 2 {
		t.Fatalf("held_released = %d/%d, want 2", sum.HeldReleased, sum.Done.HeldReleased)
	}
	last2 := be.Calls[len(be.Calls)-2:]
	if last2[0] != "KeyUp ctrl" || last2[1] != "KeyUp shift" {
		t.Fatalf("release order = %v, want [KeyUp ctrl, KeyUp shift]", last2)
	}
	warns := 0
	for _, r := range sum.Results {
		if r.Status == "warn" {
			warns++
		}
	}
	if warns != 2 {
		t.Fatalf("warn results = %d, want 2", warns)
	}
	if sum.Exit != 0 {
		t.Fatalf("exit = %d, want 0", sum.Exit)
	}
}

// TestHeldReleaseOnFailure: held keys are released even when the run fails
// fast (help.txt:520).
func TestHeldReleaseOnFailure(t *testing.T) {
	seq := parse(t, "kd[]shift", "k[]a", "k[]b")
	be := &dryrun.Backend{FailOn: func(call string) error {
		if call == "KeyDown a" {
			return errors.New("injected")
		}
		return nil
	}}
	sum := engine.Run(context.Background(), be, seq, engine.RunOptions{})

	if sum.HeldReleased != 1 {
		t.Fatalf("held_released = %d, want 1", sum.HeldReleased)
	}
	if be.Calls[len(be.Calls)-1] != "KeyUp shift" {
		t.Fatalf("last call = %q, want KeyUp shift", be.Calls[len(be.Calls)-1])
	}
	if sum.Exit != 1 {
		t.Fatalf("exit = %d, want 1", sum.Exit)
	}
}

// TestDoneCounts checks a clean run's tallies.
func TestDoneCounts(t *testing.T) {
	seq := parse(t, "k[]a", "m[]1,2", "c[]3,4")
	be := &dryrun.Backend{}
	sum := engine.Run(context.Background(), be, seq, engine.RunOptions{})
	if sum.OK != 3 || sum.Err != 0 || sum.Skip != 0 || sum.Exit != 0 {
		t.Fatalf("ok=%d err=%d skip=%d exit=%d, want 3/0/0/0", sum.OK, sum.Err, sum.Skip, sum.Exit)
	}
}

// TestWinNoMatch: win with no match is a failure (help.txt:524); qwin with
// zero matches is not.
func TestWinMatchPolicy(t *testing.T) {
	seq := parse(t, "qwin", "win[]Nope")
	be := &dryrun.Backend{} // WindowsResult is nil (empty)
	sum := engine.Run(context.Background(), be, seq, engine.RunOptions{KeepGoing: true})
	if sum.Results[0].Status != "ok" {
		t.Errorf("qwin status = %s, want ok", sum.Results[0].Status)
	}
	if sum.Results[1].Status != "err" {
		t.Errorf("win status = %s, want err", sum.Results[1].Status)
	}
}

// indexOf returns the index of the first call equal to s, or -1.
func indexOf(calls []string, s string) int {
	for i, c := range calls {
		if c == s {
			return i
		}
	}
	return -1
}

// TestKeepGoingReleasesPartialKeyDown covers the -k "keys taken by the
// failed line are released immediately" clause (help.txt:531-532). A
// multi-key kd fails on its second key; the first key it already pressed
// must be released right away (before the next line runs), NOT lingering in
// held until end-of-run releaseAll.
func TestKeepGoingReleasesPartialKeyDown(t *testing.T) {
	seq := parse(t, "kd[]ctrl+shift", "k[]z")
	be := &dryrun.Backend{FailOn: func(call string) error {
		if call == "KeyDown shift" {
			return errors.New("injected")
		}
		return nil
	}}
	sum := engine.Run(context.Background(), be, seq, engine.RunOptions{KeepGoing: true})

	if got := statuses(sum); len(got) < 2 || got[0] != "err" || got[1] != "ok" {
		t.Fatalf("statuses = %v, want [err ok ...]", got)
	}
	// The partially-pressed ctrl is released immediately, before the next
	// line reaches the backend.
	up := indexOf(be.Calls, "KeyUp ctrl")
	next := indexOf(be.Calls, "KeyDown z")
	if up < 0 {
		t.Fatalf("ctrl was not released immediately; calls=%v", be.Calls)
	}
	if next >= 0 && up > next {
		t.Fatalf("ctrl released (idx %d) after next line ran (idx %d); calls=%v", up, next, be.Calls)
	}
	// Nothing lingers to end-of-run releaseAll: the rollback already freed
	// ctrl and shift was never pressed.
	if sum.HeldReleased != 0 {
		t.Fatalf("held_released = %d, want 0 (failed line's key released immediately, not at end)", sum.HeldReleased)
	}
	// Exactly one KeyUp ctrl (the immediate rollback), no double-release.
	ups := 0
	for _, c := range be.Calls {
		if c == "KeyUp ctrl" {
			ups++
		}
	}
	if ups != 1 {
		t.Fatalf("KeyUp ctrl called %d times, want 1; calls=%v", ups, be.Calls)
	}
	if sum.Exit != 1 {
		t.Fatalf("exit = %d, want 1", sum.Exit)
	}
}

// TestExecTimeoutNotSoftened: exec timeout is E_TIMEOUT and noerr never
// softens it (help.txt:390-391, :536). A backend reporting TimedOut fails
// the line with E_TIMEOUT even when the line has noerr.
func TestExecTimeoutNotSoftened(t *testing.T) {
	seq := parse(t, "exec[noerr,timeout=1s]sleep 5")
	be := &dryrun.Backend{ExecResult: backend.ExecResult{TimedOut: true}}
	sum := engine.Run(context.Background(), be, seq, engine.RunOptions{})

	if sum.Results[0].Status != "err" {
		t.Fatalf("status = %s, want err (timeout not softened by noerr)", sum.Results[0].Status)
	}
	if sum.Results[0].ErrCode != output.ETimeout {
		t.Fatalf("code = %s, want E_TIMEOUT", sum.Results[0].ErrCode)
	}
	if sum.Exit != 1 {
		t.Fatalf("exit = %d, want 1", sum.Exit)
	}
}

// TestExecNonZeroSoftenedByNoerr is the contrast: a non-zero exit (not a
// timeout) IS softened by noerr, so the line is ok.
func TestExecNonZeroSoftenedByNoerr(t *testing.T) {
	seq := parse(t, "exec[noerr]grep -q x f")
	be := &dryrun.Backend{ExecResult: backend.ExecResult{Exit: 1}}
	sum := engine.Run(context.Background(), be, seq, engine.RunOptions{})
	if sum.Results[0].Status != "ok" {
		t.Fatalf("status = %s, want ok (non-zero exit softened by noerr)", sum.Results[0].Status)
	}
	if sum.Exit != 0 {
		t.Fatalf("exit = %d, want 0", sum.Exit)
	}
}
