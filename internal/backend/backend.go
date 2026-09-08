// Package backend is the platform-input contract the engine composes into
// clicks, drags, key sequences, repeats and interpolation (CONCEPT.md
// ch.8.2: "engine composes primitives, backend stays thin"). The Backend
// interface and these value types are what 260907-feat-darwin-backend,
// 260907-feat-windows-backend and 260908-feat-remote-ssh implement; the
// engine and its dryrun test backend are the only in-repo consumers this
// phase.
package backend

import (
	"context"
	"errors"
	"time"

	"github.com/kang-sw/gotto-hando/internal/ir"
)

// ErrBounds is the sentinel a backend wraps (fmt.Errorf("%w: ...",
// backend.ErrBounds)) when a Capture region resolves outside its frame -
// an out-of-range disp=N, or a rect= / w-frame region that does not fit
// the display/window/desktop (help.txt COORDINATES :262-264: "a coordinate
// outside its frame is E_BOUNDS"). The engine maps a Capture error
// carrying it to E_BOUNDS instead of the default E_CAPTURE.
var ErrBounds = errors.New("coordinate outside bounds")

// ErrNoWindow is the sentinel a backend wraps when a Capture that needs the
// current window (cap[w]) has none focused - so the engine maps it to
// E_NOWINDOW, consistent with a w-frame coordinate for c/m/drag (help.txt
// COORDINATES :254), instead of the default E_CAPTURE.
var ErrNoWindow = errors.New("no current window")

// Button is a mouse button (help.txt:325).
type Button string

const (
	ButtonLeft   Button = "left"
	ButtonRight  Button = "right"
	ButtonMiddle Button = "middle"
)

// Dir is a scroll direction (help.txt:339).
type Dir string

// ScrollUnit is line- or page-wise scrolling (help.txt:339).
type ScrollUnit string

const (
	ScrollLine ScrollUnit = "line"
	ScrollPage ScrollUnit = "page"
)

// Point is a resolved absolute coordinate in logical units (help.txt:245);
// the engine resolves ir.Point frames/percentages before calling the
// backend.
type Point struct {
	X, Y float64
}

// Info is the qinfo answer (help.txt:431-442).
type Info struct {
	OS       string
	OSVer    string
	Arch     string
	Ver      string
	Primary  string // "cmd" | "ctrl"
	DesktopX int
	DesktopY int
	DesktopW int
	DesktopH int
	Displays int
	Session  string // active | bridge | locked | inactive
	Perms    string

	// DisplayList is the qdisp answer (help.txt:576-586, :433): one entry
	// per display in 0-based "qdisp order" (the same order m[disp=N] /
	// cap[disp=N] address, help.txt:253). Not part of the qinfo one-line
	// summary (Displays carries the count there); only used to build
	// qdisp's continuation lines and the disp=N runtime/preflight bounds
	// checks (COORDINATES, help.txt:262-268).
	DisplayList []DisplayGeom
}

// DisplayGeom is one display's geometry for qdisp / disp=N bounds checks
// (help-macos.txt:222-232: origin, size, scale; primary is qdisp's
// trailing flag).
type DisplayGeom struct {
	X, Y, W, H int
	Scale      float64
	Primary    bool
}

// Window is one visible window (help.txt:577, :587).
type Window struct {
	ID        int
	PID       int
	App       string
	Title     string
	X, Y      int
	W, H      int
	Focused   bool
	Minimized bool
	Hidden    bool
}

// Image is a captured screenshot's pixels and geometry before encoding
// (help.txt:586); the engine/local side encodes and writes the PNG.
type Image struct {
	W, H             int
	OriginX, OriginY int
	Scale            float64
	Pixels           []byte
}

// ExecResult is one exec run's outcome (help.txt:556, :581-585).
type ExecResult struct {
	Exit      int
	Stdout    string
	Stderr    string
	Truncated bool
	// TimedOut reports that the timeout= deadline killed the process
	// (help.txt:390-391). The engine maps it to E_TIMEOUT, which noerr
	// never softens (help.txt:536); it is distinct from a non-zero Exit,
	// which noerr does soften.
	TimedOut bool
}

// CaptureReq is a resolved capture request (help.txt:400-417). Frame is
// desktop/window/display; Rect (when set) is a logical region of the frame.
type CaptureReq struct {
	Frame       string
	Display     int
	Rect        *ir.Rect
	Scale       float64
	ScaleNative bool
	Format      string
}

// ExecReq is a resolved exec request (help.txt:370-398).
type ExecReq struct {
	Argv    []string
	Cmd     string
	Shell   bool
	Timeout time.Duration
}

// Backend is one target machine's input/query surface. Every method takes a
// context for the run deadline (help.txt:528-532). The engine calls these
// primitives; click/drag/key-chords/repeat/interpolation/held-tracking/delay
// live in the engine, not here.
type Backend interface {
	Info(ctx context.Context) (Info, error)
	Preflight(ctx context.Context, seq *ir.Sequence) error

	KeyDown(ctx context.Context, key string) error
	KeyUp(ctx context.Context, key string) error
	TypeText(ctx context.Context, s string, interval time.Duration) error

	MouseMove(ctx context.Context, p Point, dur time.Duration) error
	ButtonDown(ctx context.Context, b Button) error
	ButtonUp(ctx context.Context, b Button) error
	// Scroll scrolls at the current pointer position (help.txt:339-341).
	// pageHeightPixels is the resolved height of one "page" for by=page
	// (the current window's height when a win is focused, else the primary
	// display's height); the engine resolves it GOOS-agnostically so every
	// backend gets identical by=page semantics (help.txt scroll Decision).
	// It is ignored for by=line.
	Scroll(ctx context.Context, dir Dir, ticks int, by ScrollUnit, pageHeightPixels int) error

	Windows(ctx context.Context, sel ir.Selector) ([]Window, error)
	Focus(ctx context.Context, w Window) error

	Capture(ctx context.Context, req CaptureReq) (Image, error)

	ClipboardGet(ctx context.Context) (string, error)
	ClipboardSet(ctx context.Context, s string) error

	MousePos(ctx context.Context) (Point, error)

	Open(ctx context.Context, target string) error
	Exec(ctx context.Context, req ExecReq) (ExecResult, error)
}
