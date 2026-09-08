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
	"time"

	"github.com/kang-sw/gotto-hando/internal/ir"
)

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
	Scroll(ctx context.Context, dir Dir, ticks int, by ScrollUnit) error

	Windows(ctx context.Context, sel ir.Selector) ([]Window, error)
	Focus(ctx context.Context, w Window) error

	Capture(ctx context.Context, req CaptureReq) (Image, error)

	ClipboardGet(ctx context.Context) (string, error)
	ClipboardSet(ctx context.Context, s string) error

	MousePos(ctx context.Context) (Point, error)

	Open(ctx context.Context, target string) error
	Exec(ctx context.Context, req ExecReq) (ExecResult, error)
}
