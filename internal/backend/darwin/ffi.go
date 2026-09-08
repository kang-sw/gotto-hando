//go:build darwin

// Package darwin implements backend.Backend for macOS using purego dlopen
// bindings only (no cgo, per the ticket's build-portability decision: a
// cgo build would lose easy cross-compilation for the other platform
// tickets). Every macOS API this package calls goes through purego
// (CoreGraphics, CoreFoundation, Carbon and the objc runtime for
// AppKit/NSPasteboard); ffi.go centralizes the dlopen handles and the raw
// C-function bindings so the other files (keyboard.go, mouse.go,
// clipboard.go, session.go, backend.go, preflight.go) call one shared,
// already-initialized surface instead of each re-registering symbols.
package darwin

import (
	"fmt"
	"structs"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// CoreFoundation geometry structs. purego's macOS (amd64/arm64) support
// passes/returns these by value like a native C call would (Tier 1
// struct-by-value support, purego README "Support Notes"); the
// structs.HostLayout marker is required for purego to lay them out like C
// would. The marker MUST be the struct's FIRST field, not trailing: Go
// pads a struct whose *last* field has size zero (to keep a pointer to
// that field from aliasing past the allocation), which silently inflates
// e.g. a 2x-float64 struct from 16 to 24 bytes and pushes it past
// purego's 16-byte register-return fast path into a code path that
// doesn't handle a 2-float64 shape (panic: "not reached" in
// getStruct/struct_arm64.go). purego's own syscall.go places the marker
// first for the same reason - follow that convention here.
type cgPoint struct {
	_    structs.HostLayout
	X, Y float64
}

type cgSize struct {
	_    structs.HostLayout
	W, H float64
}

type cgRect struct {
	_      structs.HostLayout
	Origin cgPoint
	Size   cgSize
}

// CGEventSourceStateID: kCGEventSourceStateHIDSystemState (the physical
// hardware state, independent of any event tap) - Preflight check 3 needs
// the PHYSICAL key/button state, not the session's synthetic-event view.
const cgEventSourceStateHIDSystemState = 1

// CGEventTapLocation: kCGHIDEventTap - inject at the lowest (hardware)
// level so events reach every app the same way physical input would.
const cgHIDEventTap = 0

const kCFStringEncodingUTF8 = 0x08000100

var (
	// Raw dlopen handles, kept only for RegisterLibFunc calls below; no
	// other file needs them directly.
	libCG     uintptr
	libCF     uintptr
	libCarbon uintptr
	libAppSvc uintptr

	// CoreFoundation
	cfStringCreateWithCString func(alloc uintptr, cstr string, encoding uint32) uintptr
	cfDictionaryGetValue      func(dict, key uintptr) uintptr
	cfBooleanGetValue         func(b uintptr) bool
	cfRelease                 func(cf uintptr)

	// CoreFoundation: collection/number/string/data readers used by the
	// Phase 2 window enumeration and capture pixel read-back.
	cfArrayGetCount        func(arr uintptr) int
	cfArrayGetValueAtIndex func(arr uintptr, idx int) uintptr
	cfNumberGetValue       func(num uintptr, theType int32, valuePtr unsafe.Pointer) bool
	cfStringGetCString     func(str uintptr, buffer *byte, bufferSize int, encoding uint32) bool
	cfDataGetLength        func(data uintptr) int
	// cfDataGetBytePtr returns *byte (not uintptr) so the capture pixel
	// read-back builds a []byte with unsafe.Slice without a vet-flagged
	// uintptr->unsafe.Pointer conversion.
	cfDataGetBytePtr func(data uintptr) *byte

	// CoreGraphics: session / displays
	cgSessionCopyCurrentDictionary func() uintptr
	cgGetActiveDisplayList         func(maxDisplays uint32, displays *uint32, displayCount *uint32) int32
	cgDisplayBounds                func(display uint32) cgRect
	cgMainDisplayID                func() uint32
	cgDisplayScreenSize            func(display uint32) cgSize

	// CoreGraphics: permissions
	cgPreflightScreenCaptureAccess func() bool
	cgRequestScreenCaptureAccess   func() bool

	// CoreGraphics: window list + screenshot capture (Phase 2). The
	// CGWindowListCreateImage / CGDisplayCreateImage family is knowingly
	// deprecated since macOS 14.4 (ticket Decision: ScreenCaptureKit is
	// post-v1); they remain synchronous purego-bindable calls.
	cgWindowListCopyWindowInfo  func(option uint32, relativeToWindow uint32) uintptr
	cgWindowListCreateImage     func(screenBounds cgRect, listOption uint32, windowID uint32, imageOption uint32) uintptr
	cgDisplayCreateImage        func(display uint32) uintptr
	cgDisplayCreateImageForRect func(display uint32, rect cgRect) uintptr
	cgImageGetWidth             func(img uintptr) int
	cgImageGetHeight            func(img uintptr) int
	cgImageGetBytesPerRow       func(img uintptr) int
	cgImageGetBitsPerPixel      func(img uintptr) int
	cgImageGetDataProvider      func(img uintptr) uintptr
	cgDataProviderCopyData      func(provider uintptr) uintptr

	// CoreGraphics: physical key/button state (Preflight check 3)
	cgEventSourceKeyState    func(stateID int32, key uint16) bool
	cgEventSourceButtonState func(stateID int32, button uint32) bool

	// CoreGraphics: event creation/posting (keyboard.go, mouse.go)
	cgEventSourceCreate             func(stateID int32) uintptr
	cgEventCreateKeyboardEvent      func(source uintptr, keycode uint16, keyDown bool) uintptr
	cgEventKeyboardSetUnicodeString func(event uintptr, length uint32, unicodeString *uint16)
	cgEventSetFlags                 func(event uintptr, flags uint64)
	cgEventSetIntegerValueField     func(event uintptr, field uint32, value int64)
	// cgEventGetIntegerValueField is used only by ffi_test.go/mouse_test.go
	// to read back a synthesized event's fields (e.g. the scroll-wheel
	// delta axes) without needing to post it - CGEventCreate* does not
	// require an unlocked session or Accessibility, only CGEventPost does.
	cgEventGetIntegerValueField     func(event uintptr, field uint32) int64
	cgEventPost                     func(tap int32, event uintptr)
	cgEventCreateMouseEvent         func(source uintptr, mouseType int32, point cgPoint, mouseButton uint32) uintptr
	cgEventCreateScrollWheelEvent   func(source uintptr, units int32, wheelCount uint32, wheel1 int32) uintptr
	cgEventCreateScrollWheelEventXY func(source uintptr, units int32, wheelCount uint32, wheel1, wheel2 int32) uintptr

	// cgEventCreateScrollWheelEventXYVariadicARM64 is CGEventCreateScrollWheelEvent's
	// C-*variadic* prototype: only source/units/wheelCount/wheel1 are named
	// parameters; wheel2/wheel3 are "..." (CGEvent.h). On Apple's arm64 ABI
	// (unlike the standard AAPCS64 that purego otherwise emulates), a
	// variadic function's "..." tail is ALWAYS passed on the stack, never
	// in a register, no matter how many integer registers are still free -
	// see "Writing ARM64 Code for Apple Platforms" > Function Calling
	// Conventions. cgEventCreateScrollWheelEventXY above binds wheel2 as a
	// fifth named parameter, so purego places it in a register (x4) same
	// as any other arg; the callee's va_arg for wheel2 reads the stack
	// instead and gets garbage/zero - confirmed empirically on this
	// darwin/arm64 machine with a small variadic C probe function dlopen'd
	// standalone (no GUI/CGEventPost needed), see the fix commit's ## AI
	// Context. Passing wheel2 through a `...any` tail alone is NOT enough
	// either: purego's own variadic support (func.go RegisterFunc) still
	// places values pulled from the `...any` slice in a free register when
	// one is available - the same probe showed a plain `...any` binding
	// (no padding) failing the same way as the fixed-arity one. The p1-p4
	// uintptr padding parameters exhaust arm64's 8 integer argument
	// registers (source/units/wheelCount/wheel1 already use 4, so 4 more
	// padding params reach 8) before wheel2 is added, which makes purego's
	// own register-vs-stack bookkeeping (func.go addInt) route wheel2 to
	// the stack - the same place the real ABI puts it. The padding values
	// are always 0 and never read by the callee (a true C variadic call's
	// va_start/va_arg begins at the first stack slot, ignoring any spare
	// registers). amd64 has no such stack-forcing rule for variadic
	// arguments (System V ABI treats them like any other argument), so
	// cgEventCreateScrollWheelEventXY above is correct as-is there; this
	// binding is used only on arm64 (mouse.go's Scroll, runtime.GOARCH
	// check).
	cgEventCreateScrollWheelEventXYVariadicARM64 func(source uintptr, units int32, wheelCount uint32, wheel1 int32, p1, p2, p3, p4 uintptr, extra ...any) uintptr

	cgEventCreate      func(source uintptr) uintptr
	cgEventGetLocation func(event uintptr) cgPoint

	// ApplicationServices: Accessibility
	axIsProcessTrusted            func() bool
	axIsProcessTrustedWithOptions func(options uintptr) bool

	// ApplicationServices: AXUIElement family (window minimize/raise/focus,
	// title correlation - Phase 2). The AXError return is 0 (kAXErrorSuccess)
	// on success.
	axUIElementCreateApplication  func(pid int32) uintptr
	axUIElementCopyAttributeValue func(element, attribute uintptr, value *uintptr) int32
	axUIElementSetAttributeValue  func(element, attribute, value uintptr) int32
	axUIElementPerformAction      func(element, action uintptr) int32

	// Carbon: Secure Input
	isSecureEventInputEnabled func() bool
)

var ffiOnce sync.Once
var ffiErr error

// initFFI dlopens the frameworks and registers every C function this
// package uses. Called once from New(); every field above is valid once it
// returns nil.
func initFFI() error {
	ffiOnce.Do(func() {
		ffiErr = dlopenAll()
		if ffiErr != nil {
			return
		}
		registerCoreFoundation()
		registerCoreGraphics()
		registerApplicationServices()
		registerCarbon()
	})
	return ffiErr
}

func dlopenAll() error {
	open := func(path string) (uintptr, error) {
		h, err := purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			return 0, fmt.Errorf("dlopen %s: %w", path, err)
		}
		return h, nil
	}
	var err error
	if libCF, err = open("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation"); err != nil {
		return err
	}
	if libCG, err = open("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics"); err != nil {
		return err
	}
	if libCarbon, err = open("/System/Library/Frameworks/Carbon.framework/Carbon"); err != nil {
		return err
	}
	if libAppSvc, err = open("/System/Library/Frameworks/ApplicationServices.framework/ApplicationServices"); err != nil {
		return err
	}
	return nil
}

