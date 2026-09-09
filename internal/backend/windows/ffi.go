//go:build windows

// Package windows implements backend.Backend for Windows using
// golang.org/x/sys/windows LazyDLL/LazyProc bindings only (no cgo, matching
// the darwin backend's build-portability decision: a cgo build would lose
// easy cross-compilation). golang.org/x/sys/windows already wraps a good
// slice of the surface this package needs (RtlGetVersion, OpenProcessToken
// + Token.IsElevated, WTSGetActiveConsoleSessionId, ProcessIdToSessionId,
// GetCurrentProcessId/Token) - those are called directly, no rebinding
// here. ffi.go centralizes only the APIs golang.org/x/sys/windows does NOT
// wrap (SendInput, GetAsyncKeyState, display enumeration/DPI, the input
// desktop probe, GetCursorPos, the clipboard family) so the other files
// (keyboard.go, mouse.go, clipboard.go, session.go, backend.go,
// preflight.go) call one shared, already-resolved surface instead of each
// re-declaring procs.
package windows

import "golang.org/x/sys/windows"

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	shcore   = windows.NewLazySystemDLL("shcore.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")

	// SendInput / physical key state (keyboard.go, mouse.go, probes.go).
	procSendInput        = user32.NewProc("SendInput")
	procGetAsyncKeyState = user32.NewProc("GetAsyncKeyState")

	// Displays and DPI (backend.go's activeDisplays / dpiScale).
	procEnumDisplayMonitors = user32.NewProc("EnumDisplayMonitors")
	procGetMonitorInfoW     = user32.NewProc("GetMonitorInfoW")
	procGetDpiForMonitor    = shcore.NewProc("GetDpiForMonitor")

	// Pointer position and virtual-desktop metrics (backend.go/mouse.go).
	procGetCursorPos     = user32.NewProc("GetCursorPos")
	procGetSystemMetrics = user32.NewProc("GetSystemMetrics")

	// Per-Monitor-V2 DPI awareness runtime fallback (init.go).
	procSetProcessDpiAwarenessContext = user32.NewProc("SetProcessDpiAwarenessContext")

	// Input-desktop session probe (session.go).
	procOpenInputDesktop          = user32.NewProc("OpenInputDesktop")
	procCloseDesktop              = user32.NewProc("CloseDesktop")
	procGetUserObjectInformationW = user32.NewProc("GetUserObjectInformationW")

	// Clipboard (clipboard.go).
	procOpenClipboard            = user32.NewProc("OpenClipboard")
	procEmptyClipboard           = user32.NewProc("EmptyClipboard")
	procGetClipboardData         = user32.NewProc("GetClipboardData")
	procSetClipboardData         = user32.NewProc("SetClipboardData")
	procRegisterClipboardFormatW = user32.NewProc("RegisterClipboardFormatW")
	procCloseClipboard           = user32.NewProc("CloseClipboard")
	procGlobalAlloc              = kernel32.NewProc("GlobalAlloc")
	procGlobalLock               = kernel32.NewProc("GlobalLock")
	procGlobalUnlock             = kernel32.NewProc("GlobalUnlock")
	procGlobalFree               = kernel32.NewProc("GlobalFree")

	// Named-pipe busy-retry (bridge_pipe.go's DialBridge). x/sys/windows
	// does not wrap WaitNamedPipe.
	procWaitNamedPipeW = kernel32.NewProc("WaitNamedPipeW")

	// Window enumeration/focus/capture (windows.go, capture.go). Not
	// wrapped by golang.org/x/sys/windows (ticket Codebase Findings).
	procSetForegroundWindow  = user32.NewProc("SetForegroundWindow")
	procShowWindow           = user32.NewProc("ShowWindow")
	procGetWindowTextW       = user32.NewProc("GetWindowTextW")
	procGetWindowTextLengthW = user32.NewProc("GetWindowTextLengthW")
	procGetWindowLongPtrW    = user32.NewProc("GetWindowLongPtrW")
	procAttachThreadInput    = user32.NewProc("AttachThreadInput")
	procIsIconic             = user32.NewProc("IsIconic")
	procPrintWindow          = user32.NewProc("PrintWindow")

	// GDI capture family (capture.go).
	procGetDC                  = user32.NewProc("GetDC")
	procReleaseDC              = user32.NewProc("ReleaseDC")
	procCreateCompatibleDC     = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	procSelectObject           = gdi32.NewProc("SelectObject")
	procDeleteDC               = gdi32.NewProc("DeleteDC")
	procDeleteObject           = gdi32.NewProc("DeleteObject")
	procBitBlt                 = gdi32.NewProc("BitBlt")
	procGetDIBits              = gdi32.NewProc("GetDIBits")

	// open.go's ShellExecuteExW; x/sys/windows only wraps the simpler
	// ShellExecuteW, not the Ex variant that yields hProcess.
	procShellExecuteExW = shell32.NewProc("ShellExecuteExW")
)

// point32 is a Win32 POINT (LONG x, y) - GetCursorPos's out-param shape.
// golang.org/x/sys/windows has no exported POINT type to reuse.
type point32 struct {
	X, Y int32
}

// monitorInfo is a Win32 MONITORINFO (not the *EX variant - device name is
// not needed, only geometry/flags). windows.Rect (Left/Top/Right/Bottom
// int32) matches RECT's layout exactly, so it is reused here rather than
// redeclared.
type monitorInfo struct {
	cbSize    uint32
	rcMonitor windows.Rect
	rcWork    windows.Rect
	dwFlags   uint32
}

