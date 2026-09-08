package ir

import (
	"regexp"
	"strconv"

	"github.com/kang-sw/gotto-hando/internal/output"
)

var labelRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,47}$`)

// Validate is EXECUTION step 2 static validation (help.txt:467-471): value
// ranges and LIMITS, held balance, and re-checks of frame/percent rules. It
// needs no target (bounds, permissions and session state are preflight). It
// returns one Diagnostic per violation; --check and --ir stop here.
// numLines is the source line count for the lines-per-run limit.
func Validate(seq *Sequence, numLines int) []Diagnostic {
	var d []Diagnostic
	add := func(line, col int, src, msg string) {
		d = append(d, Diagnostic{Line: line, Col: col, Code: output.EValidate, Msg: msg, Src: src})
	}

	if numLines > MaxLinesPerRun {
		add(MaxLinesPerRun+1, 1, "", "sequence exceeds "+strconv.Itoa(MaxLinesPerRun)+" lines (LIMITS)")
	}

	held := map[string]bool{}   // keys held by kd
	button := map[string]bool{} // buttons held by md
	totalCaptures := 0

	for i := range seq.Ops {
		op := &seq.Ops[i]
		if len(op.Src) > MaxLineBytes {
			add(op.Line, 1, op.Src, "line exceeds 64 KiB (LIMITS)")
		}
		if op.DelayMS != nil && *op.DelayMS > MaxDelayMS {
			add(op.Line, 1, op.Src, "d= exceeds 10s (LIMITS)")
		}
		switch op.Kind {
		case KindKey:
			if len(op.Keys) > MaxKeysSeq {
				add(op.Line, 1, op.Src, "more than 64 sequential keys (LIMITS)")
			}
			for _, chord := range op.Keys {
				if len(op.Mods)+len(chord) > MaxChordKeys {
					add(op.Line, 1, op.Src, "chord exceeds 8 keys including flags (LIMITS)")
					break
				}
			}
			if op.GapMS > MaxDelayMS {
				add(op.Line, 1, op.Src, "ms= exceeds 10s (LIMITS)")
			}
		case KindKeyDown:
			for _, chord := range op.Keys {
				held[chord[0]] = true
			}
		case KindKeyUp:
			for _, chord := range op.Keys {
				k := chord[0]
				if !held[k] {
					add(op.Line, 1, op.Src, "ku releases key "+strconv.Quote(k)+" not held by an earlier kd")
				}
				delete(held, k)
			}
		case KindText, KindPaste:
			if op.IntervalMS > MaxDelayMS {
				add(op.Line, 1, op.Src, "ms= exceeds 10s (LIMITS)")
			}
		case KindMove:
			if op.DurationMS > MaxDelayMS {
				add(op.Line, 1, op.Src, "ms= exceeds 10s (LIMITS)")
			}
		case KindClick:
			if button[op.Button] {
				add(op.Line, 1, op.Src, "cannot click button "+strconv.Quote(op.Button)+" already held by md")
			}
			if op.GapMS > MaxDelayMS {
				add(op.Line, 1, op.Src, "ms= exceeds 10s (LIMITS)")
			}
		case KindButtonDown:
			if button[op.Button] {
				add(op.Line, 1, op.Src, "button "+strconv.Quote(op.Button)+" already held by md")
			}
			button[op.Button] = true
		case KindButtonUp:
			if !button[op.Button] {
				add(op.Line, 1, op.Src, "mu releases button "+strconv.Quote(op.Button)+" not held by md")
			}
			delete(button, op.Button)
		case KindDrag:
			if button[op.Button] {
				add(op.Line, 1, op.Src, "cannot drag button "+strconv.Quote(op.Button)+" already held by md")
			}
			if n := len(op.Points); n < MinDragPoints || n > MaxDragPoints {
				add(op.Line, 1, op.Src, "drag needs 1..200 points (LIMITS)")
			}
			if op.Count < MinDragSteps || op.Count > MaxDragSteps {
				add(op.Line, 1, op.Src, "drag steps must be 1..200 (LIMITS)")
			}
			if op.DurationMS > MaxDelayMS {
				add(op.Line, 1, op.Src, "ms= exceeds 10s (LIMITS)")
			}
		case KindScroll:
			if op.Ticks < MinScrollTicks || op.Ticks > MaxScrollTicks {
				add(op.Line, 1, op.Src, "scroll ticks must be 1..50 (LIMITS)")
			}
		case KindFocus:
			validateSelector(op, add)
			if op.WaitMS > MaxWaitMS {
				add(op.Line, 1, op.Src, "wait= exceeds 60s (LIMITS)")
			}
		case KindQueryWindows:
			validateSelector(op, add)
		case KindOpen:
			if op.WaitMS > MaxWaitMS {
				add(op.Line, 1, op.Src, "wait= exceeds 60s (LIMITS)")
			}
		case KindExec:
			if op.TimeoutMS > MaxExecTimeoutMS {
				add(op.Line, 1, op.Src, "exec timeout= exceeds 60s (LIMITS)")
			}
		case KindCapture:
			if op.CapCount > MaxCapN {
				add(op.Line, 1, op.Src, "cap n= exceeds 120 (LIMITS)")
			}
			if op.CapCount < 1 {
				add(op.Line, 1, op.Src, "cap n= must be >= 1")
			}
			totalCaptures += op.CapCount
			if !op.ScaleNative && (op.Scale < MinScale || op.Scale > MaxScale) {
				add(op.Line, 1, op.Src, "scale must be 0.1..4.0 or native (LIMITS)")
			}
			if op.CapInterval > MaxDelayMS {
				add(op.Line, 1, op.Src, "ms= exceeds 10s (LIMITS)")
			}
			if !labelRE.MatchString(op.Label) {
				add(op.Line, 1, op.Src, "label must match [A-Za-z0-9][A-Za-z0-9_-]{0,47} (LIMITS)")
			}
		case KindSleep:
			if op.SleepMS > MaxSleepMS {
				add(op.Line, 1, op.Src, "sleep exceeds 60s (LIMITS)")
			}
		}
	}

	if totalCaptures > MaxCapturesTotal {
		add(1, 1, "", "more than 121 captures per run (LIMITS)")
	}

	return d
}

func validateSelector(op *Op, add func(line, col int, src, msg string)) {
	s := op.Selector
	switch s.Kind {
	case "id", "pid":
		if _, err := strconv.Atoi(s.Value); err != nil {
			add(op.Line, 1, op.Src, s.Kind+": selector must be numeric")
		}
	case "title":
		if s.Regex {
			if _, err := regexp.Compile(s.Value); err != nil {
				add(op.Line, 1, op.Src, "invalid regex selector: "+err.Error())
			}
		}
	}
}