func registerCoreFoundation() {
	purego.RegisterLibFunc(&cfStringCreateWithCString, libCF, "CFStringCreateWithCString")
	purego.RegisterLibFunc(&cfDictionaryGetValue, libCF, "CFDictionaryGetValue")
	purego.RegisterLibFunc(&cfBooleanGetValue, libCF, "CFBooleanGetValue")
	purego.RegisterLibFunc(&cfRelease, libCF, "CFRelease")
	purego.RegisterLibFunc(&cfArrayGetCount, libCF, "CFArrayGetCount")
	purego.RegisterLibFunc(&cfArrayGetValueAtIndex, libCF, "CFArrayGetValueAtIndex")
	purego.RegisterLibFunc(&cfNumberGetValue, libCF, "CFNumberGetValue")
	purego.RegisterLibFunc(&cfStringGetCString, libCF, "CFStringGetCString")
	purego.RegisterLibFunc(&cfDataGetLength, libCF, "CFDataGetLength")
	purego.RegisterLibFunc(&cfDataGetBytePtr, libCF, "CFDataGetBytePtr")
}

func registerCoreGraphics() {
	purego.RegisterLibFunc(&cgSessionCopyCurrentDictionary, libCG, "CGSessionCopyCurrentDictionary")
	purego.RegisterLibFunc(&cgGetActiveDisplayList, libCG, "CGGetActiveDisplayList")
	purego.RegisterLibFunc(&cgDisplayBounds, libCG, "CGDisplayBounds")
	purego.RegisterLibFunc(&cgMainDisplayID, libCG, "CGMainDisplayID")
	purego.RegisterLibFunc(&cgDisplayScreenSize, libCG, "CGDisplayScreenSize")
	purego.RegisterLibFunc(&cgPreflightScreenCaptureAccess, libCG, "CGPreflightScreenCaptureAccess")
	purego.RegisterLibFunc(&cgRequestScreenCaptureAccess, libCG, "CGRequestScreenCaptureAccess")
	purego.RegisterLibFunc(&cgWindowListCopyWindowInfo, libCG, "CGWindowListCopyWindowInfo")
	purego.RegisterLibFunc(&cgWindowListCreateImage, libCG, "CGWindowListCreateImage")
	purego.RegisterLibFunc(&cgDisplayCreateImage, libCG, "CGDisplayCreateImage")
	purego.RegisterLibFunc(&cgDisplayCreateImageForRect, libCG, "CGDisplayCreateImageForRect")
	purego.RegisterLibFunc(&cgImageGetWidth, libCG, "CGImageGetWidth")
	purego.RegisterLibFunc(&cgImageGetHeight, libCG, "CGImageGetHeight")
	purego.RegisterLibFunc(&cgImageGetBytesPerRow, libCG, "CGImageGetBytesPerRow")
	purego.RegisterLibFunc(&cgImageGetBitsPerPixel, libCG, "CGImageGetBitsPerPixel")
	purego.RegisterLibFunc(&cgImageGetDataProvider, libCG, "CGImageGetDataProvider")
	purego.RegisterLibFunc(&cgDataProviderCopyData, libCG, "CGDataProviderCopyData")
	purego.RegisterLibFunc(&cgEventSourceKeyState, libCG, "CGEventSourceKeyState")
	purego.RegisterLibFunc(&cgEventSourceButtonState, libCG, "CGEventSourceButtonState")
	purego.RegisterLibFunc(&cgEventSourceCreate, libCG, "CGEventSourceCreate")
	purego.RegisterLibFunc(&cgEventCreateKeyboardEvent, libCG, "CGEventCreateKeyboardEvent")
	purego.RegisterLibFunc(&cgEventKeyboardSetUnicodeString, libCG, "CGEventKeyboardSetUnicodeString")
	purego.RegisterLibFunc(&cgEventSetFlags, libCG, "CGEventSetFlags")
	purego.RegisterLibFunc(&cgEventSetIntegerValueField, libCG, "CGEventSetIntegerValueField")
	purego.RegisterLibFunc(&cgEventGetIntegerValueField, libCG, "CGEventGetIntegerValueField")
	purego.RegisterLibFunc(&cgEventPost, libCG, "CGEventPost")
	purego.RegisterLibFunc(&cgEventCreateMouseEvent, libCG, "CGEventCreateMouseEvent")
	purego.RegisterLibFunc(&cgEventCreateScrollWheelEvent, libCG, "CGEventCreateScrollWheelEvent")
	purego.RegisterLibFunc(&cgEventCreateScrollWheelEventXY, libCG, "CGEventCreateScrollWheelEvent")
	purego.RegisterLibFunc(&cgEventCreateScrollWheelEventXYVariadicARM64, libCG, "CGEventCreateScrollWheelEvent")
	purego.RegisterLibFunc(&cgEventCreate, libCG, "CGEventCreate")
	purego.RegisterLibFunc(&cgEventGetLocation, libCG, "CGEventGetLocation")
}

