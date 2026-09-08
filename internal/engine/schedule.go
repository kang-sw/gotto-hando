package engine

import (
	"context"
	"time"

	"github.com/kang-sw/gotto-hando/internal/backend"
)

// clock is the engine's time seam: real time in production, a fake clock in
// tests so the win[wait=] poll loop and the cap frame-series scheduler are
// exercised without any real sleep or FFI (Verification Plan: "fake clock,
// no real sleep"). It is deliberately unexported and tiny - only now/sleep
// are needed.
type clock interface {
	now() time.Time
	sleep(ctx context.Context, d time.Duration)
}

// realClock backs clock with the wall clock and the context-aware sleepCtx
// the rest of the engine already uses.
type realClock struct{}

func (realClock) now() time.Time                             { return time.Now() }
func (realClock) sleep(ctx context.Context, d time.Duration) { sleepCtx(ctx, d) }

// windowPollInterval is win[wait=DUR]'s fixed poll cadence (help.txt WINDOW
// SELECTORS :295-296: "win[wait=DUR] polls every 100ms").
const windowPollInterval = 100 * time.Millisecond

// pollForWindow runs win/qwin's window lookup, optionally polling for
// waitMS milliseconds until a match appears (help.txt:295-296). query is
// the backend lookup (a closure so tests drive the loop without the whole
// Backend). It returns the first non-empty match, or an empty slice when
// the deadline passes with none (the caller maps that to E_NOWINDOW for
// win; qwin tolerates an empty list). A query error aborts the poll
// immediately. waitMS <= 0 is a single lookup with no polling.
func pollForWindow(ctx context.Context, waitMS int, clk clock, query func() ([]backend.Window, error)) ([]backend.Window, error) {
	deadline := clk.now().Add(time.Duration(waitMS) * time.Millisecond)
	for {
		wins, err := query()
		if err != nil {
			return nil, err
		}
		if len(wins) > 0 {
			return wins, nil
		}
		now := clk.now()
		if !now.Before(deadline) {
			return nil, nil // timed out (or no wait requested): no match
		}
		wait := windowPollInterval
		if rem := deadline.Sub(now); rem < wait {
			wait = rem
		}
		clk.sleep(ctx, wait)
	}
}

// frameResult is one captured frame's timing (help.txt JSONL :609-610:
// "each frame's scheduled and actual time and the slippage between them").
// All three are milliseconds relative to the burst start; slip = actual -
// scheduled.
type frameResult struct {
	index       int
	scheduledMS int64
	actualMS    int64
	slipMS      int64
}

// runFrameSeries drives a cap[n=,ms=] frame burst on ABSOLUTE deadlines
// (start + i*interval), not per-frame sleeps, so scheduling error does not
// accumulate across frames (help.txt cap "Timing is best effort" :412,
// CAVEAT :799-800). shoot performs one frame's capture+encode+write; its
// error aborts the burst (E_CAPTURE at the call site) and the frames taken
// so far are returned. clk/start are injected for the fake-clock test.
func runFrameSeries(ctx context.Context, n, intervalMS int, start time.Time, clk clock, shoot func(index int) error) ([]frameResult, error) {
	if n < 1 {
		n = 1
	}
	interval := time.Duration(intervalMS) * time.Millisecond
	results := make([]frameResult, 0, n)
	for i := 0; i < n; i++ {
		scheduled := start.Add(time.Duration(i) * interval)
		if i > 0 {
			if d := scheduled.Sub(clk.now()); d > 0 {
				clk.sleep(ctx, d)
			}
		}
		actual := clk.now()
		if err := shoot(i); err != nil {
			return results, err
		}
		results = append(results, frameResult{
			index:       i,
			scheduledMS: scheduled.Sub(start).Milliseconds(),
			actualMS:    actual.Sub(start).Milliseconds(),
			slipMS:      actual.Sub(scheduled).Milliseconds(),
		})
	}
	return results, nil
}
