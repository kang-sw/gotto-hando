//go:build darwin

package darwin

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"

	"github.com/kang-sw/gotto-hando/internal/backend"
	"github.com/kang-sw/gotto-hando/internal/ir"
)

// kCGWindowListOptionAll enumerates every window, on- or off-screen
// (CGWindowListCopyWindowInfo option; help-macos.txt:236-240: qwin lists
// layer-0 regular-app windows including minimized/hidden). 0 == all.
const kCGWindowListOptionAll = 0

// nsApplicationActivationPolicyRegular is NSRunningApplication's regular
// activation policy (AppKit): 0. qwin/win keep only layer-0 windows whose
// owning app is Regular (no menu-bar/dock/agent processes,
// help-macos.txt:236-238).
const nsApplicationActivationPolicyRegular = 0

// nsApplicationActivateIgnoringOtherApps brings an app fully forward on
// activateWithOptions: (AppKit NSApplicationActivationOptions bit 1).
const nsApplicationActivateIgnoringOtherApps = 1 << 1

var (
	windowsOnce sync.Once

	nsRunningApplicationClass objc.Class
	selRunningAppWithPID      objc.SEL
	selActivationPolicy       objc.SEL
	selIsHidden               objc.SEL
	selIsActive               objc.SEL
	selActivateWithOptions    objc.SEL

	// NSNumber/NSDictionary back the CFBoolean and options-dictionary needs
	// (AXMinimized=false, the --request-perms prompt options) without
	// dereferencing CF constant symbols: @YES/@NO are __NSCFBoolean, which
	// is toll-free bridged to CFBoolean, and dictionaryWithObject:forKey:
	// yields a CFDictionary-bridged NSDictionary.
	nsNumberClass        objc.Class
	selNumberWithBool    objc.SEL
	nsDictionaryClass    objc.Class
	selDictWithObjectKey objc.SEL

	// Long-lived CFStrings for the AX attribute/action names (never
	// released; created once). The CFString value of each kAX* constant is
	// its own name, so cfString("AXWindows") reproduces kAXWindowsAttribute
	// exactly (same recreate-the-key trick as session.go).
	axWindowsAttr   uintptr
	axTitleAttr     uintptr
	axMinimizedAttr uintptr
	axRaiseAction   uintptr
)

// initWindows loads AppKit (for NSRunningApplication) and resolves the
// class/selectors and AX attribute strings once. AppKit may already be
// loaded by clipboard.go; a second Dlopen returns the same handle.
func initWindows() {
	windowsOnce.Do(func() {
		if _, err := purego.Dlopen("/System/Library/Frameworks/AppKit.framework/AppKit",
			purego.RTLD_NOW|purego.RTLD_GLOBAL); err != nil {
			panic(err)
		}
		nsRunningApplicationClass = objc.GetClass("NSRunningApplication")
		selRunningAppWithPID = objc.RegisterName("runningApplicationWithProcessIdentifier:")
		selActivationPolicy = objc.RegisterName("activationPolicy")
		selIsHidden = objc.RegisterName("isHidden")
		selIsActive = objc.RegisterName("isActive")
		selActivateWithOptions = objc.RegisterName("activateWithOptions:")

		nsNumberClass = objc.GetClass("NSNumber")
		selNumberWithBool = objc.RegisterName("numberWithBool:")
		nsDictionaryClass = objc.GetClass("NSDictionary")
		selDictWithObjectKey = objc.RegisterName("dictionaryWithObject:forKey:")

		axWindowsAttr = cfString("AXWindows")
		axTitleAttr = cfString("AXTitle")
		axMinimizedAttr = cfString("AXMinimized")
		axRaiseAction = cfString("AXRaise")
	})
}

// cfBoolean returns a CFBooleanRef (via a toll-free-bridged NSNumber
// @YES/@NO) for setting AX attributes / building option dictionaries
// without dereferencing the kCFBoolean* constant symbols.
func cfBoolean(v bool) uintptr {
	return uintptr(objc.ID(nsNumberClass).Send(selNumberWithBool, v))
}