func registerApplicationServices() {
	purego.RegisterLibFunc(&axIsProcessTrusted, libAppSvc, "AXIsProcessTrusted")
	purego.RegisterLibFunc(&axIsProcessTrustedWithOptions, libAppSvc, "AXIsProcessTrustedWithOptions")
	purego.RegisterLibFunc(&axUIElementCreateApplication, libAppSvc, "AXUIElementCreateApplication")
	purego.RegisterLibFunc(&axUIElementCopyAttributeValue, libAppSvc, "AXUIElementCopyAttributeValue")
	purego.RegisterLibFunc(&axUIElementSetAttributeValue, libAppSvc, "AXUIElementSetAttributeValue")
	purego.RegisterLibFunc(&axUIElementPerformAction, libAppSvc, "AXUIElementPerformAction")
}

func registerCarbon() {
	purego.RegisterLibFunc(&isSecureEventInputEnabled, libCarbon, "IsSecureEventInputEnabled")
}

// cfString creates a CFStringRef (which is toll-free bridged to NSString)
// from a Go string. The caller owns the returned reference (CF "Create
// Rule") and must cfRelease it once done.
func cfString(s string) uintptr {
	return cfStringCreateWithCString(0, s, kCFStringEncodingUTF8)
}

// CFNumber type identifiers (CFNumber.h) used by the window-info readers.
const (
	kCFNumberSInt64Type  = 4
	kCFNumberFloat64Type = 6
)

