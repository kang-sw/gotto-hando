package engine

import (
	"bytes"
	"image/png"
	"testing"

	"github.com/kang-sw/gotto-hando/internal/backend"
)

// TestEncodePNGRoundTrip encodes a synthetic backend.Image's raw RGBA
// pixels to PNG and decodes them back with image/png, asserting the
// dimensions survive and a known pixel is preserved (ticket Decision: the
// backend hands over raw tightly-packed RGBA, the GOOS-agnostic engine does
// the PNG encoding). A pure codec check - no OS capture involved.
func TestEncodePNGRoundTrip(t *testing.T) {
	const w, h = 3, 2
	// Distinct opaque colours per pixel so a channel swap or stride bug
	// would show up in the decoded values.
	pix := make([]byte, w*h*4)
	for i := 0; i < w*h; i++ {
		pix[i*4+0] = byte(10 + i)  // R
		pix[i*4+1] = byte(50 + i)  // G
		pix[i*4+2] = byte(100 + i) // B
		pix[i*4+3] = 0xff          // A
	}
	img := backend.Image{W: w, H: h, OriginX: 5, OriginY: 6, Scale: 1, Pixels: pix}

	data, err := encodePNG(img)
	if err != nil {
		t.Fatalf("encodePNG: %v", err)
	}
	decoded, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("png.Decode: %v", err)
	}
	b := decoded.Bounds()
	if b.Dx() != w || b.Dy() != h {
		t.Fatalf("decoded size = %dx%d, want %dx%d", b.Dx(), b.Dy(), w, h)
	}
	// Pixel (2,1) is index 5: R=15, G=55, B=105.
	r, g, bl, a := decoded.At(2, 1).RGBA()
	if r>>8 != 15 || g>>8 != 55 || bl>>8 != 105 || a>>8 != 0xff {
		t.Fatalf("pixel(2,1) = %d,%d,%d,%d, want 15,55,105,255", r>>8, g>>8, bl>>8, a>>8)
	}
}

// TestEncodePNGRejectsMalformed: a too-short pixel buffer (or non-positive
// dimensions) is a clear error rather than a panic, so a backend bug
// surfaces as E_CAPTURE at the call site instead of crashing the run.
func TestEncodePNGRejectsMalformed(t *testing.T) {
	cases := []struct {
		name string
		img  backend.Image
	}{
		{"short buffer", backend.Image{W: 4, H: 4, Pixels: make([]byte, 10)}},
		{"zero width", backend.Image{W: 0, H: 4, Pixels: make([]byte, 64)}},
		{"zero height", backend.Image{W: 4, H: 0, Pixels: make([]byte, 64)}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := encodePNG(c.img); err == nil {
				t.Fatalf("encodePNG(%+v) = nil error, want a malformed-image error", c.img)
			}
		})
	}
}

// TestCapturePathAutomaticAndExplicit locks the two path shapes (help.txt
// OUTPUT "Capture paths" :601-606, cap path :413-416): the automatic
// <out>/NNNN-label-timestamp.png name, the -NN burst suffix, and an
// explicit payload path that overrides <out>/name (splicing -NN before the
// extension for a burst).
func TestCapturePathAutomaticAndExplicit(t *testing.T) {
	const ts = "20260907T131502.114Z"
	cases := []struct {
		name                 string
		out, explicit, label string
		seq, index, n        int
		want                 string
	}{
		{"automatic single", "/tmp/gh", "", "cap", 0, 0, 1, "/tmp/gh/0000-cap-" + ts + ".png"},
		{"automatic burst", "/tmp/gh", "", "orbit", 3, 1, 5, "/tmp/gh/0003-orbit-" + ts + "-01.png"},
		{"explicit single", "/tmp/gh", "./shot.png", "cap", 0, 0, 1, "./shot.png"},
		{"explicit burst", "/tmp/gh", "./shot.png", "cap", 0, 2, 3, "./shot-02.png"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := capturePath(c.out, c.explicit, c.label, ts, c.seq, c.index, c.n)
			if got != c.want {
				t.Fatalf("capturePath = %q, want %q", got, c.want)
			}
		})
	}
}
