//go:build darwin

package darwin

import (
	"context"
	"runtime"
	"time"

	"github.com/kang-sw/gotto-hando/internal/backend"
)

// CGEventType mouse event constants (CGEventTypes.h).
const (
	cgEventLeftMouseDown     = 1
	cgEventLeftMouseUp       = 2
	cgEventRightMouseDown    = 3
	cgEventRightMouseUp      = 4
	cgEventMouseMoved        = 5
	cgEventLeftMouseDragged  = 6
	cgEventRightMouseDragged = 7
	cgEventOtherMouseDown    = 25
	cgEventOtherMouseUp      = 26
	cgEventOtherMouseDragged = 27
)

// CGScrollEventUnit (CGEventTypes.h): kCGScrollEventUnitLine (each wheel
// unit is one "tick", matching help.txt scroll's ticks= payload :339-341
// more directly than pixel units would - used for by=line, the default)
// and kCGScrollEventUnitPixel (used only for by=page, per the ticket
// Decision - see Scroll's doc comment for the CONCEPT.md reconciliation).
const (
	cgScrollEventUnitPixel = 0
	cgScrollEventUnitLine  = 1
)

// CGEventField kCGMouseEventClickState: the click-burst counter
// (help-macos.txt CAVEATS: "Double clicks are sent with the click-state
// field set").
const cgMouseEventClickState = 1

// moveInterval is the interpolation step for a MouseMove with dur > 0
// (help.txt:322-323: "ms>0 interpolates the motion over that time"),
// chosen for smooth (~60fps) synthetic motion.
const moveInterval = 16 * time.Millisecond

// doubleClickWindow is the max gap between a button's up and the next down
// for the pair to carry an incremented CGEventFlags click-state
// (help-macos.txt CAVEATS: "Double clicks are sent with the click-state
// field set"). macOS's own system double-click interval is user
// configurable (typically ~350-500ms); this fixed value approximates it -
// exact fidelity is unverifiable without the deferred interactive subset.
const doubleClickWindow = 400 * time.Millisecond

func mouseButtonNum(b backend.Button) uint32 {
	switch b {
	case backend.ButtonRight:
		return cgMouseButtonRight
	case backend.ButtonMiddle:
		return cgMouseButtonCenter
	default:
		return cgMouseButtonLeft
	}
}

// MouseMove moves the pointer, interpolating over dur when > 0
// (help.txt:322-323). While a button is held (drag.go's polyline), it
// posts *Dragged events instead of MouseMoved so apps see a continuous
// drag rather than a move-teleport-move sequence.
func (b *Backend) MouseMove(ctx context.Context, p backend.Point, dur time.Duration) error {
	from, _ := b.MousePos(ctx)
	if dur <= 0 {
		return b.postMoveEvent(p)
	}
	steps := int(dur / moveInterval)
	if steps < 1 {
		steps = 1
	}
	for i := 1; i <= steps; i++ {
		t := float64(i) / float64(steps)
		cur := backend.Point{X: from.X + (p.X-from.X)*t, Y: from.Y + (p.Y-from.Y)*t}
		if err := b.postMoveEvent(cur); err != nil {
			return err
		}
		if i < steps {
			sleepCtx(ctx, moveInterval)
		}
	}
	return nil
}

func (b *Backend) postMoveEvent(p backend.Point) error {
	typ := int32(cgEventMouseMoved)
	btn := uint32(cgMouseButtonLeft)
	if b.heldButton != "" {
		btn = mouseButtonNum(b.heldButton)
		switch b.heldButton {
		case backend.ButtonRight:
			typ = cgEventRightMouseDragged
		case backend.ButtonMiddle:
			typ = cgEventOtherMouseDragged
		default:
			typ = cgEventLeftMouseDragged
		}
	}
	ev := cgEventCreateMouseEvent(b.evtSource, typ, cgPoint{X: p.X, Y: p.Y}, btn)
	if ev == 0 {
		return nil
	}
	cgEventSetFlags(ev, b.heldFlags)
	cgEventPost(cgHIDEventTap, ev)
	cfRelease(ev)
	return nil
}

// ButtonDown presses a mouse button at the current pointer position
// (help.txt:329-332: md moves first only when x,y is given - that move
// happens via a separate MouseMove call before this one, engine/run.go).
func (b *Backend) ButtonDown(ctx context.Context, bt backend.Button) error {
	pos, _ := b.MousePos(ctx)
	count := b.nextClickState(bt)
	var typ int32
	switch bt {
	case backend.ButtonRight:
		typ = cgEventRightMouseDown
	case backend.ButtonMiddle:
		typ = cgEventOtherMouseDown
	default:
		typ = cgEventLeftMouseDown
	}
	ev := cgEventCreateMouseEvent(b.evtSource, typ, cgPoint{X: pos.X, Y: pos.Y}, mouseButtonNum(bt))
	if ev == 0 {
		return nil
	}
	cgEventSetIntegerValueField(ev, cgMouseEventClickState, int64(count))
	cgEventSetFlags(ev, b.heldFlags)
	cgEventPost(cgHIDEventTap, ev)
	cfRelease(ev)
	b.heldButton = bt
	return nil
}

