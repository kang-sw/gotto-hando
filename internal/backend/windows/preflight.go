//go:build windows

package windows

import (
	"context"
	"fmt"

	"github.com/kang-sw/gotto-hando/internal/backend"
	"github.com/kang-sw/gotto-hando/internal/ir"
	"github.com/kang-sw/gotto-hando/internal/output"
)

// exemptFromSession is Preflight check 1's exemption list: a run made only
// of these kinds (and comments, which never become an Op) never needs an
// unlocked GUI session (help-windows.txt CHECK: "a run of only qinfo qdisp
// qmouse sleep set and comments never fails it or the session check").
// Identical kind set to darwin's exemptFromSession.
var exemptFromSession = map[ir.Kind]bool{
	ir.KindQueryInfo: true, ir.KindQueryDisp: true, ir.KindQueryMouse: true,
	ir.KindSleep: true, ir.KindSet: true,
}

// injectsInputGated is Preflight check 3's gated command set - the
// windows equivalent of darwin's accessibilityGated, renamed because no
// permission is involved here: Windows needs no grant for SendInput, but
// the held-key guard is still gated on "does this run inject input" so
// that a query-only run (qinfo/--ping recovery) stays usable even with a
// key stuck down (ticket Decision: "gate it on the same
// run-injects-input condition"). KindFocus and KindOpen[wait=] stay in the
// set for forward-compat even though their Backend methods are Phase-2
// stubs, matching darwin's set exactly.
var injectsInputGated = map[ir.Kind]bool{
	ir.KindKey: true, ir.KindKeyDown: true, ir.KindKeyUp: true,
	ir.KindText: true, ir.KindMove: true, ir.KindClick: true,
	ir.KindButtonDown: true, ir.KindButtonUp: true, ir.KindDrag: true,
	ir.KindScroll: true, ir.KindPaste: true, ir.KindFocus: true,
}

// Preflight implements the five preflight checks in the literal order the
// ticket specifies, short-circuiting on the first failure (help-windows.txt
// CHECK). It never prompts (perms=n/a - Windows has no permission dialogs
// for input/capture).
func (b *Backend) Preflight(ctx context.Context, seq *ir.Sequence) error {
	needsSession := RequiresSession(seq)
	needsInputInjection := false
	for i := range seq.Ops {
		op := &seq.Ops[i]
		if injectsInputGated[op.Kind] || (op.Kind == ir.KindOpen && op.HasWait) {
			needsInputInjection = true
		}
	}

	// 1. GUI session unlocked (own or bridge).
	if needsSession {
		session := b.session.State()
		if session != "active" && session != "bridge" {
			return &backend.PreflightError{Code: output.ESession,
				Msg: fmt.Sprintf("no unlocked GUI session (session=%s)", session)}
		}
	}

	// 2. Permissions: perms=n/a, never fails on Windows.

	// 3. No key/mouse button physically held - input-injection gated, same
	// policy as darwin (matches the qinfo/--ping recovery loop staying
	// alive with a stuck key).
	if needsInputInjection {
		if b.keys.AnyPhysicallyHeld(allVKCodes()) {
			return &backend.PreflightError{Code: output.EInput,
				Msg: "a key or mouse button is already physically held"}
		}
	}

	// 4. Absolute and disp= coordinates inside the real desktop
	// (help.txt:262-264; the r/w/% half is a run-time check,
	// internal/engine/compose.go resolve()).
	displays := b.displays.Active()
	deskX, deskY, deskW, deskH := unionBounds(displays)
	for _, p := range collectPreflightPoints(seq) {
		if p.XPct || p.YPct || p.Frame == "pointer" || p.Frame == "window" {
			continue // runtime-checked, not preflight (help.txt:266-268)
		}
		var ok bool
		switch p.Frame {
		case "display":
			// disp=N coordinates are DISPLAY-RELATIVE (help.txt:250-253),
			// same reasoning as darwin's preflight.go: bounds-check against
			// the named display's own size, not its absolute origin.
			ok = p.Disp >= 0 && p.Disp < len(displays) && pointInRect(p.X, p.Y,
				0, 0, displays[p.Disp].W, displays[p.Disp].H)
		default: // desktop
			ok = pointInRect(p.X, p.Y, deskX, deskY, deskW, deskH)
		}
		if !ok {
			return &backend.PreflightError{Code: output.EBounds,
				Msg: fmt.Sprintf("coordinate %g,%g outside the desktop", p.X, p.Y)}
		}
	}

	// 5. Every key name supported - always passes given full scan-code
	// table coverage (keys.go), but the loop stays for defensive parity: a
	// future KEY NAMES addition without a matching table entry should
	// still surface E_INPUT, not panic or silently pass.
	if needsInputInjection {
		for _, sym := range collectKeySymbols(seq) {
			if _, _, ok := keycodeFor(sym); !ok {
				return &backend.PreflightError{Code: output.EInput, Msg: unsupportedKeyMsg(sym)}
			}
		}
	}

	return nil
}

// RequiresSession reports whether seq contains any op outside
// exemptFromSession. It is the single source of truth for Preflight check 1;
// bridge routing deliberately does not use it because remote diagnostics must
// observe the bridge's GUI session too.
func RequiresSession(seq *ir.Sequence) bool {
	for i := range seq.Ops {
		if !exemptFromSession[seq.Ops[i].Kind] {
			return true
		}
	}
	return false
}

func pointInRect(x, y float64, rx, ry, rw, rh int) bool {
	return x >= float64(rx) && x <= float64(rx+rw) && y >= float64(ry) && y <= float64(ry+rh)
}

// collectPreflightPoints gathers every point this sequence carries for
// commands that have coordinates in Phase 1 (move/click/button_down/drag;
// w-frame cap and win are Phase 2). Identical logic to darwin's.
func collectPreflightPoints(seq *ir.Sequence) []ir.Point {
	var pts []ir.Point
	for i := range seq.Ops {
		op := &seq.Ops[i]
		switch op.Kind {
		case ir.KindMove:
			pts = append(pts, op.Point)
		case ir.KindClick, ir.KindButtonDown:
			if op.HasPoint {
				pts = append(pts, op.Point)
			}
		case ir.KindDrag:
			pts = append(pts, op.Points...)
		}
	}
	return pts
}

// collectKeySymbols gathers every canonical key symbol this sequence uses,
// across modifier flags (op.Mods, help.txt:228) and key/chord payloads
// (op.Keys). Identical logic to darwin's.
func collectKeySymbols(seq *ir.Sequence) []string {
	var syms []string
	for i := range seq.Ops {
		op := &seq.Ops[i]
		syms = append(syms, op.Mods...)
		for _, chord := range op.Keys {
			syms = append(syms, chord...)
		}
	}
	return syms
}
