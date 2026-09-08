//go:build darwin

package darwin

// The probe interfaces are the Phase 1 test seam: Backend holds one field
// per probe, defaulted to the real*Probe implementation in New() and
// directly settable from same-package _test.go files (tests live in
// `package darwin`, so no exported override API is needed).

// sessionProbe answers the GUI session state (help-macos.txt SESSION,
// LOCK, SECURE INPUT; help.txt qinfo session=, :434-440).
type sessionProbe interface {
	// State returns one of "active", "bridge", "locked", "inactive".
	State() string
}

// permissionProbe answers the two TCC grants qinfo reports
// (help-macos.txt WHEN YOU NEED THIS, :16-24).
type permissionProbe interface {
	Accessibility() bool
	ScreenRecording() bool
}

// keyStateProbe answers Preflight check 3: is any key or mouse button
// PHYSICALLY held right now (help.txt Constraints: "CGEventSourceKeyState
// over every virtual key code + CGEventSourceButtonState").
type keyStateProbe interface {
	// AnyKeyHeld reports whether any of the given keycodes is currently
	// physically down.
	AnyKeyHeld(codes []uint16) bool
	// AnyButtonHeld reports whether any of left/right/middle is currently
	// physically down.
	AnyButtonHeld() bool
}

// secureInputProbe answers whether Secure Input is currently enabled
// (help-macos.txt SESSION, LOCK, SECURE INPUT, :215-218). This is a
// RUN-TIME check inside keyboard.go, not a Preflight check.
type secureInputProbe interface {
	Enabled() bool
}

// realSessionProbe backs sessionProbe with CGSessionCopyCurrentDictionary
// (session.go).
type realSessionProbe struct{}

func (realSessionProbe) State() string { return sessionState() }

// realPermissionProbe backs permissionProbe with AXIsProcessTrusted /
// CGPreflightScreenCaptureAccess.
type realPermissionProbe struct{}

func (realPermissionProbe) Accessibility() bool   { return axIsProcessTrusted() }
func (realPermissionProbe) ScreenRecording() bool { return cgPreflightScreenCaptureAccess() }

// realKeyStateProbe backs keyStateProbe with CGEventSourceKeyState /
// CGEventSourceButtonState over the HID (physical hardware) source state.
type realKeyStateProbe struct{}

func (realKeyStateProbe) AnyKeyHeld(codes []uint16) bool {
	for _, c := range codes {
		if cgEventSourceKeyState(cgEventSourceStateHIDSystemState, c) {
			return true
		}
	}
	return false
}

// Mouse button constants (CGMouseButton: kCGMouseButtonLeft=0,
// kCGMouseButtonRight=1, kCGMouseButtonCenter=2).
const (
	cgMouseButtonLeft   = 0
	cgMouseButtonRight  = 1
	cgMouseButtonCenter = 2
)

func (realKeyStateProbe) AnyButtonHeld() bool {
	for _, b := range []uint32{cgMouseButtonLeft, cgMouseButtonRight, cgMouseButtonCenter} {
		if cgEventSourceButtonState(cgEventSourceStateHIDSystemState, b) {
			return true
		}
	}
	return false
}

// realSecureInputProbe backs secureInputProbe with IsSecureEventInputEnabled.
type realSecureInputProbe struct{}

func (realSecureInputProbe) Enabled() bool { return isSecureEventInputEnabled() }
