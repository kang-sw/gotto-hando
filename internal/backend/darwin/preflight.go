//go:build darwin

package darwin

import (
	"context"
	"fmt"

	"github.com/kang-sw/gotto-hando/internal/backend"
	"github.com/kang-sw/gotto-hando/internal/ir"
	"github.com/kang-sw/gotto-hando/internal/output"
)

// exemptFromSession is Preflight check 1's exemption list: a run made only
// of these kinds (and comments, which never become an Op) never needs an
// unlocked GUI session (help-macos.txt CHECK: "a run of only qinfo qdisp
// qmouse sleep set and comments never fails on session or permissions").
var exemptFromSession = map[ir.Kind]bool{
	ir.KindQueryInfo: true, ir.KindQueryDisp: true, ir.KindQueryMouse: true,
	ir.KindSleep: true, ir.KindSet: true,
}

// accessibilityGated is Preflight check 2's gated command set
// (help-macos.txt WHEN YOU NEED THIS / CHECK: "perms=accessibility:* gates
// input (k kd ku txt m c md mu drag scroll paste) and window control (win,
// open[wait=])"). open only when it carries wait= (checked separately,
// HasWait).
var accessibilityGated = map[ir.Kind]bool{
	ir.KindKey: true, ir.KindKeyDown: true, ir.KindKeyUp: true,
	ir.KindText: true, ir.KindMove: true, ir.KindClick: true,
	ir.KindButtonDown: true, ir.KindButtonUp: true, ir.KindDrag: true,
	ir.KindScroll: true, ir.KindPaste: true, ir.KindFocus: true,
}

// Preflight implements the five preflight checks in the literal order the
// ticket specifies, short-circuiting on the first failure (help.txt
// Constraints list; help-macos.txt CHECK). It never prompts.
func (b *Backend) Preflight(ctx context.Context, seq *ir.Sequence) error {
	needsSession := false
	needsAccessibility := false
	for i := range seq.Ops {
		op := &seq.Ops[i]
		if !exemptFromSession[op.Kind] {
			needsSession = true
		}
		if accessibilityGated[op.Kind] || (op.Kind == ir.KindOpen && op.HasWait) {
			needsAccessibility = true
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

	if needsAccessibility {
		// 2. Accessibility permission.
		if !b.perm.Accessibility() {
			return &backend.PreflightError{Code: output.EPermission,
				Msg: "Accessibility permission not granted (see --help-macos)"}
		}
		// 3. No key/button physically held.
		if b.keys.AnyKeyHeld(allKeycodes()) || b.keys.AnyButtonHeld() {
			return &backend.PreflightError{Code: output.EInput,
				Msg: "a key or mouse button is already physically held"}
		}
	}

	// 4. Absolute and disp= coordinates inside the real desktop
	// (help.txt:262-264; the r/w/% half is a run-time check,
	// internal/engine/compose.go resolve()).
	displays := activeDisplays()
	deskX, deskY, deskW, deskH := unionBounds(displays)
	for _, p := range collectPreflightPoints(seq) {
		if p.XPct || p.YPct || p.Frame == "pointer" || p.Frame == "window" {
			continue // runtime-checked, not preflight (help.txt:266-268)
		}
		var ok bool
		switch p.Frame {
		case "display":
			ok = p.Disp >= 0 && p.Disp < len(displays) && pointInRect(p.X, p.Y,
				displays[p.Disp].X, displays[p.Disp].Y, displays[p.Disp].W, displays[p.Disp].H)
		default: // desktop
			ok = pointInRect(p.X, p.Y, deskX, deskY, deskW, deskH)
		}
		if !ok {
			return &backend.PreflightError{Code: output.EBounds,
				Msg: fmt.Sprintf("coordinate %g,%g outside the desktop", p.X, p.Y)}
		}
	}

	// 5. Every key name supported on darwin.
	if needsAccessibility {
		for _, sym := range collectKeySymbols(seq) {
			if _, ok := keycodeFor(sym); !ok {
				return &backend.PreflightError{Code: output.EInput, Msg: unsupportedKeyMsg(sym)}
			}
		}
	}

	return nil
}

func pointInRect(x, y float64, rx, ry, rw, rh int) bool {
	return x >= float64(rx) && x <= float64(rx+rw) && y >= float64(ry) && y <= float64(ry+rh)
}

// collectPreflightPoints gathers every point this sequence carries for
// commands that have coordinates in Phase 1 (move/click/button_down/drag;
// w-frame cap and win are Phase 2).
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
// (op.Keys).
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
