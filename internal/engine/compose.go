package engine

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kang-sw/gotto-hando/internal/backend"
	"github.com/kang-sw/gotto-hando/internal/ir"
	"github.com/kang-sw/gotto-hando/internal/output"
)

// errBounds is resolve()'s run-time coordinate-bounds sentinel
// (help.txt:262-268: r/w/% coordinates are checked right before their line
// runs, failing that line with E_BOUNDS - unlike absolute/disp=
// coordinates, which Preflight already rejected before any line ran). The
// three engine.go call sites (doMove/doClick/doDrag, and button_down's
// optional move) use errors.Is against this sentinel to pick E_BOUNDS over
// the default E_INPUT.
var errBounds = errors.New("coordinate outside bounds")

// boundsAwareCode picks E_BOUNDS for a resolve() bounds failure, else the
// default E_INPUT every other move/click/drag error already used
// (run.go's KindMove/KindClick/KindButtonDown/KindDrag call sites).
func boundsAwareCode(err error) output.ErrorCode {
	if errors.Is(err, errBounds) {
		return output.EBounds
	}
	return output.EInput
}

// pressMods presses the command's modifier flags in canonical order and
// returns a release func that lifts them in reverse (help.txt:228). The
// release runs even when the command fails midway, so a failed line's own
// transient keys are released immediately (ERROR POLICY, help.txt:521-522).
func (st *engineState) pressMods(ctx context.Context, mods []string) (func(), error) {
	var pressed []string
	release := func() {
		for i := len(pressed) - 1; i >= 0; i-- {
			_ = st.be.KeyUp(ctx, pressed[i])
		}
	}
	for _, m := range mods {
		if err := st.be.KeyDown(ctx, m); err != nil {
			release()
			return func() {}, err
		}
		pressed = append(pressed, m)
	}
	return release, nil
}

// doKey presses each sequential key/chord repeat times with the gap
// (help.txt:305-311). A chord is pressed in order and released in reverse.
func (st *engineState) doKey(ctx context.Context, op *ir.Op) error {
	release, err := st.pressMods(ctx, op.Mods)
	if err != nil {
		return err
	}
	defer release()
	gap := time.Duration(op.GapMS) * time.Millisecond
	for r := 0; r < op.Repeat; r++ {
		for _, chord := range op.Keys {
			var down []string
			for _, k := range chord {
				if err := st.be.KeyDown(ctx, k); err != nil {
					for i := len(down) - 1; i >= 0; i-- {
						_ = st.be.KeyUp(ctx, down[i])
					}
					return err
				}
				down = append(down, k)
			}
			for i := len(down) - 1; i >= 0; i-- {
				if err := st.be.KeyUp(ctx, down[i]); err != nil {
					return err
				}
			}
			sleepCtx(ctx, gap)
		}
	}
	return nil
}

// doClick moves first when a point is given, then presses/releases the
// button count times (help.txt:325-327).
func (st *engineState) doClick(ctx context.Context, op *ir.Op) error {
	release, err := st.pressMods(ctx, op.Mods)
	if err != nil {
		return err
	}
	defer release()
	if op.HasPoint {
		if err := st.doMoveTo(ctx, op.Point); err != nil {
			return err
		}
	}
	b := backend.Button(op.Button)
	gap := time.Duration(op.GapMS) * time.Millisecond
	for i := 0; i < op.Count; i++ {
		if err := st.be.ButtonDown(ctx, b); err != nil {
			return err
		}
		if err := st.be.ButtonUp(ctx, b); err != nil {
			return err
		}
		sleepCtx(ctx, gap)
	}
	return nil
}

func (st *engineState) doMove(ctx context.Context, op *ir.Op) error {
	p, err := st.resolve(ctx, op.Point)
	if err != nil {
		return err
	}
	return st.be.MouseMove(ctx, p, time.Duration(op.DurationMS)*time.Millisecond)
}

