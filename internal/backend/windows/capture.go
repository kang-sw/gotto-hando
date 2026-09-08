//go:build windows

package windows

import (
	"context"
	"errors"
	"fmt"
	"math"
	"unsafe"

	"github.com/kang-sw/gotto-hando/internal/backend"
	"github.com/kang-sw/gotto-hando/internal/ir"
	"golang.org/x/sys/windows"
)

// SRCCOPY (BitBlt's dwRop, wingdi.h) and PW_RENDERFULLCONTENT (PrintWindow's
// nFlags, winuser.h - renders DirectComposition/DirectX surfaces, not just
// the GDI-visible client area).
const (
	srcCopy             = 0x00CC0020
	pwRenderfullcontent = 2
	biRGB               = 0
	dibRGBColors        = 0
)

var (
	// errNoCaptureWindow wraps backend.ErrNoWindow so a cap[w] with no
	// current window maps to E_NOWINDOW (like c[w]/m[w]/drag[w]), not
	// E_CAPTURE (engine/capture.go doCapture) - mirrors darwin's capture.go.
	errNoCaptureWindow = fmt.Errorf("%w: no current window to capture (cap[w]; none focused)", backend.ErrNoWindow)
	errCaptureFailed   = errors.New("screen capture failed (a GDI call failed, or the session is headless/disconnected)")
)

// captureRegion is a resolved capture rectangle: its top-left in absolute
// physical-pixel coordinates and its size in pixels (Windows coordinates are
// always physical pixels per Per-Monitor-V2 DPI awareness, unlike darwin's
// logical points - help-windows.txt DPI AND COORDINATES).
type captureRegion struct {
	originX, originY int
	w, h             int
}

// captureRect resolves a CaptureReq's frame + rect= to an absolute pixel
// rectangle, bounds-checked against the real display/window geometry
// (help.txt cap :400-407, COORDINATES :262-264). Pure (geometry only, no
// FFI), duplicated from darwin's capture.go per the ticket's per-backend
// duplication precedent - same rect/w/disp= math and E_BOUNDS cases, unit
// tested with injected synthetic displays/window.
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

// focusedWindow returns the OS-focused top-level window, or nil. cap[w]
// resolves the current window this way (the backend has no engine window
// state); after win[] focused a window it is the GetForegroundWindow one,
// so this matches "the current window" for the common flow. Identical
// shape to darwin's focusedWindow.
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

// Capture takes a screenshot (help.txt cap :400-417). The backend returns
// raw pixels only (ticket Decision); the engine encodes the PNG. cap[w]
// captures the OS-focused window via PrintWindow (a black result is ok per
// the ticket Decision - only a GDI API failure or zero-size bitmap is
// E_CAPTURE); bare cap / cap[disp=N] / a rect= capture the desktop or a
// display region via GetDC(0) + BitBlt (virtual-desktop coordinates already
// select the right region, so no per-monitor DC is needed).
func (b *Backend) Capture(ctx context.Context, req backend.CaptureReq) (backend.Image, error) {
	displays := b.displays.Active()
	var window *backend.Window
	if req.Frame == "window" {
		window = b.focusedWindow(ctx)
	}
	region, err := captureRect(req, displays, window)
	if err != nil {
		return backend.Image{}, err
	}

	pixels, err := captureRegionPixels(region, req.Frame, window)
	if err != nil {
		return backend.Image{}, err
	}

	// nativeScale is always 1: Per-Monitor-V2 DPI awareness already yields
	// physical pixels with no backing-pixel/point distinction (unlike
	// darwin's Retina points), so scale=1 and scale=native coincide
	// (help-windows.txt DPI AND COORDINATES).
	const nativeScale = 1.0
	targetW, targetH, effScale := scaledDims(region.w, region.h, nativeScale, req)
	px := pixels
	if targetW != region.w || targetH != region.h {
		px = resizeRGBANearest(pixels, region.w, region.h, targetW, targetH)
	}
	return backend.Image{
		W: targetW, H: targetH,
		OriginX: region.originX, OriginY: region.originY,
		Scale:  effScale,
		Pixels: px,
	}, nil
}

