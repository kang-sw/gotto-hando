package ir_test

import (
	"bytes"
	"testing"

	"github.com/kang-sw/gotto-hando/internal/ir"
)

// allKindsSeq builds one Op per ir.Kind with representative, non-zero field
// values, so the round-trip test (Marshal(Unmarshal(Marshal(seq))) ==
// Marshal(seq)) exercises every field opNode/decodeOp know about.
func allKindsSeq() *ir.Sequence {
	delay := 250
	txtms := 10
	keyms := 40
	rect := ir.Rect{X: 10, Y: 20, W: 300, H: 400, XPct: true, YPct: true}
	return &ir.Sequence{
		V:        1,
		Defaults: ir.Defaults{DelayMS: 100, TextIntervalMS: 0, KeyGapMS: 30},
		Ops: []ir.Op{
			{Line: 1, Src: "win[wait=500ms]Blender", Kind: ir.KindFocus,
				Selector: ir.Selector{Kind: "title", Value: "Blender", Regex: true}, WaitMS: 500, HasWait: true},
			{Line: 2, Src: "qwin[]Blender", Kind: ir.KindQueryWindows,
				Selector: ir.Selector{Kind: "app", Value: "Blender"}},
			{Line: 3, Src: "k[c]v", Kind: ir.KindKey,
				Mods: []string{"ctrl"}, Keys: [][]string{{"v"}}, Repeat: 2, GapMS: 30},
			{Line: 4, Src: "kd[]a", Kind: ir.KindKeyDown, Keys: [][]string{{"a"}}},
			{Line: 5, Src: "ku[]a", Kind: ir.KindKeyUp, Keys: [][]string{{"a"}}},
			{Line: 6, Src: "txt[ms=66]hi", Kind: ir.KindText, Text: "hi", IntervalMS: 66},
			{Line: 7, Src: "m[]10,20", Kind: ir.KindMove,
				Point: ir.Point{Frame: "desktop", X: 10, Y: 20}, DurationMS: 100},
			{Line: 8, Src: "c[n=2,w]50%,50%", Kind: ir.KindClick,
				Button: "left", Count: 2, GapMS: 60, Mods: []string{"shift"},
				HasPoint: true, Point: ir.Point{Frame: "window", X: 50, Y: 50, XPct: true, YPct: true}},
			{Line: 9, Src: "md[]", Kind: ir.KindButtonDown,
				Button: "right", Mods: []string{}, HasPoint: true,
				Point: ir.Point{Frame: "display", Disp: 1, X: 5, Y: 5}},
			{Line: 10, Src: "mu[]", Kind: ir.KindButtonUp, Button: "right"},
			{Line: 11, Src: "drag[]1,1;2,2", Kind: ir.KindDrag,
				Button: "left", DurationMS: 200, Count: 5, Mods: []string{},
				Points: []ir.Point{{Frame: "desktop", X: 1, Y: 1}, {Frame: "desktop", X: 2, Y: 2}}},
			{Line: 12, Src: "scroll[]down", Kind: ir.KindScroll,
				ScrollDir: "down", Ticks: 3, ScrollBy: "line"},
			{Line: 13, Src: "clip[]hello", Kind: ir.KindClipboard, Text: "hello"},
			{Line: 14, Src: "paste[]hello", Kind: ir.KindPaste, Text: "hello", IntervalMS: 20},
			{Line: 15, Src: "qclip[f]", Kind: ir.KindQueryClip, FromFile: true},
			{Line: 16, Src: "rclip[img]/tmp/shot.bmp", Kind: ir.KindRClip, Path: "/tmp/shot.bmp", RClipType: "image"},
			{Line: 16, Src: "open[wait=1s]Blender", Kind: ir.KindOpen, Target: "Blender", WaitMS: 1000, HasWait: true},
			{Line: 17, Src: "exec[]git status", Kind: ir.KindExec,
				Argv: []string{"git", "status"}, Shell: false, Noerr: false, TimeoutMS: 10000},
			{Line: 18, Src: "exec[shell]dir /b", Kind: ir.KindExec,
				Shell: true, Cmd: "dir /b", Noerr: true, TimeoutMS: 5000},
			{Line: 19, Src: "cap[]", Kind: ir.KindCapture,
				Frame: "display", Disp: 1, Rect: &rect, Scale: 1.5, Format: "png",
				CapCount: 2, CapInterval: 100, Label: "after"},
			{Line: 20, Src: "cap[scale=native]", Kind: ir.KindCapture,
				Frame: "desktop", ScaleNative: true, Format: "png", CapCount: 1, CapInterval: 0, Label: ""},
			{Line: 21, Src: "sleep[]1s", Kind: ir.KindSleep, SleepMS: 1000},
			{Line: 22, Src: "set[]delay=50ms", Kind: ir.KindSet,
				SetDelay: &delay, SetTxtms: &txtms, SetKeyms: &keyms},
			{Line: 23, Src: "qinfo", Kind: ir.KindQueryInfo},
			{Line: 24, Src: "qdisp", Kind: ir.KindQueryDisp},
			{Line: 25, Src: "qmouse", Kind: ir.KindQueryMouse, DelayMS: intPtr(75)},
		},
	}
}

func intPtr(i int) *int { return &i }

func TestUnmarshalRoundTripsEveryKind(t *testing.T) {
	seq := allKindsSeq()
	want, err := ir.Marshal(seq)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	got1, err := ir.Unmarshal(want)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	remarshaled, err := ir.Marshal(got1)
	if err != nil {
		t.Fatalf("Marshal(Unmarshal(...)): %v", err)
	}
	if !bytes.Equal(want, remarshaled) {
		t.Fatalf("round-trip mismatch\n--- want ---\n%s\n--- got ---\n%s", want, remarshaled)
	}
}

func TestUnmarshalMissingVersionErrors(t *testing.T) {
	_, err := ir.Unmarshal([]byte(`{"ops":[],"defaults":{}}`))
	if err == nil {
		t.Fatal("Unmarshal() = nil error, want an error for missing \"v\"")
	}
}

func TestUnmarshalMalformedJSONErrors(t *testing.T) {
	_, err := ir.Unmarshal([]byte(`not json`))
	if err == nil {
		t.Fatal("Unmarshal() = nil error, want an error for malformed JSON")
	}
}

func TestUnmarshalUnknownOpErrors(t *testing.T) {
	doc := `{"v":1,"ops":[{"line":1,"src":"x","op":"not_a_real_op"}],"defaults":{}}`
	_, err := ir.Unmarshal([]byte(doc))
	if err == nil {
		t.Fatal("Unmarshal() = nil error, want an error for an unknown op kind")
	}
}

func TestUnmarshalIgnoresUnknownTopLevelKeys(t *testing.T) {
	doc := `{"v":1,"ops":[],"defaults":{"delay_ms":100,"text_interval_ms":0,"key_gap_ms":30},
	"run":{"deadline_ms":5000,"keep_going":true,"quiet":false,"cap_on_error":false}}`
	seq, err := ir.Unmarshal([]byte(doc))
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if seq.V != 1 || len(seq.Ops) != 0 {
		t.Fatalf("Unmarshal() = %+v, want V=1 and no ops", seq)
	}
}