func (st *engineState) doMoveTo(ctx context.Context, pt ir.Point) error {
	p, err := st.resolve(ctx, pt)
	if err != nil {
		return err
	}
	return st.be.MouseMove(ctx, p, 0)
}

// resolve converts an ir.Point (frame + optional percentages) into an
// absolute backend.Point (help.txt:245-268). Frame sizes come from the
// current window, the pointer, or the desktop; Info is fetched lazily only
// when a percentage needs a frame size.
func (st *engineState) resolve(ctx context.Context, p ir.Point) (backend.Point, error) {
	var ox, oy, fw, fh float64
	switch p.Frame {
	case "window":
		if st.window != nil {
			ox, oy = float64(st.window.X), float64(st.window.Y)
			fw, fh = float64(st.window.W), float64(st.window.H)
		}
	case "pointer":
		pos, err := st.be.MousePos(ctx)
		if err != nil {
			return backend.Point{}, err
		}
		ox, oy = pos.X, pos.Y
	case "display":
		// disp=N is DISPLAY-RELATIVE (help.txt:250-253: m[disp=1]0,0 is
		// the top-left of display 1), so the origin is that display's
		// absolute (X,Y), not the desktop's (0,0) - and percents are
		// against that display's own size, not the desktop's. An
		// out-of-range display index is a bounds failure, matching
		// checkBounds' own index guard below.
		info := st.getInfo(ctx)
		if p.Disp < 0 || p.Disp >= len(info.DisplayList) {
			return backend.Point{}, fmt.Errorf("%w: display %d does not exist", errBounds, p.Disp)
		}
		d := info.DisplayList[p.Disp]
		ox, oy = float64(d.X), float64(d.Y)
		if p.XPct || p.YPct {
			fw, fh = float64(d.W), float64(d.H)
		}
	default: // desktop
		if p.XPct || p.YPct {
			info := st.getInfo(ctx)
			fw, fh = float64(info.DesktopW), float64(info.DesktopH)
		}
	}
	x := ox + p.X
	if p.XPct {
		x = ox + p.X/100*fw
	}
	y := oy + p.Y
	if p.YPct {
		y = oy + p.Y/100*fh
	}
	res := backend.Point{X: x, Y: y}

	// Run-time bounds check (help.txt:262-268): r is always checked here
	// ("checked on the resulting position"); desktop/display coordinates
	// are checked here only when they carry a percent flag - the
	// non-percent case was already rejected in Preflight before any line
	// ran, so re-checking it here would be redundant (and st.window is
	// always nil in Phase 1, so a window-frame point has nothing to check
	// against yet - Phase 2).
	if p.Frame != "window" && (p.Frame == "pointer" || p.XPct || p.YPct) {
		if err := checkBounds(st.getInfo(ctx), p, res); err != nil {
			return backend.Point{}, err
		}
	}
	return res, nil
}

// checkBounds validates a resolved point against the desktop (frame
// "desktop"/"pointer") or the named display's bounds (frame "display",
// help.txt:253: disp=N is 0-based, qdisp order). An out-of-range display
// index counts as out of bounds too.
func checkBounds(info backend.Info, p ir.Point, res backend.Point) error {
	var x, y, w, h int
	if p.Frame == "display" {
		if p.Disp < 0 || p.Disp >= len(info.DisplayList) {
			return fmt.Errorf("%w: display %d does not exist", errBounds, p.Disp)
		}
		d := info.DisplayList[p.Disp]
		x, y, w, h = d.X, d.Y, d.W, d.H
	} else {
		x, y, w, h = info.DesktopX, info.DesktopY, info.DesktopW, info.DesktopH
	}
	if res.X < float64(x) || res.X > float64(x+w) || res.Y < float64(y) || res.Y > float64(y+h) {
		return fmt.Errorf("%w: %g,%g outside %d,%d %dx%d", errBounds, res.X, res.Y, x, y, w, h)
	}
	return nil
}
