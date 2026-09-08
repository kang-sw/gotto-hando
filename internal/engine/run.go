// Package engine executes an ir.Sequence against a backend.Backend,
// composing the thin backend primitives into clicks, drags, key chords,
// repeats and interpolation (CONCEPT.md ch.8.2). It implements the
// EXECUTION run step (help.txt:495-500), the STATE MACHINE (:502-517) and
// the ERROR POLICY (:518-541). It is exercised this phase only through the
// dryrun backend in tests; the CLI does not wire it (no real backend
// exists).
package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/kang-sw/gotto-hando/internal/backend"
	"github.com/kang-sw/gotto-hando/internal/ir"
	"github.com/kang-sw/gotto-hando/internal/output"
)

// RunOptions are the run-level flags (a subset of help.txt OPTIONS relevant
// to the engine).
type RunOptions struct {
	KeepGoing  bool // -k
	CapOnError bool // --cap-on-error
}

// Summary is the finished run: the per-line results, the auto-release warns,
// the done line and the process exit code (help.txt EXIT CODES).
type Summary struct {
	Results       []output.Result
	Done          output.Done
	Exit          int
	OK, Err, Skip int
	HeldReleased  int
}

type engineState struct {
	be      backend.Backend
	held    heldState
	window  *backend.Window
	delayMS int
	txtMS   int
	keyMS   int
	info    *backend.Info
}

var kindCmd = map[ir.Kind]string{
	ir.KindFocus: "win", ir.KindQueryWindows: "qwin", ir.KindKey: "k",
	ir.KindKeyDown: "kd", ir.KindKeyUp: "ku", ir.KindText: "txt",
	ir.KindMove: "m", ir.KindClick: "c", ir.KindButtonDown: "md",
	ir.KindButtonUp: "mu", ir.KindDrag: "drag", ir.KindScroll: "scroll",
	ir.KindClipboard: "clip", ir.KindPaste: "paste", ir.KindQueryClip: "qclip",
	ir.KindOpen: "open", ir.KindExec: "exec", ir.KindCapture: "cap",
	ir.KindSleep: "sleep", ir.KindSet: "set", ir.KindQueryInfo: "qinfo",
	ir.KindQueryDisp: "qdisp", ir.KindQueryMouse: "qmouse",
}

// Run executes the sequence. On a failed line, fail-fast marks the rest skip
// and releases held keys; -k continues, releasing the failed line's own
// transient keys immediately (the per-op helpers do this via defer). The
// exit code is 1 if any line failed, else 0.
func Run(ctx context.Context, be backend.Backend, seq *ir.Sequence, opt RunOptions) Summary {
	st := &engineState{
		be:      be,
		delayMS: seq.Defaults.DelayMS,
		txtMS:   seq.Defaults.TextIntervalMS,
		keyMS:   seq.Defaults.KeyGapMS,
	}
	var sum Summary
	failed := false

	for i := range seq.Ops {
		op := &seq.Ops[i]
		if failed && !opt.KeepGoing {
			sum.Results = append(sum.Results, output.Result{Line: op.Line, Status: "skip", Cmd: kindCmd[op.Kind]})
			sum.Skip++
			continue
		}
		r := st.execute(ctx, op)
		sum.Results = append(sum.Results, r)
		switch r.Status {
		case "err":
			sum.Err++
			failed = true
		case "skip":
			sum.Skip++
		default: // ok / warn
			sum.OK++
		}
		if r.Status != "err" {
			sleepCtx(ctx, delayFor(op, st.delayMS))
		}
	}

	// End of run: release still-held keys/buttons in reverse order
	// (help.txt:498-500, :515-516).
	n, rels := st.held.releaseAll(ctx, be)
	sum.HeldReleased = n
	for _, ar := range rels {
		sum.Results = append(sum.Results, output.Result{
			Line: ar.line, Status: "warn", Cmd: ar.cmd, Src: ar.src,
			Detail: fmt.Sprintf("auto-released %s", ar.what),
		})
	}

	sum.Done = output.Done{OK: sum.OK, Err: sum.Err, Skip: sum.Skip, HeldReleased: n}
	if failed {
		sum.Exit = output.ExitRuntimeFailure
	} else {
		sum.Exit = output.ExitOK
	}
	return sum
}

func (st *engineState) getInfo(ctx context.Context) backend.Info {
	if st.info == nil {
		info, _ := st.be.Info(ctx)
		st.info = &info
	}
	return *st.info
}