// Windows enumerates layer-0 windows of regular-activation-policy apps and
// applies the WINDOW SELECTOR filter (help.txt WINDOW SELECTORS :286-297,
// help-macos.txt:236-240). Matches are returned front-to-back in z-order,
// as CGWindowListCopyWindowInfo already orders them, so the engine's
// frontmost-wins rule (:294) reads element 0. Titles come back empty
// without Screen Recording (help-macos.txt:21-24); that is not an error.
func (b *Backend) Windows(ctx context.Context, sel ir.Selector) ([]backend.Window, error) {
	initWindows()
	arr := cgWindowListCopyWindowInfo(kCGWindowListOptionAll, 0)
	if arr == 0 {
		return nil, nil
	}
	defer cfRelease(arr)

	runningApps := map[int]objc.ID{}
	axApps := map[int]uintptr{}
	defer func() {
		for _, el := range axApps {
			if el != 0 {
				cfRelease(el)
			}
		}
	}()

	count := cfArrayGetCount(arr)
	var all []backend.Window
	focusedAssigned := false
	for i := 0; i < count; i++ {
		dict := cfArrayGetValueAtIndex(arr, i)
		if int(dictNumberInt64(dict, "kCGWindowLayer")) != 0 {
			continue
		}
		pid := int(dictNumberInt64(dict, "kCGWindowOwnerPID"))
		ra, ok := runningApps[pid]
		if !ok {
			ra = objc.ID(nsRunningApplicationClass).Send(selRunningAppWithPID, int32(pid))
			runningApps[pid] = ra
		}
		if ra == 0 || objc.Send[int](ra, selActivationPolicy) != nsApplicationActivationPolicyRegular {
			continue
		}

		title := dictString(dict, "kCGWindowName")
		x, y, w, h := dictBounds(dict)
		onscreen := dictBool(dict, "kCGWindowIsOnscreen")

		win := backend.Window{
			ID:    int(dictNumberInt64(dict, "kCGWindowNumber")),
			PID:   pid,
			App:   dictString(dict, "kCGWindowOwnerName"),
			Title: title,
			X:     x, Y: y, W: w, H: h,
			Hidden: objc.Send[bool](ra, selIsHidden),
		}
		// Minimized has no CGWindow flag; correlate to the app's AX window
		// by title and read AXMinimized (help-macos.txt:238-240). Only
		// off-screen windows can be minimized, so skip the AX round-trip
		// for on-screen ones.
		if !onscreen {
			appEl, ok := axApps[pid]
			if !ok {
				appEl = axUIElementCreateApplication(int32(pid))
				axApps[pid] = appEl
			}
			win.Minimized = axMinimized(appEl, title)
		}
		// The OS-focused window is the frontmost (first in z-order)
		// on-screen window whose app is active.
		if !focusedAssigned && onscreen && objc.Send[bool](ra, selIsActive) {
			win.Focused = true
			focusedAssigned = true
		}
		all = append(all, win)
	}
	return filterWindows(all, sel), nil
}

// Focus unminimizes (AXMinimized=false), raises (AXRaise) the AX window
// correlated to w by title, and activates its app frontmost
// (help-macos.txt:238-240). Best effort: any AX step that fails is skipped
// and the app activation still runs. macOS gives no reliable "focus
// refused" signal from these calls, so this returns nil (the engine maps a
// no-match list to E_NOWINDOW upstream).
func (b *Backend) Focus(ctx context.Context, w backend.Window) error {
	initWindows()
	appEl := axUIElementCreateApplication(int32(w.PID))
	if appEl != 0 {
		defer cfRelease(appEl)
		var arr uintptr
		if axUIElementCopyAttributeValue(appEl, axWindowsAttr, &arr) == 0 && arr != 0 {
			n := cfArrayGetCount(arr)
			for i := 0; i < n; i++ {
				el := cfArrayGetValueAtIndex(arr, i)
				if axStringAttr(el, axTitleAttr) == w.Title {
					axUIElementSetAttributeValue(el, axMinimizedAttr, cfBoolean(false))
					axUIElementPerformAction(el, axRaiseAction)
					break
				}
			}
			cfRelease(arr)
		}
	}
	ra := objc.ID(nsRunningApplicationClass).Send(selRunningAppWithPID, int32(w.PID))
	if ra != 0 {
		objc.Send[bool](ra, selActivateWithOptions, uint(nsApplicationActivateIgnoringOtherApps))
	}
	return nil
}

