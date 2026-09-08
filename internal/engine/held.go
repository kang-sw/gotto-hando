package engine

import (
	"context"

	"github.com/kang-sw/gotto-hando/internal/backend"
)

// heldKey is one key pressed by kd and not yet released by ku (STATE
// MACHINE, help.txt:507-509). line is the kd line, used to attribute the
// auto-release warning (help.txt:314).
type heldKey struct {
	sym  string
	line int
	src  string
}

type heldButton struct {
	btn  backend.Button
	line int
	src  string
}

// heldState tracks keys/buttons held across lines so they can be released in
// reverse order when the run ends, fails or is interrupted (help.txt:515).
type heldState struct {
	keys    []heldKey
	buttons []heldButton
}

func (h *heldState) pressKey(sym string, line int, src string) {
	h.keys = append(h.keys, heldKey{sym, line, src})
}

func (h *heldState) releaseKey(sym string) {
	for i := len(h.keys) - 1; i >= 0; i-- {
		if h.keys[i].sym == sym {
			h.keys = append(h.keys[:i], h.keys[i+1:]...)
			return
		}
	}
}

func (h *heldState) pressButton(b backend.Button, line int, src string) {
	h.buttons = append(h.buttons, heldButton{b, line, src})
}

func (h *heldState) releaseButton(b backend.Button) {
	for i := len(h.buttons) - 1; i >= 0; i-- {
		if h.buttons[i].btn == b {
			h.buttons = append(h.buttons[:i], h.buttons[i+1:]...)
			return
		}
	}
}

// releaseAll releases every still-held key and button in reverse order,
// calling the backend best-effort (help.txt:515-516). It returns the count
// released and one warn Result per released key/button (help.txt:566).
func (h *heldState) releaseAll(ctx context.Context, be backend.Backend) (int, []autoRelease) {
	var rel []autoRelease
	for i := len(h.keys) - 1; i >= 0; i-- {
		k := h.keys[i]
		_ = be.KeyUp(ctx, k.sym)
		rel = append(rel, autoRelease{line: k.line, src: k.src, cmd: "kd", what: k.sym})
	}
	for i := len(h.buttons) - 1; i >= 0; i-- {
		b := h.buttons[i]
		_ = be.ButtonUp(ctx, b.btn)
		rel = append(rel, autoRelease{line: b.line, src: b.src, cmd: "md", what: string(b.btn)})
	}
	n := len(h.keys) + len(h.buttons)
	h.keys = nil
	h.buttons = nil
	return n, rel
}

type autoRelease struct {
	line int
	src  string
	cmd  string
	what string
}
