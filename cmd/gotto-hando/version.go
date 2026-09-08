package main

import "github.com/kang-sw/gotto-hando/assets"

// version returns the version token from help.txt:1 (assets.Version,
// parsed once and cached there).
func version() string {
	return assets.Version()
}
