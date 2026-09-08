//go:build windows

package windows

import (
	"context"
	"errors"
	"testing"

	"github.com/kang-sw/gotto-hando/internal/backend"
	"github.com/kang-sw/gotto-hando/internal/ir"
	"github.com/kang-sw/gotto-hando/internal/output"
)

// fakeSession/fakeKeys back the Preflight test seam (probes.go's
// sessionProbe/keyStateProbe interfaces) - same shape as darwin's
// preflight_test.go, minus the permission probe Windows does not have.
type fakeSession struct{ state string }

func (f fakeSession) State() string { return f.state }

type fakeKeys struct{ held bool }

func (f fakeKeys) AnyPhysicallyHeld(codes []uint8) bool { return f.held }

// fakeDisplays backs displayProbe with a fixed, synthetic display list -
// the seam preflight_test.go uses to inject a non-(0,0)-origin display
// without depending on this machine's real monitor geometry.
type fakeDisplays struct{ list []backend.DisplayGeom }

func (f fakeDisplays) Active() []backend.DisplayGeom { return f.list }

// newTestBackend builds a Backend with fake session/key probes and the
// REAL EnumDisplayMonitors-backed display probe (Preflight's coordinate-
// bounds check calls b.displays.Active(); querying real monitor geometry
// needs no unlocked session, so this is safe from a locked/remote test
// session). Use newTestBackendWithDisplays when the test needs known,
// non-(0,0)-origin geometry instead.
func newTestBackend(t *testing.T, session string, held bool) *Backend {
	t.Helper()
	return &Backend{
		session:  fakeSession{state: session},
		keys:     fakeKeys{held: held},
		displays: realDisplayProbe{},
	}
}

