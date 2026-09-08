package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kang-sw/gotto-hando/internal/backend"
)

// fakeClock is a virtual clock: now() reports the current virtual time and
// sleep() advances it (optionally overshooting by jitter to model a
// scheduler that oversleeps), so pollForWindow and runFrameSeries run with
// zero real delay and fully deterministic timing (Verification Plan: "fake
// clock, no real sleep").
type fakeClock struct {
	t      time.Time
	jitter time.Duration // extra time every sleep overshoots by
	sleeps []time.Duration
}

func (c *fakeClock) now() time.Time { return c.t }

func (c *fakeClock) sleep(_ context.Context, d time.Duration) {
	c.sleeps = append(c.sleeps, d)
	c.t = c.t.Add(d + c.jitter)
}

func msDur(ms int) time.Duration { return time.Duration(ms) * time.Millisecond }

// TestPollForWindowImmediateMatch: a query that matches on the first look
// returns at once with no sleeping, whatever wait= was asked for
// (help.txt WINDOW SELECTORS :295-296).
func TestPollForWindowImmediateMatch(t *testing.T) {
	clk := &fakeClock{}
	win := backend.Window{ID: 7}
	got, err := pollForWindow(context.Background(), 500, clk, func() ([]backend.Window, error) {
		return []backend.Window{win}, nil
	})
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if len(got) != 1 || got[0].ID != 7 {
		t.Fatalf("got = %+v, want the single window id=7", got)
	}
	if len(clk.sleeps) != 0 {
		t.Fatalf("sleeps = %v, want none (matched immediately)", clk.sleeps)
	}
}

// TestPollForWindowNoWait: with wait=0 (win without wait=) a missing window
// is a single lookup, no polling, empty result (the caller maps that to
// E_NOWINDOW).
func TestPollForWindowNoWait(t *testing.T) {
	clk := &fakeClock{}
	got, err := pollForWindow(context.Background(), 0, clk, func() ([]backend.Window, error) {
		return nil, nil
	})
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if len(got) != 0 {
		t.Fatalf("got = %+v, want empty", got)
	}
	if len(clk.sleeps) != 0 {
		t.Fatalf("sleeps = %v, want none (no wait requested)", clk.sleeps)
	}
}