// ButtonUp releases a button pressed by ButtonDown.
func (b *Backend) ButtonUp(ctx context.Context, bt backend.Button) error {
	pos, _ := b.MousePos(ctx)
	count := b.clickState[string(bt)]
	if count == 0 {
		count = 1
	}
	var typ int32
	switch bt {
	case backend.ButtonRight:
		typ = cgEventRightMouseUp
	case backend.ButtonMiddle:
		typ = cgEventOtherMouseUp
	default:
		typ = cgEventLeftMouseUp
	}
	ev := cgEventCreateMouseEvent(b.evtSource, typ, cgPoint{X: pos.X, Y: pos.Y}, mouseButtonNum(bt))
	if ev == 0 {
		return nil
	}
	cgEventSetIntegerValueField(ev, cgMouseEventClickState, int64(count))
	cgEventSetFlags(ev, b.heldFlags)
	cgEventPost(cgHIDEventTap, ev)
	cfRelease(ev)
	b.heldButton = ""
	b.lastButtonUp[string(bt)] = time.Now()
	return nil
}

// nextClickState computes the CGMouseEventClickState for a new ButtonDown:
// 1 for an isolated click, incrementing while consecutive downs land
// within doubleClickWindow of the previous up (help-macos.txt CAVEATS).
func (b *Backend) nextClickState(bt backend.Button) int {
	key := string(bt)
	last, ok := b.lastButtonUp[key]
	if ok && time.Since(last) <= doubleClickWindow {
		b.clickState[key]++
	} else {
		b.clickState[key] = 1
	}
	return b.clickState[key]
}

// Scroll scrolls at the current pointer position (help.txt:339-341).
//
// Unit reconciliation (ticket Decision vs. CONCEPT.md ch.8): CONCEPT.md's
// Korean summary describes scroll generally as line-unit
// (kCGScrollEventUnitLine), which is correct for the default by=line case
// below. The ticket's Decisions section is explicit and more specific for
// by=page: "one page = the current window's height (the display's height
// when no current window), sent in pixel units." That pixel-units clause
// is authoritative for by=page specifically; it does not contradict
// CONCEPT.md's line-unit statement, which is about by=line.
func (b *Backend) Scroll(ctx context.Context, dir backend.Dir, ticks int, by backend.ScrollUnit) error {
	units, amount := scrollUnitsAndAmount(ticks, by, b.pageHeightPixels())
	var vertical, horizontal int32
	switch dir {
	case "up":
		vertical = int32(amount)
	case "down":
		vertical = -int32(amount)
	case "left":
		horizontal = -int32(amount)
	case "right":
		horizontal = int32(amount)
	}
	var ev uintptr
	if horizontal != 0 {
		if runtime.GOARCH == "arm64" {
			// ffi.go's cgEventCreateScrollWheelEventXYVariadicARM64 doc
			// comment: Apple's arm64 ABI forces a C-variadic function's
			// "..." tail onto the stack, so wheel2 (horizontal) needs the
			// register-exhausting p1-p4 padding to land where the callee
			// reads it; the fixed-arity XY binding used on amd64 below
			// would silently drop wheel2 here.
			ev = cgEventCreateScrollWheelEventXYVariadicARM64(b.evtSource, units, 2, vertical, 0, 0, 0, 0, horizontal)
		} else {
			ev = cgEventCreateScrollWheelEventXY(b.evtSource, units, 2, vertical, horizontal)
		}
	} else {
		ev = cgEventCreateScrollWheelEvent(b.evtSource, units, 1, vertical)
	}
	if ev == 0 {
		return nil
	}
	cgEventSetFlags(ev, b.heldFlags)
	cgEventPost(cgHIDEventTap, ev)
	cfRelease(ev)
	return nil
}

// scrollUnitsAndAmount is Scroll's pure unit-selection logic, split out so
// it is testable without any CGEvent* call: by=line sends `ticks` in line
// units (kCGScrollEventUnitLine); by=page sends `ticks * pageHeightPixels`
// in pixel units (kCGScrollEventUnitPixel), per the ticket Decision (see
// Scroll's doc comment for the CONCEPT.md reconciliation).
func scrollUnitsAndAmount(ticks int, by backend.ScrollUnit, pageHeightPixels int) (units int32, amount int) {
	if by == backend.ScrollPage {
		return cgScrollEventUnitPixel, ticks * pageHeightPixels
	}
	return cgScrollEventUnitLine, ticks
}

// pageHeightPixels is one page's worth of pixel-unit scroll: the current
// window's height, or the primary display's height when there is no
// current window (ticket Decision, "one page = the current window's
// height (the display's height when no current window)"). Phase 1 has no
// win yet (st.window is always nil in the engine), so this always reads
// the primary display's height.
func (b *Backend) pageHeightPixels() int {
	displays := activeDisplays()
	for _, d := range displays {
		if d.Primary && d.H > 0 {
			return d.H
		}
	}
	return 900 // conservative fallback if no primary display is reported
}
