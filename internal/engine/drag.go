package engine

import (
	"context"
	"time"

	"github.com/kang-sw/gotto-hando/internal/backend"
	"github.com/kang-sw/gotto-hando/internal/ir"
)

// doDrag moves to the first point, presses the button, follows the polyline
// in evenly spaced steps, and releases (help.txt:334-338). The button is
// released even when the drag fails midway.
func (st *engineState) doDrag(ctx context.Context, op *ir.Op) error {
	release, err := st.pressMods(ctx, op.Mods)
	if err != nil {
		return err
	}
	defer release()
	if len(op.Points) == 0 {
		return nil
	}
	first, err := st.resolve(ctx, op.Points[0])
	if err != nil {
		return err
	}
	if err := st.be.MouseMove(ctx, first, 0); err != nil {
		return err
	}
	b := backend.Button(op.Button)
	if err := st.be.ButtonDown(ctx, b); err != nil {
		return err
	}
	// Always release the button, even on a mid-drag failure.
	err = st.dragPath(ctx, op)
	if upErr := st.be.ButtonUp(ctx, b); upErr != nil && err == nil {
		err = upErr
	}
	return err
}

func (st *engineState) dragPath(ctx context.Context, op *ir.Op) error {
	total := time.Duration(op.DurationMS) * time.Millisecond
	segments := len(op.Points) - 1
	if segments < 1 {
		segments = 1
	}
	per := total / time.Duration(segments)
	for i := 1; i < len(op.Points); i++ {
		p, err := st.resolve(ctx, op.Points[i])
		if err != nil {
			return err
		}
		if err := st.be.MouseMove(ctx, p, per); err != nil {
			return err
		}
	}
	return nil
}