func (st *engineState) execute(ctx context.Context, op *ir.Op) output.Result {
	cmd := kindCmd[op.Kind]
	res := output.Result{Line: op.Line, Cmd: cmd, Src: op.Src, Status: "ok"}
	fail := func(code output.ErrorCode, msg string) output.Result {
		res.Status = "err"
		res.ErrCode = code
		res.ErrMsg = msg
		return res
	}

	switch op.Kind {
	case ir.KindKey:
		if err := st.doKey(ctx, op); err != nil {
			return fail(output.EInput, err.Error())
		}
	case ir.KindKeyDown:
		for _, ch := range op.Keys {
			if err := st.be.KeyDown(ctx, ch[0]); err != nil {
				return fail(output.EInput, err.Error())
			}
			st.held.pressKey(ch[0], op.Line, op.Src)
		}
	case ir.KindKeyUp:
		for _, ch := range op.Keys {
			if err := st.be.KeyUp(ctx, ch[0]); err != nil {
				return fail(output.EInput, err.Error())
			}
			st.held.releaseKey(ch[0])
		}
	case ir.KindText:
		if err := st.be.TypeText(ctx, op.Text, time.Duration(op.IntervalMS)*time.Millisecond); err != nil {
			return fail(output.EInput, err.Error())
		}
	case ir.KindMove:
		if err := st.doMove(ctx, op); err != nil {
			return fail(output.EInput, err.Error())
		}
	case ir.KindClick:
		if err := st.doClick(ctx, op); err != nil {
			return fail(output.EInput, err.Error())
		}
	case ir.KindButtonDown:
		if op.HasPoint {
			if err := st.doMoveTo(ctx, op.Point); err != nil {
				return fail(output.EInput, err.Error())
			}
		}
		b := backend.Button(op.Button)
		if err := st.be.ButtonDown(ctx, b); err != nil {
			return fail(output.EInput, err.Error())
		}
		st.held.pressButton(b, op.Line, op.Src)
	case ir.KindButtonUp:
		b := backend.Button(op.Button)
		if err := st.be.ButtonUp(ctx, b); err != nil {
			return fail(output.EInput, err.Error())
		}
		st.held.releaseButton(b)
	case ir.KindDrag:
		if err := st.doDrag(ctx, op); err != nil {
			return fail(output.EInput, err.Error())
		}
	case ir.KindScroll:
		if err := st.be.Scroll(ctx, backend.Dir(op.ScrollDir), op.Ticks, backend.ScrollUnit(op.ScrollBy)); err != nil {
			return fail(output.EInput, err.Error())
		}
	case ir.KindClipboard:
		if err := st.be.ClipboardSet(ctx, op.Text); err != nil {
			return fail(output.EClipboard, err.Error())
		}
	case ir.KindPaste:
		if err := st.be.ClipboardSet(ctx, op.Text); err != nil {
			return fail(output.EClipboard, err.Error())
		}
		sleepCtx(ctx, time.Duration(op.IntervalMS)*time.Millisecond)
		if err := st.pressPrimaryV(ctx); err != nil {
			return fail(output.EInput, err.Error())
		}
	case ir.KindQueryClip:
		s, err := st.be.ClipboardGet(ctx)
		if err != nil {
			return fail(output.EClipboard, err.Error())
		}
		res.AlwaysShow = true
		res.Detail = s
	case ir.KindFocus:
		return st.doFocus(ctx, op, res, fail)
	case ir.KindQueryWindows:
		if _, err := st.be.Windows(ctx, op.Selector); err != nil {
			return fail(output.EUnknown, err.Error())
		}
		res.AlwaysShow = true
	case ir.KindOpen:
		if err := st.be.Open(ctx, op.Target); err != nil {
			return fail(output.EExec, err.Error())
		}
	case ir.KindExec:
		return st.doExec(ctx, op, res, fail)
	case ir.KindCapture:
		req := backend.CaptureReq{Frame: op.Frame, Display: op.Disp, Rect: op.Rect,
			Scale: op.Scale, ScaleNative: op.ScaleNative, Format: op.Format}
		if _, err := st.be.Capture(ctx, req); err != nil {
			return fail(output.ECapture, err.Error())
		}
		res.AlwaysShow = true
	case ir.KindSleep:
		sleepCtx(ctx, time.Duration(op.SleepMS)*time.Millisecond)
	case ir.KindSet:
		if op.SetDelay != nil {
			st.delayMS = *op.SetDelay
		}
		if op.SetTxtms != nil {
			st.txtMS = *op.SetTxtms
		}
		if op.SetKeyms != nil {
			st.keyMS = *op.SetKeyms
		}
	case ir.KindQueryInfo:
		if _, err := st.be.Info(ctx); err != nil {
			return fail(output.EUnknown, err.Error())
		}
		res.AlwaysShow = true
	case ir.KindQueryDisp:
		if _, err := st.be.Info(ctx); err != nil {
			return fail(output.EUnknown, err.Error())
		}
		res.AlwaysShow = true
	case ir.KindQueryMouse:
		if _, err := st.be.MousePos(ctx); err != nil {
			return fail(output.EUnknown, err.Error())
		}
		res.AlwaysShow = true
	}
	return res
}

func (st *engineState) doFocus(ctx context.Context, op *ir.Op, res output.Result, fail func(output.ErrorCode, string) output.Result) output.Result {
	wins, err := st.be.Windows(ctx, op.Selector)
	if err != nil {
		return fail(output.ENoWindow, err.Error())
	}
	if len(wins) == 0 {
		return fail(output.ENoWindow, "no matching window")
	}
	w := wins[0]
	if err := st.be.Focus(ctx, w); err != nil {
		return fail(output.ENoWindow, err.Error())
	}
	st.window = &w
	if len(wins) > 1 {
		res.Status = "warn"
	}
	res.Detail = fmt.Sprintf("id=%d app=%s matched=%d", w.ID, w.App, len(wins))
	return res
}

func (st *engineState) doExec(ctx context.Context, op *ir.Op, res output.Result, fail func(output.ErrorCode, string) output.Result) output.Result {
	req := backend.ExecReq{Argv: op.Argv, Cmd: op.Cmd, Shell: op.Shell,
		Timeout: time.Duration(op.TimeoutMS) * time.Millisecond}
	r, err := st.be.Exec(ctx, req)
	if err != nil {
		return fail(output.EExec, err.Error())
	}
	if r.Exit != 0 && !op.Noerr {
		return fail(output.EExec, fmt.Sprintf("exit=%d", r.Exit))
	}
	res.Detail = fmt.Sprintf("exit=%d", r.Exit)
	res.AlwaysShow = true
	return res
}

func (st *engineState) pressPrimaryV(ctx context.Context) error {
	if err := st.be.KeyDown(ctx, "primary"); err != nil {
		return err
	}
	if err := st.be.KeyDown(ctx, "v"); err != nil {
		_ = st.be.KeyUp(ctx, "primary")
		return err
	}
	_ = st.be.KeyUp(ctx, "v")
	return st.be.KeyUp(ctx, "primary")
}
