package engine_test

import (
	"context"
	"testing"

	"github.com/kang-sw/gotto-hando/internal/backend"
	"github.com/kang-sw/gotto-hando/internal/backend/dryrun"
	"github.com/kang-sw/gotto-hando/internal/engine"
	"github.com/kang-sw/gotto-hando/internal/output"
)

// focusedWin is a single OS-focused window fixture for the w-frame resolve
// tests: origin (0,0), 1280x800, Focused so currentWindow picks it up.
func focusedWin() []backend.Window {
	return []backend.Window{{ID: 1, PID: 501, App: "Safari", Title: "Ex", X: 0, Y: 0, W: 1280, H: 800, Focused: true}}
}

func hasCall(calls []string, want string) bool {
	for _, c := range calls {
		if c == want {
			return true
		}
	}
	return false
}

// TestResolveWindowFrameAfterWin: m[w]x,y after a prior win resolves against
// the win-focused window (st.window), the happy path for the "trickiest
// resolve() change" (test finding: window-frame branch was untested).
func TestResolveWindowFrameAfterWin(t *testing.T) {
	seq := parse(t, "win[]app:Safari", "m[w]100,50")
	be := &dryrun.Backend{WindowsResult: focusedWin()}
	sum := engine.Run(context.Background(), be, seq, engine.RunOptions{})

	if len(sum.Results) != 2 || sum.Results[1].Status != "ok" {
		t.Fatalf("Results = %+v, want win ok then move ok", sum.Results)
	}
	if !hasCall(be.Calls, "MouseMove 100,50") {
		t.Fatalf("calls = %v, want MouseMove 100,50 resolved against the focused window", be.Calls)
	}
}

// TestResolveWindowFrameCurrentWindowFallback: m[w]x,y with NO prior win
// falls back to the OS-focused window from Windows() (currentWindow's
// fallback path).
func TestResolveWindowFrameCurrentWindowFallback(t *testing.T) {
	seq := parse(t, "m[w]100,50")
	be := &dryrun.Backend{WindowsResult: focusedWin()}
	sum := engine.Run(context.Background(), be, seq, engine.RunOptions{})

	if sum.Results[0].Status != "ok" {
		t.Fatalf("status = %q, want ok (resolved via OS-focused fallback)", sum.Results[0].Status)
	}
	if !hasCall(be.Calls, "MouseMove 100,50") {
		t.Fatalf("calls = %v, want MouseMove 100,50", be.Calls)
	}
}

// TestResolveWindowFrameNoWindowIsNoWindow: m[w]x,y with neither a prior win
// nor any focused window -> E_NOWINDOW (errNoWindow -> boundsAwareCode).
func TestResolveWindowFrameNoWindowIsNoWindow(t *testing.T) {
	seq := parse(t, "m[w]100,50")
	be := &dryrun.Backend{WindowsResult: nil} // nothing focused
	sum := engine.Run(context.Background(), be, seq, engine.RunOptions{})

	r := sum.Results[0]
	if r.Status != "err" || r.ErrCode != output.ENoWindow {
		t.Fatalf("result = status=%q code=%q, want err/E_NOWINDOW", r.Status, r.ErrCode)
	}
}

// TestResolveWindowFrameOutOfBoundsIsBounds: m[w]x,y resolving inside a real
// window but outside its rectangle -> E_BOUNDS, distinct from the
// no-window E_NOWINDOW case.
func TestResolveWindowFrameOutOfBoundsIsBounds(t *testing.T) {
	seq := parse(t, "m[w]2000,50") // window is 1280 wide; x=2000 is off it
	be := &dryrun.Backend{WindowsResult: focusedWin()}
	sum := engine.Run(context.Background(), be, seq, engine.RunOptions{})

	r := sum.Results[0]
	if r.Status != "err" || r.ErrCode != output.EBounds {
		t.Fatalf("result = status=%q code=%q, want err/E_BOUNDS", r.Status, r.ErrCode)
	}
}

// TestScrollPageHeightFromFocusedWindow: scroll[by=page] after a win threads
// the focused window's height into Scroll's pageHeightPixels (dryrun records
// "Scroll <dir> <ticks> <by> page=<h>").
func TestScrollPageHeightFromFocusedWindow(t *testing.T) {
	seq := parse(t, "win[]app:Safari", "scroll[by=page]down 3")
	be := &dryrun.Backend{WindowsResult: focusedWin()}
	sum := engine.Run(context.Background(), be, seq, engine.RunOptions{})

	if sum.Results[1].Status != "ok" {
		t.Fatalf("scroll status = %q, want ok", sum.Results[1].Status)
	}
	if !hasCall(be.Calls, "Scroll down 3 page page=800") {
		t.Fatalf("calls = %v, want Scroll down 3 page page=800 (window height)", be.Calls)
	}
}

// TestScrollPageHeightFromPrimaryDisplay: with no focused window, by=page
// falls back to the primary display's height.
func TestScrollPageHeightFromPrimaryDisplay(t *testing.T) {
	seq := parse(t, "scroll[by=page]down 3")
	be := &dryrun.Backend{InfoResult: backend.Info{DisplayList: []backend.DisplayGeom{
		{X: 0, Y: 0, W: 2560, H: 1440, Primary: true},
	}}}
	sum := engine.Run(context.Background(), be, seq, engine.RunOptions{})

	if sum.Results[0].Status != "ok" {
		t.Fatalf("status = %q, want ok", sum.Results[0].Status)
	}
	if !hasCall(be.Calls, "Scroll down 3 page page=1440") {
		t.Fatalf("calls = %v, want Scroll down 3 page page=1440 (primary display height)", be.Calls)
	}
}

// TestScrollPageHeightConservativeFallback: no window and no primary display
// reported -> the 900px conservative fallback.
func TestScrollPageHeightConservativeFallback(t *testing.T) {
	seq := parse(t, "scroll[by=page]down 3")
	be := &dryrun.Backend{} // empty Info: no DisplayList
	sum := engine.Run(context.Background(), be, seq, engine.RunOptions{})

	if sum.Results[0].Status != "ok" {
		t.Fatalf("status = %q, want ok", sum.Results[0].Status)
	}
	if !hasCall(be.Calls, "Scroll down 3 page page=900") {
		t.Fatalf("calls = %v, want Scroll down 3 page page=900 (conservative fallback)", be.Calls)
	}
}
