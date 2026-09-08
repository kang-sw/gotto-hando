//go:build darwin

package darwin

import "testing"

// TestBuildOpenArgv locks buildOpenArgv's app-name-vs-path split
// (help-macos.txt CAVEAT :247-248, ticket Decision): a bare word with no
// "/" is a bundle name (`open -a <name>`); anything containing "/" runs
// directly as a path (`open <path>`).
func TestBuildOpenArgv(t *testing.T) {
	cases := []struct {
		name   string
		target string
		want   []string
	}{
		{"bundle name", "TextEdit", []string{"/usr/bin/open", "-a", "TextEdit"}},
		{"bundle name with spaces", "Visual Studio Code", []string{"/usr/bin/open", "-a", "Visual Studio Code"}},
		{"absolute app path", "/Applications/TextEdit.app", []string{"/usr/bin/open", "/Applications/TextEdit.app"}},
		{"relative path", "./notes.txt", []string{"/usr/bin/open", "./notes.txt"}},
		{"path with spaces", "/Users/me/My Documents/notes.txt", []string{"/usr/bin/open", "/Users/me/My Documents/notes.txt"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := buildOpenArgv(tc.target)
			if !strSliceEqual(got, tc.want) {
				t.Errorf("buildOpenArgv(%q) = %v, want %v", tc.target, got, tc.want)
			}
		})
	}
}
