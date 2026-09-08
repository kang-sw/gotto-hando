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

// TestPreflightAbortCarriesCode: run.go's Preflight wiring (260907-feat-
// darwin-backend Phase 1) unwraps a *backend.PreflightError from
// Backend.Preflight into Summary.Aborted/AbortCode/AbortMsg with
// Exit==output.AbortExit(code) (here E_SESSION -> exit 4, help.txt EXIT
// CODES). No line runs: Results/Done stay empty.
func TestPreflightAbortCarriesCode(t *testing.T) {
	seq := parse(t, "k[]a")
	be := &dryrun.Backend{FailOn: func(call string) error {
		if call == "Preflight" {
			return &backend.PreflightError{Code: output.ESession, Msg: "no unlocked GUI session (session=locked)"}
		}
		return nil
	}}
	sum := engine.Run(context.Background(), be, seq, engine.RunOptions{})

	if !sum.Aborted {
		t.Fatal("Aborted = false, want true")
	}
	if sum.AbortCode != output.ESession {
		t.Fatalf("AbortCode = %s, want %s", sum.AbortCode, output.ESession)
	}
	if sum.AbortMsg != "no unlocked GUI session (session=locked)" {
		t.Fatalf("AbortMsg = %q", sum.AbortMsg)
	}
	if sum.Exit != output.ExitPreflight {
		t.Fatalf("Exit = %d, want %d", sum.Exit, output.ExitPreflight)
	}
	if len(sum.Results) != 0 {
		t.Fatalf("Results = %v, want empty (aborted run touches no line)", sum.Results)
	}
	// The line after Preflight never reached the backend either.
	for _, c := range be.Calls {
		if c != "Preflight" {
			t.Fatalf("unexpected call %q after an aborted Preflight; calls=%v", c, be.Calls)
		}
	}
}

// TestPreflightAbortUncodedErrorIsUnknown: a Preflight error that is not a
// *backend.PreflightError still aborts (never runs a line), but with
// E_UNKNOWN since there is no code to unwrap - and E_UNKNOWN still maps to
// exit 4 (AbortExit's default case), not a distinct exit.
func TestPreflightAbortUncodedErrorIsUnknown(t *testing.T) {
	seq := parse(t, "k[]a")
	be := &dryrun.Backend{FailOn: func(call string) error {
		if call == "Preflight" {
			return errors.New("dlopen CoreGraphics: boom")
		}
		return nil
	}}
	sum := engine.Run(context.Background(), be, seq, engine.RunOptions{})

	if !sum.Aborted {
		t.Fatal("Aborted = false, want true")
	}
	if sum.AbortCode != output.EUnknown {
		t.Fatalf("AbortCode = %s, want %s", sum.AbortCode, output.EUnknown)
	}
	if sum.Exit != output.ExitPreflight {
		t.Fatalf("Exit = %d, want %d", sum.Exit, output.ExitPreflight)
	}
}

// TestResolveBoundsFailureIsLineErrorNotAbort: an out-of-bounds r-frame
// point fails only the ONE line with E_BOUNDS at run time (help.txt:262-
// 268: r/w/% coordinates are checked "right before their line runs", not
// during Preflight) - the run itself is not aborted, and exit is the
// normal run-time-failure code (1), not the abort code (4).
func TestResolveBoundsFailureIsLineErrorNotAbort(t *testing.T) {
	seq := parse(t, "m[r]0,-40")
	be := &dryrun.Backend{
		MousePosResult: backend.Point{X: 0, Y: 0},
		InfoResult:     backend.Info{DesktopX: 0, DesktopY: 0, DesktopW: 100, DesktopH: 100},
	}
	sum := engine.Run(context.Background(), be, seq, engine.RunOptions{})

	if sum.Aborted {
		t.Fatal("Aborted = true, want false (a bounds failure is a line error, not an abort)")
	}
	if len(sum.Results) != 1 || sum.Results[0].Status != "err" {
		t.Fatalf("Results = %+v, want a single err result", sum.Results)
	}
	if sum.Results[0].ErrCode != output.EBounds {
		t.Fatalf("ErrCode = %s, want %s", sum.Results[0].ErrCode, output.EBounds)
	}
	if sum.Exit != output.ExitRuntimeFailure {
		t.Fatalf("Exit = %d, want %d", sum.Exit, output.ExitRuntimeFailure)
	}
	// MouseMove must never have been called: resolve() failed first.
	for _, c := range be.Calls {
		if c == "MouseMove" || (len(c) >= 9 && c[:9] == "MouseMove") {
			t.Fatalf("MouseMove should not have been called after a bounds failure; calls=%v", be.Calls)
		}
	}
}

