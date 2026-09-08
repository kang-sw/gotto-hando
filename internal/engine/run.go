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
	"errors"
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

	// OutDir is the resolved capture directory (help.txt OUTPUT "Capture
	// paths"); the engine writes cap PNGs under it. cmd/gotto-hando
	// computes it via output.DefaultOutDir before calling Run.
	OutDir string
	// InlineCaptures is --inline-captures: cap encodes to base64 in the
	// --jsonl object ("data"/"fmt") instead of writing a file
	// (help.txt --inline-captures :72-77).
	InlineCaptures bool

	// OnResult, when non-nil, is called synchronously right after each
	// line's Result is computed, before the inter-line delay - the bridge
	// (internal/bridge, 260908-feat-remote-ssh Phase 0) uses this to stream
	// JSONL per line instead of waiting for the whole Summary. nil is a
	// no-op, so every existing caller (the local-run path, every other
	// test) is unaffected.
	//
	// INVARIANT: every value appended to Summary.Results MUST also be
	// passed to OnResult (when non-nil) exactly once - the streaming
	// consumer (the bridge) relies solely on OnResult and never iterates
	// Summary.Results, so a Results append without a matching OnResult call
	// (a skip branch, the cap-on-error extra, the end-of-run auto-release
	// warns) is silently dropped over the wire while still printing locally.
	// New Results appends below must keep this pairing.
	OnResult func(output.Result)
}

// Summary is the finished run: the per-line results, the auto-release warns,
// the done line and the process exit code (help.txt EXIT CODES).
type Summary struct {
	Results       []output.Result
	Done          output.Done
	Exit          int
	OK, Err, Skip int
	HeldReleased  int

	// Aborted marks a Preflight failure (help.txt OUTPUT "Abort",
	// :561-565): no line ran, Results/Done stay empty, and the caller
	// prints the abort object instead (AbortCode/AbortMsg carry the
	// details, Exit is already output.AbortExit(AbortCode)).
	Aborted   bool
	AbortCode output.ErrorCode
	AbortMsg  string
}

