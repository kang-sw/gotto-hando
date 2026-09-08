package assets

import (
	"regexp"
	"sync"
)

// versionLineRE extracts the version token from Help's first line
// ("gotto-hando 0.1.0 - ...", help.txt:1). help.txt is the single source
// of truth for the version string; nothing else in this program hardcodes
// it.
var versionLineRE = regexp.MustCompile(`^gotto-hando (\S+)`)

var (
	versionOnce sync.Once
	versionStr  string
)

// Version returns the version token from help.txt:1, parsed once and
// cached. Exported so internal/backend/darwin (qinfo's ver= field) can read
// it without importing cmd/gotto-hando (wrong import direction).
func Version() string {
	versionOnce.Do(func() {
		m := versionLineRE.FindStringSubmatch(Help)
		if m == nil {
			versionStr = "unknown"
			return
		}
		versionStr = m[1]
	})
	return versionStr
}
