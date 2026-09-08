//go:build windows

package windows

import (
	"context"
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

// errClipboardSetFailed is ClipboardSet's failure sentinel (help.txt:353-
// 355: "If setting the clipboard fails no key is sent (E_CLIPBOARD)").
// run.go's KindClipboard/KindPaste cases map any ClipboardSet error to
// output.EClipboard.
var errClipboardSetFailed = errors.New("SetClipboardData returned NULL")

// ClipboardSet sets the target's clipboard (help.txt:352-354: clip/paste)
// via the standard OpenClipboard/EmptyClipboard/GlobalAlloc(GMEM_MOVEABLE)/
// GlobalLock/UTF-16 copy/GlobalUnlock/SetClipboardData(CF_UNICODETEXT)/
// CloseClipboard sequence.
func (b *Backend) ClipboardSet(ctx context.Context, s string) error {
	if err := openClipboard(); err != nil {
		return err
	}
	defer procCloseClipboard.Call()
	procEmptyClipboard.Call()

	units, err := windows.UTF16FromString(s)
	if err != nil {
		return err
	}
	size := uintptr(len(units)) * 2 // UTF-16 code units, 2 bytes each, NUL included
	h, _, _ := procGlobalAlloc.Call(gmemMoveable, size)
	if h == 0 {
		return errors.New("GlobalAlloc failed")
	}
	ptr, _, _ := procGlobalLock.Call(h)
	if ptr == 0 {
		return errors.New("GlobalLock failed")
	}
	// go vet's unsafeptr check flags this uintptr->unsafe.Pointer
	// conversion ("possible misuse of unsafe.Pointer"): its heuristic only
	// recognizes a handful of syntactic patterns (reflect.Value.Pointer(),
	// pointer<->uintptr arithmetic on an EXISTING Go pointer) as safe, none
	// of which apply to a raw OS-owned address returned by a Win32 API
	// call. This is unavoidable for any cgo-free Windows syscall wrapper
	// that touches GlobalLock/VirtualAlloc-class memory - confirmed by
	// reproducing the identical diagnostic in golang.org/x/sys/windows's
	// own generated code (zsyscall_windows.go's getSidIdentifierAuthority
	// and 17 similar functions) when that package is vetted as part of the
	// analyzing module. ptr denotes memory GlobalAlloc/GlobalLock owns
	// (never Go-heap, so the GC-relocation danger unsafe.Pointer's
	// documentation warns about does not apply here).
	dst := unsafe.Slice((*uint16)(unsafe.Pointer(ptr)), len(units))
	copy(dst, units)
	procGlobalUnlock.Call(h)

	// SetClipboardData returns a BOOL-equivalent handle; checking it
	// (rather than discarding the result) is what makes help.txt's
	// documented paste E_CLIPBOARD-before-keys failure path reachable at
	// all. On success, the system owns h - it must NOT be GlobalFree'd
	// here.
	r, _, _ := procSetClipboardData.Call(cfUnicodeText, h)
	if r == 0 {
		return errClipboardSetFailed
	}
	return nil
}

// ClipboardGet reads the target's clipboard (help.txt:356-358: qclip). A
// clipboard with no CF_UNICODETEXT data (empty, or another format only)
// reads back as "", matching darwin's ClipboardGet, which never errors on
// an empty pasteboard either.
func (b *Backend) ClipboardGet(ctx context.Context) (string, error) {
	if err := openClipboard(); err != nil {
		return "", err
	}
	defer procCloseClipboard.Call()

	h, _, _ := procGetClipboardData.Call(cfUnicodeText)
	if h == 0 {
		return "", nil
	}
	ptr, _, _ := procGlobalLock.Call(h)
	if ptr == 0 {
		return "", nil
	}
	defer procGlobalUnlock.Call(h)
	// Same documented, unavoidable go vet unsafeptr false positive as
	// ClipboardSet's dst conversion above.
	return windows.UTF16PtrToString((*uint16)(unsafe.Pointer(ptr))), nil
}

func openClipboard() error {
	r, _, err := procOpenClipboard.Call(0)
	if r == 0 {
		return err
	}
	return nil
}
