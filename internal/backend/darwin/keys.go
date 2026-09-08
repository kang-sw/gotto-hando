//go:build darwin

package darwin

import "strconv"

// keyCodes maps every canonical IR key symbol (help.txt KEY NAMES,
// :448-458) to its macOS virtual keycode (kVK_* in
// <Carbon/HIToolbox/Events.h>; the values are the standard ANSI-US
// physical-position table, hardcoded here since HIToolbox does not export
// them as dlsym-able symbols).
//
// Cross-reference: this table must enumerate the SAME symbol set as
// internal/syntax/keys.go's keyNames table (parse-time canonicalization).
// The two are intentionally separate maps for two different concerns
// (parse-time alias canonicalization vs. platform keycode mapping) - no
// clean shared-table import path exists without exporting parser
// internals. Adding/removing a KEY NAMES entry in one file must be
// mirrored in the other.
//
// Symbols deliberately ABSENT here (a lookup miss is Preflight check 5's
// "key name unsupported on this platform" signal, help.txt:456-458):
//   - volup, voldown, mute: macOS has no CGEventCreateKeyboardEvent
//     keycode for these; they are NX_KEYTYPE system-defined events, a
//     different (unsupported) injection mechanism entirely
//     (help-macos.txt CAVEATS: "cannot be produced on macOS").
//   - f21, f22, f23, f24, printscreen, scrolllock, pause: no kVK_* constant
//     exists for these on any Mac keyboard (the standard table stops at
//     F20; Mac keyboards have no PrintScreen/ScrollLock/Pause key). This is
//     a genuine macOS hardware/API gap beyond the three names the ticket
//     text calls out explicitly; omitting them (same "unsupported key
//     name" E_INPUT signal) is the technically honest behavior rather than
//     inventing a keycode with no physical meaning.
var keyCodes = buildKeyCodes()

func buildKeyCodes() map[string]uint16 {
	m := map[string]uint16{
		// letters (ANSI-US positions)
		"a": 0x00, "b": 0x0B, "c": 0x08, "d": 0x02, "e": 0x0E, "f": 0x03,
		"g": 0x05, "h": 0x04, "i": 0x22, "j": 0x26, "k": 0x28, "l": 0x25,
		"m": 0x2E, "n": 0x2D, "o": 0x1F, "p": 0x23, "q": 0x0C, "r": 0x0F,
		"s": 0x01, "t": 0x11, "u": 0x20, "v": 0x09, "w": 0x0D, "x": 0x07,
		"y": 0x10, "z": 0x06,

		// digits (ANSI-US positions)
		"0": 0x1D, "1": 0x12, "2": 0x13, "3": 0x14, "4": 0x15,
		"5": 0x17, "6": 0x16, "7": 0x1A, "8": 0x1C, "9": 0x19,

		// function keys F1-F20 (no kVK_* exists beyond F20)
		"f1": 0x7A, "f2": 0x78, "f3": 0x63, "f4": 0x76, "f5": 0x60,
		"f6": 0x61, "f7": 0x62, "f8": 0x64, "f9": 0x65, "f10": 0x6D,
		"f11": 0x67, "f12": 0x6F, "f13": 0x69, "f14": 0x6B, "f15": 0x71,
		"f16": 0x6A, "f17": 0x40, "f18": 0x4F, "f19": 0x50, "f20": 0x5A,

		// numpad
		"numpad0": 0x52, "numpad1": 0x53, "numpad2": 0x54, "numpad3": 0x55,
		"numpad4": 0x56, "numpad5": 0x57, "numpad6": 0x58, "numpad7": 0x59,
		"numpad8": 0x5B, "numpad9": 0x5C,
		"decimal": 0x41, "numadd": 0x45, "numsub": 0x4E, "nummul": 0x43,
		"numdiv": 0x4B, "numenter": 0x4C,

		// modifiers (left-hand physical keys; primary resolves to Command
		// directly here, not through a separate indirection, per the plan)
		"ctrl": 0x3B, "shift": 0x38, "alt": 0x3A, "meta": 0x37,
		"primary": 0x37,

		// named keys
		"enter": 0x24, "tab": 0x30, "esc": 0x35, "space": 0x31,
		"backspace": 0x33, "delete": 0x75, "insert": 0x72,
		"home": 0x73, "end": 0x77, "pageup": 0x74, "pagedown": 0x79,
		"up": 0x7E, "down": 0x7D, "left": 0x7B, "right": 0x7C,
		"period": 0x2F, "comma": 0x2B, "minus": 0x1B, "equal": 0x18,
		"slash": 0x2C, "backslash": 0x2A, "semicolon": 0x29, "quote": 0x27,
		"grave": 0x32, "lbracket": 0x21, "rbracket": 0x1E,
		"capslock": 0x39,
	}
	return m
}

// keycodeFor resolves a canonical IR key symbol (help.txt:664; already
// canonicalized by the parser - internal/syntax/keys.go aliases resolve to
// these names before the backend ever sees them) to its macOS virtual
// keycode. ok is false for a name this platform cannot produce.
func keycodeFor(sym string) (uint16, bool) {
	c, ok := keyCodes[sym]
	return c, ok
}

// allKeycodes returns the deduplicated set of every keycode this table
// knows, for Preflight check 3 (held-key scan over the full table,
// help.txt Constraints: "CGEventSourceKeyState over every virtual key
// code").
func allKeycodes() []uint16 {
	seen := make(map[uint16]bool, len(keyCodes))
	out := make([]uint16, 0, len(keyCodes))
	for _, c := range keyCodes {
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out
}

// unsupportedKeyMsg formats Preflight check 5's E_INPUT message for a key
// name this platform's keycode table has no entry for.
func unsupportedKeyMsg(sym string) string {
	return "key name " + strconv.Quote(sym) + " is not supported on macOS"
}
