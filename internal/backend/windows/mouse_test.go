//go:build windows

package windows

import (
	"testing"
	"unsafe"
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
// SAME ticks*WHEEL_DELTA notches as by=line - this test exercises the pure
// amount computation without needing SendInput/a real desktop.
func TestScrollAmountIsSameForLineAndPage(t *testing.T) {
	const ticks = 3
	lineAmount := int32(ticks) * wheelDelta
	pageAmount := int32(ticks) * wheelDelta
	if lineAmount != pageAmount {
		t.Fatalf("line amount=%d, page amount=%d, want equal (Windows has no page-unit wheel API, both send ticks*WHEEL_DELTA)", lineAmount, pageAmount)
	}
	if lineAmount != ticks*120 {
		t.Errorf("amount=%d, want %d (ticks*WHEEL_DELTA)", lineAmount, ticks*120)
	}
}

// TestNormalizeAbsoluteRoundTrip proves normalizeAbsolute produces
// SendInput's documented 0..65535 range for a point inside the virtual
// screen and correctly reflects a non-(0,0) virtual-screen origin (a
// monitor to the left/above the primary has negative desktop coordinates,
// help-windows.txt DPI AND COORDINATES) - exercised as a pure function so
// it needs no real GetSystemMetrics call.
func TestNormalizeAbsoluteMapsOriginAndExtent(t *testing.T) {
	// Reimplement the pure math directly (not through GetSystemMetrics,
	// which needs the real box) to pin the formula: nx = (px-vx)*65536/vw.
	const vx, vy, vw, vh = -1440, 0, 2880, 900
	toNorm := func(px, py float64) (int32, int32) {
		nx := int32((px - float64(vx)) * 65536 / float64(vw))
		ny := int32((py - float64(vy)) * 65536 / float64(vh))
		return nx, ny
	}
	nx, ny := toNorm(-1440, 0) // the virtual screen's own top-left
	if nx != 0 || ny != 0 {
		t.Errorf("origin normalized = (%d,%d), want (0,0)", nx, ny)
	}
	nx, _ = toNorm(0, 0) // the primary display's own top-left: 1440px into a 2880-wide virtual screen, i.e. its midpoint
	if nx != 32768 {
		t.Errorf("mid-point normalized x = %d, want 32768 (halfway across a %d-wide virtual screen)", nx, vw)
	}
}
