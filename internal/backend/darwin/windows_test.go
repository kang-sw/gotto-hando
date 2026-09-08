//go:build darwin

package darwin

import (
	"errors"
	"testing"

	"github.com/kang-sw/gotto-hando/internal/backend"
	"github.com/kang-sw/gotto-hando/internal/ir"
)

// sampleWindows is a small z-ordered fixture spanning the four selector
// kinds (id/pid/app/title) with mixed case so the case-insensitive
// substring rules are exercised.
func sampleWindows() []backend.Window {
	return []backend.Window{
		{ID: 10, PID: 501, App: "Safari", Title: "GitHub - kang-sw"},
		{ID: 11, PID: 502, App: "Terminal", Title: "zsh - build"},
		{ID: 12, PID: 501, App: "Safari", Title: "Docs"},
	}
}

// TestSelectorMatching checks filterWindows across every WINDOW SELECTOR
// kind (help.txt WINDOW SELECTORS :286-297): id/pid exact, app/title
// case-insensitive substring, title RE2 regex, and the no-match case. Pure
// (no FFI), so it runs off the synthetic list.
func TestSelectorMatching(t *testing.T) {
	wins := sampleWindows()
	cases := []struct {
		name    string
		sel     ir.Selector
		wantIDs []int
	}{
		{"id exact", ir.Selector{Kind: "id", Value: "11"}, []int{11}},
		{"id no match", ir.Selector{Kind: "id", Value: "99"}, nil},
		{"pid exact matches all owned", ir.Selector{Kind: "pid", Value: "501"}, []int{10, 12}},
		{"app substring case-insensitive", ir.Selector{Kind: "app", Value: "safari"}, []int{10, 12}},
		{"title substring case-insensitive", ir.Selector{Kind: "title", Value: "docs"}, []int{12}},
		{"title regex anchored", ir.Selector{Kind: "title", Value: "^zsh", Regex: true}, []int{11}},
		{"title regex alternation", ir.Selector{Kind: "title", Value: "GitHub|Docs", Regex: true}, []int{10, 12}},
		{"empty title matches every window", ir.Selector{Kind: "title", Value: ""}, []int{10, 11, 12}},
		{"bad regex matches nothing", ir.Selector{Kind: "title", Value: "(", Regex: true}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := filterWindows(wins, c.sel)
			var ids []int
			for _, w := range got {
				ids = append(ids, w.ID)
			}
			if !equalInts(ids, c.wantIDs) {
				t.Fatalf("ids = %v, want %v", ids, c.wantIDs)
			}
		})
	}
}

// TestFilterPreservesZOrder confirms filterWindows keeps the input z-order
// (front-to-back), so the engine's frontmost-wins rule (help.txt :294) reads
// element 0.
func TestFilterPreservesZOrder(t *testing.T) {
	got := filterWindows(sampleWindows(), ir.Selector{Kind: "app", Value: "Safari"})
	if len(got) != 2 || got[0].ID != 10 || got[1].ID != 12 {
		t.Fatalf("got ids in order %v, want [10 12]", ids(got))
	}
}

// twoDisplays is a synthetic two-monitor layout: a 2560x1440 primary at the
// origin and a 1920x1080 secondary to its right, matching qdisp order so
// disp=N addresses index N.
func twoDisplays() []backend.DisplayGeom {
	return []backend.DisplayGeom{
		{X: 0, Y: 0, W: 2560, H: 1440, Scale: 2, Primary: true},
		{X: 2560, Y: 0, W: 1920, H: 1080, Scale: 1},
	}
}

// TestCaptureRectGeometry covers captureRect's pure frame + rect= math
// (help.txt cap :400-407, COORDINATES :262-264) with injected synthetic
// displays/window: desktop union, a whole display, a whole window, a
// frame-relative rect and a percentage rect.
func TestCaptureRectGeometry(t *testing.T) {
	displays := twoDisplays()
	win := &backend.Window{X: 100, Y: 200, W: 800, H: 600}

	cases := []struct {
		name   string
		req    backend.CaptureReq
		window *backend.Window
		want   captureRegion
	}{
		{
			"desktop is the display union",
			backend.CaptureReq{Frame: "desktop"}, nil,
			captureRegion{originX: 0, originY: 0, w: 4480, h: 1440},
		},
		{
			"whole secondary display",
			backend.CaptureReq{Frame: "display", Display: 1}, nil,
			captureRegion{originX: 2560, originY: 0, w: 1920, h: 1080},
		},
		{
			"whole current window",
			backend.CaptureReq{Frame: "window"}, win,
			captureRegion{originX: 100, originY: 200, w: 800, h: 600},
		},
		{
			"rect within display, frame-relative origin",
			backend.CaptureReq{Frame: "display", Display: 1, Rect: &ir.Rect{X: 10, Y: 20, W: 300, H: 200}}, nil,
			captureRegion{originX: 2570, originY: 20, w: 300, h: 200},
		},
		{
			"percentage rect within window",
			backend.CaptureReq{Frame: "window", Rect: &ir.Rect{X: 50, Y: 50, W: 100, H: 100, XPct: true, YPct: true}}, win,
			captureRegion{originX: 500, originY: 500, w: 100, h: 100},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := captureRect(c.req, displays, c.window)
			if err != nil {
				t.Fatalf("captureRect: %v", err)
			}
			if got != c.want {
				t.Fatalf("region = %+v, want %+v", got, c.want)
			}
		})
	}
}

