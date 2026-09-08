//go:build windows

package windows

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unsafe"

	"github.com/kang-sw/gotto-hando/internal/backend"
	"github.com/kang-sw/gotto-hando/internal/ir"
	"golang.org/x/sys/windows"
)

// GWL_EXSTYLE (GetWindowLongPtrW's nIndex) and WS_EX_TOOLWINDOW (WinUser.h) -
// qwin excludes tool windows (help-windows.txt DPI AND COORDINATES).
// gwlExstyle is nIndex's 32-bit two's-complement bit pattern (same idiom
// x/sys/windows uses for STD_INPUT_HANDLE = -10 & (1<<32-1)): converting the
// literal -20 directly to uintptr is a compile-time "constant overflows
// uintptr" error, and GetWindowLongPtrW's callee only reads the low 32 bits
// of the pointer-sized argument slot regardless of the upper bits' zero- vs
// sign-extension.
const (
	gwlExstyle     = -20 & (1<<32 - 1)
	wsExToolwindow = 0x00000080
)

// SW_RESTORE (WinUser.h) - ShowWindow's nCmdShow for Focus (un-minimize
// before raising, matching darwin's AXMinimized=false step).
const swRestore = 9

// Windows enumerates top-level windows via EnumWindows (which already
// visits them in Z order, front-to-back, matching the engine's
// frontmost-wins rule, help.txt WINDOW SELECTORS :294) and applies the qwin
// filter (visible, not cloaked, non-empty title, not a tool window -
// help-windows.txt DPI AND COORDINATES) before the WINDOW SELECTOR match
// (help.txt :286-297).
func (b *Backend) Windows(ctx context.Context, sel ir.Selector) ([]backend.Window, error) {
	all := enumerateWindows()
	return filterWindows(all, sel), nil
}

// enumerateWindows walks every top-level window via EnumWindows and keeps
// the ones passing the qwin filter, building each as a backend.Window. Pure
// FFI glue (no selector matching here) so filterWindows/selectorMatches
// stay unit-testable off a synthetic list, same split as darwin's Windows.
func enumerateWindows() []backend.Window {
	var all []backend.Window
	fg := windows.GetForegroundWindow()
	cb := windows.NewCallback(func(hwnd windows.HWND, lparam uintptr) uintptr {
		if w, ok := windowFromHWND(hwnd, fg); ok {
			all = append(all, w)
		}
		return 1 // continue enumeration
	})
	// EnumWindows failing is not a hard error worth surfacing (an empty
	// result behaves like "no windows"); its own error is intentionally
	// dropped, matching darwin's Windows() returning (nil, nil) on an empty
	// CGWindowListCopyWindowInfo.
	_ = windows.EnumWindows(cb, nil)
	return all
}

// windowFromHWND applies the qwin filter to one HWND (visible, not cloaked,
// non-empty title, not a tool window) and, when it passes, builds its
// backend.Window. Hidden always stays false: the qwin filter already
// excludes anything that would otherwise need it set (invisible or cloaked
// windows never reach here).
func windowFromHWND(hwnd, fg windows.HWND) (backend.Window, bool) {
	if !windows.IsWindowVisible(hwnd) {
		return backend.Window{}, false
	}
	var cloaked uint32
	if err := windows.DwmGetWindowAttribute(hwnd, windows.DWMWA_CLOAKED,
		unsafe.Pointer(&cloaked), uint32(unsafe.Sizeof(cloaked))); err == nil && cloaked != 0 {
		return backend.Window{}, false
	}
	title := windowText(hwnd)
	if title == "" {
		return backend.Window{}, false
	}
	exStyle, _, _ := procGetWindowLongPtrW.Call(uintptr(hwnd), uintptr(gwlExstyle))
	if uint32(exStyle)&wsExToolwindow != 0 {
		return backend.Window{}, false
	}

	var pid uint32
	windows.GetWindowThreadProcessId(hwnd, &pid)

	var rect windows.Rect
	// DWM extended frame bounds excludes the invisible drop-shadow border
	// (help-windows.txt DPI AND COORDINATES); a query failure leaves rect
	// zeroed rather than dropping the window - an unqueryable frame is rare
	// (only ever seen on an already-destroyed hwnd) and still a legitimate
	// qwin match by title/app/id/pid.
	windows.DwmGetWindowAttribute(hwnd, windows.DWMWA_EXTENDED_FRAME_BOUNDS,
		unsafe.Pointer(&rect), uint32(unsafe.Sizeof(rect)))

	minimized, _, _ := procIsIconic.Call(uintptr(hwnd))

	return backend.Window{
		ID:        int(hwnd),
		PID:       int(pid),
		App:       imageBasename(pid),
		Title:     title,
		X:         int(rect.Left),
		Y:         int(rect.Top),
		W:         int(rect.Right - rect.Left),
		H:         int(rect.Bottom - rect.Top),
		Minimized: minimized != 0,
		Focused:   hwnd == fg,
		Hidden:    false,
	}, true
}

