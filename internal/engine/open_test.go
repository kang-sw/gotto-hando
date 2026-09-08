package engine_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/kang-sw/gotto-hando/internal/backend"
	"github.com/kang-sw/gotto-hando/internal/backend/dryrun"
	"github.com/kang-sw/gotto-hando/internal/engine"
	"github.com/kang-sw/gotto-hando/internal/output"
)

// TestOpenWithoutWaitNeverPolls: open without wait= does nothing after
// Open() succeeds (help.txt: "No default delay ... use open[wait=5s] ...
// to wait for the UI") - the backend's Windows lookup must never be called.
func TestOpenWithoutWaitNeverPolls(t *testing.T) {
	seq := parse(t, "open[]TextEdit")
	be := &dryrun.Backend{}
	sum := engine.Run(context.Background(), be, seq, engine.RunOptions{})

	if len(sum.Results) != 1 || sum.Results[0].Status != "ok" {
		t.Fatalf("Results = %+v, want a single ok result", sum.Results)
	}
	for _, c := range be.Calls {
		if strings.HasPrefix(c, "Windows ") {
			t.Fatalf("Windows was called (calls=%v), want no poll without wait=", be.Calls)
		}
	}
}

// TestOpenWithoutWaitNeverFocusesOrSetsWindow: open must not call Focus and
// must not become the current window - only win does either (help.txt
// :356-357).
func TestOpenWithoutWaitNeverFocusesOrSetsWindow(t *testing.T) {
	seq := parse(t, "open[]TextEdit")
	be := &dryrun.Backend{}
	engine.Run(context.Background(), be, seq, engine.RunOptions{})
	for _, c := range be.Calls {
		if strings.HasPrefix(c, "Focus ") {
			t.Fatalf("Focus was called (calls=%v), want open to never focus", be.Calls)
		}
	}
}

// TestOpenWaitSucceedsWithoutPollingDelay: open[wait=] with a window
// already present on the first lookup succeeds immediately (no polling
// sleep needed - pollForWindow returns on its first non-empty match).
func TestOpenWaitSucceedsWithoutPollingDelay(t *testing.T) {
	seq := parse(t, "open[wait=50ms]TextEdit")
	be := &dryrun.Backend{WindowsResult: []backend.Window{
		{ID: 1, App: "TextEdit", Title: "Untitled"},
	}}
	start := time.Now()
	sum := engine.Run(context.Background(), be, seq, engine.RunOptions{})
	elapsed := time.Since(start)

	if len(sum.Results) != 1 || sum.Results[0].Status != "ok" {
		t.Fatalf("Results = %+v, want a single ok result", sum.Results)
	}
	if elapsed > 40*time.Millisecond {
		t.Errorf("Run() took %s, want well under the 50ms wait (a first-lookup match needs no poll delay)", elapsed)
	}
	var sawWindows bool
	for _, c := range be.Calls {
		if c == "Windows app:TextEdit" {
			sawWindows = true
		}
	}
	if !sawWindows {
		t.Fatalf("backend calls = %v, want a Windows app:TextEdit lookup", be.Calls)
	}
}

// TestOpenWaitTimesOutToNoWindow: open[wait=] with no matching window ever
// appearing returns E_NOWINDOW after the wait duration elapses (help.txt
// ERROR CODES: "win or open[wait=] found no window").
func TestOpenWaitTimesOutToNoWindow(t *testing.T) {
	seq := parse(t, "open[wait=50ms]TextEdit")
	be := &dryrun.Backend{WindowsResult: nil}
	start := time.Now()
	sum := engine.Run(context.Background(), be, seq, engine.RunOptions{})
	elapsed := time.Since(start)

	if len(sum.Results) != 1 {
		t.Fatalf("Results = %+v, want one result", sum.Results)
	}
	r := sum.Results[0]
	if r.Status != "err" || r.ErrCode != output.ENoWindow {
		t.Fatalf("status=%q code=%q, want err/E_NOWINDOW", r.Status, r.ErrCode)
	}
	if elapsed < 40*time.Millisecond {
		t.Errorf("Run() took %s, want at least ~50ms (the wait= duration)", elapsed)
	}
}

// TestOpenLaunchFailureIsExec: a backend Open error (launch failure) is
// E_EXEC (help-macos.txt "A wrong name is E_EXEC").
func TestOpenLaunchFailureIsExec(t *testing.T) {
	seq := parse(t, "open[]NoSuchApp")
	be := &dryrun.Backend{FailOn: func(call string) error {
		if strings.HasPrefix(call, "Open ") {
			return context.DeadlineExceeded // any non-nil launch error
		}
		return nil
	}}
	sum := engine.Run(context.Background(), be, seq, engine.RunOptions{})
	if len(sum.Results) != 1 {
		t.Fatalf("Results = %+v, want one result", sum.Results)
	}
	r := sum.Results[0]
	if r.Status != "err" || r.ErrCode != output.EExec {
		t.Fatalf("status=%q code=%q, want err/E_EXEC", r.Status, r.ErrCode)
	}
}

// TestOpenAppSelectorFromTarget is the table test for openAppSelector,
// driven indirectly through doOpen's Windows lookup call (openAppSelector
// itself is unexported to package engine; open_test.go is package
// engine_test like window_test.go/engine_test.go): a bare name is used
// verbatim; a .app path is reduced to its basename with the suffix
// stripped; a generic file path is a best-effort basename guess (plan
// Escalations - known limitation for the non-.app case).
func TestOpenAppSelectorFromTarget(t *testing.T) {
	cases := []struct {
		line      string
		wantValue string
	}{
		{"open[wait=50ms]TextEdit", "TextEdit"},
		{"open[wait=50ms]/Applications/TextEdit.app", "TextEdit"},
		{"open[wait=50ms]/Users/me/notes.txt", "notes.txt"},
	}
	for _, tc := range cases {
		t.Run(tc.line, func(t *testing.T) {
			seq := parse(t, tc.line)
			be := &dryrun.Backend{WindowsResult: []backend.Window{{ID: 1, App: "TextEdit"}}}
			engine.Run(context.Background(), be, seq, engine.RunOptions{})
			want := fmt.Sprintf("Windows app:%s", tc.wantValue)
			var found bool
			for _, c := range be.Calls {
				if c == want {
					found = true
				}
			}
			if !found {
				t.Errorf("backend calls = %v, want a %q lookup", be.Calls, want)
			}
		})
	}
}
