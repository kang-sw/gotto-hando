// Package ir is the intermediate representation of a parsed gotto-hando
// sequence: the typed op list that assets/help.txt's == IR JSON ==
// (:625-679) serializes and that the darwin/windows/remote backends read.
// The types here are a downstream contract (260907-feat-darwin-backend,
// 260907-feat-windows-backend, 260908-feat-remote-ssh), so field shapes
// follow the help.txt example field-for-field.
package ir

import "github.com/kang-sw/gotto-hando/internal/output"

// SchemaVersion is the IR "v" field (help.txt:631). It is independent of
// the tool version and is bumped only when the IR shape changes.
const SchemaVersion = 1

// Kind is the "op" discriminator (help.txt:633+). The eight kinds shown in
// the == IR JSON == example are normative; the rest name the remaining
// COMMANDS groups.
type Kind string

const (
	KindFocus        Kind = "focus"           // win
	KindQueryWindows Kind = "query_windows"   // qwin
	KindKey          Kind = "key"             // k
	KindKeyDown      Kind = "key_down"        // kd
	KindKeyUp        Kind = "key_up"          // ku
	KindText         Kind = "text"            // txt
	KindMove         Kind = "move"            // m
	KindClick        Kind = "click"           // c
	KindButtonDown   Kind = "button_down"     // md
	KindButtonUp     Kind = "button_up"       // mu
	KindDrag         Kind = "drag"            // drag
	KindScroll       Kind = "scroll"          // scroll
	KindClipboard    Kind = "clipboard"       // clip
	KindPaste        Kind = "paste"           // paste
	KindQueryClip    Kind = "query_clipboard" // qclip
	KindOpen         Kind = "open"            // open
	KindExec         Kind = "exec"            // exec
	KindCapture      Kind = "capture"         // cap
	KindSleep        Kind = "sleep"           // sleep
	KindSet          Kind = "set"             // set
	KindQueryInfo    Kind = "query_info"      // qinfo
	KindQueryDisp    Kind = "query_displays"  // qdisp
	KindQueryMouse   Kind = "query_mouse"     // qmouse
)

// Sequence is a whole parsed input (help.txt:630-660): schema version, the
// op list and the initial defaults.
type Sequence struct {
	V        int
	Ops      []Op
	Defaults Defaults
}

// Defaults is the "defaults" object (help.txt:659): the initial delay,
// txt per-character interval and k gap in milliseconds. The initial delay
// comes from --delay (else 100ms); set changes these at run time.
type Defaults struct {
	DelayMS        int
	TextIntervalMS int
	KeyGapMS       int
}

// Selector is a WINDOW SELECTOR (help.txt:286-297, IR :634): the kind
// (id, pid, app, title), the raw value, and whether the title value is an
// RE2 regex (the r flag).
type Selector struct {
	Kind  string // "id", "pid", "app", "title"
	Value string
	Regex bool
}

// Point is a COORDINATES coordinate with its frame (help.txt:245-268, IR
// :641-646). Frame is one of "desktop", "pointer", "window", "display";
// Disp is the 0-based display index when Frame == "display". XPct/YPct
// mark a per-coordinate N% value.
type Point struct {
	Frame string
	Disp  int
	X, Y  float64
	XPct  bool
	YPct  bool
}

// Rect is a cap rect=x:y:w:h region (help.txt:406-407). X,Y are coords
// (percent/negative allowed); W,H are plain numbers.
type Rect struct {
	X, Y, W, H float64
	XPct, YPct bool
}

// Op is one command line's IR. It is a wide struct: only the fields
// relevant to Kind are populated, and json.go emits exactly the subset the
// == IR JSON == example shows for that kind. Line and Src are kept on
// every op for error reporting (help.txt:661).
type Op struct {
	Line int
	Src  string
	Kind Kind

	// DelayMS is the per-line d= override (STATE MACHINE delay precedence,
	// help.txt:210, :510). nil when the command uses the running global
	// delay. Serialized as "line_delay_ms" only when set.
	DelayMS *int

	// focus / query_windows
	Selector Selector
	WaitMS   int
	HasWait  bool // win/open/qwin: wait= was given (or command supports it)

	// key / key_down / key_up
	Mods   []string   // canonical modifier symbols: ctrl shift alt meta primary
	Keys   [][]string // k: chord list; kd/ku: one single-key chord per held key
	Repeat int
	GapMS  int

	// text / clipboard / paste / query_clipboard
	Text       string
	IntervalMS int    // txt interval / paste settle
	HasText    bool   // whether Text is meaningful (post-inline)
	FromFile   bool   // f flag was given
	FilePath   string // local file path before the inline pass (not serialized)

	// move / click / button_down / drag
	Point      Point
	HasPoint   bool
	Points     []Point
	DurationMS int // move / drag total duration
	Button     string
	Count      int

	// scroll
	ScrollDir string
	Ticks     int
	ScrollBy  string // "line" | "page"

	// capture
	Frame       string
	Disp        int
	Rect        *Rect
	Scale       float64
	ScaleNative bool
	Format      string
	Label       string
	CapCount    int
	CapInterval int

	// exec
	Argv      []string
	Cmd       string
	Shell     bool
	Noerr     bool
	TimeoutMS int

	// open
	Target string

	// sleep
	SleepMS int

	// set
	SetDelay *int
	SetTxtms *int
	SetKeyms *int
}

// Diagnostic is one parse (E_SYNTAX) or validation (E_VALIDATE) error
// (help.txt EXECUTION :460-471). Line/Col are 1-based; Src is the offending
// source line, printed indented two spaces under the diagnostic.
type Diagnostic struct {
	Line int
	Col  int
	Code output.ErrorCode
	Msg  string
	Src  string
}