// windowText reads a window's title via GetWindowTextLengthW +
// GetWindowTextW; "" for a title-less (or unqueryable) window.
func windowText(hwnd windows.HWND) string {
	n, _, _ := procGetWindowTextLengthW.Call(uintptr(hwnd))
	if n == 0 {
		return ""
	}
	buf := make([]uint16, n+1)
	procGetWindowTextW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return windows.UTF16ToString(buf)
}

// imageBasename resolves a process id to its executable's base name (e.g.
// "notepad.exe") via OpenProcess + QueryFullProcessImageName; "" when the
// process cannot be opened (a protected/elevated process from a
// non-elevated caller - a window still enumerates, just without an App
// value, degrading like darwin's empty-App-without-permission case).
func imageBasename(pid uint32) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_PATH)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return ""
	}
	return filepath.Base(windows.UTF16ToString(buf[:size]))
}

// Focus brings w to the foreground (help-windows.txt CAVEATS): un-minimize
// (SW_RESTORE) then SetForegroundWindow; a plain SetForegroundWindow call
// often only flashes the taskbar button (a Windows foreground-lock
// restriction), so a failed attempt retries once via input-thread
// attachment (AttachThreadInput) plus a synthetic Alt tap - the documented
// mechanism, verbatim - before giving up. Returns an error (mapped to
// E_NOWINDOW by internal/engine/run.go doFocus) if the window still is not
// foreground after the retry.
func (b *Backend) Focus(ctx context.Context, w backend.Window) error {
	hwnd := windows.HWND(w.ID)
	procShowWindow.Call(uintptr(hwnd), swRestore)
	procSetForegroundWindow.Call(uintptr(hwnd))
	if windows.GetForegroundWindow() == hwnd {
		return nil
	}

	fg := windows.GetForegroundWindow()
	var fgPID uint32
	fgTID, _ := windows.GetWindowThreadProcessId(fg, &fgPID)
	curTID := windows.GetCurrentThreadId()

	procAttachThreadInput.Call(uintptr(curTID), uintptr(fgTID), 1)
	// Synthetic Alt tap: some foreground-lock enforcement relaxes right
	// after the calling thread synthesizes a key event (help-windows.txt
	// CAVEATS). Reuses the same SendInput scan-code path as KeyDown/KeyUp.
	b.KeyDown(ctx, "alt")
	b.KeyUp(ctx, "alt")
	procSetForegroundWindow.Call(uintptr(hwnd))
	procAttachThreadInput.Call(uintptr(curTID), uintptr(fgTID), 0)

	if windows.GetForegroundWindow() != hwnd {
		return fmt.Errorf("window did not come to the foreground after the SetForegroundWindow retry")
	}
	return nil
}

// filterWindows applies a WINDOW SELECTOR to a z-ordered window list
// (help.txt WINDOW SELECTORS :286-297). Pure (no FFI), duplicated verbatim
// from darwin's windows.go per the ticket's per-backend duplication
// precedent (no shared package exists yet for this ~35-line helper).
func filterWindows(wins []backend.Window, sel ir.Selector) []backend.Window {
	var re *regexp.Regexp
	if sel.Kind == "title" && sel.Regex {
		var err error
		if re, err = regexp.Compile(sel.Value); err != nil {
			return nil
		}
	}
	out := make([]backend.Window, 0, len(wins))
	for _, w := range wins {
		if selectorMatches(w, sel, re) {
			out = append(out, w)
		}
	}
	return out
}

func selectorMatches(w backend.Window, sel ir.Selector, re *regexp.Regexp) bool {
	switch sel.Kind {
	case "id":
		return strconv.Itoa(w.ID) == sel.Value
	case "pid":
		return strconv.Itoa(w.PID) == sel.Value
	case "app":
		return strings.Contains(strings.ToLower(w.App), strings.ToLower(sel.Value))
	case "title":
		if re != nil {
			return re.MatchString(w.Title)
		}
		return strings.Contains(strings.ToLower(w.Title), strings.ToLower(sel.Value))
	}
	return false
}
