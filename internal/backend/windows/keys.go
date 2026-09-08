//go:build windows

package windows

import "strconv"

// keyEntry is one canonical IR key symbol's PS/2 Set-1 scan code (the
// Microsoft Keyboard Scan Code Specification's "make code") plus whether it
// is sent as an extended (E0-prefixed) key. SendInput's KEYEVENTF_SCANCODE
// path needs no virtual-key code at all (wVk=0) - only the physical
// scan code and the extended flag matter, per help-windows.txt CAVEATS:
// "every key carries its scan code so DirectInput apps see it" and
// "Arrow, Home/End, Insert/Delete, Page keys, Win and right Ctrl/Alt are
// sent as extended keys."
//
// Cross-reference: this table must enumerate the SAME symbol set as
// internal/syntax/keys.go's keyNames table (parse-time canonicalization),
// same discipline as darwin's keys.go header comment. UNLIKE darwin, this
// table has NO omissions: every documented KEY NAMES symbol
// (assets/help.txt:448-458) resolves here, including volup/voldown/mute -
// Windows has real extended scan codes for the multimedia keys that macOS
// has no CGEvent keycode for at all (ticket Decision: "every documented
// key name is supported on Windows").
type keyEntry struct {
	scan     uint16
	extended bool
}

var keyScanCodes = buildKeyScanCodes()

func buildKeyScanCodes() map[string]keyEntry {
	e := func(scan uint16) keyEntry { return keyEntry{scan: scan} }
	ext := func(scan uint16) keyEntry { return keyEntry{scan: scan, extended: true} }
	return map[string]keyEntry{
		// letters (US QWERTY physical positions)
		"a": e(0x1E), "b": e(0x30), "c": e(0x2E), "d": e(0x20),
		"e": e(0x12), "f": e(0x21), "g": e(0x22), "h": e(0x23),
		"i": e(0x17), "j": e(0x24), "k": e(0x25), "l": e(0x26),
		"m": e(0x32), "n": e(0x31), "o": e(0x18), "p": e(0x19),
		"q": e(0x10), "r": e(0x13), "s": e(0x1F), "t": e(0x14),
		"u": e(0x16), "v": e(0x2F), "w": e(0x11), "x": e(0x2D),
		"y": e(0x15), "z": e(0x2C),

		// digits (top-row physical positions)
		"0": e(0x0B), "1": e(0x02), "2": e(0x03), "3": e(0x04), "4": e(0x05),
		"5": e(0x06), "6": e(0x07), "7": e(0x08), "8": e(0x09), "9": e(0x0A),

		// function keys F1-F24 (F13-F24 are legitimate documented Set-1
		// make codes even though few physical keyboards have those keys).
		"f1": e(0x3B), "f2": e(0x3C), "f3": e(0x3D), "f4": e(0x3E), "f5": e(0x3F),
		"f6": e(0x40), "f7": e(0x41), "f8": e(0x42), "f9": e(0x43), "f10": e(0x44),
		"f11": e(0x57), "f12": e(0x58), "f13": e(0x64), "f14": e(0x65), "f15": e(0x66),
		"f16": e(0x67), "f17": e(0x68), "f18": e(0x69), "f19": e(0x6A), "f20": e(0x6B),
		"f21": e(0x6C), "f22": e(0x6D), "f23": e(0x6E), "f24": e(0x76),

		// numpad digits/decimal - physical numpad scan codes WITHOUT the
		// extended flag, deliberately shared with the nav-cluster codes
		// below (the E0 prefix is what disambiguates them at the OS level;
		// help-windows.txt CAVEATS: "Keypad keys are sent by scan code
		// (physical position) regardless of NumLock").
		"numpad0": e(0x52), "numpad1": e(0x4F), "numpad2": e(0x50), "numpad3": e(0x51),
		"numpad4": e(0x4B), "numpad5": e(0x4C), "numpad6": e(0x4D), "numpad7": e(0x47),
		"numpad8": e(0x48), "numpad9": e(0x49),
		"decimal": e(0x53), "numadd": e(0x4E), "numsub": e(0x4A), "nummul": e(0x37),
		// numdiv/numenter ARE extended (the numpad / and Enter keys are
		// E0-prefixed to distinguish them from the main slash/Enter keys).
		"numdiv":   ext(0x35),
		"numenter": ext(0x1C),

		// modifiers (left-hand physical keys; primary resolves to Ctrl
		// directly here, per the plan / help.txt "Windows: primary=ctrl").
		"ctrl": e(0x1D), "shift": e(0x2A), "alt": e(0x38),
		"meta":    ext(0x5B), // left Windows key
		"primary": e(0x1D),

		// named keys
		"enter": e(0x1C), "tab": e(0x0F), "esc": e(0x01), "space": e(0x39),
		"backspace": e(0x0E),
		// nav cluster - extended per CAVEATS.
		"insert": ext(0x52), "delete": ext(0x53), "home": ext(0x47), "end": ext(0x4F),
		"pageup": ext(0x49), "pagedown": ext(0x51),
		"up": ext(0x48), "down": ext(0x50), "left": ext(0x4B), "right": ext(0x4D),
		"period": e(0x34), "comma": e(0x33), "minus": e(0x0C), "equal": e(0x0D),
		"slash": e(0x35), "backslash": e(0x2B), "semicolon": e(0x27), "quote": e(0x28),
		"grave": e(0x29), "lbracket": e(0x1A), "rbracket": e(0x1B),
		"capslock": e(0x3A),
		// printscreen/pause have unusual, non-uniform multi-scan Set-1
		// sequences with no simple single-scan-code break code (the
		// documented raw make sequences are "E0 2A E0 37" and
		// "E1 1D 45 E1 9D C5" respectively); this table sends the single
		// dominant scan code each is most commonly recognized by
		// (E0 37 / 45) - the deferred interactive verification subset
		// (real SendInput against a physical app) is where a Set-1
		// multi-scan quirk here would actually surface.
		"printscreen": ext(0x37),
		"scrolllock":  e(0x46),
		"pause":       e(0x45),
		// multimedia keys - real extended Set-1 scan codes SendInput can
		// inject; macOS has no CGEvent keycode equivalent at all (darwin's
		// keys.go documents these as unsupported there).
		"volup":   ext(0x30),
		"voldown": ext(0x2E),
		"mute":    ext(0x20),
	}
}

