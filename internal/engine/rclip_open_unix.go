//go:build darwin || linux

package engine

import (
	"os"
	"syscall"
)

// openRClip uses O_NONBLOCK so a FIFO substituted after the pathname check
// cannot stall the bridge before loadRClip validates the opened descriptor.
func openRClip(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}
