package syntax

import "strings"

// keyNames is the KEY NAMES table (help.txt:448-458). Ranges (a-z, 0-9,
// f1-f24, numpad0-9) and the |-alias groups are expanded below. The map
// value is the canonical IR symbol (help.txt:664: modifier key names are
// symbols); alias members canonicalize to the first name of their group.
var keyNames = buildKeyNames()

func buildKeyNames() map[string]string {
	m := map[string]string{}
	add := func(canon string, aliases ...string) {
		m[canon] = canon
		for _, a := range aliases {
			m[a] = canon
		}
	}
	for c := 'a'; c <= 'z'; c++ {
		add(string(c))
	}
	for d := '0'; d <= '9'; d++ {
		add(string(d))
	}
	for i := 1; i <= 24; i++ {
		add("f" + itoa(i))
	}
	for i := 0; i <= 9; i++ {
		add("numpad" + itoa(i))
	}
	add("ctrl")
	add("shift")
	add("alt", "option")
	add("meta", "cmd", "win")
	add("primary")
	add("enter", "return")
	add("tab")
	add("esc", "escape")
	add("space")
	add("backspace")
	add("delete")
	add("insert")
	add("home")
	add("end")
	add("pageup")
	add("pagedown")
	add("up")
	add("down")
	add("left")
	add("right")
	add("period")
	add("comma")
	add("minus")
	add("equal")
	add("slash")
	add("backslash")
	add("semicolon")
	add("quote")
	add("grave")
	add("lbracket")
	add("rbracket")
	add("decimal")
	add("numadd")
	add("numsub")
	add("nummul")
	add("numdiv")
	add("numenter")
	add("capslock")
	add("printscreen")
	add("scrolllock")
	add("pause")
	add("volup")
	add("voldown")
	add("mute")
	return m
}

// canonKey returns the canonical IR symbol for a key name (case-insensitive)
// and whether the name is in the KEY NAMES table.
func canonKey(name string) (string, bool) {
	c, ok := keyNames[strings.ToLower(name)]
	return c, ok
}

// flagSymbol maps a modifier-key flag (c s a m p) to its IR symbol
// (help.txt:664, :226-234).
var flagSymbol = map[rune]string{
	'c': "ctrl",
	's': "shift",
	'a': "alt",
	'm': "meta",
	'p': "primary",
}

// flagOrder is the canonical press order c, s, a, m/p (help.txt:228); the
// IR mods list follows it.
var flagOrder = []rune{'c', 's', 'a', 'm', 'p'}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
