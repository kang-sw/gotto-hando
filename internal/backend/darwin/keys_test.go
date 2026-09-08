//go:build darwin

package darwin

import "testing"

// canonicalKeyNames mirrors the KEY NAMES table (help.txt:448-458) as
// canonicalized by internal/syntax/keys.go's buildKeyNames (unexported,
// so this list is a deliberate duplicate - see keys.go's header comment:
// "this table must enumerate the SAME symbol set as
// internal/syntax/keys.go's keyNames table... Adding/removing a KEY NAMES
// entry in one file must be mirrored in the other").
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

// unsupportedOnDarwin is the documented omission set: names in the KEY
// NAMES contract that macOS genuinely cannot produce (keys.go's header
// comment). A lookup miss here is Preflight check 5's signal, not a bug.
var unsupportedOnDarwin = map[string]bool{
	"f21": true, "f22": true, "f23": true, "f24": true,
	"printscreen": true, "scrolllock": true, "pause": true,
	"volup": true, "voldown": true, "mute": true,
}

func TestKeycodeForCoversEverySupportedCanonicalName(t *testing.T) {
	for _, sym := range canonicalKeyNames() {
		_, ok := keycodeFor(sym)
		want := !unsupportedOnDarwin[sym]
		if ok != want {
			t.Errorf("keycodeFor(%q) ok=%v, want %v", sym, ok, want)
		}
	}
}

func TestKeycodeForUnsupportedNamesExplicit(t *testing.T) {
	// The ticket text explicitly calls out volup/voldown/mute; the rest
	// (f21-f24, printscreen, scrolllock, pause) are this implementation's
	// own documented extension of the same "unsupported key name" rule.
	for sym := range unsupportedOnDarwin {
		if _, ok := keycodeFor(sym); ok {
			t.Errorf("keycodeFor(%q) = ok, want unsupported (no macOS kVK_* constant exists)", sym)
		}
	}
}

func TestPrimaryResolvesToCommandDirectly(t *testing.T) {
	primary, ok := keycodeFor("primary")
	if !ok {
		t.Fatal("keycodeFor(primary) not found")
	}
	meta, ok := keycodeFor("meta")
	if !ok {
		t.Fatal("keycodeFor(meta) not found")
	}
	if primary != meta {
		t.Errorf("primary=%#x, meta=%#x, want equal (primary resolves to Command directly, plan requirement)", primary, meta)
	}
}

func TestAllKeycodesDeduplicated(t *testing.T) {
	codes := allKeycodes()
	seen := make(map[uint16]bool, len(codes))
	for _, c := range codes {
		if seen[c] {
			t.Errorf("allKeycodes() contains duplicate %#x", c)
		}
		seen[c] = true
	}
	// primary and meta share a keycode, so allKeycodes() must be strictly
	// smaller than len(keyCodes) (dedup actually happened, not a no-op).
	if len(codes) >= len(keyCodes) {
		t.Errorf("allKeycodes() len=%d, keyCodes len=%d, want dedup to shrink it (primary==meta)", len(codes), len(keyCodes))
	}
}