// TestCaptureRectBounds covers the failure cases: an out-of-range disp=N and
// a rect that spills past its frame both wrap backend.ErrBounds (-> E_BOUNDS,
// help.txt COORDINATES :262-264); a w-frame with no current window is
// errNoCaptureWindow (-> E_NOWINDOW upstream).
func TestCaptureRectBounds(t *testing.T) {
	displays := twoDisplays()
	win := &backend.Window{X: 100, Y: 200, W: 800, H: 600}

	cases := []struct {
		name    string
		req     backend.CaptureReq
		window  *backend.Window
		wantErr error
	}{
		{"disp index too high", backend.CaptureReq{Frame: "display", Display: 2}, nil, backend.ErrBounds},
		{"disp index negative", backend.CaptureReq{Frame: "display", Display: -1}, nil, backend.ErrBounds},
		{"rect wider than window", backend.CaptureReq{Frame: "window", Rect: &ir.Rect{X: 0, Y: 0, W: 1000, H: 100}}, win, backend.ErrBounds},
		{"rect origin below frame", backend.CaptureReq{Frame: "display", Display: 0, Rect: &ir.Rect{X: -10, Y: 0, W: 100, H: 100}}, nil, backend.ErrBounds},
		{"rect zero size", backend.CaptureReq{Frame: "display", Display: 0, Rect: &ir.Rect{X: 0, Y: 0, W: 0, H: 100}}, nil, backend.ErrBounds},
		{"window frame with no current window", backend.CaptureReq{Frame: "window"}, nil, errNoCaptureWindow},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := captureRect(c.req, displays, c.window)
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("err = %v, want it to wrap %v", err, c.wantErr)
			}
		})
	}
}

// TestBGRAtoRGBA feeds bgraToRGBA a synthetic BGRA buffer with a stride
// WIDER than w*4 (row padding) and a premultiplied/transparent pixel, then
// asserts the exact tightly-packed RGBA output. This catches the two bug
// classes pngcodec_test.go cannot (it only round-trips already-RGBA bytes):
// an R/B channel swap, and a stride miscalculation that would leak the row
// padding into the packed output. Alpha is forced opaque regardless of the
// source alpha (v1 behavior).
func TestBGRAtoRGBA(t *testing.T) {
	const w, h, stride = 2, 2, 12 // w*4 == 8, so 4 bytes of row padding
	src := make([]byte, stride*h)
	// Poison the padding and the whole buffer so a stride bug shows up as a
	// wrong byte rather than an incidental zero.
	for i := range src {
		src[i] = 0xEE
	}
	// row 0: pixel(0,0) opaque, pixel(1,0) premultiplied-transparent (A=128).
	copy(src[0:4], []byte{10, 20, 30, 255}) // B,G,R,A -> R=30,G=20,B=10
	copy(src[4:8], []byte{1, 2, 3, 128})    // -> R=3,G=2,B=1, alpha forced 255
	// bytes src[8:12] stay 0xEE padding and must be ignored.
	// row 1: pixel(0,1), pixel(1,1) fully transparent (A=0).
	copy(src[12:16], []byte{40, 50, 60, 255}) // -> R=60,G=50,B=40
	copy(src[16:20], []byte{7, 8, 9, 0})      // -> R=9,G=8,B=7, alpha forced 255
	// bytes src[20:24] stay 0xEE padding.

	got := bgraToRGBA(src, w, h, stride)
	want := []byte{
		30, 20, 10, 255, 3, 2, 1, 255, // row 0
		60, 50, 40, 255, 9, 8, 7, 255, // row 1
	}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d (should be tightly packed w*h*4, no padding)", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("byte %d = %d, want %d\n got=%v\nwant=%v", i, got[i], want[i], got, want)
		}
	}
}

// TestCaptureNoWindowWrapsErrNoWindow proves cap[w] with no current window
// wraps backend.ErrNoWindow (so engine/capture.go maps it to E_NOWINDOW,
// consistent with c[w]/m[w]/drag[w]), not a bare error that would fall
// through to E_CAPTURE.
func TestCaptureNoWindowWrapsErrNoWindow(t *testing.T) {
	_, err := captureRect(backend.CaptureReq{Frame: "window"}, twoDisplays(), nil)
	if !errors.Is(err, backend.ErrNoWindow) {
		t.Fatalf("err = %v, want it to wrap backend.ErrNoWindow", err)
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func ids(wins []backend.Window) []int {
	var out []int
	for _, w := range wins {
		out = append(out, w.ID)
	}
	return out
}