// TestPollForWindowMatchesAfterPolling: a window that appears on the third
// poll returns after two 100ms sleeps (the fixed windowPollInterval,
// help.txt :295-296).
func TestPollForWindowMatchesAfterPolling(t *testing.T) {
	clk := &fakeClock{}
	attempts := 0
	got, err := pollForWindow(context.Background(), 500, clk, func() ([]backend.Window, error) {
		attempts++
		if attempts >= 3 {
			return []backend.Window{{ID: 1}}, nil
		}
		return nil, nil
	})
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if len(got) != 1 {
		t.Fatalf("got = %+v, want a match", got)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
	want := []time.Duration{msDur(100), msDur(100)}
	if len(clk.sleeps) != 2 || clk.sleeps[0] != want[0] || clk.sleeps[1] != want[1] {
		t.Fatalf("sleeps = %v, want %v", clk.sleeps, want)
	}
}

// TestPollForWindowTimeoutClampsLastInterval: a window that never appears
// polls at 0,100,200,250 and times out at the deadline; the final sleep is
// clamped to the 50ms remaining, never overshooting wait=250.
func TestPollForWindowTimeoutClampsLastInterval(t *testing.T) {
	clk := &fakeClock{}
	got, err := pollForWindow(context.Background(), 250, clk, func() ([]backend.Window, error) {
		return nil, nil
	})
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if len(got) != 0 {
		t.Fatalf("got = %+v, want empty (timed out)", got)
	}
	want := []time.Duration{msDur(100), msDur(100), msDur(50)}
	if len(clk.sleeps) != len(want) {
		t.Fatalf("sleeps = %v, want %v", clk.sleeps, want)
	}
	var total time.Duration
	for i, s := range clk.sleeps {
		if s != want[i] {
			t.Fatalf("sleeps = %v, want %v", clk.sleeps, want)
		}
		total += s
	}
	if total != msDur(250) {
		t.Fatalf("total slept = %v, want the 250ms deadline exactly", total)
	}
}

// TestPollForWindowQueryError aborts the poll and returns the error at once.
func TestPollForWindowQueryError(t *testing.T) {
	clk := &fakeClock{}
	boom := errors.New("backend down")
	_, err := pollForWindow(context.Background(), 500, clk, func() ([]backend.Window, error) {
		return nil, boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the injected error", err)
	}
	if len(clk.sleeps) != 0 {
		t.Fatalf("sleeps = %v, want none (error aborts before sleeping)", clk.sleeps)
	}
}

// TestFrameSeriesSingleFrame: n<1 is normalised to one frame, taken
// immediately with zero scheduled/actual/slip and no sleeping.
func TestFrameSeriesSingleFrame(t *testing.T) {
	clk := &fakeClock{}
	shots := 0
	got, err := runFrameSeries(context.Background(), 0, 100, clk.now(), clk, func(int) error {
		shots++
		return nil
	})
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if shots != 1 || len(got) != 1 {
		t.Fatalf("shots=%d results=%d, want 1 each", shots, len(got))
	}
	if got[0] != (frameResult{index: 0, scheduledMS: 0, actualMS: 0, slipMS: 0}) {
		t.Fatalf("frame = %+v, want all-zero", got[0])
	}
	if len(clk.sleeps) != 0 {
		t.Fatalf("sleeps = %v, want none", clk.sleeps)
	}
}

// TestFrameSeriesOnSchedule: with an ideal clock (sleep advances exactly)
// three frames land on 0/100/200 with zero slip - the happy path.
func TestFrameSeriesOnSchedule(t *testing.T) {
	clk := &fakeClock{}
	start := clk.now()
	got, err := runFrameSeries(context.Background(), 3, 100, start, clk, func(int) error { return nil })
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	want := []frameResult{
		{index: 0, scheduledMS: 0, actualMS: 0, slipMS: 0},
		{index: 1, scheduledMS: 100, actualMS: 100, slipMS: 0},
		{index: 2, scheduledMS: 200, actualMS: 200, slipMS: 0},
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("frame %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestFrameSeriesAbsoluteDeadlineNoDrift is the core anti-drift assertion
// (help.txt cap "Timing is best effort" :411, CAVEAT :799-800): the
// scheduler sleeps to ABSOLUTE deadlines (start + i*interval), so a clock
// that oversleeps by 10ms every frame keeps slip PINNED at ~10ms instead of
// accumulating (20, 30, ...) as per-frame sleeps would. Scheduled times stay
// exact multiples of the interval regardless.
func TestFrameSeriesAbsoluteDeadlineNoDrift(t *testing.T) {
	clk := &fakeClock{jitter: msDur(10)}
	start := clk.now()
	got, err := runFrameSeries(context.Background(), 4, 100, start, clk, func(int) error { return nil })
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	for i, fr := range got {
		if fr.scheduledMS != int64(i*100) {
			t.Fatalf("frame %d scheduledMS = %d, want %d (exact multiple, no drift)", i, fr.scheduledMS, i*100)
		}
	}
	// Frame 0 takes no sleep (slip 0); every later frame slips exactly the
	// one 10ms overshoot, never a growing sum.
	if got[0].slipMS != 0 {
		t.Fatalf("frame 0 slip = %d, want 0", got[0].slipMS)
	}
	for i := 1; i < len(got); i++ {
		if got[i].slipMS != 10 {
			t.Fatalf("frame %d slip = %d, want a constant 10 (proves no accumulation)", i, got[i].slipMS)
		}
	}
}

// TestFrameSeriesShootErrorAborts: a failing frame stops the burst and
// returns the frames taken so far (the call site maps the error to
// E_CAPTURE).
func TestFrameSeriesShootErrorAborts(t *testing.T) {
	clk := &fakeClock{}
	boom := errors.New("screenshot failed")
	got, err := runFrameSeries(context.Background(), 5, 100, clk.now(), clk, func(i int) error {
		if i == 2 {
			return boom
		}
		return nil
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the injected error", err)
	}
	if len(got) != 2 {
		t.Fatalf("frames = %d, want the 2 taken before the failure", len(got))
	}
}
