//go:build windows

package windows

import (
	"context"
	"fmt"
	"time"
	"unsafe"

	"github.com/kang-sw/gotto-hando/internal/backend"
)

// moveInterval is the interpolation step for a MouseMove with dur > 0
// (help.txt:322-323: "ms>0 interpolates the motion over that time"), the
// same ~60fps step darwin's MouseMove uses.
const moveInterval = 16 * time.Millisecond

// wheelDelta is WHEEL_DELTA (WinUser.h): the fixed notch size every
// SendInput wheel event is a signed multiple of. Windows has no pixel- or
// page-unit wheel injection API (unlike darwin's kCGScrollEventUnitPixel);
// see Scroll's doc comment for the by=page reconciliation.
const wheelDelta = 120

// MouseMove moves the pointer, interpolating over dur when > 0
// (help.txt:322-323), via absolute+virtual-desktop SendInput events. There
// is no click-state/drag-vs-move bookkeeping to maintain here (unlike
// darwin's *Dragged-vs-Moved event-type choice): SendInput's
// MOUSEEVENTF_MOVE always posts a plain move, and Windows itself infers
// drag semantics purely from the receiving app observing a button already
// down across subsequent WM_MOUSEMOVE messages - there is no separate
// wire-level "dragged" event type to choose (ticket Codebase Findings).
func (b *Backend) MouseMove(ctx context.Context, p backend.Point, dur time.Duration) error {
	from, _ := b.MousePos(ctx)
	if dur <= 0 {
		return postMoveEvent(p)
	}
	steps := int(dur / moveInterval)
	if steps < 1 {
		steps = 1
	}
	for i := 1; i <= steps; i++ {
		t := float64(i) / float64(steps)
		cur := backend.Point{X: from.X + (p.X-from.X)*t, Y: from.Y + (p.Y-from.Y)*t}
		if err := postMoveEvent(cur); err != nil {
			return err
		}
		if i < steps {
			sleepCtx(ctx, moveInterval)
		}
	}
	return nil
}

func postMoveEvent(p backend.Point) error {
	nx, ny := normalizeAbsolute(p.X, p.Y)
	return sendMouseInput(nx, ny, 0, mouseeventfMove|mouseeventfAbsolute|mouseeventfVirtualdesk)
}

// normalizeAbsolute converts a physical-pixel virtual-desktop coordinate to
// SendInput's required 0..65535 normalized range, per MOUSEEVENTF_ABSOLUTE
// + MOUSEEVENTF_VIRTUALDESK's documented contract (normalized against the
// full virtual screen's bounding box, not just the primary monitor). Thin
// wrapper around the pure normalizeAbsoluteAgainst so the real
// GetSystemMetrics-backed virtualScreenRect() stays the only impure part.
func normalizeAbsolute(px, py float64) (int32, int32) {
	vx, vy, vw, vh := virtualScreenRect()
	return normalizeAbsoluteAgainst(px, py, vx, vy, vw, vh)
}

// normalizeAbsoluteAgainst is normalizeAbsolute's pure math, split out
// (analogous to virtualScreenRect() already being its own seam) so
// mouse_test.go can call the REAL normalization formula directly against a
// synthetic virtual-screen rect instead of reimplementing the arithmetic in
// a test-local duplicate.
func normalizeAbsoluteAgainst(px, py float64, vx, vy, vw, vh int32) (int32, int32) {
	if vw <= 0 {
		vw = 1
	}
	if vh <= 0 {
		vh = 1
	}
	nx := int32((px - float64(vx)) * 65536 / float64(vw))
	ny := int32((py - float64(vy)) * 65536 / float64(vh))
	return nx, ny
}

func virtualScreenRect() (x, y, w, h int32) {
	xr, _, _ := procGetSystemMetrics.Call(smXvirtualscreen)
	yr, _, _ := procGetSystemMetrics.Call(smYvirtualscreen)
	wr, _, _ := procGetSystemMetrics.Call(smCxvirtualscreen)
	hr, _, _ := procGetSystemMetrics.Call(smCyvirtualscreen)
	return int32(xr), int32(yr), int32(wr), int32(hr)
}