func newTestBackendWithDisplays(t *testing.T, displays []backend.DisplayGeom) *Backend {
	t.Helper()
	return &Backend{
		session:  fakeSession{state: "active"},
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
// even with a physically held key: the exemption list means check 3
// (input-injection gated) never runs either (help-windows.txt CHECK: "a
// run of only qinfo qdisp qmouse sleep set and comments never fails it or
// the session check").
func TestPreflightQueryOnlyPassesWhenLocked(t *testing.T) {
	b := newTestBackend(t, "locked", true)
	seq := seqOf(
		ir.Op{Kind: ir.KindQueryInfo},
		ir.Op{Kind: ir.KindQueryDisp},
		ir.Op{Kind: ir.KindQueryMouse},
		ir.Op{Kind: ir.KindSleep},
		ir.Op{Kind: ir.KindSet},
	)
	if err := b.Preflight(context.Background(), seq); err != nil {
		t.Fatalf("Preflight() = %v, want nil (query-only sequence must never fail on session or held-key)", err)
	}
}

// (b) session locked + a `k` line -> E_SESSION (check 1 fires before check
// 3, even though a key is also held here).
func TestPreflightLockedSessionWithKeyOp(t *testing.T) {
	b := newTestBackend(t, "locked", false)
	seq := seqOf(ir.Op{Kind: ir.KindKey, Keys: [][]string{{"a"}}})
	code := preflightCode(t, b.Preflight(context.Background(), seq))
	if code != output.ESession {
		t.Errorf("code = %s, want %s", code, output.ESession)
	}
}

// (c) a held key/button reported by the fake keyStateProbe + a `k` line ->
// E_INPUT (check 3, after session passes; Windows has no permission check
// to sit between them, unlike darwin's check 2).
func TestPreflightHeldKeyOrButtonBlocks(t *testing.T) {
	b := newTestBackend(t, "active", true)
	seq := seqOf(ir.Op{Kind: ir.KindKey, Keys: [][]string{{"a"}}})
	code := preflightCode(t, b.Preflight(context.Background(), seq))
	if code != output.EInput {
		t.Errorf("code = %s, want %s", code, output.EInput)
	}
}

// A query-only run must stay exempt from check 3 EVEN when a key is
// physically held (the qinfo/--ping recovery loop staying alive with a
// stuck key, ticket Decision) - a stronger variant of (a) proving the
// input-injection gate, not just the session exemption, is what protects
// it.
func TestPreflightHeldKeyDoesNotBlockQueryOnlyRun(t *testing.T) {
	b := newTestBackend(t, "active", true)
	seq := seqOf(ir.Op{Kind: ir.KindQueryInfo})
	if err := b.Preflight(context.Background(), seq); err != nil {
		t.Fatalf("Preflight() = %v, want nil (query-only run is exempt from check 3 even with a held key)", err)
	}
}

// bridge session state is equivalent to active for check 1.
func TestPreflightBridgeSessionPasses(t *testing.T) {
	b := newTestBackend(t, "bridge", false)
	seq := seqOf(ir.Op{Kind: ir.KindKey, Keys: [][]string{{"a"}}})
	if err := b.Preflight(context.Background(), seq); err != nil {
		t.Fatalf("Preflight() = %v, want nil (bridge session is accepted like active)", err)
	}
}

// inactive session + a `k` line -> E_SESSION (the documented over-ssh
// result with no bridge, help-windows.txt INTERACTIVE SESSION REQUIRED).
func TestPreflightInactiveSessionWithKeyOp(t *testing.T) {
	b := newTestBackend(t, "inactive", false)
	seq := seqOf(ir.Op{Kind: ir.KindKey, Keys: [][]string{{"a"}}})
	code := preflightCode(t, b.Preflight(context.Background(), seq))
	if code != output.ESession {
		t.Errorf("code = %s, want %s", code, output.ESession)
	}
}

// check 5: a key name this table has no entry for surfaces E_INPUT with an
// unsupported-key message, only reached once checks 1-4 pass. Every
// canonical KEY NAMES symbol resolves on Windows (keys_test.go), so this
// test proves check 5's loop is still live defensive parity, not a
// tautology, by using a symbol outside the canonical table entirely
// (this backend layer does not itself validate that a symbol is
// canonical - internal/syntax already guarantees that upstream).
func TestPreflightUnsupportedKeyNameSurfacesEInput(t *testing.T) {
	b := newTestBackend(t, "active", false)
	seq := seqOf(ir.Op{Kind: ir.KindKey, Keys: [][]string{{"not-a-real-key-name"}}})
	code := preflightCode(t, b.Preflight(context.Background(), seq))
	if code != output.EInput {
		t.Errorf("code = %s, want %s", code, output.EInput)
	}
}

// synthOffsetDisplays is a two-display fixture: display 0 at origin (0,0),
// display 1 to its right at (1440,0) - both 1440x900. Used to prove check 4
// bounds a disp=N coordinate against the NAMED display's own rectangle
// (0<=x<=W, 0<=y<=H), not its absolute X/Y origin - same fixture and same
// review-driven regression darwin's preflight_test.go locks in.
var synthOffsetDisplays = []backend.DisplayGeom{
	{X: 0, Y: 0, W: 1440, H: 900, Primary: true},
	{X: 1440, Y: 0, W: 1440, H: 900},
}

func TestPreflightDisplayFrameCoordIsRelativeToItsOwnOrigin(t *testing.T) {
	b := newTestBackendWithDisplays(t, synthOffsetDisplays)
	seq := seqOf(ir.Op{Kind: ir.KindMove, Point: ir.Point{Frame: "display", Disp: 1, X: 0, Y: 0}})
	if err := b.Preflight(context.Background(), seq); err != nil {
		t.Fatalf("Preflight() = %v, want nil (disp=1 0,0 is display 1's own top-left, in bounds)", err)
	}
}

func TestPreflightDisplayFrameCoordOutOfRangeAborts(t *testing.T) {
	b := newTestBackendWithDisplays(t, synthOffsetDisplays)
	seq := seqOf(ir.Op{Kind: ir.KindMove, Point: ir.Point{Frame: "display", Disp: 1, X: 2000, Y: 0}})
	code := preflightCode(t, b.Preflight(context.Background(), seq))
	if code != output.EBounds {
		t.Errorf("code = %s, want %s", code, output.EBounds)
	}
}