type engineState struct {
	be      backend.Backend
	held    heldState
	window  *backend.Window
	delayMS int
	txtMS   int
	keyMS   int
	info    *backend.Info

	// outDir/inlineCaptures/capSeq back cap file writing: the capture
	// directory, whether to inline base64 instead of writing, and the
	// per-run NNNN counter (help.txt OUTPUT "Capture paths").
	outDir         string
	inlineCaptures bool
	capSeq         int
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
	if err := be.Preflight(ctx, seq); err != nil {
		var pfErr *backend.PreflightError
		code := output.EUnknown
		msg := err.Error()
		if errors.As(err, &pfErr) {
			code = pfErr.Code
			msg = pfErr.Msg
		}
		return Summary{Aborted: true, AbortCode: code, AbortMsg: msg, Exit: output.AbortExit(code)}
	}

	st := &engineState{
		be:             be,
		delayMS:        seq.Defaults.DelayMS,
		txtMS:          seq.Defaults.TextIntervalMS,
		keyMS:          seq.Defaults.KeyGapMS,
		outDir:         opt.OutDir,
		inlineCaptures: opt.InlineCaptures,
	}
	var sum Summary
	failed := false
	runStart := time.Now()

	for i := range seq.Ops {
		op := &seq.Ops[i]
		// A cancelled ctx means the caller is gone (the bridge's
		// disconnect-detection path, internal/bridge): stop unconditionally,
		// independent of -k/KeepGoing - once the caller is gone, "keep
		// going" is moot. releaseAll below still runs unconditionally, so
		// held keys/buttons are released the same as any other fail-fast
		// stop.
		if ctx.Err() != nil {
			r := output.Result{Line: op.Line, Status: "skip", Cmd: kindCmd[op.Kind]}
			sum.Results = append(sum.Results, r)
			sum.Skip++
			if opt.OnResult != nil {
				opt.OnResult(r)
			}
			continue
		}
		if failed && !opt.KeepGoing {
			r := output.Result{Line: op.Line, Status: "skip", Cmd: kindCmd[op.Kind]}
			sum.Results = append(sum.Results, r)
			sum.Skip++
			if opt.OnResult != nil {
				opt.OnResult(r)
			}
			continue
		}
		opStart := time.Now()
		r := st.execute(ctx, op)
		r.TMS = time.Since(opStart).Milliseconds()
		sum.Results = append(sum.Results, r)
		if opt.OnResult != nil {
			opt.OnResult(r)
		}
		switch r.Status {
		case "err":
			sum.Err++
			failed = true
			// --cap-on-error: after a failed line, take one extra capture
			// (help.txt --cap-on-error :58-59, EXECUTION :513). Its own
			// result is counted like any other line so the done totals match
			// the printed lines.
			if opt.CapOnError {
				cr := st.captureOnError(ctx, op.Line)
				sum.Results = append(sum.Results, cr)
				if opt.OnResult != nil {
					opt.OnResult(cr)
				}
				if cr.Status == "err" {
					sum.Err++
				} else {
					sum.OK++
				}
			}
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
		r := output.Result{
			Line: ar.line, Status: "warn", Cmd: ar.cmd, Src: ar.src,
			Detail: fmt.Sprintf("auto-released %s", ar.what),
		}
		sum.Results = append(sum.Results, r)
		if opt.OnResult != nil {
			opt.OnResult(r)
		}
	}

	sum.Done = output.Done{OK: sum.OK, Err: sum.Err, Skip: sum.Skip, HeldReleased: n,
		ElapsedMS: time.Since(runStart).Milliseconds()}
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
		var pressed []string
		for _, ch := range op.Keys {
			if err := st.be.KeyDown(ctx, ch[0]); err != nil {
				// The failed line's own keys are released immediately so
				// held state stays consistent (help.txt:531-532), rather
				// than lingering in held until end-of-run releaseAll.
				st.rollbackKeys(ctx, pressed)
				return fail(output.EInput, err.Error())
			}
			st.held.pressKey(ch[0], op.Line, op.Src)
			pressed = append(pressed, ch[0])
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
			return fail(boundsAwareCode(err), err.Error())
		}
	case ir.KindClick:
		if err := st.doClick(ctx, op); err != nil {
			return fail(boundsAwareCode(err), err.Error())
		}
	case ir.KindButtonDown:
		if op.HasPoint {
			if err := st.doMoveTo(ctx, op.Point); err != nil {
				return fail(boundsAwareCode(err), err.Error())
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
			return fail(boundsAwareCode(err), err.Error())
		}
	case ir.KindScroll:
		pageH := 0
		if op.ScrollBy == "page" {
			pageH = st.pageHeight(ctx)
		}
		if err := st.be.Scroll(ctx, backend.Dir(op.ScrollDir), op.Ticks, backend.ScrollUnit(op.ScrollBy), pageH); err != nil {
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
		res.JSON = []output.KV{{Key: "text", Val: s}}
	case ir.KindFocus:
		return st.doFocus(ctx, op, res, fail)
	case ir.KindQueryWindows:
		wins, err := st.be.Windows(ctx, op.Selector)
		if err != nil {
			return fail(output.EUnknown, err.Error())
		}
		res.AlwaysShow = true
		res.Extra = formatQueryWindows(wins)
		res.JSON = queryWindowsJSON(wins)
	case ir.KindOpen:
		return st.doOpen(ctx, op, res, fail)
	case ir.KindExec:
		return st.doExec(ctx, op, res, fail)
	case ir.KindCapture:
		return st.doCapture(ctx, op, res, fail)
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
		info, err := st.be.Info(ctx)
		if err != nil {
			return fail(output.EUnknown, err.Error())
		}
		res.AlwaysShow = true
		res.Detail = formatQueryInfo(info)
		res.JSON = queryInfoJSON(info)
	case ir.KindQueryDisp:
		info, err := st.be.Info(ctx)
		if err != nil {
			return fail(output.EUnknown, err.Error())
		}
		res.AlwaysShow = true
		res.Extra = formatQueryDisp(info)
		res.JSON = queryDispJSON(info)
	case ir.KindQueryMouse:
		pos, err := st.be.MousePos(ctx)
		if err != nil {
			return fail(output.EUnknown, err.Error())
		}
		res.AlwaysShow = true
		res.Extra = formatQueryMouse(pos)
		res.JSON = queryMouseJSON(pos)
	}
	return res
}

func (st *engineState) doFocus(ctx context.Context, op *ir.Op, res output.Result, fail func(output.ErrorCode, string) output.Result) output.Result {
	// win[wait=DUR] polls the window list every 100ms until a match appears
	// or DUR elapses (help.txt WINDOW SELECTORS :295-296); without wait= it
	// is a single lookup. The same poll loop backs open[wait=] in Phase 3.
	waitMS := 0
	if op.HasWait {
		waitMS = op.WaitMS
	}
	wins, err := pollForWindow(ctx, waitMS, realClock{}, func() ([]backend.Window, error) {
		return st.be.Windows(ctx, op.Selector)
	})
	if err != nil {
		return fail(output.ENoWindow, err.Error())
	}
	if len(wins) == 0 {
		return fail(output.ENoWindow, "no matching window")
	}
	// The frontmost (z-order) match wins; the backend returns matches in
	// z-order (help.txt WINDOW SELECTORS :294).
	w := wins[0]
	if err := st.be.Focus(ctx, w); err != nil {
		return fail(output.ENoWindow, err.Error())
	}
	st.window = &w
	if len(wins) > 1 {
		res.Status = "warn"
	}
	// OUTPUT win line (help.txt :600): id/app/matched, then the window's
	// origin, size and quoted title.
	res.Detail = formatFocusDetail(w, len(wins))
	res.JSON = focusJSON(w, len(wins))
	return res
}

func (st *engineState) doExec(ctx context.Context, op *ir.Op, res output.Result, fail func(output.ErrorCode, string) output.Result) output.Result {
	req := backend.ExecReq{Argv: op.Argv, Cmd: op.Cmd, Shell: op.Shell,
		Timeout: time.Duration(op.TimeoutMS) * time.Millisecond}
	start := time.Now()
	r, err := st.be.Exec(ctx, req)
	ms := time.Since(start).Milliseconds()
	if err != nil {
		// Spawn failure / unexpected Wait error only - a timeout or a
		// non-zero exit are both reported through r with a nil err (darwin
		// exec.go's contract), so this path never sees them.
		return fail(output.EExec, err.Error())
	}
	res.Detail = formatExecDetail(r, ms)
	res.Extra = formatExecOutputLines(r)
	res.JSON = execJSON(r)
	// timeout expiry is E_TIMEOUT and noerr never softens it (help.txt:390,
	// :536); it is checked before the exit-code softening below. The two
	// err branches below set res.Status/ErrCode/ErrMsg directly instead of
	// calling fail(...): fail closes over execute()'s own res variable, not
	// this by-value res copy, so routing through it here would silently
	// drop the Detail/Extra/JSON just attached above (help.txt: "Output
	// lines also follow err results (non-zero exit, timeout)").
	if r.TimedOut {
		res.Status = "err"
		res.ErrCode = output.ETimeout
		res.ErrMsg = fmt.Sprintf("exec timed out after %s", req.Timeout)
		return res
	}
	if r.Exit != 0 && !op.Noerr {
		res.Status = "err"
		res.ErrCode = output.EExec
		res.ErrMsg = fmt.Sprintf("exit=%d", r.Exit)
		return res
	}
	res.AlwaysShow = true
	return res
}

// doOpen runs open (help.txt open :363-368): launch, then - only when
// wait= was given (op.HasWait) - poll for a window of the app through the
// shared 100ms pollForWindow loop (the same mechanism win[wait=] uses).
// Deliberately does not call st.be.Focus and does not set st.window; only
// win "becomes the current window" (help.txt:356-357) - open is never
// mentioned there, and without wait= it does nothing after Open() succeeds
// (help.txt: "No default delay ... use open[wait=5s] ... to wait for the
// UI").
func (st *engineState) doOpen(ctx context.Context, op *ir.Op, res output.Result, fail func(output.ErrorCode, string) output.Result) output.Result {
	if err := st.be.Open(ctx, op.Target); err != nil {
		return fail(output.EExec, err.Error())
	}
	if !op.HasWait {
		return res
	}
	sel := openAppSelector(op.Target)
	wins, err := pollForWindow(ctx, op.WaitMS, realClock{}, func() ([]backend.Window, error) {
		return st.be.Windows(ctx, sel)
	})
	if err != nil {
		return fail(output.ENoWindow, err.Error())
	}
	if len(wins) == 0 {
		return fail(output.ENoWindow, fmt.Sprintf("no window of %q appeared", op.Target))
	}
	return res
}

// rollbackKeys releases keys a single kd line already pressed before it
// failed partway, in reverse order, and drops them from held state. It
// implements the -k "keys taken by the failed line are released
// immediately" clause (help.txt:531-532).
func (st *engineState) rollbackKeys(ctx context.Context, keys []string) {
	for i := len(keys) - 1; i >= 0; i-- {
		_ = st.be.KeyUp(ctx, keys[i])
		st.held.releaseKey(keys[i])
	}
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
