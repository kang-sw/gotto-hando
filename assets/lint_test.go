package assets

import (
	"regexp"
	"strings"
	"testing"
)

// sectionHeaderRE is the format contract for a "== SECTION ==" header line:
// two "="-runs bracketing a name of letters, digits, spaces, and a small
// punctuation set (seen in practice: "/" in "exec ON WINDOWS", parentheses
// in "SESSION BRIDGE FOR SSH (TASK SCHEDULER)", "," and ":" elsewhere).
var sectionHeaderRE = regexp.MustCompile(`^== [A-Za-z0-9 ,/:()-]+ ==$`)

// TestHelpTextFormatLint is drift test (f): every embedded help text stays
// within an 80-column line budget, every "== ... ==" header matches the
// canonical header shape, and the file ends with exactly one trailing
// newline (no trailing blank line, no missing final newline).
func TestHelpTextFormatLint(t *testing.T) {
	cases := []struct {
		name string
		text string
	}{
		{"help.txt", Help},
		{"help-macos.txt", HelpMacos},
		{"help-windows.txt", HelpWindows},
		{"help-remote.txt", HelpRemote},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if !strings.HasSuffix(c.text, "\n") {
				t.Fatalf("%s does not end with a trailing newline", c.name)
			}
			if strings.HasSuffix(c.text, "\n\n") {
				t.Fatalf("%s ends with more than one trailing newline", c.name)
			}
			body := strings.TrimSuffix(c.text, "\n")
			lines := strings.Split(body, "\n")
			headerCount := 0
			for i, line := range lines {
				if len(line) > 80 {
					t.Errorf("%s:%d exceeds 80 columns (%d): %q", c.name, i+1, len(line), line)
				}
				if strings.HasPrefix(line, "== ") {
					headerCount++
					if !sectionHeaderRE.MatchString(line) {
						t.Errorf("%s:%d header does not match %s: %q", c.name, i+1, sectionHeaderRE.String(), line)
					}
				}
			}
			if headerCount == 0 {
				t.Fatalf("%s: found zero '== ... ==' header lines; the file or the lint scan is broken", c.name)
			}
		})
	}
}
