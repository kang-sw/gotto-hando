//go:build windows

package engine

import (
	"os"

	"golang.org/x/sys/windows"
)

// openRClip requests overlapped I/O so opening a substituted named pipe does
// not wait for a server. loadRClip validates the resulting handle is regular
// before attempting any read.
func openRClip(path string) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ, windows.FILE_SHARE_READ,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OVERLAPPED, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(h), path), nil
}
