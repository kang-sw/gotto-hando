//go:build darwin

package darwin

import (
	"context"
	"errors"
	"fmt"
	"math"
	"unsafe"

	"github.com/kang-sw/gotto-hando/internal/backend"
	"github.com/kang-sw/gotto-hando/internal/ir"
)

// CGWindowListCreateImage list options / image options (CGWindow.h).
const (
	kCGWindowListOptionOnScreenOnly    = 1 << 0
	kCGWindowListOptionIncludingWindow = 1 << 3
	kCGNullWindowID                    = 0
	kCGWindowImageDefault              = 0
	// kCGWindowImageBoundsIgnoreFraming excludes the window's drop shadow
	// and framing so the captured image is tight to the window's own bounds
	// (CGWindow.h). Without it CGWindowListCreateImage returns an image
	// LARGER than window.W/H with a top-left shifted up/left of
	// window.X/window.Y, so the reported origin/size and the derived
	// nativeScale (pw/region.w) no longer match the window's qwin geometry.
	kCGWindowImageBoundsIgnoreFraming = 1 << 0
)

var (
	// errNoCaptureWindow wraps backend.ErrNoWindow so a cap[w] with no
	// current window maps to E_NOWINDOW (like c[w]/m[w]/drag[w]), not
	// E_CAPTURE (engine/capture.go doCapture).
	errNoCaptureWindow = fmt.Errorf("%w: no current window to capture (cap[w]; none focused)", backend.ErrNoWindow)
	errCaptureFailed   = errors.New("screenshot failed (Screen Recording permission or an off-screen frame)")
)

// captureRegion is a resolved capture rectangle: its top-left in absolute
// logical (point) coordinates and its size in points.
type captureRegion struct {
	originX, originY int
	w, h             int
}

// captureRect resolves a CaptureReq's frame + rect= to an absolute logical
// rectangle, bounds-checked against the real display/window geometry
// (help.txt cap :400-407, COORDINATES :262-264). It is pure (geometry only,
// no FFI) so the rect/w/disp= math and the E_BOUNDS cases are unit-tested
// with injected synthetic displays/window. window is the current window for
// Frame=="window" (nil -> errNoCaptureWindow). An out-of-range disp=N or a
// rect that does not fit its frame wraps backend.ErrBounds.
func captureRect(req backend.CaptureReq, displays []backend.DisplayGeom, window *backend.Window) (captureRegion, error) {
	var fx, fy, fw, fh int
	switch req.Frame {
	case "window":
		if window == nil {
			return captureRegion{}, errNoCaptureWindow
		}
		fx, fy, fw, fh = window.X, window.Y, window.W, window.H
	case "display":
		if req.Display < 0 || req.Display >= len(displays) {
			return captureRegion{}, fmt.Errorf("%w: display %d does not exist", backend.ErrBounds, req.Display)
		}
		d := displays[req.Display]
		fx, fy, fw, fh = d.X, d.Y, d.W, d.H
	default: // desktop
		fx, fy, fw, fh = unionBounds(displays)
	}

	if req.Rect == nil {
		return captureRegion{originX: fx, originY: fy, w: fw, h: fh}, nil
	}

	// rect= X,Y are frame-relative coordinates (percent/negative allowed);
	// W,H are plain sizes (help.txt cap rect= :406-407).
	rx := fx + int(req.Rect.X)
	if req.Rect.XPct {
		rx = fx + int(req.Rect.X/100*float64(fw))
	}
	ry := fy + int(req.Rect.Y)
	if req.Rect.YPct {
		ry = fy + int(req.Rect.Y/100*float64(fh))
	}
	rw, rh := int(req.Rect.W), int(req.Rect.H)
	if rw <= 0 || rh <= 0 || rx < fx || ry < fy || rx+rw > fx+fw || ry+rh > fy+fh {
		return captureRegion{}, fmt.Errorf("%w: rect %d,%d %dx%d outside frame %d,%d %dx%d",
			backend.ErrBounds, rx, ry, rw, rh, fx, fy, fw, fh)
	}
	return captureRegion{originX: rx, originY: ry, w: rw, h: rh}, nil
}

