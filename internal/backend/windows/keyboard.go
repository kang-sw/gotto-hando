//go:build windows

package windows

import (
	"context"
	"fmt"
	"time"
	"unicode/utf16"
	"unsafe"
)

// KeyDown presses one physical key (help.txt:299-314) via SendInput's
// KEYEVENTF_SCANCODE path. Unlike darwin's CGEvent, which carries a
// process-wide heldFlags bit-field on every event because CGEventFlags
// travels on the event, SendInput's modifier state is entirely determined
// by the actual key-down/up events the engine already sends for chord
// composition (kd/ku, k[cs]a, ...) - there is no shared-flags field to
// maintain here (a deliberate simplification vs. darwin, not a gap; see
// the ticket's Codebase Findings on keyboard.go).
func (b *Backend) KeyDown(ctx context.Context, key string) error {
	scan, extended, ok := keycodeFor(key)
	if !ok {
		return fmt.Errorf("%s", unsupportedKeyMsg(key))
	}
	return sendKeyScan(scan, extended, false)
}

// KeyUp releases a key pressed by KeyDown.
func (b *Backend) KeyUp(ctx context.Context, key string) error {
	scan, extended, ok := keycodeFor(key)
	if !ok {
		return fmt.Errorf("%s", unsupportedKeyMsg(key))
	}
	return sendKeyScan(scan, extended, true)
}

func sendKeyScan(scan uint16, extended, up bool) error {
	flags := uint32(keyeventfScancode)
	if extended {
		flags |= keyeventfExtendedkey
	}
	if up {
		flags |= keyeventfKeyup
	}
	return sendKeyboardInput(0, scan, flags)
}

// TypeText types Unicode text character by character (help.txt:316-319).
// \n presses Return and \t presses Tab (already-resolved runes from the
// parser's TEXT ESCAPES pass) via the scan-code path (pressNamed); every
// other rune goes through KEYEVENTF_UNICODE, one SendInput down+up pair
// per UTF-16 code unit - a supplementary-plane rune (a surrogate pair)
// becomes two consecutive down+up pairs, one per surrogate half, per
// help-windows.txt CAVEATS "txt sends UTF-16 units (surrogate pairs in
// order); a per-character ms= gap applies per Unicode character" (the
// interval sleep below is per RUNE, not per code unit, matching that
// clause).
func (b *Backend) TypeText(ctx context.Context, s string, interval time.Duration) error {
	for _, r := range s {
		var err error
		switch r {
		case '\n':
			err = b.pressNamed(ctx, "enter")
		case '\t':
			err = b.pressNamed(ctx, "tab")
		default:
			err = postUnicodeRune(r)
		}
		if err != nil {
			return err
		}
		sleepCtx(ctx, interval)
	}
	return nil
}

// pressNamed presses and releases a named key (used by TypeText's \n/\t
// TEXT ESCAPES).
func (b *Backend) pressNamed(ctx context.Context, sym string) error {
	if err := b.KeyDown(ctx, sym); err != nil {
		return err
	}
	return b.KeyUp(ctx, sym)
}

// postUnicodeRune sends one Unicode character as one down+up
// KEYEVENTF_UNICODE pair per UTF-16 code unit (wScan carries the code unit
// directly; wVk=0 per the documented KEYEVENTF_UNICODE usage - the
// physical virtual key is irrelevant when the Unicode flag is set).
func postUnicodeRune(r rune) error {
	units := utf16.Encode([]rune{r})
	for _, u := range units {
		if err := sendKeyboardInput(0, u, keyeventfUnicode); err != nil {
			return err
		}
		if err := sendKeyboardInput(0, u, keyeventfUnicode|keyeventfKeyup); err != nil {
			return err
		}
	}
	return nil
}

func sendKeyboardInput(vk, scan uint16, flags uint32) error {
	rec := keybdInputRecord{typ: inputKeyboard, ki: keybdInput{wVk: vk, wScan: scan, dwFlags: flags}}
	r, _, err := procSendInput.Call(1, uintptr(unsafe.Pointer(&rec)), unsafe.Sizeof(rec))
	if r == 0 {
		return fmt.Errorf("SendInput (keyboard) failed: %w", err)
	}
	return nil
}

// sleepCtx waits d, honoring context cancellation early - mirrors
// internal/engine's timing.go helper; duplicated here (rather than
// exported from engine) because backend must not import engine (engine
// imports backend, not the reverse, CONCEPT.md ch.8.2). Identical to
// darwin's.
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
