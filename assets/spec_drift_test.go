package assets

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// sectionNames returns every "== NAME ==" header found at the start of a
// line in a help text, in encounter order (help.txt "^== " header idiom,
// see also cmd/gotto-hando/options_drift_test.go's extractOptionsTableFlags
// for the sibling section-scoping technique).
func sectionNames(help string) []string {
	var names []string
	for _, line := range strings.Split(help, "\n") {
		if strings.HasPrefix(line, "== ") && strings.HasSuffix(line, " ==") {
			names = append(names, line)
		}
	}
	return names
}

// specSeeRE matches a spec-file line of the exact literal form the ticket's
// Spec Impact text fixes: "See `assets/help[-<os>].txt` section
// `== NAME ==`". It is anchored on the literal "See `assets/" prefix and
// captures the "== NAME ==" section-header text, robust to whatever
// markdown heading level or surrounding prose the spec file uses.
var specSeeRE = regexp.MustCompile("See `assets/help[a-z-]*\\.txt` section `(== [A-Za-z0-9 ,/:()-]+ ==)`")

// specReferencedSections reads a spec file (tolerating absence) and returns
// every "== NAME ==" section it references via a "See `assets/...txt`
// section `== NAME ==`" line.
func specReferencedSections(t *testing.T, path string) (names []string, present bool) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false
		}
		t.Fatalf("read %s: %v", path, err)
	}
	for _, m := range specSeeRE.FindAllStringSubmatch(string(data), -1) {
		names = append(names, m[1])
	}
	return names, true
}

// TestSpecAnchorCoverage is drift test (a): every "See `assets/help*.txt`
// section `== NAME ==`" reference in a spec pointer file must name a real
// help-text section (forward direction), and every help-text section must
// be referenced by a spec entry (reverse direction).
//
// The pointer spec files (ai-docs/spec/help.md etc.) are a ticket-closeout
// deliverable created AFTER this phase (ticket Spec Impact: "so the
// reverse-direction drift test is green from the day it lands"), so the
// reverse check t.Skip's when a spec file doesn't exist yet. The forward
// check still runs (vacuously true today, zero spec files) and both
// directions start enforcing, unmodified, the moment closeout adds the
// spec files.
func TestSpecAnchorCoverage(t *testing.T) {
	cases := []struct {
		help     string
		helpName string
		specFile string
	}{
		{Help, "help.txt", "../ai-docs/spec/help.md"},
		{HelpMacos, "help-macos.txt", "../ai-docs/spec/help-macos.md"},
		{HelpWindows, "help-windows.txt", "../ai-docs/spec/help-windows.md"},
		{HelpRemote, "help-remote.txt", "../ai-docs/spec/help-remote.md"},
	}
	for _, c := range cases {
		t.Run(c.helpName, func(t *testing.T) {
			helpSections := sectionNames(c.help)
			if len(helpSections) == 0 {
				t.Fatalf("extractor found zero '== ... ==' sections in %s; the extractor or the file is broken", c.helpName)
			}
			helpSet := map[string]bool{}
			for _, s := range helpSections {
				helpSet[s] = true
			}

			specSections, present := specReferencedSections(t, c.specFile)

			// Forward: every spec-referenced section name must be real.
			for _, s := range specSections {
				if !helpSet[s] {
					t.Errorf("%s references %q which is not a section of %s", c.specFile, s, c.helpName)
				}
			}

			if !present {
				t.Skipf("%s does not exist yet (ticket-closeout deliverable, created after Phase 3); reverse-direction coverage check skipped until it lands", c.specFile)
			}

			// Reverse: every help-text section must be referenced.
			specSet := map[string]bool{}
			for _, s := range specSections {
				specSet[s] = true
			}
			var missing []string
			for _, s := range helpSections {
				if !specSet[s] {
					missing = append(missing, s)
				}
			}
			if len(missing) > 0 {
				sort.Strings(missing)
				t.Errorf("%s has no spec entry for %d section(s): %v", c.specFile, len(missing), missing)
			}
		})
	}
}
