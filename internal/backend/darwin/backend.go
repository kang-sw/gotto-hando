//go:build darwin

package darwin

import (
	"context"
	"os"
	"regexp"
	"runtime"
	"time"

	"github.com/kang-sw/gotto-hando/assets"
	"github.com/kang-sw/gotto-hando/internal/backend"
)

// Backend is the darwin backend.Backend implementation. Every probe field
// defaults to its real*Probe implementation in New(); same-package tests
// override them directly (probes.go).
type Backend struct {
	session  sessionProbe
	perm     permissionProbe
	keys     keyStateProbe
	secure   secureInputProbe
	displays displayProbe

	// evtSource is a single process-lifetime CGEventSourceRef, created once
	// (kCGEventSourceStateHIDSystemState) and reused for every synthesized
	// event; CGEventCreate* accepts NULL too, but a real source makes
	// synthesized events indistinguishable from physical ones to apps that
	// inspect the event source.
	evtSource uintptr

	// heldFlags is the CGEventFlags bit-OR of every modifier currently
	// pressed via KeyDown (ctrl/shift/alt/meta/primary), applied to every
	// keyboard AND mouse event this backend posts (help-macos.txt CAVEATS:
	// "Some apps only honour modifier flags carried on the event, not
	// separate modifier key-downs; gotto-hando sets both"). Sequential
	// engine execution only, no mutex needed.
	heldFlags uint64

	// clickState/lastButtonUp/heldButton back mouse.go's click-burst
	// tracking (double-click detection) and MouseMove's *Dragged-vs-Moved
	// event-type choice while a button is held (drag.go's polyline).
	clickState   map[string]int
	lastButtonUp map[string]time.Time
	heldButton   backend.Button
}

// New dlopens every framework this backend needs and returns a ready
// Backend. It never prompts for permissions (help-macos.txt CHECK: "qinfo
// never prompts") - Info/Preflight only read the current TCC/session
// state.
func New() (*Backend, error) {
	if err := initFFI(); err != nil {
		return nil, err
	}
	be := &Backend{
		session:      realSessionProbe{},
		perm:         realPermissionProbe{},
		keys:         realKeyStateProbe{},
		secure:       realSecureInputProbe{},
		displays:     realDisplayProbe{},
		clickState:   map[string]int{},
		lastButtonUp: map[string]time.Time{},
	}
	be.evtSource = cgEventSourceCreate(cgEventSourceStateHIDSystemState)
	return be, nil
}

// NewBridge returns a Backend for `gotto-hando --bridge`
// (260908-feat-remote-ssh Phase 2): identical to New() except session is
// bridgeSessionProbe{} instead of the real CGSessionCopyCurrentDictionary
// probe - the bridge process IS the interactive GUI session, so qinfo
// answered through it always reports session=bridge. Every other probe
// stays real: the bridge genuinely can query TCC grants, physical key/
// button state and display geometry.
func NewBridge() (*Backend, error) {
	if err := initFFI(); err != nil {
		return nil, err
	}
	be := &Backend{
		session:      bridgeSessionProbe{},
		perm:         realPermissionProbe{},
		keys:         realKeyStateProbe{},
		secure:       realSecureInputProbe{},
		displays:     realDisplayProbe{},
		clickState:   map[string]int{},
		lastButtonUp: map[string]time.Time{},
	}
	be.evtSource = cgEventSourceCreate(cgEventSourceStateHIDSystemState)
	return be, nil
}

var _ backend.Backend = (*Backend)(nil)

// Info answers qinfo (help.txt:431-434, help-macos.txt CHECK example
// :12-14) and qdisp's per-display continuation lines
// (help.txt:582, help-macos.txt DISPLAYS AND COORDINATES).
func (b *Backend) Info(ctx context.Context) (backend.Info, error) {
	displays := activeDisplays()

	info := backend.Info{
		OS:          runtime.GOOS,
		OSVer:       macOSVersion(),
		Arch:        runtime.GOARCH,
		Ver:         assets.Version(),
		Primary:     "cmd",
		Displays:    len(displays),
		DisplayList: displays,
		Session:     b.session.State(),
		Perms:       permsString(b.perm),
	}
	info.DesktopX, info.DesktopY, info.DesktopW, info.DesktopH = unionBounds(displays)
	return info, nil
}