// synthMultiDisplayInfo is a two-display backend.Info fixture: display 0 is
// the primary at origin (0,0), display 1 sits to its right at (1440,0), both
// 1440x900 - the desktop union is 2880x900. Used to prove disp=N is
// translated relative to the NAMED display's own origin, not the desktop's
// (help.txt COORDINATES :250-253: m[disp=1]0,0 is display 1's top-left).
func synthMultiDisplayInfo() backend.Info {
	return backend.Info{
		DesktopX: 0, DesktopY: 0, DesktopW: 2880, DesktopH: 900,
		DisplayList: []backend.DisplayGeom{
			{X: 0, Y: 0, W: 1440, H: 900, Primary: true},
			{X: 1440, Y: 0, W: 1440, H: 900},
		},
	}
}

// TestResolveDisplayFrameTranslatesOrigin: m[disp=1]0,0 must resolve to the
// display's absolute origin (1440,0), not the untranslated (0,0) that
// compose.go's resolve() produced before the "display" case was added
// (Critical review finding: disp=N ignored the display's origin).
func TestResolveDisplayFrameTranslatesOrigin(t *testing.T) {
	seq := parse(t, "m[disp=1]0,0")
	be := &dryrun.Backend{InfoResult: synthMultiDisplayInfo()}
	sum := engine.Run(context.Background(), be, seq, engine.RunOptions{})

	if len(sum.Results) != 1 || sum.Results[0].Status != "ok" {
		t.Fatalf("Results = %+v, want a single ok result", sum.Results)
	}
	want := "MouseMove 1440,0"
	found := false
	for _, c := range be.Calls {
		if c == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("calls = %v, want a %q call", be.Calls, want)
	}
}

// TestResolveDisplayFramePercentUsesDisplaySize: m[disp=1]50%,50% must land
// at display 1's center (1440+720, 450) = (2160,450), not the desktop's
// center - resolve() must use the named display's W/H for percent frame
// sizes, not the desktop's.
func TestResolveDisplayFramePercentUsesDisplaySize(t *testing.T) {
	seq := parse(t, "m[disp=1]50%,50%")
	be := &dryrun.Backend{InfoResult: synthMultiDisplayInfo()}
	sum := engine.Run(context.Background(), be, seq, engine.RunOptions{})

	if len(sum.Results) != 1 || sum.Results[0].Status != "ok" {
		t.Fatalf("Results = %+v, want a single ok result", sum.Results)
	}
	want := "MouseMove 2160,450"
	found := false
	for _, c := range be.Calls {
		if c == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("calls = %v, want a %q call", be.Calls, want)
	}
}

// TestResolveDisplayFrameOutOfRangeIsBounds: an out-of-range disp= index
// (percent-flagged, so it reaches resolve()'s runtime path rather than
// Preflight) fails the line with E_BOUNDS, not a panic or E_INPUT.
func TestResolveDisplayFrameOutOfRangeIsBounds(t *testing.T) {
	seq := parse(t, "m[disp=5]50%,50%")
	be := &dryrun.Backend{InfoResult: synthMultiDisplayInfo()}
	sum := engine.Run(context.Background(), be, seq, engine.RunOptions{})

	if len(sum.Results) != 1 || sum.Results[0].Status != "err" {
		t.Fatalf("Results = %+v, want a single err result", sum.Results)
	}
	if sum.Results[0].ErrCode != output.EBounds {
		t.Fatalf("ErrCode = %s, want %s", sum.Results[0].ErrCode, output.EBounds)
	}
}
