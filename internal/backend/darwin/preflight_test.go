//go:build darwin

package darwin

import (
	"context"
	"errors"
	"testing"

	"github.com/kang-sw/gotto-hando/internal/backend"
	"github.com/kang-sw/gotto-hando/internal/ir"
	"github.com/kang-sw/gotto-hando/internal/output"
)

// fakeSession/fakePerm/fakeKeys back the Preflight test seam
// (probes.go's sessionProbe/permissionProbe/keyStateProbe interfaces).
type fakeSession struct{ state string }

func (f fakeSession) State() string { return f.state }

type fakePerm struct{ accessibility, screen bool }

func (f fakePerm) Accessibility() bool   { return f.accessibility }
func (f fakePerm) ScreenRecording() bool { return f.screen }

type fakeKeys struct{ keyHeld, buttonHeld bool }

func (f fakeKeys) AnyKeyHeld(codes []uint16) bool { return f.keyHeld }
func (f fakeKeys) AnyButtonHeld() bool            { return f.buttonHeld }

// fakeDisplays backs displayProbe with a fixed, synthetic display list -
// the seam preflight_test.go uses to inject a non-(0,0)-origin display
// without depending on this machine's real screen geometry.
type fakeDisplays struct{ list []backend.DisplayGeom }

func (f fakeDisplays) Active() []backend.DisplayGeom { return f.list }

// newTestBackend builds a Backend with fake probes wired in and real FFI
// initialized (Preflight's coordinate-bounds check calls b.displays.Active(),
// defaulted here to the real CGGetActiveDisplayList-backed probe - dlopen
// succeeds regardless of GUI session lock state, so this is safe to call
// from a locked/remote test session). Use newTestBackendWithDisplays instead
// when the test needs to control display geometry.
func newTestBackend(t *testing.T, session string, accessibility bool, keyHeld, buttonHeld bool) *Backend {
	t.Helper()
	if err := initFFI(); err != nil {
		t.Fatalf("initFFI: %v", err)
	}
	return &Backend{
		session:  fakeSession{state: session},
		perm:     fakePerm{accessibility: accessibility},
		keys:     fakeKeys{keyHeld: keyHeld, buttonHeld: buttonHeld},
		displays: realDisplayProbe{},
	}
}

// newTestBackendWithDisplays is newTestBackend but with a synthetic
// displayProbe instead of the real one, for check-4 (disp=N bounds) tests
// that need known, non-(0,0)-origin display geometry.
func newTestBackendWithDisplays(t *testing.T, displays []backend.DisplayGeom) *Backend {
	t.Helper()
	if err := initFFI(); err != nil {
		t.Fatalf("initFFI: %v", err)
	}
	return &Backend{
		session:  fakeSession{state: "active"},
		perm:     fakePerm{accessibility: true},
		keys:     fakeKeys{},
		displays: fakeDisplays{list: displays},
	}
}

func seqOf(ops ...ir.Op) *ir.Sequence {
	return &ir.Sequence{V: 1, Ops: ops}
}

func preflightCode(t *testing.T, err error) output.ErrorCode {
	t.Helper()
	if err == nil {
		t.Fatal("Preflight returned nil, want a *backend.PreflightError")
	}
	var pfErr *backend.PreflightError
	if !errors.As(err, &pfErr) {
		t.Fatalf("Preflight error %v is not a *backend.PreflightError", err)
	}
	return pfErr.Code
}

// (a) session locked + only qinfo/qdisp/qmouse/sleep/set lines -> passes,
// even though Accessibility is missing and a key is physically held: the
// exemption list means checks 2/3 never run either.
func TestPreflightQueryOnlyPassesWhenLocked(t *testing.T) {
	b := newTestBackend(t, "locked", false, true, true)
	seq := seqOf(
		ir.Op{Kind: ir.KindQueryInfo},
		ir.Op{Kind: ir.KindQueryDisp},
		ir.Op{Kind: ir.KindQueryMouse},
		ir.Op{Kind: ir.KindSleep},
		ir.Op{Kind: ir.KindSet},
	)
	if err := b.Preflight(context.Background(), seq); err != nil {
		t.Fatalf("Preflight() = %v, want nil (query-only sequence must never fail on session/permission/held-key)", err)
	}
}

