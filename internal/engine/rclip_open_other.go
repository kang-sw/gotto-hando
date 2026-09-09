//go:build !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris && !windows

package engine

import "os"

// openRClip keeps the GOOS-neutral engine buildable on platforms without a
// portable nonblocking-open flag. loadRClip still rejects non-regular files
// before and after opening; supported desktop targets use their safe openers.
func openRClip(path string) (*os.File, error) {
	return os.Open(path)
}
