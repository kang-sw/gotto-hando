//go:build darwin

package darwin

import (
	"context"
	"errors"
	"sync"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/cstrings"
	"github.com/ebitengine/purego/objc"
)

// pasteboardTypeString is NSPasteboardTypeString's fixed UTI value
// ("public.utf8-plain-text"); constructed directly rather than dlsym'd
// from AppKit's exported NSString constant symbol (simpler and equally
// stable - it is a documented, unchanging UTI).
const pasteboardTypeString = "public.utf8-plain-text"

var (
	nsPasteboardClass    objc.Class
	selGeneralPasteboard objc.SEL
	selClearContents     objc.SEL
	selSetStringForType  objc.SEL
	selStringForType     objc.SEL
	clipboardOnce        sync.Once
)

// initClipboard loads AppKit (for NSPasteboard - ffi.go's dlopenAll does
// not load it, only the frameworks the other files need) and resolves the
// class/selectors once.
func initClipboard() {
	clipboardOnce.Do(func() {
		if _, err := purego.Dlopen("/System/Library/Frameworks/AppKit.framework/AppKit",
			purego.RTLD_NOW|purego.RTLD_GLOBAL); err != nil {
			panic(err)
		}
		nsPasteboardClass = objc.GetClass("NSPasteboard")
		selGeneralPasteboard = objc.RegisterName("generalPasteboard")
		selClearContents = objc.RegisterName("clearContents")
		selSetStringForType = objc.RegisterName("setString:forType:")
		selStringForType = objc.RegisterName("stringForType:")
	})
}

// errClipboardSetFailed is ClipboardSet's failure sentinel
// (help.txt:353-355: "If setting the clipboard fails no key is sent
// (E_CLIPBOARD)"). run.go's KindClipboard/KindPaste cases map any
// ClipboardSet error to output.EClipboard.
var errClipboardSetFailed = errors.New("NSPasteboard setString:forType: returned false")

// ClipboardSet sets the target's clipboard (help.txt:352-354: clip/paste).
func (b *Backend) ClipboardSet(ctx context.Context, s string) error {
	initClipboard()
	pb := objc.ID(nsPasteboardClass).Send(selGeneralPasteboard)
	pb.Send(selClearContents)
	typ := cfString(pasteboardTypeString)
	defer cfRelease(typ)
	str := cfString(s)
	defer cfRelease(str)
	// setString:forType: returns a BOOL; checking it (rather than
	// discarding the Send result) is what makes help.txt's documented
	// paste E_CLIPBOARD-before-keys failure path reachable at all.
	if ok := objc.Send[bool](pb, selSetStringForType, objc.ID(str), objc.ID(typ)); !ok {
		return errClipboardSetFailed
	}
	return nil
}

// ClipboardGet reads the target's clipboard (help.txt:356-358: qclip).
func (b *Backend) ClipboardGet(ctx context.Context) (string, error) {
	initClipboard()
	pb := objc.ID(nsPasteboardClass).Send(selGeneralPasteboard)
	typ := cfString(pasteboardTypeString)
	defer cfRelease(typ)
	str := pb.Send(selStringForType, objc.ID(typ))
	return cstrings.NSStringToString(str), nil
}