// ButtonDown presses a mouse button at the current pointer position
// (help.txt:329-332: md moves first only when x,y is given - that move
// happens via a separate MouseMove call before this one, engine/run.go).
// No click-burst/click-state bookkeeping (unlike darwin's
// CGMouseEventClickState field): Windows detects double-clicks itself from
// SendInput-posted event timestamps against GetDoubleClickTime(), so
// ButtonDown/ButtonUp need not compute or carry a click count (ticket
// Codebase Findings).
func (b *Backend) ButtonDown(ctx context.Context, bt backend.Button) error {
	return sendMouseInput(0, 0, 0, buttonFlag(bt, true))
}

// ButtonUp releases a button pressed by ButtonDown.
func (b *Backend) ButtonUp(ctx context.Context, bt backend.Button) error {
	return sendMouseInput(0, 0, 0, buttonFlag(bt, false))
}

func buttonFlag(bt backend.Button, down bool) uint32 {
	switch bt {
	case backend.ButtonRight:
		if down {
			return mouseeventfRightdown
		}
		return mouseeventfRightup
	case backend.ButtonMiddle:
		if down {
			return mouseeventfMiddledown
		}
		return mouseeventfMiddleup
	default:
		if down {
			return mouseeventfLeftdown
		}
		return mouseeventfLeftup
	}
}

// Scroll scrolls at the current pointer position (help.txt:339-341).
//
// Unit reconciliation (Codebase Findings risk signal, ticket Decisions does
// not address by=page for Windows): SendInput's MOUSEEVENTF_WHEEL/HWHEEL
// has no pixel- or page-unit variant - a Windows wheel event is always a
// signed multiple of WHEEL_DELTA (120), interpreted by the receiving
// app/OS using the user's own "lines to scroll" setting; there is no
// wire-level way to say "scroll one page" distinct from "scroll N lines."
// Recommended-and-implemented default: by=page sends the SAME
// ticks*WHEEL_DELTA notches as by=line (Windows cannot natively
// distinguish them at the injection layer) - documented as a CAVEATS
// bullet in assets/help-windows.txt.
func (b *Backend) Scroll(ctx context.Context, dir backend.Dir, ticks int, by backend.ScrollUnit) error {
	amount := scrollAmount(ticks, by)
	switch dir {
	case "up":
		return sendMouseInput(0, 0, uint32(amount), mouseeventfWheel)
	case "down":
		return sendMouseInput(0, 0, uint32(-amount), mouseeventfWheel)
	case "left":
		return sendMouseInput(0, 0, uint32(-amount), mouseeventfHwheel)
	case "right":
		return sendMouseInput(0, 0, uint32(amount), mouseeventfHwheel)
	}
	return nil
}

// scrollAmount is Scroll's pure notch-count math, split out so
// mouse_test.go can pin the by=page reconciliation decision (see Scroll's
// doc comment above) against the REAL computation instead of a duplicate.
// Windows has no pixel/page-unit wheel injection API, so by is accepted but
// does not change the result: both by=line and by=page send
// ticks*WHEEL_DELTA notches.
func scrollAmount(ticks int, by backend.ScrollUnit) int32 {
	return int32(ticks) * wheelDelta
}

func sendMouseInput(dx, dy int32, mouseData uint32, dwFlags uint32) error {
	rec := mouseInputRecord{typ: inputMouse, mi: mouseInput{dx: dx, dy: dy, mouseData: mouseData, dwFlags: dwFlags}}
	r, _, err := procSendInput.Call(1, uintptr(unsafe.Pointer(&rec)), unsafe.Sizeof(rec))
	if r == 0 {
		return fmt.Errorf("SendInput (mouse) failed: %w", err)
	}
	return nil
}
