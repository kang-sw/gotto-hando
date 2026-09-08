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

// kCGScrollEventUnitLine: each wheel unit is one "tick" (a physical wheel
// notch), matching help.txt scroll's ticks= payload (:339-341) more
// directly than pixel units would.
const cgScrollEventUnitLine = 1

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
// by=page uses the current display's height as one page's worth of scroll
// (no current-window fallback needed: st.window is always nil in Phase 1,
// win being Phase 2).
func (b *Backend) Scroll(ctx context.Context, dir backend.Dir, ticks int, by backend.ScrollUnit) error {
	amount := ticks
	if by == backend.ScrollPage {
		amount = ticks * b.pageLines()
	}
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
			ev = cgEventCreateScrollWheelEventXYVariadicARM64(b.evtSource, cgScrollEventUnitLine, 2, vertical, 0, 0, 0, 0, horizontal)
		} else {
			ev = cgEventCreateScrollWheelEventXY(b.evtSource, cgScrollEventUnitLine, 2, vertical, horizontal)
		}
	} else {
		ev = cgEventCreateScrollWheelEvent(b.evtSource, cgScrollEventUnitLine, 1, vertical)
	}
	if ev == 0 {
		return nil
	}
	cgEventSetFlags(ev, b.heldFlags)
	cgEventPost(cgHIDEventTap, ev)
	cfRelease(ev)
	return nil
}

// pageLines approximates "one page" as the primary display's height in
// lines, roughly matching a typical scrollable view's visible line count.
// A precise measurement needs the frontmost window/view, which Phase 1
// does not have (win is Phase 2); this is a best-effort constant scale
// rather than that.
func (b *Backend) pageLines() int {
	displays := activeDisplays()
	for _, d := range displays {
		if d.Primary {
			if d.H > 0 {
				return d.H / 20 // ~20 points per line, a reasonable default
			}
		}
	}
	return 20
}
