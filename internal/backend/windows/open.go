//go:build windows

package windows

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Open launches an app or path via ShellExecuteExW with
// SEE_MASK_NOCLOSEPROCESS (help.txt open :363-368, ticket Decision). A
// non-zero ShellExecuteExW failure (r == 0; a small hInstApp <= 32 is the
// legacy SE_ERR_* code) is returned as-is; the engine's existing KindOpen
// case maps it to E_EXEC. On success it also records the launched PID and
// target basename on *Backend so Windows's app-selector matching (below,
// windows.go) can prefer that specific process's window for a subsequent
// open[wait=] poll.
func (b *Backend) Open(ctx context.Context, target string) error {
	filePtr, err := syscall.UTF16PtrFromString(target)
	if err != nil {
		return fmt.Errorf("open: invalid target %q: %w", target, err)
	}

	var info shellExecuteInfoW
	info.cbSize = uint32(unsafe.Sizeof(info))
	info.fMask = seeMaskNocloseprocess
	info.lpFile = filePtr
	info.nShow = swShownormal

	r, _, callErr := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&info)))
	if r == 0 {
		if info.hInstApp != 0 && info.hInstApp <= 32 {
			return fmt.Errorf("ShellExecuteExW failed: SE_ERR code %d", info.hInstApp)
		}
		return fmt.Errorf("ShellExecuteExW failed: %w", callErr)
	}

	b.lastOpenPID = 0
	b.lastOpenBasename = openTargetBasename(target)
	if info.hProcess != 0 {
		hProcess := windows.Handle(info.hProcess)
		if pid, err := windows.GetProcessId(hProcess); err == nil {
			b.lastOpenPID = pid
		}
		windows.CloseHandle(hProcess)
	}
	return nil
}

// openTargetBasename mirrors internal/engine/exec.go's openAppSelector
// exactly (a bare name with no "/" is used verbatim; a path is reduced to
// its base name with a trailing ".app" suffix stripped) so
// b.lastOpenBasename compares equal to the sel.Value Windows() receives for
// the same target - duplicated rather than imported because backend must
// not import engine (CONCEPT.md ch.8.2).
func openTargetBasename(target string) string {
	name := target
	if strings.Contains(target, "/") {
		name = filepath.Base(strings.TrimSuffix(strings.TrimRight(target, "/"), ".app"))
	}
	return name
}