// (b) session locked + a `k` line -> E_SESSION (check 1 fires before
// check 2, even though Accessibility is also missing here).
func TestPreflightLockedSessionWithKeyOp(t *testing.T) {
	b := newTestBackend(t, "locked", false, false, false)
	seq := seqOf(ir.Op{Kind: ir.KindKey, Keys: [][]string{{"a"}}})
	code := preflightCode(t, b.Preflight(context.Background(), seq))
	if code != output.ESession {
		t.Errorf("code = %s, want %s", code, output.ESession)
	}
}

// (c) session ok, Accessibility missing + a `k` line -> E_PERMISSION.
func TestPreflightMissingAccessibility(t *testing.T) {
	b := newTestBackend(t, "active", false, false, false)
	seq := seqOf(ir.Op{Kind: ir.KindKey, Keys: [][]string{{"a"}}})
	code := preflightCode(t, b.Preflight(context.Background(), seq))
	if code != output.EPermission {
		t.Errorf("code = %s, want %s", code, output.EPermission)
	}
}

// (d) a held key reported by the fake keyStateProbe + a `k` line ->
// E_INPUT (check 3, after session/permission both pass).
func TestPreflightHeldKeyBlocks(t *testing.T) {
	b := newTestBackend(t, "active", true, true, false)
	seq := seqOf(ir.Op{Kind: ir.KindKey, Keys: [][]string{{"a"}}})
	code := preflightCode(t, b.Preflight(context.Background(), seq))
	if code != output.EInput {
		t.Errorf("code = %s, want %s", code, output.EInput)
	}
}

// A held mouse button (not a key) must trip the same check 3.
func TestPreflightHeldButtonBlocks(t *testing.T) {
	b := newTestBackend(t, "active", true, false, true)
	seq := seqOf(ir.Op{Kind: ir.KindKey, Keys: [][]string{{"a"}}})
	code := preflightCode(t, b.Preflight(context.Background(), seq))
	if code != output.EInput {
		t.Errorf("code = %s, want %s", code, output.EInput)
	}
}

// bridge session state is equivalent to active for check 1.
func TestPreflightBridgeSessionPasses(t *testing.T) {
	b := newTestBackend(t, "bridge", true, false, false)
	seq := seqOf(ir.Op{Kind: ir.KindKey, Keys: [][]string{{"a"}}})
	if err := b.Preflight(context.Background(), seq); err != nil {
		t.Fatalf("Preflight() = %v, want nil (bridge session is accepted like active)", err)
	}
}

// check 5: a key name unsupported on darwin (volup) surfaces E_INPUT with
// an unsupported-key message, only reached once checks 1-4 pass.
func TestPreflightUnsupportedKeyName(t *testing.T) {
	b := newTestBackend(t, "active", true, false, false)
	seq := seqOf(ir.Op{Kind: ir.KindKey, Keys: [][]string{{"volup"}}})
	code := preflightCode(t, b.Preflight(context.Background(), seq))
	if code != output.EInput {
		t.Errorf("code = %s, want %s", code, output.EInput)
	}
}

// synthOffsetDisplays is a two-display fixture: display 0 at origin (0,0),
// display 1 to its right at (1440,0) - both 1440x900. Used to prove check 4
// bounds a disp=N coordinate against the NAMED display's own rectangle
// (0<=x<=W, 0<=y<=H), not its absolute X/Y origin (Critical review finding:
// disp=N ignored the display's origin, so a valid m[disp=1]0,0 wrongly
// aborted E_BOUNDS for any display whose origin wasn't (0,0)).
var synthOffsetDisplays = []backend.DisplayGeom{
	{X: 0, Y: 0, W: 1440, H: 900, Primary: true},
	{X: 1440, Y: 0, W: 1440, H: 900},
}

// check 4: a disp=1 coordinate at that display's own top-left (0,0) must
// NOT abort E_BOUNDS even though display 1's absolute origin is (1440,0) -
// disp=N is display-relative (help.txt COORDINATES :250-253).
func TestPreflightDisplayFrameCoordIsRelativeToItsOwnOrigin(t *testing.T) {
	b := newTestBackendWithDisplays(t, synthOffsetDisplays)
	seq := seqOf(ir.Op{Kind: ir.KindMove, Point: ir.Point{Frame: "display", Disp: 1, X: 0, Y: 0}})
	if err := b.Preflight(context.Background(), seq); err != nil {
		t.Fatalf("Preflight() = %v, want nil (disp=1 0,0 is display 1's own top-left, in bounds)", err)
	}
}

