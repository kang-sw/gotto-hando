package engine

import (
	"context"
	"time"

	"github.com/kang-sw/gotto-hando/internal/backend"
	"github.com/kang-sw/gotto-hando/internal/ir"
)

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
	default: // desktop, display
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
	return backend.Point{X: x, Y: y}, nil
}
