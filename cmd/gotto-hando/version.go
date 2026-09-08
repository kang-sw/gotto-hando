package main

import (
	"regexp"
	"sync"

	"github.com/kang-sw/gotto-hando/assets"
)

// versionLineRE extracts the version token from assets.Help's first line
// ("gotto-hando 0.1.0 - ...", help.txt:1). help.txt is the single source
// of truth for the version string; nothing else in this program hardcodes
// it.
var versionLineRE = regexp.MustCompile(`^gotto-hando (\S+)`)

var (
	versionOnce sync.Once
	versionStr  string
)

// version returns the version token from help.txt:1, parsed once and
// cached.
func version() string {
	versionOnce.Do(func() {
		m := versionLineRE.FindStringSubmatch(assets.Help)
		if m == nil {
			versionStr = "unknown"
			return
		}
		versionStr = m[1]
	})
	return versionStr
}
