//go:build windows

package windows

import (
	"errors"
	"testing"
	"unsafe"

	"github.com/kang-sw/gotto-hando/internal/backend"
	"github.com/kang-sw/gotto-hando/internal/ir"
)

// twoDisplays is a synthetic two-monitor layout: a 2560x1440 primary at the
// origin and a 1920x1080 secondary to its right, matching qdisp order so
// disp=N addresses index N. Identical fixture to darwin's capture_test.go.
func twoDisplays() []backend.DisplayGeom {
	return []backend.DisplayGeom{
		{X: 0, Y: 0, W: 2560, H: 1440, Scale: 1, Primary: true},
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

// TestBGRAtoRGBA feeds bgraToRGBA a synthetic BGRA buffer with a stride
// WIDER than w*4 (row padding) and a transparent pixel, then asserts the
// exact tightly-packed RGBA output. This catches an R/B channel swap and a
// stride miscalculation that would leak the row padding into the packed
// output. Alpha is forced opaque regardless of the source alpha (v1
// behavior) - identical assertions to darwin's TestBGRAtoRGBA, since
// readDIBits's 32bpp BI_RGB DIB has the same B,G,R,X byte order.
func TestBGRAtoRGBA(t *testing.T) {
	const w, h, stride = 2, 2, 12 // w*4 == 8, so 4 bytes of row padding
	src := make([]byte, stride*h)
	for i := range src {
		src[i] = 0xEE
	}
	copy(src[0:4], []byte{10, 20, 30, 255}) // row 0, px 0: B,G,R,A -> R=30,G=20,B=10
	copy(src[4:8], []byte{1, 2, 3, 0})      // row 0, px 1: A=0 (transparent) -> alpha still forced 255
	// src[8:12] stays 0xEE padding and must be ignored.
	copy(src[12:16], []byte{40, 50, 60, 255}) // row 1, px 0
	copy(src[16:20], []byte{7, 8, 9, 128})    // row 1, px 1
	// src[20:24] stays 0xEE padding.

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

// TestBitmapInfoHeaderSize pins bitmapInfoHeader's size to the real Win32
// BITMAPINFOHEADER's 40 bytes (biSize+biWidth+biHeight = 12, biPlanes+
// biBitCount = 4, biCompression+biSizeImage+biXPelsPerMeter+biYPelsPerMeter+
// biClrUsed+biClrImportant = 20, total 36... plus the leading biSize's own
// 4 already counted = 40). GetDIBits reads exactly this many bytes as the
// header; a field reordering that introduces Go padding would silently
// corrupt every capture on Windows - same spirit as Phase 1's INPUT
// struct-size assertion in ffi_test.go/mouse_test.go.
func TestBitmapInfoHeaderSize(t *testing.T) {
	const want = 40
	if got := unsafe.Sizeof(bitmapInfoHeader{}); got != want {
		t.Fatalf("sizeof(bitmapInfoHeader) = %d, want %d (the real BITMAPINFOHEADER size GetDIBits expects)", got, want)
	}
}
