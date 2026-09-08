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

// Test review finding: internal/engine/query.go's six formatting functions
// (formatQueryInfo/queryInfoJSON/formatQueryDisp/queryDispJSON/
// formatQueryMouse/queryMouseJSON) had zero coverage before this file -
// they build qinfo/qdisp/qmouse's plain Detail/Extra lines and --jsonl
// fields (help.txt:431-447, :576-586, help-macos.txt CHECK) and are
// lock-independent (plain backend.Info/backend.Point structs, no real OS
// interaction), so they are exercised here end-to-end through engine.Run
// with a dryrun.Backend fixture - the same pattern engine_test.go's
// TestResolveBoundsFailureIsLineErrorNotAbort already uses for
// InfoResult/MousePosResult, just asserting Detail/Extra/JSON content
// instead of only feeding the bounds check.

// synthQueryInfo is a fixed backend.Info fixture: two displays (one
// primary, one not, different scale) so qdisp's per-line formatting
// (index, scale, the trailing "primary" flag) is exercised both ways.
func synthQueryInfo() backend.Info {
	return backend.Info{
		OS: "darwin", OSVer: "14.5", Arch: "arm64", Ver: "0.1.0",
		Primary:  "cmd",
		DesktopX: 0, DesktopY: 0, DesktopW: 2560, DesktopH: 1440,
		Displays: 2,
		DisplayList: []backend.DisplayGeom{
			{X: 0, Y: 0, W: 2560, H: 1440, Scale: 2, Primary: true},
			{X: 2560, Y: 0, W: 1920, H: 1080, Scale: 1, Primary: false},
		},
		Session: "active",
		Perms:   "accessibility:ok,screen:ok",
	}
}

// TestQueryInfoFormat locks in qinfo's plain Detail line (help.txt:431-434,
// help-macos.txt CHECK example :12-14) and --jsonl field shape/order.
func TestQueryInfoFormat(t *testing.T) {
	seq := parse(t, "qinfo")
	be := &dryrun.Backend{InfoResult: synthQueryInfo()}
	sum := engine.Run(context.Background(), be, seq, engine.RunOptions{})

	if len(sum.Results) != 1 || sum.Results[0].Status != "ok" {
		t.Fatalf("Results = %+v, want a single ok result", sum.Results)
	}
	r := sum.Results[0]

	wantDetail := "os=darwin osver=14.5 arch=arm64 ver=0.1.0 primary=cmd desktop=0,0 2560x1440 displays=2 session=active perms=accessibility:ok,screen:ok"
	if r.Detail != wantDetail {
		t.Errorf("Detail = %q, want %q", r.Detail, wantDetail)
	}
	if !r.AlwaysShow {
		t.Error("AlwaysShow = false, want true (qinfo is never hidden under -q, help.txt:65-66)")
	}

	wantJSON := []output.KV{
		{Key: "os", Val: "darwin"}, {Key: "osver", Val: "14.5"},
		{Key: "arch", Val: "arm64"}, {Key: "ver", Val: "0.1.0"},
		{Key: "primary", Val: "cmd"},
		{Key: "desktop_x", Val: 0}, {Key: "desktop_y", Val: 0},
		{Key: "desktop_w", Val: 2560}, {Key: "desktop_h", Val: 1440},
		{Key: "displays", Val: 2},
		{Key: "session", Val: "active"}, {Key: "perms", Val: "accessibility:ok,screen:ok"},
	}
	if !reflect.DeepEqual(r.JSON, wantJSON) {
		t.Errorf("JSON = %+v, want %+v", r.JSON, wantJSON)
	}
}

// TestQueryDispFormat locks in qdisp's one-Extra-line-per-display plain
// format (help.txt:435-436, :582) and its --jsonl "displays" array shape.
func TestQueryDispFormat(t *testing.T) {
	seq := parse(t, "qdisp")
	be := &dryrun.Backend{InfoResult: synthQueryInfo()}
	sum := engine.Run(context.Background(), be, seq, engine.RunOptions{})

	if len(sum.Results) != 1 || sum.Results[0].Status != "ok" {
		t.Fatalf("Results = %+v, want a single ok result", sum.Results)
	}
	r := sum.Results[0]

	wantExtra := []string{
		"  0\t0,0 2560x1440\tscale=2\tprimary",
		"  1\t2560,0 1920x1080\tscale=1\t",
	}
	if !reflect.DeepEqual(r.Extra, wantExtra) {
		t.Errorf("Extra = %+v, want %+v", r.Extra, wantExtra)
	}
	if !r.AlwaysShow {
		t.Error("AlwaysShow = false, want true")
	}

	wantJSON := []output.KV{
		{Key: "displays", Val: []map[string]any{
			{"idx": 0, "x": 0, "y": 0, "w": 2560, "h": 1440, "scale": float64(2), "primary": true},
			{"idx": 1, "x": 2560, "y": 0, "w": 1920, "h": 1080, "scale": float64(1), "primary": false},
		}},
	}
	if !reflect.DeepEqual(r.JSON, wantJSON) {
		t.Errorf("JSON = %+v, want %+v", r.JSON, wantJSON)
	}
}

// TestQueryMouseFormat locks in qmouse's single "  x,y" Extra line
// (help.txt:445-446, :583) and its --jsonl x/y fields.
func TestQueryMouseFormat(t *testing.T) {
	seq := parse(t, "qmouse")
	be := &dryrun.Backend{MousePosResult: backend.Point{X: 123, Y: 456}}
	sum := engine.Run(context.Background(), be, seq, engine.RunOptions{})

	if len(sum.Results) != 1 || sum.Results[0].Status != "ok" {
		t.Fatalf("Results = %+v, want a single ok result", sum.Results)
	}
	r := sum.Results[0]

	wantExtra := []string{"  123,456"}
	if !reflect.DeepEqual(r.Extra, wantExtra) {
		t.Errorf("Extra = %+v, want %+v", r.Extra, wantExtra)
	}
	if !r.AlwaysShow {
		t.Error("AlwaysShow = false, want true")
	}

	wantJSON := []output.KV{{Key: "x", Val: float64(123)}, {Key: "y", Val: float64(456)}}
	if !reflect.DeepEqual(r.JSON, wantJSON) {
		t.Errorf("JSON = %+v, want %+v", r.JSON, wantJSON)
	}
}
