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

	// CoreGraphics: session / displays
	cgSessionCopyCurrentDictionary func() uintptr
	cgGetActiveDisplayList         func(maxDisplays uint32, displays *uint32, displayCount *uint32) int32
	cgDisplayBounds                func(display uint32) cgRect
	cgMainDisplayID                func() uint32
	cgDisplayScreenSize            func(display uint32) cgSize

	// CoreGraphics: permissions
	cgPreflightScreenCaptureAccess func() bool

	// CoreGraphics: physical key/button state (Preflight check 3)
	cgEventSourceKeyState    func(stateID int32, key uint16) bool
	cgEventSourceButtonState func(stateID int32, button uint32) bool

	// CoreGraphics: event creation/posting (keyboard.go, mouse.go)
	cgEventSourceCreate             func(stateID int32) uintptr
	cgEventCreateKeyboardEvent      func(source uintptr, keycode uint16, keyDown bool) uintptr
	cgEventKeyboardSetUnicodeString func(event uintptr, length uint32, unicodeString *uint16)
	cgEventSetFlags                 func(event uintptr, flags uint64)
	cgEventSetIntegerValueField     func(event uintptr, field uint32, value int64)
	cgEventPost                     func(tap int32, event uintptr)
	cgEventCreateMouseEvent         func(source uintptr, mouseType int32, point cgPoint, mouseButton uint32) uintptr
	cgEventCreateScrollWheelEvent   func(source uintptr, units int32, wheelCount uint32, wheel1 int32) uintptr
	cgEventCreateScrollWheelEventXY func(source uintptr, units int32, wheelCount uint32, wheel1, wheel2 int32) uintptr
	cgEventCreate                   func(source uintptr) uintptr
	cgEventGetLocation              func(event uintptr) cgPoint

	// ApplicationServices: Accessibility
	axIsProcessTrusted func() bool

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
}

func registerCoreGraphics() {
	purego.RegisterLibFunc(&cgSessionCopyCurrentDictionary, libCG, "CGSessionCopyCurrentDictionary")
	purego.RegisterLibFunc(&cgGetActiveDisplayList, libCG, "CGGetActiveDisplayList")
	purego.RegisterLibFunc(&cgDisplayBounds, libCG, "CGDisplayBounds")
	purego.RegisterLibFunc(&cgMainDisplayID, libCG, "CGMainDisplayID")
	purego.RegisterLibFunc(&cgDisplayScreenSize, libCG, "CGDisplayScreenSize")
	purego.RegisterLibFunc(&cgPreflightScreenCaptureAccess, libCG, "CGPreflightScreenCaptureAccess")
	purego.RegisterLibFunc(&cgEventSourceKeyState, libCG, "CGEventSourceKeyState")
	purego.RegisterLibFunc(&cgEventSourceButtonState, libCG, "CGEventSourceButtonState")
	purego.RegisterLibFunc(&cgEventSourceCreate, libCG, "CGEventSourceCreate")
	purego.RegisterLibFunc(&cgEventCreateKeyboardEvent, libCG, "CGEventCreateKeyboardEvent")
	purego.RegisterLibFunc(&cgEventKeyboardSetUnicodeString, libCG, "CGEventKeyboardSetUnicodeString")
	purego.RegisterLibFunc(&cgEventSetFlags, libCG, "CGEventSetFlags")
	purego.RegisterLibFunc(&cgEventSetIntegerValueField, libCG, "CGEventSetIntegerValueField")
	purego.RegisterLibFunc(&cgEventPost, libCG, "CGEventPost")
	purego.RegisterLibFunc(&cgEventCreateMouseEvent, libCG, "CGEventCreateMouseEvent")
	purego.RegisterLibFunc(&cgEventCreateScrollWheelEvent, libCG, "CGEventCreateScrollWheelEvent")
	purego.RegisterLibFunc(&cgEventCreateScrollWheelEventXY, libCG, "CGEventCreateScrollWheelEvent")
	purego.RegisterLibFunc(&cgEventCreate, libCG, "CGEventCreate")
	purego.RegisterLibFunc(&cgEventGetLocation, libCG, "CGEventGetLocation")
}

func registerApplicationServices() {
	purego.RegisterLibFunc(&axIsProcessTrusted, libAppSvc, "AXIsProcessTrusted")
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
