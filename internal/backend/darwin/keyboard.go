//go:build darwin

package darwin

import (
	"context"
	"errors"
	"fmt"
	"time"
	"unicode/utf16"
)

// CGEventFlags modifier bits (CGEventTypes.h). Applied to every keyboard
// AND mouse event this backend posts, on top of the real modifier
// key-down/up events pressMods already sends (help-macos.txt CAVEATS:
// "Some apps only honour modifier flags carried on the event, not separate
// modifier key-downs; gotto-hando sets both").
const (
	cgEventFlagMaskShift     = 0x00020000
	cgEventFlagMaskControl   = 0x00040000
	cgEventFlagMaskAlternate = 0x00080000
	cgEventFlagMaskCommand   = 0x00100000
)

// modifierFlagBit reports the CGEventFlags bit for a modifier symbol and
// whether sym is a modifier at all (ctrl/shift/alt/meta/primary - primary
// resolves to Command on macOS, help.txt:238-239).
func modifierFlagBit(sym string) (uint64, bool) {
	switch sym {
	case "ctrl":
		return cgEventFlagMaskControl, true
	case "shift":
		return cgEventFlagMaskShift, true
	case "alt":
		return cgEventFlagMaskAlternate, true
	case "meta", "primary":
		return cgEventFlagMaskCommand, true
	default:
		return 0, false
	}
}

var errSecureInput = errors.New("Secure Input is enabled; synthetic key events are blocked (help-macos.txt SESSION, LOCK, SECURE INPUT)")

// checkSecureInput implements the run-time (not Preflight) Secure Input
// check (help-macos.txt:215-218, ticket Constraints: "Secure Input is NOT a
// preflight check - it fails the affected line at run time with E_INPUT").
func (b *Backend) checkSecureInput() error {
	if b.secure.Enabled() {
		return errSecureInput
	}
	return nil
}

// KeyDown presses one physical key (help.txt:299-314). Modifier symbols
// also update heldFlags so subsequent events (including this one) carry
// the CGEventFlags bit.
func (b *Backend) KeyDown(ctx context.Context, key string) error {
	if err := b.checkSecureInput(); err != nil {
		return err
	}
	code, ok := keycodeFor(key)
	if !ok {
		return fmt.Errorf("%s", unsupportedKeyMsg(key))
	}
	if bit, isMod := modifierFlagBit(key); isMod {
		b.heldFlags |= bit
	}
	ev := cgEventCreateKeyboardEvent(b.evtSource, code, true)
	if ev == 0 {
		return fmt.Errorf("CGEventCreateKeyboardEvent failed for key %q", key)
	}
	cgEventSetFlags(ev, b.heldFlags)
	cgEventPost(cgHIDEventTap, ev)
	cfRelease(ev)
	return nil
}

// KeyUp releases a key pressed by KeyDown.
func (b *Backend) KeyUp(ctx context.Context, key string) error {
	code, ok := keycodeFor(key)
	if !ok {
		return fmt.Errorf("%s", unsupportedKeyMsg(key))
	}
	if bit, isMod := modifierFlagBit(key); isMod {
		b.heldFlags &^= bit
	}
	ev := cgEventCreateKeyboardEvent(b.evtSource, code, false)
	if ev == 0 {
		return fmt.Errorf("CGEventCreateKeyboardEvent failed for key %q", key)
	}
	cgEventSetFlags(ev, b.heldFlags)
	cgEventPost(cgHIDEventTap, ev)
	cfRelease(ev)
	return nil
}

// TypeText types Unicode text character by character (help.txt:316-319).
// \n presses Return and \t presses Tab (already-resolved runes from the
// parser's TEXT ESCAPES pass, help.txt:104-107, internal/syntax/escape.go)
// - every other rune goes through CGEventKeyboardSetUnicodeString so it
// bypasses layout/IME entirely (help-macos.txt CAVEATS).
func (b *Backend) TypeText(ctx context.Context, s string, interval time.Duration) error {
	if err := b.checkSecureInput(); err != nil {
		return err
	}
	for _, r := range s {
		var err error
		switch r {
		case '\n':
			err = b.pressNamed(ctx, "enter")
		case '\t':
			err = b.pressNamed(ctx, "tab")
		default:
			err = b.postUnicodeRune(r)
		}
		if err != nil {
			return err
		}
		sleepCtx(ctx, interval)
	}
	return nil
}

// pressNamed presses and releases a named key (used by TypeText's \n/\t
// TEXT ESCAPES), applying the current heldFlags like any other key event.
func (b *Backend) pressNamed(ctx context.Context, sym string) error {
	if err := b.KeyDown(ctx, sym); err != nil {
		return err
	}
	return b.KeyUp(ctx, sym)
}

// postUnicodeRune sends one Unicode character as a down+up keyboard event
// pair with virtualKey 0 (the Unicode string overrides the displayed
// character, so the physical keycode is irrelevant).
func (b *Backend) postUnicodeRune(r rune) error {
	units := utf16.Encode([]rune{r})
	if len(units) == 0 {
		return nil
	}
	down := cgEventCreateKeyboardEvent(b.evtSource, 0, true)
	if down == 0 {
		return fmt.Errorf("CGEventCreateKeyboardEvent failed for rune %q", r)
	}
	cgEventKeyboardSetUnicodeString(down, uint32(len(units)), &units[0])
	cgEventSetFlags(down, b.heldFlags)
	cgEventPost(cgHIDEventTap, down)
	cfRelease(down)

	up := cgEventCreateKeyboardEvent(b.evtSource, 0, false)
	if up == 0 {
		return fmt.Errorf("CGEventCreateKeyboardEvent failed for rune %q", r)
	}
	cgEventKeyboardSetUnicodeString(up, uint32(len(units)), &units[0])
	cgEventSetFlags(up, b.heldFlags)
	cgEventPost(cgHIDEventTap, up)
	cfRelease(up)
	return nil
}

// sleepCtx waits d, honoring context cancellation early - mirrors
// internal/engine's timing.go helper; duplicated here (rather than
// exported from engine) because backend must not import engine (engine
// imports backend, not the reverse, CONCEPT.md ch.8.2).
func sleepCtx(ctx context.Context, d time.Duration) {
	if d <= 0 {
		return
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
