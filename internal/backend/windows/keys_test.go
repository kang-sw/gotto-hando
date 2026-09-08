//go:build windows

package windows

import "testing"

// canonicalKeyNames mirrors the KEY NAMES table (help.txt:448-458) as
// canonicalized by internal/syntax/keys.go's buildKeyNames (unexported, so
// this list is a deliberate duplicate - same discipline as darwin's
// keys_test.go).
func canonicalKeyNames() []string {
	var out []string
	for c := 'a'; c <= 'z'; c++ {
		out = append(out, string(c))
	}
	for d := '0'; d <= '9'; d++ {
		out = append(out, string(d))
	}
	for i := 1; i <= 24; i++ {
		out = append(out, "f"+itoa(i))
	}
	for i := 0; i <= 9; i++ {
		out = append(out, "numpad"+itoa(i))
	}
	out = append(out,
		"ctrl", "shift", "alt", "meta", "primary",
		"enter", "tab", "esc", "space", "backspace", "delete", "insert",
		"home", "end", "pageup", "pagedown", "up", "down", "left", "right",
		"period", "comma", "minus", "equal", "slash", "backslash",
		"semicolon", "quote", "grave", "lbracket", "rbracket",
		"decimal", "numadd", "numsub", "nummul", "numdiv", "numenter",
		"capslock", "printscreen", "scrolllock", "pause",
		"volup", "voldown", "mute",
	)
	return out
}

func itoa(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	return string(rune('0'+i/10)) + string(rune('0'+i%10))
}

// TestKeycodeForCoversEveryCanonicalName is the mirror image of darwin's
// TestKeycodeForCoversEverySupportedCanonicalName: unlike darwin, this
// table has NO documented omissions (ticket Decision: "every documented
// key name is supported on Windows"), so every canonical KEY NAMES symbol
// must resolve.
func TestKeycodeForCoversEveryCanonicalName(t *testing.T) {
	for _, sym := range canonicalKeyNames() {
		if _, _, ok := keycodeFor(sym); !ok {
			t.Errorf("keycodeFor(%q) ok=false, want true (every canonical KEY NAMES symbol must resolve on Windows)", sym)
		}
	}
}

func TestKeycodeForUnknownSymbolFails(t *testing.T) {
	if _, _, ok := keycodeFor("not-a-real-key-name"); ok {
		t.Error("keycodeFor(bogus) ok=true, want false")
	}
}

func TestPrimaryResolvesToCtrlDirectly(t *testing.T) {
	primaryScan, primaryExt, ok := keycodeFor("primary")
	if !ok {
		t.Fatal("keycodeFor(primary) not found")
	}
	ctrlScan, ctrlExt, ok := keycodeFor("ctrl")
	if !ok {
		t.Fatal("keycodeFor(ctrl) not found")
	}
	if primaryScan != ctrlScan || primaryExt != ctrlExt {
		t.Errorf("primary=(%#x,%v), ctrl=(%#x,%v), want equal (primary resolves to Ctrl directly on Windows, help.txt primary=ctrl)",
			primaryScan, primaryExt, ctrlScan, ctrlExt)
	}
}

// TestExtendedKeyFlags locks in the CAVEATS-documented extended-key list
// (assets/help-windows.txt CAVEATS: "Arrow, Home/End, Insert/Delete, Page
// keys, Win and right Ctrl/Alt are sent as extended keys") against the
// numpad digits, which are explicitly NOT extended (same CAVEATS bullet:
// "Keypad keys are sent by scan code (physical position) regardless of
// NumLock").
func TestExtendedKeyFlags(t *testing.T) {
	extended := []string{
		"up", "down", "left", "right", "home", "end", "insert", "delete",
		"pageup", "pagedown", "meta", "numenter", "numdiv",
		"volup", "voldown", "mute", "printscreen",
	}
	for _, sym := range extended {
		_, ext, ok := keycodeFor(sym)
		if !ok {
			t.Fatalf("keycodeFor(%q) not found", sym)
		}
		if !ext {
			t.Errorf("keycodeFor(%q) extended=false, want true", sym)
		}
	}

	nonExtended := []string{
		"a", "1", "ctrl", "primary", "alt", "shift", "enter", "tab",
		"numpad0", "numpad9", "nummul", "numadd", "numsub", "decimal",
		"capslock", "scrolllock", "pause",
	}
	for _, sym := range nonExtended {
		_, ext, ok := keycodeFor(sym)
		if !ok {
			t.Fatalf("keycodeFor(%q) not found", sym)
		}
		if ext {
			t.Errorf("keycodeFor(%q) extended=true, want false", sym)
		}
	}
}

// TestNumpadDigitsUseNavClusterScanCodesWithoutExtendedFlag proves the
// CAVEATS distinction directly: numpad7/8/9/4/6/1/2/3/0 share their raw
// scan code with home/up/pageup/left/right/end/down/pagedown/insert, and
// ONLY the extended flag (set on the nav-cluster symbol, clear on the
// numpad symbol) disambiguates them - exactly what the OS uses to tell
// them apart at the hardware level.
func TestNumpadDigitsUseNavClusterScanCodesWithoutExtendedFlag(t *testing.T) {
	pairs := map[string]string{
		"numpad7": "home", "numpad8": "up", "numpad9": "pageup",
		"numpad4": "left", "numpad6": "right",
		"numpad1": "end", "numpad2": "down", "numpad3": "pagedown",
		"numpad0": "insert",
	}
	for numSym, navSym := range pairs {
		numScan, numExt, ok := keycodeFor(numSym)
		if !ok {
			t.Fatalf("keycodeFor(%q) not found", numSym)
		}
		navScan, navExt, ok := keycodeFor(navSym)
		if !ok {
			t.Fatalf("keycodeFor(%q) not found", navSym)
		}
		if numScan != navScan {
			t.Errorf("%s scan=%#x, %s scan=%#x, want equal", numSym, numScan, navSym, navScan)
		}
		if numExt {
			t.Errorf("%s extended=true, want false", numSym)
		}
		if !navExt {
			t.Errorf("%s extended=false, want true", navSym)
		}
	}
}

func TestAllVKCodesRange(t *testing.T) {
	codes := allVKCodes()
	if len(codes) != 254 {
		t.Fatalf("allVKCodes() len=%d, want 254 (VK codes 1-254)", len(codes))
	}
	if codes[0] != 1 || codes[len(codes)-1] != 254 {
		t.Errorf("allVKCodes() = [%d..%d], want [1..254]", codes[0], codes[len(codes)-1])
	}
}