// MousePos answers qmouse (help.txt:445-446, :583).
func (b *Backend) MousePos(ctx context.Context) (backend.Point, error) {
	ev := cgEventCreate(0)
	if ev == 0 {
		return backend.Point{}, nil
	}
	defer cfRelease(ev)
	p := cgEventGetLocation(ev)
	return backend.Point{X: p.X, Y: p.Y}, nil
}

func permsString(p permissionProbe) string {
	accessibility := "missing"
	if p.Accessibility() {
		accessibility = "ok"
	}
	screen := "missing"
	if p.ScreenRecording() {
		screen = "ok"
	}
	return "accessibility:" + accessibility + ",screen:" + screen
}

// activeDisplays lists the active displays in qdisp order (0-based, the
// order CGGetActiveDisplayList returns, help.txt:253) with each display's
// logical (point) bounds and scale (help-macos.txt DISPLAYS AND
// COORDINATES: "one point = 2 pixels (scale=2 in qdisp)").
func activeDisplays() []backend.DisplayGeom {
	const maxDisplays = 32
	var ids [maxDisplays]uint32
	var count uint32
	if rc := cgGetActiveDisplayList(maxDisplays, &ids[0], &count); rc != 0 {
		return nil
	}
	primary := cgMainDisplayID()
	out := make([]backend.DisplayGeom, 0, count)
	for i := 0; i < int(count); i++ {
		id := ids[i]
		bounds := cgDisplayBounds(id)
		out = append(out, backend.DisplayGeom{
			X:       int(bounds.Origin.X),
			Y:       int(bounds.Origin.Y),
			W:       int(bounds.Size.W),
			H:       int(bounds.Size.H),
			Scale:   displayScale(id, bounds),
			Primary: id == primary,
		})
	}
	return out
}

// displayScale derives qdisp's scale= (help-macos.txt:223-224: "on Retina
// displays one point = 2 pixels") from the ratio of CGDisplayScreenSize's
// physical size to the point bounds. When either is unavailable (e.g. no
// physical size reported), 1 is a safe non-HiDPI default.
func displayScale(id uint32, bounds cgRect) float64 {
	if bounds.Size.W <= 0 {
		return 1
	}
	// CGDisplayScreenSize returns millimeters, not pixels, so it cannot
	// derive scale on its own; scale is better read straight from the
	// backing pixel dimensions when available. Phase 1 has no pixel-mode
	// query wired (Capture is Phase 2), so report the conservative 1:1
	// default here; a HiDPI-aware scale is Phase 2 work alongside cap.
	return 1
}

// unionBounds is the desktop bounding box (help.txt qinfo desktop=X,Y WxH,
// :433; help-macos.txt: "origin (0,0) is the top-left of the primary
// display; displays to the left/above have negative coordinates"): the
// smallest rectangle enclosing every active display.
func unionBounds(displays []backend.DisplayGeom) (x, y, w, h int) {
	if len(displays) == 0 {
		return 0, 0, 0, 0
	}
	minX, minY := displays[0].X, displays[0].Y
	maxX, maxY := displays[0].X+displays[0].W, displays[0].Y+displays[0].H
	for _, d := range displays[1:] {
		if d.X < minX {
			minX = d.X
		}
		if d.Y < minY {
			minY = d.Y
		}
		if d.X+d.W > maxX {
			maxX = d.X + d.W
		}
		if d.Y+d.H > maxY {
			maxY = d.Y + d.H
		}
	}
	return minX, minY, maxX - minX, maxY - minY
}

var productVersionRE = regexp.MustCompile(`(?s)<key>ProductVersion</key>\s*<string>([^<]+)</string>`)

// macOSVersion reads the product version (qinfo osver=, help.txt:432,
// help-macos.txt CHECK example "osver=14.5") from SystemVersion.plist -
// there is no dlsym-able API for it, and this file is always readable.
func macOSVersion() string {
	b, err := os.ReadFile("/System/Library/CoreServices/SystemVersion.plist")
	if err != nil {
		return "unknown"
	}
	m := productVersionRE.FindSubmatch(b)
	if m == nil {
		return "unknown"
	}
	return string(m[1])
}
