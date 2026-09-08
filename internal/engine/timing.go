package engine

import (
	"context"
	"time"

	"github.com/kang-sw/gotto-hando/internal/ir"
)

// noDefaultDelay lists the op kinds that take no global delay; only an
// explicit d= applies to them (help.txt:212-214, :426, :497).
var noDefaultDelay = map[ir.Kind]bool{
	ir.KindQueryInfo:    true,
	ir.KindQueryDisp:    true,
	ir.KindQueryMouse:   true,
	ir.KindQueryWindows: true,
	ir.KindQueryClip:    true,
	ir.KindSleep:        true,
	ir.KindSet:          true,
	ir.KindCapture:      true,
	ir.KindExec:         true,
	ir.KindOpen:         true,
}

// delayFor computes the post-command wait per the STATE MACHINE precedence
// (help.txt:210, :495-497, :510): an explicit d= on the line beats the
// running global delay, which beats --delay, which defaults to 100ms. The
// no-default-delay commands wait only when they carry an explicit d=.
func delayFor(op *ir.Op, runningDelayMS int) time.Duration {
	if op.DelayMS != nil {
		return time.Duration(*op.DelayMS) * time.Millisecond
	}
	if noDefaultDelay[op.Kind] {
		return 0
	}
	return time.Duration(runningDelayMS) * time.Millisecond
}

func sleepCtx(ctx context.Context, d time.Duration) {
	if d <= 0 {
		return
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