// Capture takes a screenshot (help.txt cap :400-417). The backend returns
// raw pixels only (ticket Decision); the engine encodes the PNG. cap[w]
// captures the OS-focused window (the window a prior win focused, which
// Focus activated frontmost); cap[disp=N] a display; bare cap the whole
// desktop. Scale handling (scale=1 downscales Retina to points, native
// keeps full pixels, F resamples) happens here because it is inherently
// tied to the platform's backing-pixel ratio.
func (b *Backend) Capture(ctx context.Context, req backend.CaptureReq) (backend.Image, error) {
	initWindows()
	displays := b.displays.Active()
	var window *backend.Window
	if req.Frame == "window" {
		window = b.focusedWindow(ctx)
	}
	region, err := captureRect(req, displays, window)
	if err != nil {
		return backend.Image{}, err
	}

	var cgImg uintptr
	switch req.Frame {
	case "window":
		var screen cgRect
		if req.Rect != nil {
			screen = cgRectOf(region)
		} else {
			screen = cgRectNull()
		}
		cgImg = cgWindowListCreateImage(screen, kCGWindowListOptionIncludingWindow, uint32(window.ID), kCGWindowImageBoundsIgnoreFraming)
	case "display":
		id := activeDisplayIDs()[req.Display]
		if req.Rect != nil {
			// CGDisplayCreateImageForRect takes a display-local rect.
			local := cgRect{Origin: cgPoint{X: float64(region.originX - displays[req.Display].X), Y: float64(region.originY - displays[req.Display].Y)},
				Size: cgSize{W: float64(region.w), H: float64(region.h)}}
			cgImg = cgDisplayCreateImageForRect(id, local)
		} else {
			cgImg = cgDisplayCreateImage(id)
		}
	default: // desktop
		cgImg = cgWindowListCreateImage(cgRectOf(region), kCGWindowListOptionOnScreenOnly, kCGNullWindowID, kCGWindowImageDefault)
	}
	if cgImg == 0 {
		return backend.Image{}, errCaptureFailed
	}
	defer cfRelease(cgImg)

	return imageFromCG(cgImg, region, req)
}

// focusedWindow returns the OS-focused layer-0 regular-app window, or nil.
// cap[w] resolves the current window this way (the backend has no engine
// window state); after win[] focused a window it is the frontmost/focused
// one, so this matches "the current window" for the common flow.
func (b *Backend) focusedWindow(ctx context.Context) *backend.Window {
	wins, err := b.Windows(ctx, ir.Selector{Kind: "title", Value: ""})
	if err != nil {
		return nil
	}
	for i := range wins {
		if wins[i].Focused {
			return &wins[i]
		}
	}
	return nil
}

// imageFromCG reads a CGImage's pixels back into a tightly-packed RGBA
// buffer and applies the requested scale. CGImage screenshots are 32-bit
// BGRA (kCGBitmapByteOrder32Little + premultiplied-first); it is converted
// to RGBA and forced opaque (screenshots have no meaningful alpha).
func imageFromCG(cgImg uintptr, region captureRegion, req backend.CaptureReq) (backend.Image, error) {
	pw := cgImageGetWidth(cgImg)
	ph := cgImageGetHeight(cgImg)
	stride := cgImageGetBytesPerRow(cgImg)
	if pw <= 0 || ph <= 0 {
		return backend.Image{}, errCaptureFailed
	}
	provider := cgImageGetDataProvider(cgImg)
	if provider == 0 {
		return backend.Image{}, errCaptureFailed
	}
	data := cgDataProviderCopyData(provider)
	if data == 0 {
		return backend.Image{}, errCaptureFailed
	}
	defer cfRelease(data)
	ptr := cfDataGetBytePtr(data)
	length := cfDataGetLength(data)
	if ptr == nil || length < stride*ph {
		return backend.Image{}, errCaptureFailed
	}
	src := unsafe.Slice(ptr, length)

	// Native (backing-pixel) RGBA, padding stripped, BGRA -> RGBA.
	native := bgraToRGBA(src, pw, ph, stride)

	// nativeScale = backing pixels per logical point of the captured region.
	nativeScale := 1.0
	if region.w > 0 {
		nativeScale = float64(pw) / float64(region.w)
	}
	targetW, targetH, effScale := scaledDims(pw, ph, nativeScale, req)
	pixels := native
	if targetW != pw || targetH != ph {
		pixels = resizeRGBANearest(native, pw, ph, targetW, targetH)
	}
	return backend.Image{
		W: targetW, H: targetH,
		OriginX: region.originX, OriginY: region.originY,
		Scale:  effScale,
		Pixels: pixels,
	}, nil
}