const monitorinfofPrimary = 0x00000001

// MONITOR_DEFAULTTONEAREST for EnumDisplayMonitors's hdcMonitor arg is not
// used here (EnumDisplayMonitors is always called with hdc=0 to enumerate
// every display regardless of a clip region).

// MDT_EFFECTIVE_DPI (shcore.h) - GetDpiForMonitor's dpiType, the DPI that
// determines qdisp's scale= (help-windows.txt DPI AND COORDINATES: "1.25 =
// 125%").
const mdtEffectiveDpi = 0

// GetSystemMetrics indices for the virtual desktop bounding box
// (mouse.go's absolute-coordinate normalization).
const (
	smXvirtualscreen  = 76
	smYvirtualscreen  = 77
	smCxvirtualscreen = 78
	smCyvirtualscreen = 79
)

// DESKTOP_READOBJECTS: the minimal access right needed to read an input
// desktop's name via GetUserObjectInformationW (session.go); this must
// succeed even on the Winlogon secure desktop, which a normal user has no
// switch/write rights to.
const desktopReadobjects = 0x0001

// UOI_NAME: GetUserObjectInformationW's index for the desktop/window
// station's name (session.go).
const uoiName = 2

// CF_UNICODETEXT / GMEM_MOVEABLE (clipboard.go).
const (
	cfUnicodeText = 13
	cfDIB         = 8
	gmemMoveable  = 0x0002
)

// INPUT.type values (WinUser.h).
const (
	inputMouse    = 0
	inputKeyboard = 1
)

// KEYBDINPUT.dwFlags bits (WinUser.h) - keyboard.go.
const (
	keyeventfExtendedkey = 0x0001
	keyeventfKeyup       = 0x0002
	keyeventfUnicode     = 0x0004
	keyeventfScancode    = 0x0008
)

// MOUSEINPUT.dwFlags bits (WinUser.h) - mouse.go.
const (
	mouseeventfMove        = 0x0001
	mouseeventfLeftdown    = 0x0002
	mouseeventfLeftup      = 0x0004
	mouseeventfRightdown   = 0x0008
	mouseeventfRightup     = 0x0010
	mouseeventfMiddledown  = 0x0020
	mouseeventfMiddleup    = 0x0040
	mouseeventfWheel       = 0x0800
	mouseeventfHwheel      = 0x1000
	mouseeventfAbsolute    = 0x8000
	mouseeventfVirtualdesk = 0x4000
)

// mouseInput is a Win32 MOUSEINPUT (WinUser.h).
type mouseInput struct {
	dx, dy      int32
	mouseData   uint32
	dwFlags     uint32
	time        uint32
	dwExtraInfo uintptr
}

// keybdInput is a Win32 KEYBDINPUT (WinUser.h).
type keybdInput struct {
	wVk         uint16
	wScan       uint16
	dwFlags     uint32
	time        uint32
	dwExtraInfo uintptr
}

// mouseInputRecord / keybdInputRecord are Go's stand-in for the C union
// INPUT { DWORD type; union { MOUSEINPUT mi; KEYBDINPUT ki; ... }; } - Go
// has no union, so each variant is a separate struct with the SAME total
// size as the real INPUT struct (40 bytes on amd64: 4-byte type + 4-byte
// alignment padding + a 32-byte union, the size of the largest member,
// MOUSEINPUT). mouseInputRecord reaches 40 bytes on its own (Go
// auto-pads `mi` to an 8-byte boundary after `typ`, and MOUSEINPUT's own
// size rounds up to 32 for the same reason - its dwExtraInfo field is a
// uintptr). keybdInputRecord's `ki` is naturally smaller (20 bytes,
// rounded by Go to 24), so an explicit 8-byte `padding` field brings the
// struct to the same 40 bytes SendInput's cbSize parameter expects
// regardless of which variant is sent - keyboard.go/mouse_test.go assert
// this equality so a future field reordering cannot silently regress it.
type mouseInputRecord struct {
	typ uint32
	mi  mouseInput
}

type keybdInputRecord struct {
	typ     uint32
	ki      keybdInput
	padding uint64
}

// SEE_MASK_NOCLOSEPROCESS (shellapi.h) - ShellExecuteExW's fMask bit that
// asks it to hand back hProcess (open.go needs the launched process's PID
// for open[wait=]'s PID-then-basename window resolution). SW_SHOWNORMAL is
// ShellExecuteExW's nShow.
const (
	seeMaskNocloseprocess = 0x00000040
	swShownormal          = 1
)

// shellExecuteInfoW is a hand-rolled Win32 SHELLEXECUTEINFOW (shellapi.h);
// x/sys/windows only wraps the simpler ShellExecuteW, not the Ex variant
// this ticket needs for hProcess. Field order/types mirror the C struct
// exactly so Go's natural (unpacked) alignment reproduces the real ABI
// layout - ffi_test.go/exec_test.go pin its size (112 bytes on amd64) the
// same way ffi.go's mouseInputRecord/keybdInputRecord sizes are pinned.
type shellExecuteInfoW struct {
	cbSize       uint32
	fMask        uint32
	hwnd         uintptr
	lpVerb       *uint16
	lpFile       *uint16
	lpParameters *uint16
	lpDirectory  *uint16
	nShow        int32
	hInstApp     uintptr
	lpIDList     uintptr
	lpClass      *uint16
	hkeyClass    uintptr
	dwHotKey     uint32
	hIcon        uintptr
	hProcess     uintptr
}
