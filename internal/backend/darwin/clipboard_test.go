//go:build darwin

package darwin

import (
	"context"
	"testing"
)

// TestClipboardSetGetRoundTrip is the lock-independent regression test for
// the Minor review finding: ClipboardSet used to discard
// setString:forType:'s BOOL return, so a set failure could never surface
// as help.txt's documented E_CLIPBOARD (:353-355). This exercises the real
// NSPasteboard (construction/access needs neither an unlocked session nor
// Accessibility) and checks both that a normal set succeeds (no error) and
// that the written value round-trips through ClipboardGet.
func TestClipboardSetGetRoundTrip(t *testing.T) {
	if err := initFFI(); err != nil {
		t.Fatalf("initFFI: %v", err)
	}
	b := &Backend{}
	const want = "gotto-hando clipboard round-trip probe"
	if err := b.ClipboardSet(context.Background(), want); err != nil {
		t.Fatalf("ClipboardSet(%q) = %v, want nil", want, err)
	}
	got, err := b.ClipboardGet(context.Background())
	if err != nil {
		t.Fatalf("ClipboardGet() = %v, want nil", err)
	}
	if got != want {
		t.Fatalf("ClipboardGet() = %q, want %q", got, want)
	}
}

func TestClipboardImageUsesOwnedNSDataSelectors(t *testing.T) {
	if err := initFFI(); err != nil {
		t.Fatalf("initFFI: %v", err)
	}
	initClipboard()
	if selAlloc == 0 || selInitWithBytesLength == 0 || selRelease == 0 {
		t.Fatalf("owned NSData selectors not initialized: alloc=%v init=%v release=%v", selAlloc, selInitWithBytesLength, selRelease)
	}
}
