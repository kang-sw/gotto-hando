//go:build darwin

package darwin

import (
	"runtime"
	"testing"

	"github.com/kang-sw/gotto-hando/internal/backend"
)

// kCGScrollWheelEventDeltaAxis1/2 (CGEventTypes.h) are the vertical/
// horizontal line-delta fields a kCGScrollEventUnitLine scroll event
// carries - exactly what wheel1/wheel2 in CGEventCreateScrollWheelEvent's
// call become, read back without needing to post the event (construction
// alone needs neither an unlocked session nor Accessibility, only
// CGEventPost does).
const (
	kCGScrollWheelEventDeltaAxis1 = 11
	kCGScrollWheelEventDeltaAxis2 = 12
)

// TestScrollWheelEventXYVariadicARM64HorizontalDeltaSurvivesRealABI is the
// lock-independent regression test for the Important review finding
// (mouse.go Scroll / ffi.go cgEventCreateScrollWheelEventXYVariadicARM64):
// on Apple's arm64 ABI, CGEventCreateScrollWheelEvent's wheel2 (the
// horizontal delta) is part of the C-variadic "..." tail and MUST be
// placed on the stack, never in a register, regardless of how many
// integer registers are still free - unlike the standard AAPCS64 purego
// otherwise emulates. This test calls the REAL bound C function (no
// GUI/injection involved, so it runs cleanly on a locked/remote session)
// and reads the resulting event's fields straight back via
// CGEventGetIntegerValueField, so a future purego upgrade or binding
// change that regresses this padding trick is caught without ever posting
// a synthetic event.
func TestScrollWheelEventXYVariadicARM64HorizontalDeltaSurvivesRealABI(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("cgEventCreateScrollWheelEventXYVariadicARM64 is used only on arm64 (see ffi.go's doc comment); amd64's fixed-arity XY binding needs no padding")
	}
	if err := initFFI(); err != nil {
		t.Fatalf("initFFI: %v", err)
	}
	src := cgEventSourceCreate(cgEventSourceStateHIDSystemState)
	if src == 0 {
		t.Skip("CGEventSourceCreate returned NULL in this environment")
	}

	const vertical, horizontal = int32(5), int32(7)
	ev := cgEventCreateScrollWheelEventXYVariadicARM64(src, cgScrollEventUnitLine, 2, vertical, 0, 0, 0, 0, horizontal)
	if ev == 0 {
		t.Fatal("CGEventCreateScrollWheelEvent returned NULL")
	}
	defer cfRelease(ev)

	if gotV := cgEventGetIntegerValueField(ev, kCGScrollWheelEventDeltaAxis1); gotV != int64(vertical) {
		t.Errorf("vertical (wheel1, DeltaAxis1) = %d, want %d", gotV, vertical)
	}
	if gotH := cgEventGetIntegerValueField(ev, kCGScrollWheelEventDeltaAxis2); gotH != int64(horizontal) {
		t.Errorf("horizontal (wheel2, DeltaAxis2) = %d, want %d - a 0 here means the arm64 variadic-tail padding regressed and wheel2 is being read from the wrong ABI slot again (see ffi.go's cgEventCreateScrollWheelEventXYVariadicARM64 doc comment)", gotH, horizontal)
	}

	// Contrast: the fixed-arity XY binding (correct on amd64) reproduces
	// the original bug on arm64 - wheel2 lands in a register the callee
	// never reads, so DeltaAxis2 comes back 0/garbage instead of
	// `horizontal`. This proves the test above actually exercises the fix,
	// not just a tautology.
	evBuggy := cgEventCreateScrollWheelEventXY(src, cgScrollEventUnitLine, 2, vertical, horizontal)
	if evBuggy == 0 {
		t.Fatal("CGEventCreateScrollWheelEvent (fixed-arity) returned NULL")
	}
	defer cfRelease(evBuggy)
	if gotH := cgEventGetIntegerValueField(evBuggy, kCGScrollWheelEventDeltaAxis2); gotH == int64(horizontal) {
		t.Fatal("fixed-arity XY binding unexpectedly read the correct horizontal delta on arm64 - either this machine's ABI changed or the contrast assumption is stale; re-check whether cgEventCreateScrollWheelEventXYVariadicARM64 is still needed")
	}
}

// TestScrollUnitsAndAmount locks in the by=line/by=page unit split (Fit +
// correctness-minor review finding): by=line stays in kCGScrollEventUnitLine
// (matching help.txt scroll's ticks= payload and CONCEPT.md's general
// description), while by=page switches to kCGScrollEventUnitPixel with the
// magnitude scaled by the page height in pixels - the ticket Decision's
// literal "sent in pixel units" clause, which the pre-fix code violated by
// sending a height/20 line-count approximation through the line-unit API.
func TestScrollUnitsAndAmount(t *testing.T) {
	const pageHeightPixels = 900

	units, amount := scrollUnitsAndAmount(3, backend.ScrollLine, pageHeightPixels)
	if units != cgScrollEventUnitLine {
		t.Errorf("by=line units = %d, want cgScrollEventUnitLine (%d)", units, cgScrollEventUnitLine)
	}
	if amount != 3 {
		t.Errorf("by=line amount = %d, want 3 ticks unscaled", amount)
	}

	units, amount = scrollUnitsAndAmount(3, backend.ScrollPage, pageHeightPixels)
	if units != cgScrollEventUnitPixel {
		t.Errorf("by=page units = %d, want cgScrollEventUnitPixel (%d)", units, cgScrollEventUnitPixel)
	}
	if want := 3 * pageHeightPixels; amount != want {
		t.Errorf("by=page amount = %d, want %d (ticks * page height in pixels)", amount, want)
	}
}
