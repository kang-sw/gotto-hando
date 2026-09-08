//go:build windows

package windows

import (
	"context"
	"fmt"
	"runtime"
	"unsafe"

	"github.com/kang-sw/gotto-hando/assets"
	"github.com/kang-sw/gotto-hando/internal/backend"
	"golang.org/x/sys/windows"
)

// Backend is the windows backend.Backend implementation. Every probe field
// defaults to its real*Probe implementation in New(); same-package tests
// override them directly (probes.go) - same seam shape as darwin's
// Backend.
type Backend struct {
	session  sessionProbe
	keys     keyStateProbe
	displays displayProbe
}

// New returns a ready Backend. Unlike darwin's dlopen-based New(), there is
// no dlopen-time failure mode here: NewLazySystemDLL is lazy and only
// errors on first Call(), so New() itself cannot fail - the (Backend,
// error) return shape is kept anyway to match darwin's New() and the
// dispatch_windows.go call site.
func New() (*Backend, error) {
	return &Backend{
		session:  realSessionProbe{},
		keys:     realKeyStateProbe{},
		displays: realDisplayProbe{},
	}, nil
}

var _ backend.Backend = (*Backend)(nil)

// Info answers qinfo (help.txt:431-434, help-windows.txt CHECK example
// :11-13) and qdisp's per-display continuation lines (help.txt:582).
func (b *Backend) Info(ctx context.Context) (backend.Info, error) {
	displays := b.displays.Active()
	info := backend.Info{
		OS:          runtime.GOOS,
		OSVer:       osVersion(),
		Arch:        runtime.GOARCH,
		Ver:         assets.Version(),
		Primary:     "ctrl",
		Displays:    len(displays),
		DisplayList: displays,
		Session:     b.session.State(),
		// elevated=0|1 has no dedicated Info field: it is packed into the
		// free-form Perms string (ticket Decisions), so qinfo's
		// "perms=%s" verbatim formatting (internal/engine/query.go) prints
		// "perms=n/a elevated=0|1" with zero engine change.
		Perms: fmt.Sprintf("n/a elevated=%d", elevatedBit()),
	}
	info.DesktopX, info.DesktopY, info.DesktopW, info.DesktopH = unionBounds(displays)
	return info, nil
}

// MousePos answers qmouse (help.txt:445-446, :583).
func (b *Backend) MousePos(ctx context.Context) (backend.Point, error) {
	var p point32
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	return backend.Point{X: float64(p.X), Y: float64(p.Y)}, nil
}

// osVersion reads the REAL build number via RtlGetVersion (ntdll), which -
// unlike GetVersionEx/VerifyVersionInfo - is not subject to the
// application-compatibility shim's version-lie for manifest-less binaries
// (ticket Codebase Findings: "real build number regardless of
// compatibility-shim manifest lies").
func osVersion() string {
	v := windows.RtlGetVersion()
	return fmt.Sprintf("%d.%d.%d", v.MajorVersion, v.MinorVersion, v.BuildNumber)
}

// elevatedBit reads whether this process's token is elevated (UAC/UIPI,
// help-windows.txt UAC AND ELEVATED APPS) via golang.org/x/sys/windows's
// own Token.IsElevated helper (OpenProcessToken + GetTokenInformation
// TokenElevation, already wrapped there - no ffi.go binding needed).
func elevatedBit() int {
	if windows.GetCurrentProcessToken().IsElevated() {
		return 1
	}
	return 0
}

// unionBounds is the desktop bounding box (help.txt qinfo desktop=X,Y WxH):
// the smallest rectangle enclosing every active display. A small,
// windows-local duplicate of darwin's backend.go helper of the same name -
// no shared-package extraction needed for a ~15-line pure function
// (surgical changes).
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
