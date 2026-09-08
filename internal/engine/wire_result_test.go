package engine_test

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/kang-sw/gotto-hando/internal/backend"
	"github.com/kang-sw/gotto-hando/internal/backend/dryrun"
	"github.com/kang-sw/gotto-hando/internal/engine"
	"github.com/kang-sw/gotto-hando/internal/output"
)

// jsonFields runs a real engine.Run over a dryrun.Backend and captures the
// Result's own r.JSON pairs re-encoded as map[string]json.RawMessage plus
// t_ms - the exact shape the bridge forwarder decodes a JSONL line into
// (cmd/gotto-hando/dispatch_windows.go's forwardToBridge) - so this test
// exercises ResultDetailFromJSON against the real wire shape, not a
// hand-typed fixture that could drift from the encoder.
func jsonFields(t *testing.T, r output.Result) map[string]json.RawMessage {
	t.Helper()
	m := map[string]json.RawMessage{}
	for _, kv := range r.JSON {
		b, err := json.Marshal(kv.Val)
		if err != nil {
			t.Fatalf("marshal %s: %v", kv.Key, err)
		}
		m[kv.Key] = b
	}
	b, err := json.Marshal(r.TMS)
	if err != nil {
		t.Fatalf("marshal t_ms: %v", err)
	}
	m["t_ms"] = b
	return m
}

func TestResultDetailFromJSONQinfo(t *testing.T) {
	seq := parse(t, "qinfo")
	be := &dryrun.Backend{InfoResult: synthQueryInfo()}
	sum := engine.Run(context.Background(), be, seq, engine.RunOptions{})
	r := sum.Results[0]

	detail, extra, alwaysShow, ok := engine.ResultDetailFromJSON("qinfo", jsonFields(t, r))
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if detail != r.Detail {
		t.Errorf("detail = %q, want %q", detail, r.Detail)
	}
	if extra != nil {
		t.Errorf("extra = %v, want nil", extra)
	}
	if !alwaysShow {
		t.Error("alwaysShow = false, want true")
	}
}

func TestResultDetailFromJSONQclip(t *testing.T) {
	seq := parse(t, "qclip")
	be := &dryrun.Backend{Clipboard: "hello-bridge"}
	sum := engine.Run(context.Background(), be, seq, engine.RunOptions{})
	r := sum.Results[0]

	detail, _, alwaysShow, ok := engine.ResultDetailFromJSON("qclip", jsonFields(t, r))
	if !ok || detail != "hello-bridge" || !alwaysShow {
		t.Fatalf("got (%q, alwaysShow=%v, ok=%v), want (hello-bridge, true, true)", detail, alwaysShow, ok)
	}
}

func TestResultDetailFromJSONWin(t *testing.T) {
	seq := parse(t, "win[]Blender")
	be := &dryrun.Backend{WindowsResult: []backend.Window{
		{ID: 2314, PID: 4120, App: "Blender", Title: "Untitled - Blender", X: 0, Y: 25, W: 1440, H: 875},
	}}
	sum := engine.Run(context.Background(), be, seq, engine.RunOptions{})
	r := sum.Results[0]

	detail, _, alwaysShow, ok := engine.ResultDetailFromJSON("win", jsonFields(t, r))
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if detail != r.Detail {
		t.Errorf("detail = %q, want %q", detail, r.Detail)
	}
	if alwaysShow {
		t.Error("alwaysShow = true, want false (win is not a query result)")
	}
}

func TestResultDetailFromJSONQwin(t *testing.T) {
	seq := parse(t, "qwin")
	be := &dryrun.Backend{WindowsResult: []backend.Window{
		{ID: 2314, PID: 4120, App: "Blender", Title: "Untitled - Blender", X: 0, Y: 25, W: 1440, H: 875, Focused: true},
		{ID: 2290, PID: 612, App: "Terminal", Title: "zsh - 120x40", X: 1440, Y: 25, W: 1120, H: 875},
	}}
	sum := engine.Run(context.Background(), be, seq, engine.RunOptions{})
	r := sum.Results[0]

	_, extra, alwaysShow, ok := engine.ResultDetailFromJSON("qwin", jsonFields(t, r))
	if !ok || !alwaysShow {
		t.Fatalf("ok=%v alwaysShow=%v, want true/true", ok, alwaysShow)
	}
	if !reflect.DeepEqual(extra, r.Extra) {
		t.Errorf("extra = %+v, want %+v", extra, r.Extra)
	}
}

func TestResultDetailFromJSONQdisp(t *testing.T) {
	seq := parse(t, "qdisp")
	be := &dryrun.Backend{InfoResult: synthQueryInfo()}
	sum := engine.Run(context.Background(), be, seq, engine.RunOptions{})
	r := sum.Results[0]

	_, extra, alwaysShow, ok := engine.ResultDetailFromJSON("qdisp", jsonFields(t, r))
	if !ok || !alwaysShow {
		t.Fatalf("ok=%v alwaysShow=%v, want true/true", ok, alwaysShow)
	}
	if !reflect.DeepEqual(extra, r.Extra) {
		t.Errorf("extra = %+v, want %+v", extra, r.Extra)
	}
}

func TestResultDetailFromJSONQmouse(t *testing.T) {
	seq := parse(t, "qmouse")
	be := &dryrun.Backend{MousePosResult: backend.Point{X: 123, Y: 456}}
	sum := engine.Run(context.Background(), be, seq, engine.RunOptions{})
	r := sum.Results[0]

	_, extra, alwaysShow, ok := engine.ResultDetailFromJSON("qmouse", jsonFields(t, r))
	if !ok || !alwaysShow {
		t.Fatalf("ok=%v alwaysShow=%v, want true/true", ok, alwaysShow)
	}
	if !reflect.DeepEqual(extra, r.Extra) {
		t.Errorf("extra = %+v, want %+v", extra, r.Extra)
	}
}

func TestResultDetailFromJSONExec(t *testing.T) {
	seq := parse(t, "exec[]git status")
	be := &dryrun.Backend{ExecResult: backend.ExecResult{Exit: 0, Stdout: "clean\n", Stderr: ""}}
	sum := engine.Run(context.Background(), be, seq, engine.RunOptions{})
	r := sum.Results[0]

	detail, extra, alwaysShow, ok := engine.ResultDetailFromJSON("exec", jsonFields(t, r))
	if !ok || !alwaysShow {
		t.Fatalf("ok=%v alwaysShow=%v, want true/true", ok, alwaysShow)
	}
	if detail != r.Detail {
		t.Errorf("detail = %q, want %q", detail, r.Detail)
	}
	if !reflect.DeepEqual(extra, r.Extra) {
		t.Errorf("extra = %+v, want %+v", extra, r.Extra)
	}
}

func TestResultDetailFromJSONUnknownCmdFallsBack(t *testing.T) {
	_, _, _, ok := engine.ResultDetailFromJSON("cap", map[string]json.RawMessage{})
	if ok {
		t.Fatal("ok = true, want false (cap is intentionally excluded, Out of Scope)")
	}
	_, _, _, ok = engine.ResultDetailFromJSON("k", map[string]json.RawMessage{})
	if ok {
		t.Fatal("ok = true, want false (k has no Detail in plain mode)")
	}
}
