//go:build windows

package windows

import (
	"sync"
	"unsafe"

	"github.com/kang-sw/gotto-hando/internal/backend"
	"golang.org/x/sys/windows"
)

// The probe interfaces are the Phase 1 test seam, same shape as darwin's
// probes.go: Backend holds one field per probe, defaulted to the
// real*Probe implementation in New() and directly settable from
// same-package _test.go files (tests live in `package windows`, so no
// exported override API is needed). Windows drops darwin's
// permissionProbe (perms=n/a, nothing to probe) and secureInputProbe (no
// Windows equivalent - UIPI silently drops input to elevated windows
// instead of a queryable session-wide flag, a documented CAVEAT rather
// than a probe).

// sessionProbe answers the GUI session state (help-windows.txt CHECK;
// help.txt qinfo session=).
type sessionProbe interface {
	// State returns one of "active", "bridge", "locked", "inactive".
	State() string
}

// keyStateProbe answers Preflight check 3: is any key or mouse button
// PHYSICALLY held right now (help-windows.txt CHECK: "GetAsyncKeyState
// over every virtual key"). Unlike darwin's separate key/button probes, a
// single method suffices - GetAsyncKeyState's VK code space already
// covers mouse buttons (VK_LBUTTON/VK_RBUTTON/VK_MBUTTON) alongside every
// keyboard key.
type keyStateProbe interface {
	// AnyPhysicallyHeld reports whether any of the given virtual-key codes
	// is currently physically down.
	AnyPhysicallyHeld(codes []uint8) bool
}

// displayProbe answers the active display list Preflight check 4 bounds
// absolute/disp= coordinates against (help.txt COORDINATES). A real
// display's geometry cannot be controlled from a test, so this is the seam
// a preflight_test.go case uses to inject synthetic, non-(0,0)-origin
// displays.
type displayProbe interface {
	Active() []backend.DisplayGeom
}

// realSessionProbe backs sessionProbe with session.go's sessionState.
type realSessionProbe struct{}

func (realSessionProbe) State() string { return sessionState() }

// bridgeSessionProbe backs a Backend built by NewBridge() (backend.go):
// the bridge process (`gotto-hando --bridge`) genuinely runs inside the
// interactive GUI session, so qinfo answered through it always reports
// session=bridge (help-windows.txt CHECK :17-19) regardless of the actual
// desktop/session-id probe result - which is why NewBridge() only swaps
// this one probe and keeps the real keys/displays probes (the bridge
// process really can query physical key state and monitor geometry).
type bridgeSessionProbe struct{}

func (bridgeSessionProbe) State() string { return "bridge" }

// realKeyStateProbe backs keyStateProbe with GetAsyncKeyState.
type realKeyStateProbe struct{}

func (realKeyStateProbe) AnyPhysicallyHeld(codes []uint8) bool {
	for _, c := range codes {
		r, _, _ := procGetAsyncKeyState.Call(uintptr(int32(c)))
		if r&0x8000 != 0 {
			return true
		}
	}
	return false
}

// realDisplayProbe backs displayProbe with the real EnumDisplayMonitors
// query (backend.go's activeDisplays).
type realDisplayProbe struct{}

func (realDisplayProbe) Active() []backend.DisplayGeom { return activeDisplays() }

// displayEnum holds the EnumDisplayMonitors callback state, built exactly
// once via sync.Once rather than fresh on every activeDisplays() call.
// windows.NewCallback allocates a permanent trampoline slot from a small,
// process-wide, never-freed pool; a fresh one per call would eventually
// exhaust it and panic ("too many callback functions") in a long-lived
// process (a resident `--bridge`, or any repeated qdisp/Preflight
// coordinate-bounds check) - same fix, same reasoning as windows.go's
// windowEnum for EnumWindows.
var displayEnum struct {
	once     sync.Once
	callback uintptr
	mu       sync.Mutex
	result   []backend.DisplayGeom
}

func displayEnumCallback() uintptr {
	displayEnum.once.Do(func() {
		displayEnum.callback = windows.NewCallback(func(hMonitor uintptr, hdcMonitor uintptr, lprcMonitor *windows.Rect, dwData uintptr) uintptr {
			var mi monitorInfo
			mi.cbSize = uint32(unsafe.Sizeof(mi))
			procGetMonitorInfoW.Call(hMonitor, uintptr(unsafe.Pointer(&mi)))
			displayEnum.result = append(displayEnum.result, backend.DisplayGeom{
				X:       int(mi.rcMonitor.Left),
				Y:       int(mi.rcMonitor.Top),
				W:       int(mi.rcMonitor.Right - mi.rcMonitor.Left),
				H:       int(mi.rcMonitor.Bottom - mi.rcMonitor.Top),
				Scale:   dpiScale(hMonitor),
				Primary: mi.dwFlags&monitorinfofPrimary != 0,
			})
			return 1 // continue enumeration
		})
	})
	return displayEnum.callback
}

// activeDisplays lists the active displays in qdisp order (0-based, the
// order EnumDisplayMonitors's callback is invoked in - not required to
// match any OS-native monitor numbering, only to be internally consistent
// with itself, same as help.txt:253's "qdisp order" contract) with each
// display's geometry and DPI-derived scale (help-windows.txt DPI AND
// COORDINATES: "qdisp lists each monitor's origin, size and scale (1.25 =
// 125%)").
func activeDisplays() []backend.DisplayGeom {
	displayEnum.mu.Lock()
	defer displayEnum.mu.Unlock()
	displayEnum.result = nil
	procEnumDisplayMonitors.Call(0, 0, displayEnumCallback(), 0)
	out := displayEnum.result
	displayEnum.result = nil
	return out
}

// dpiScale reads the monitor's effective DPI (help-windows.txt: "scale
// (1.25 = 125%)"); a query failure (dpiX==0) falls back to 1 (non-scaled),
// the same conservative default darwin's displayScale uses when it cannot
// determine physical scale.
func dpiScale(hMonitor uintptr) float64 {
	var dpiX, dpiY uint32
	hr, _, _ := procGetDpiForMonitor.Call(hMonitor, mdtEffectiveDpi,
		uintptr(unsafe.Pointer(&dpiX)), uintptr(unsafe.Pointer(&dpiY)))
	if hr != 0 || dpiX == 0 {
		return 1
	}
	return float64(dpiX) / 96.0
}