// axMinimized reports whether the app's AX window whose AXTitle matches
// title is minimized. A missing AXWindows list or no title match reads as
// false (help-macos.txt duplicate-title caveat: the correlation is a
// heuristic, degrading gracefully).
func axMinimized(appEl uintptr, title string) bool {
	if appEl == 0 {
		return false
	}
	var arr uintptr
	if axUIElementCopyAttributeValue(appEl, axWindowsAttr, &arr) != 0 || arr == 0 {
		return false
	}
	defer cfRelease(arr)
	n := cfArrayGetCount(arr)
	for i := 0; i < n; i++ {
		el := cfArrayGetValueAtIndex(arr, i)
		if axStringAttr(el, axTitleAttr) == title {
			return axBoolAttr(el, axMinimizedAttr)
		}
	}
	return false
}

// axStringAttr reads a CFString-valued AX attribute (owned per the Copy
// Rule, released here); "" when absent.
func axStringAttr(el, attr uintptr) string {
	var val uintptr
	if axUIElementCopyAttributeValue(el, attr, &val) != 0 || val == 0 {
		return ""
	}
	defer cfRelease(val)
	return cfStringGo(val)
}

// axBoolAttr reads a CFBoolean-valued AX attribute; false when absent.
func axBoolAttr(el, attr uintptr) bool {
	var val uintptr
	if axUIElementCopyAttributeValue(el, attr, &val) != 0 || val == 0 {
		return false
	}
	defer cfRelease(val)
	return cfBooleanGetValue(val)
}

// dictValue reads one CFDictionary entry by a Go-string key. It owns and
// releases the created CFString key; the returned value follows the CF Get
// Rule (not owned, not released).
func dictValue(dict uintptr, key string) uintptr {
	k := cfString(key)
	defer cfRelease(k)
	return cfDictionaryGetValue(dict, k)
}

func dictNumberInt64(dict uintptr, key string) int64 { return cfNumberInt64(dictValue(dict, key)) }
func dictString(dict uintptr, key string) string     { return cfStringGo(dictValue(dict, key)) }

func dictBool(dict uintptr, key string) bool {
	v := dictValue(dict, key)
	if v == 0 {
		return false
	}
	return cfBooleanGetValue(v)
}

// dictBounds reads a kCGWindowBounds sub-dictionary (X/Y/Width/Height
// CFNumbers, in logical points) into an integer rectangle.
func dictBounds(dict uintptr) (x, y, w, h int) {
	b := dictValue(dict, "kCGWindowBounds")
	if b == 0 {
		return 0, 0, 0, 0
	}
	return int(cfNumberFloat64(dictValue(b, "X"))),
		int(cfNumberFloat64(dictValue(b, "Y"))),
		int(cfNumberFloat64(dictValue(b, "Width"))),
		int(cfNumberFloat64(dictValue(b, "Height")))
}

// filterWindows applies a WINDOW SELECTOR to a z-ordered window list
// (help.txt WINDOW SELECTORS :286-297). It is pure (no FFI) so the
// matching predicates - id/pid exact, app/title case-insensitive
// substring, title RE2 regex - are unit-tested against synthetic lists. A
// regex that fails to compile matches nothing (validation already rejects
// bad regexes upstream, so this only guards a defensive path).
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