// captureRegionPixels grabs region's pixels into a tightly-packed RGBA
// buffer: PrintWindow for a window frame, GetDC(0)+BitBlt for
// desktop/display/rect. Every GDI handle is released via defer on every
// path, including early-return errors.
func captureRegionPixels(region captureRegion, frame string, window *backend.Window) ([]byte, error) {
	if region.w <= 0 || region.h <= 0 {
		return nil, errCaptureFailed
	}

	hdcScreen, _, _ := procGetDC.Call(0)
	if hdcScreen == 0 {
		// A headless/disconnected session has no display DC to hand back
		// (help-windows.txt: "capture of a headless/disconnected session
		// returns E_CAPTURE").
		return nil, errCaptureFailed
	}
	defer procReleaseDC.Call(0, hdcScreen)

	hdcMem, _, _ := procCreateCompatibleDC.Call(hdcScreen)
	if hdcMem == 0 {
		return nil, errCaptureFailed
	}
	defer procDeleteDC.Call(hdcMem)

	hBitmap, _, _ := procCreateCompatibleBitmap.Call(hdcScreen, uintptr(region.w), uintptr(region.h))
	if hBitmap == 0 {
		return nil, errCaptureFailed
	}
	defer procDeleteObject.Call(hBitmap)

	oldObj, _, _ := procSelectObject.Call(hdcMem, hBitmap)
	defer procSelectObject.Call(hdcMem, oldObj)

	if frame == "window" {
		hwnd := windows.HWND(window.ID)
		r, _, _ := procPrintWindow.Call(uintptr(hwnd), hdcMem, uintptr(pwRenderfullcontent))
		if r == 0 {
			return nil, errCaptureFailed
		}
	} else {
		r, _, _ := procBitBlt.Call(hdcMem, 0, 0, uintptr(region.w), uintptr(region.h),
			hdcScreen, uintptr(region.originX), uintptr(region.originY), srcCopy)
		if r == 0 {
			return nil, errCaptureFailed
		}
	}

	return readDIBits(hdcMem, hBitmap, region.w, region.h)
}

// bitmapInfoHeader is a Win32 BITMAPINFOHEADER (wingdi.h); no color table
// follows it here because GetDIBits is always asked for an uncompressed
// 32bpp DIB (BI_RGB), which carries no palette.
type bitmapInfoHeader struct {
	biSize          uint32
	biWidth         int32
	biHeight        int32
	biPlanes        uint16
	biBitCount      uint16
	biCompression   uint32
	biSizeImage     uint32
	biXPelsPerMeter int32
	biYPelsPerMeter int32
	biClrUsed       uint32
	biClrImportant  uint32
}

// readDIBits reads hBitmap's pixels via GetDIBits into a tightly-packed RGBA
// buffer. A negative biHeight requests a top-down DIB (rows are not
// bottom-up, so no row-order flip is needed) and a 32bpp BI_RGB DIB's row
// stride is always w*4 (already DWORD-aligned) - no padding to strip, unlike
// darwin's CGImage rows.
func readDIBits(hdc, hBitmap uintptr, w, h int) ([]byte, error) {
	var bi bitmapInfoHeader
	bi.biSize = uint32(unsafe.Sizeof(bi))
	bi.biWidth = int32(w)
	bi.biHeight = -int32(h)
	bi.biPlanes = 1
	bi.biBitCount = 32
	bi.biCompression = biRGB

	buf := make([]byte, w*h*4)
	r, _, _ := procGetDIBits.Call(hdc, hBitmap, 0, uintptr(h),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&bi)), dibRGBColors)
	if r == 0 {
		return nil, errCaptureFailed
	}
	return bgraToRGBA(buf, w, h, w*4), nil
}

// bgraToRGBA converts a 32-bit BGRA/BGRX DIB pixel buffer (a GetDIBits
// BI_RGB DIB has no meaningful alpha channel - screenshots are effectively
// opaque) into a tightly-packed, forced-opaque RGBA buffer with stride w*4 -
// the shape engine/capture.go's encodePNG expects. Pure (no FFI); the
// channel swap and the opaque-alpha write are unit-tested off a synthetic
// buffer, identical logic to darwin's bgraToRGBA.
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
// factor for a capture (help.txt cap scale= :409-411). scale=native keeps
// the full native pixels (effScale = nativeScale, always 1 here);
// scale=1/scale=F gives points*F, and points==pixels on Windows (no
// backing-pixel distinction) so scale=1 is a no-op resize. Identical logic
// to darwin's scaledDims, with nativeScale pinned to 1.
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
// (a v1 downscale/upscale; a higher-quality filter is post-v1). Identical to
// darwin's resizeRGBANearest.
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