// bgraToRGBA converts a CGImage screenshot's 32-bit BGRA pixel buffer
// (kCGBitmapByteOrder32Little + premultiplied-first, with a row stride that
// may exceed w*4 by padding) into a tightly-packed, forced-opaque RGBA
// buffer with stride w*4 - the shape engine/capture.go's encodePNG expects.
// It is pure (no FFI, no CGImage), so imageFromCG only has to feed it the
// right bytes; the channel swap and the opaque-alpha write are unit-tested
// off a synthetic buffer. Forcing alpha opaque without un-premultiplying is
// v1-acceptable: screenshots are effectively opaque, and only a window
// grab's rounded-corner edge pixels carry meaningful alpha.
func bgraToRGBA(src []byte, w, h, stride int) []byte {
	dst := make([]byte, w*h*4)
	for y := 0; y < h; y++ {
		srow := src[y*stride : y*stride+w*4]
		drow := dst[y*w*4 : (y+1)*w*4]
		for x := 0; x < w; x++ {
			s := srow[x*4 : x*4+4]
			d := drow[x*4 : x*4+4]
			d[0] = s[2] // R <- B
			d[1] = s[1] // G
			d[2] = s[0] // B <- R
			d[3] = 0xff // opaque
		}
	}
	return dst
}

// scaledDims computes the output pixel dimensions and the effective scale
// factor for a capture (help.txt cap scale= :409-411, help-macos.txt
// :225-230). scale=native keeps the full backing pixels (effScale =
// nativeScale); scale=1 downscales Retina so image pixels == points; scale=F
// gives points*F. pw/ph are native pixels, nativeScale backing-pixels/point.
func scaledDims(pw, ph int, nativeScale float64, req backend.CaptureReq) (w, h int, effScale float64) {
	if req.ScaleNative {
		return pw, ph, nativeScale
	}
	f := req.Scale
	if f <= 0 {
		f = 1
	}
	pointW := float64(pw) / nativeScale
	pointH := float64(ph) / nativeScale
	w = int(math.Round(pointW * f))
	h = int(math.Round(pointH * f))
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	return w, h, f
}

// resizeRGBANearest nearest-neighbour resamples a tightly-packed RGBA image
// (a v1 downscale/upscale; a higher-quality filter is post-v1). Used only
// when the requested scale differs from the native pixel size.
func resizeRGBANearest(src []byte, sw, sh, dw, dh int) []byte {
	dst := make([]byte, dw*dh*4)
	for y := 0; y < dh; y++ {
		sy := y * sh / dh
		for x := 0; x < dw; x++ {
			sx := x * sw / dw
			si := (sy*sw + sx) * 4
			di := (y*dw + x) * 4
			copy(dst[di:di+4], src[si:si+4])
		}
	}
	return dst
}

// cgRectOf builds a CGRect (absolute screen points) from a captureRegion.
func cgRectOf(r captureRegion) cgRect {
	return cgRect{Origin: cgPoint{X: float64(r.originX), Y: float64(r.originY)},
		Size: cgSize{W: float64(r.w), H: float64(r.h)}}
}

// cgRectNull is CGRectNull ({{INFINITY,INFINITY},{0,0}}): passed to
// CGWindowListCreateImage it means "the window's own bounds".
func cgRectNull() cgRect {
	inf := math.Inf(1)
	return cgRect{Origin: cgPoint{X: inf, Y: inf}, Size: cgSize{W: 0, H: 0}}
}

// activeDisplayIDs lists the CGDirectDisplayIDs in qdisp order (the same
// order activeDisplays() reports geometry), so cap[disp=N] addresses the
// display N that qdisp's line N describes.
func activeDisplayIDs() []uint32 {
	const maxDisplays = 32
	var ids [maxDisplays]uint32
	var count uint32
	if rc := cgGetActiveDisplayList(maxDisplays, &ids[0], &count); rc != 0 {
		return nil
	}
	return ids[:count]
}