// cfStringGo copies a CFStringRef into a Go string via CFStringGetCString
// (up to 1 KiB, enough for a window title). A 0 ref or a failed copy reads
// as "" - a missing/empty title is not an error (help-macos.txt:21-24:
// titles come back empty without Screen Recording).
func cfStringGo(ref uintptr) string {
	if ref == 0 {
		return ""
	}
	var buf [1024]byte
	if !cfStringGetCString(ref, &buf[0], len(buf), kCFStringEncodingUTF8) {
		return ""
	}
	n := 0
	for n < len(buf) && buf[n] != 0 {
		n++
	}
	return string(buf[:n])
}

// cfNumberInt64 reads a CFNumberRef as an int64 (window id, pid, layer). A
// 0 ref or a failed read yields 0.
func cfNumberInt64(ref uintptr) int64 {
	if ref == 0 {
		return 0
	}
	var v int64
	if !cfNumberGetValue(ref, kCFNumberSInt64Type, unsafe.Pointer(&v)) {
		return 0
	}
	return v
}

// cfNumberFloat64 reads a CFNumberRef as a float64 (window bounds fields).
func cfNumberFloat64(ref uintptr) float64 {
	if ref == 0 {
		return 0
	}
	var v float64
	if !cfNumberGetValue(ref, kCFNumberFloat64Type, unsafe.Pointer(&v)) {
		return 0
	}
	return v
}
