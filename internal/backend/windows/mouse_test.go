//go:build windows

package windows

import (
	"testing"
	"unsafe"

	"github.com/kang-sw/gotto-hando/internal/backend"
)

// TestInputRecordSizesMatch is the lock-independent regression test for
// ffi.go's mouseInputRecord/keybdInputRecord doc comment: Go has no union,
// so each INPUT variant is hand-padded to the SAME total size the real C
// INPUT struct has (40 bytes on amd64). SendInput's cbSize parameter must
// be sizeof(INPUT) regardless of which variant is sent - a future field
// reordering in ffi.go that breaks this equality would silently corrupt
// every SendInput call (wrong cbSize -> ERROR_INVALID_PARAMETER, or worse,
// a misread struct), so this pins the invariant directly rather than
// relying on manual arithmetic staying correct by inspection.
func TestInputRecordSizesMatch(t *testing.T) {
	mouseSize := unsafe.Sizeof(mouseInputRecord{})
	keybdSize := unsafe.Sizeof(keybdInputRecord{})
	if mouseSize != keybdSize {
		t.Fatalf("sizeof(mouseInputRecord)=%d, sizeof(keybdInputRecord)=%d, want equal (both must equal sizeof(INPUT))", mouseSize, keybdSize)
	}
	// 40 is sizeof(INPUT) on amd64 (4-byte type + 4-byte alignment padding
	// + a 32-byte union) - the concrete value this ticket's cross-compile
	// target (GOARCH=amd64) must produce; pinning it (not just the
	// equality above) catches a padding miscalculation that happens to
	// keep both variants equal but wrong.
	const wantSize = 40
	if mouseSize != wantSize {
		t.Errorf("sizeof(mouseInputRecord)=%d, want %d (sizeof(INPUT) on amd64)", mouseSize, wantSize)
	}
}

// TestScrollAmountIsSameForLineAndPage locks in mouse.go Scroll's
// documented by=page decision (ticket Codebase Findings risk signal):
// Windows has no pixel/page-unit wheel injection API, so by=page sends the
// SAME ticks*WHEEL_DELTA notches as by=line. Calls the real scrollAmount
// (mouse.go), not a duplicate of its arithmetic, so a regression in the
// actual amount math (wrong sign, forgot *wheelDelta, off-by-one) fails
// this test.
func TestScrollAmountIsSameForLineAndPage(t *testing.T) {
	const ticks = 3
	lineAmount := scrollAmount(ticks, backend.ScrollLine)
	pageAmount := scrollAmount(ticks, backend.ScrollPage)
	if lineAmount != pageAmount {
		t.Fatalf("line amount=%d, page amount=%d, want equal (Windows has no page-unit wheel API, both send ticks*WHEEL_DELTA)", lineAmount, pageAmount)
	}
	if lineAmount != ticks*120 {
		t.Errorf("amount=%d, want %d (ticks*WHEEL_DELTA)", lineAmount, ticks*120)
	}
}

// TestNormalizeAbsoluteMapsOriginAndExtent proves normalizeAbsoluteAgainst
// (mouse.go), the real pure normalization helper (not a re-derivation of
// its formula), produces SendInput's documented 0..65535 range for a point
// inside the virtual screen and correctly reflects a non-(0,0)
// virtual-screen origin (a monitor to the left/above the primary has
// negative desktop coordinates, help-windows.txt DPI AND COORDINATES).
// Calling normalizeAbsoluteAgainst directly against a synthetic rect (an
// analogous seam to virtualScreenRect()) means this needs no real
// GetSystemMetrics call.
func TestNormalizeAbsoluteMapsOriginAndExtent(t *testing.T) {
	const vx, vy, vw, vh = -1440, 0, 2880, 900
	nx, ny := normalizeAbsoluteAgainst(-1440, 0, vx, vy, vw, vh) // the virtual screen's own top-left
	if nx != 0 || ny != 0 {
		t.Errorf("origin normalized = (%d,%d), want (0,0)", nx, ny)
	}
	nx, _ = normalizeAbsoluteAgainst(0, 0, vx, vy, vw, vh) // the primary display's own top-left: 1440px into a 2880-wide virtual screen, i.e. its midpoint
	if nx != 32768 {
		t.Errorf("mid-point normalized x = %d, want 32768 (halfway across a %d-wide virtual screen)", nx, vw)
	}
	nx, ny = normalizeAbsoluteAgainst(1439, 899, vx, vy, vw, vh) // the virtual screen's bottom-right-most pixel (extent-1, not the exclusive vw/vh edge)
	if nx != 65513 || ny != 65463 {
		t.Errorf("bottom-right-most pixel normalized = (%d,%d), want (65513,65463)", nx, ny)
	}
}