// keycodeFor resolves a canonical IR key symbol (already canonicalized by
// the parser - internal/syntax/keys.go aliases resolve to these names
// before the backend ever sees them) to its Windows scan code and extended
// flag. ok is false only for a name outside the canonical KEY NAMES table
// entirely (this table itself has no omissions, per keys.go's header
// comment).
func keycodeFor(sym string) (scan uint16, extended bool, ok bool) {
	entry, ok := keyScanCodes[sym]
	return entry.scan, entry.extended, ok
}

// allVKCodes returns every virtual-key code Preflight check 3 scans with
// GetAsyncKeyState (help-windows.txt CHECK: "GetAsyncKeyState over every
// virtual key"). Unlike darwin's per-symbol keycode table, GetAsyncKeyState
// operates on the small, dense VK_* code space (1-254) directly - scanning
// literally every VK code (rather than deriving a subset from
// keyScanCodes, which holds SCAN codes, not VK codes) is both simpler and
// matches the help text's "every virtual key" more literally; it covers
// every keyboard key AND every mouse button (VK_LBUTTON=1, VK_RBUTTON=2,
// VK_MBUTTON=4, VK_XBUTTON1/2=5/6) in the same pass.
func allVKCodes() []uint8 {
	codes := make([]uint8, 0, 254)
	for v := 1; v <= 254; v++ {
		codes = append(codes, uint8(v))
	}
	return codes
}

// unsupportedKeyMsg formats Preflight check 5's E_INPUT message for a key
// name this platform's scan-code table has no entry for (never reached in
// practice given full canonical coverage; kept for defensive parity, same
// as darwin's).
func unsupportedKeyMsg(sym string) string {
	return "key name " + strconv.Quote(sym) + " is not supported on Windows"
}