// check 4: a coordinate outside the named display's own rectangle still
// aborts E_BOUNDS (e.g. disp=1's width is 1440, so x=2000 is out of range).
func TestPreflightDisplayFrameCoordOutOfRangeAborts(t *testing.T) {
	b := newTestBackendWithDisplays(t, synthOffsetDisplays)
	seq := seqOf(ir.Op{Kind: ir.KindMove, Point: ir.Point{Frame: "display", Disp: 1, X: 2000, Y: 0}})
	code := preflightCode(t, b.Preflight(context.Background(), seq))
	if code != output.EBounds {
		t.Errorf("code = %s, want %s", code, output.EBounds)
	}
}

// newTestBackendWithScreen builds a Backend with an active session,
// Accessibility granted, no held keys, a controllable Screen Recording
// grant and a synthetic display list - the seam the cap-only Screen
// Recording gate (check 3b) needs, with zero FFI/GUI dependency.
func newTestBackendWithScreen(t *testing.T, screen bool) *Backend {
	t.Helper()
	if err := initFFI(); err != nil {
		t.Fatalf("initFFI: %v", err)
	}
	return &Backend{
		session:  fakeSession{state: "active"},
		perm:     fakePerm{accessibility: true, screen: screen},
		keys:     fakeKeys{},
		displays: fakeDisplays{list: synthOffsetDisplays},
	}
}

// check 3b: a cap-only sequence with Screen Recording missing -> E_PERMISSION
// (the cap-only 6th gate; help-macos.txt CHECK :21-24). Accessibility is
// granted here, so this proves the screen gate fires independently of the
// Accessibility gate.
func TestPreflightCapMissingScreenRecording(t *testing.T) {
	b := newTestBackendWithScreen(t, false)
	seq := seqOf(ir.Op{Kind: ir.KindCapture, Frame: "desktop"})
	code := preflightCode(t, b.Preflight(context.Background(), seq))
	if code != output.EPermission {
		t.Errorf("code = %s, want %s", code, output.EPermission)
	}
}

// check 3b is cap-ONLY: a qwin/win sequence with Screen Recording missing
// must still PASS Preflight (help-macos.txt CHECK :21-24: without Screen
// Recording qwin/win run, only titles come back empty; cap alone needs it).
func TestPreflightWindowOpsPassWithoutScreenRecording(t *testing.T) {
	b := newTestBackendWithScreen(t, false)
	seq := seqOf(
		ir.Op{Kind: ir.KindQueryWindows},
		ir.Op{Kind: ir.KindFocus, Selector: ir.Selector{Kind: "app", Value: "Safari"}},
	)
	if err := b.Preflight(context.Background(), seq); err != nil {
		t.Fatalf("Preflight() = %v, want nil (screen gate is cap-only; qwin/win run without Screen Recording)", err)
	}
}

// open[wait=] is Accessibility-gated like win/k/etc (help-macos.txt CHECK:
// "perms=accessibility:* gates ... window control (win, open[wait=])"), now
// that op.HasWait is actually wired (Phase 3 step 1 fix) - Preflight's
// accessibilityGated check reads `op.Kind == ir.KindOpen && op.HasWait`.
func TestPreflightOpenWaitRequiresAccessibility(t *testing.T) {
	b := newTestBackend(t, "active", false, false, false)
	seq := seqOf(ir.Op{Kind: ir.KindOpen, Target: "TextEdit", HasWait: true, WaitMS: 5000})
	code := preflightCode(t, b.Preflight(context.Background(), seq))
	if code != output.EPermission {
		t.Errorf("code = %s, want %s", code, output.EPermission)
	}
}

// open without wait= needs no Accessibility (ticket Decision: "clip, qclip,
// exec, open (without wait=) need no permission").
func TestPreflightOpenWithoutWaitNeedsNoAccessibility(t *testing.T) {
	b := newTestBackend(t, "active", false, false, false)
	seq := seqOf(ir.Op{Kind: ir.KindOpen, Target: "TextEdit"})
	if err := b.Preflight(context.Background(), seq); err != nil {
		t.Fatalf("Preflight() = %v, want nil (open without wait= needs no Accessibility)", err)
	}
}
