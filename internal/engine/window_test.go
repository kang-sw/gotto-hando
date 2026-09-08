package engine_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/kang-sw/gotto-hando/internal/backend"
	"github.com/kang-sw/gotto-hando/internal/backend/dryrun"
	"github.com/kang-sw/gotto-hando/internal/engine"
	"github.com/kang-sw/gotto-hando/internal/output"
)

// synthWindows is a two-window fixture: a focused Safari window and a
// minimized+hidden Terminal window, so qwin's flags field ("* focused, min,
// hidden", help.txt OUTPUT :578-579) is exercised in every state.
func synthWindows() []backend.Window {
	return []backend.Window{
		{ID: 42, PID: 501, App: "Safari", Title: "Example", X: 100, Y: 200, W: 1280, H: 800, Focused: true},
		{ID: 43, PID: 777, App: "Terminal", Title: "zsh", X: 0, Y: 0, W: 640, H: 480, Minimized: true, Hidden: true},
	}
}

// TestQueryWindowsFormat locks qwin's plain Extra line format (help.txt
// OUTPUT :576-578: "<id>\t<pid>\t<app>\t<x>,<y> <w>x<h>\t<flags>\t<title>")
// and its --jsonl "windows" array, driven end-to-end through engine.Run with
// a dryrun backend (the same pattern query_test.go uses for qinfo/qdisp).
func TestQueryWindowsFormat(t *testing.T) {
	seq := parse(t, "qwin")
	be := &dryrun.Backend{WindowsResult: synthWindows()}
	sum := engine.Run(context.Background(), be, seq, engine.RunOptions{})

	if len(sum.Results) != 1 || sum.Results[0].Status != "ok" {
		t.Fatalf("Results = %+v, want a single ok result", sum.Results)
	}
	r := sum.Results[0]

	wantExtra := []string{
		"  42\t501\tSafari\t100,200 1280x800\t*\tExample",
		"  43\t777\tTerminal\t0,0 640x480\tmin hidden\tzsh",
	}
	if !reflect.DeepEqual(r.Extra, wantExtra) {
		t.Fatalf("Extra = %#v\nwant %#v", r.Extra, wantExtra)
	}
	if !r.AlwaysShow {
		t.Error("AlwaysShow = false, want true (a query is never hidden under -q)")
	}

	wantJSON := []output.KV{{Key: "windows", Val: []map[string]any{
		{"id": 42, "pid": 501, "app": "Safari", "title": "Example",
			"x": 100, "y": 200, "w": 1280, "h": 800,
			"focused": true, "minimized": false, "hidden": false},
		{"id": 43, "pid": 777, "app": "Terminal", "title": "zsh",
			"x": 0, "y": 0, "w": 640, "h": 480,
			"focused": false, "minimized": true, "hidden": true},
	}}}
	if !reflect.DeepEqual(r.JSON, wantJSON) {
		t.Fatalf("JSON = %#v\nwant %#v", r.JSON, wantJSON)
	}
}

// TestQueryWindowsEmpty: qwin tolerates zero matches (help.txt :296-297) -
// an ok result with no Extra lines, not an error.
func TestQueryWindowsEmpty(t *testing.T) {
	seq := parse(t, "qwin[]app:nope")
	be := &dryrun.Backend{WindowsResult: nil}
	sum := engine.Run(context.Background(), be, seq, engine.RunOptions{})
	if len(sum.Results) != 1 || sum.Results[0].Status != "ok" {
		t.Fatalf("Results = %+v, want a single ok result", sum.Results)
	}
	if len(sum.Results[0].Extra) != 0 {
		t.Fatalf("Extra = %+v, want none", sum.Results[0].Extra)
	}
}

// TestFocusWindowDetail locks win's plain Detail (help.txt OUTPUT :600:
// "id=<id> app=<app> matched=<n> <x>,<y> <w>x<h> <title>") and confirms the
// backend was actually asked to Focus the frontmost match.
func TestFocusWindowDetail(t *testing.T) {
	seq := parse(t, "win[]app:Safari")
	be := &dryrun.Backend{WindowsResult: synthWindows()[:1]}
	sum := engine.Run(context.Background(), be, seq, engine.RunOptions{})

	if len(sum.Results) != 1 || sum.Results[0].Status != "ok" {
		t.Fatalf("Results = %+v, want a single ok result", sum.Results)
	}
	want := `id=42 app=Safari matched=1 100,200 1280x800 "Example"`
	if sum.Results[0].Detail != want {
		t.Fatalf("Detail = %q, want %q", sum.Results[0].Detail, want)
	}
	var focused bool
	for _, c := range be.Calls {
		if c == "Focus 42" {
			focused = true
		}
	}
	if !focused {
		t.Fatalf("backend calls = %v, want a Focus 42", be.Calls)
	}
}

// TestFocusNoMatchIsNoWindow: win with no matching window is an err result
// carrying E_NOWINDOW (help.txt WINDOW SELECTORS :295: no match, no wait ->
// E_NOWINDOW), and Focus is never attempted.
func TestFocusNoMatchIsNoWindow(t *testing.T) {
	seq := parse(t, "win[]app:Ghost")
	be := &dryrun.Backend{WindowsResult: nil}
	sum := engine.Run(context.Background(), be, seq, engine.RunOptions{})

	if len(sum.Results) != 1 {
		t.Fatalf("Results = %+v, want one result", sum.Results)
	}
	r := sum.Results[0]
	if r.Status != "err" || r.ErrCode != output.ENoWindow {
		t.Fatalf("result = status=%q code=%q, want err/E_NOWINDOW", r.Status, r.ErrCode)
	}
	for _, c := range be.Calls {
		if c == "Focus 0" {
			t.Fatalf("Focus should not run on no match; calls=%v", be.Calls)
		}
	}
}
