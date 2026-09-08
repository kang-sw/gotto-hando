package syntax

import "github.com/kang-sw/gotto-hando/internal/ir"

// payloadKind selects the per-command payload sub-grammar (COMMANDS,
// help.txt:299-448).
type payloadKind int

const (
	plNone        payloadKind = iota // no payload
	plKeys                           // k kd ku: space-separated key names / chords
	plText                           // txt clip paste: verbatim text (or [f] path)
	plQclip                          // qclip: optional local path (or [f])
	plPoint                          // m: required x,y
	plOptPoint                       // c md: optional x,y
	plPoints                         // drag: polyline
	plScroll                         // scroll: up|down|left|right [ticks]
	plSelector                       // win: required selector
	plOptSelector                    // qwin: optional selector
	plTarget                         // open: app | path
	plExec                           // exec: command...
	plCap                            // cap: optional local path
	plSleep                          // sleep: DUR
)

// cmdSpec is one command's static contract: its IR kind, payload grammar,
// and the modifiers it accepts. The allowed-modifier sets are the union of
// the COMMANDS [...] list and the common-mod "Accepted by" lists
// (help.txt:205-243); every command additionally accepts d= (help.txt:210).
// coordFrame marks the commands for which r/w/disp select a coordinate
// frame (mutually exclusive); for win/qwin r means regex instead.
type cmdSpec struct {
	name       string
	kind       ir.Kind
	payload    payloadKind
	single     map[rune]bool
	word       map[string]bool
	kv         map[string]bool
	coordFrame bool
}

func flags(rs ...rune) map[rune]bool {
	m := map[rune]bool{}
	for _, r := range rs {
		m[r] = true
	}
	return m
}

func words(ws ...string) map[string]bool {
	m := map[string]bool{}
	for _, w := range ws {
		m[w] = true
	}
	return m
}

// keys builds a kv-key set that always includes "d" (the universal delay
// modifier, help.txt:210-212).
func kvkeys(ks ...string) map[string]bool {
	m := map[string]bool{"d": true}
	for _, k := range ks {
		m[k] = true
	}
	return m
}

var commands = buildCommands()

func buildCommands() map[string]cmdSpec {
	list := []cmdSpec{
		{"k", ir.KindKey, plKeys, flags('c', 's', 'a', 'm', 'p'), nil, kvkeys("n", "ms"), false},
		{"kd", ir.KindKeyDown, plKeys, nil, nil, kvkeys(), false},
		{"ku", ir.KindKeyUp, plKeys, nil, nil, kvkeys(), false},
		{"txt", ir.KindText, plText, flags('f'), nil, kvkeys("ms"), false},
		{"m", ir.KindMove, plPoint, flags('c', 's', 'a', 'm', 'p', 'r', 'w'), nil, kvkeys("ms", "disp"), true},
		{"c", ir.KindClick, plOptPoint, flags('c', 's', 'a', 'm', 'p', 'r', 'w'), nil, kvkeys("b", "n", "ms", "disp"), true},
		{"md", ir.KindButtonDown, plOptPoint, flags('c', 's', 'a', 'm', 'p', 'r', 'w'), nil, kvkeys("b", "disp"), true},
		{"mu", ir.KindButtonUp, plNone, flags('c', 's', 'a', 'm', 'p'), nil, kvkeys("b"), false},
		{"drag", ir.KindDrag, plPoints, flags('c', 's', 'a', 'm', 'p', 'r', 'w'), nil, kvkeys("b", "ms", "steps", "disp"), true},
		{"scroll", ir.KindScroll, plScroll, flags('c', 's', 'a', 'm', 'p'), nil, kvkeys("by"), false},
		{"clip", ir.KindClipboard, plText, flags('f'), nil, kvkeys(), false},
		{"paste", ir.KindPaste, plText, flags('f'), nil, kvkeys("ms"), false},
		{"qclip", ir.KindQueryClip, plQclip, flags('f'), nil, kvkeys(), false},
		{"win", ir.KindFocus, plSelector, flags('r'), nil, kvkeys("wait"), false},
		{"qwin", ir.KindQueryWindows, plOptSelector, flags('r'), nil, kvkeys(), false},
		{"open", ir.KindOpen, plTarget, nil, nil, kvkeys("wait"), false},
		{"exec", ir.KindExec, plExec, nil, words("shell", "noerr"), kvkeys("timeout"), false},
		{"cap", ir.KindCapture, plCap, flags('w'), nil, kvkeys("disp", "rect", "scale", "n", "ms", "label"), true},
		{"sleep", ir.KindSleep, plSleep, nil, nil, kvkeys(), false},
		{"set", ir.KindSet, plNone, nil, nil, kvkeys("delay", "txtms", "keyms"), false},
		{"qinfo", ir.KindQueryInfo, plNone, nil, nil, kvkeys(), false},
		{"qdisp", ir.KindQueryDisp, plNone, nil, nil, kvkeys(), false},
		{"qmouse", ir.KindQueryMouse, plNone, nil, nil, kvkeys(), false},
	}
	m := map[string]cmdSpec{}
	for _, c := range list {
		m[c.name] = c
	}
	return m
}
