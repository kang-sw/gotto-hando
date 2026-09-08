package main

import (
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/kang-sw/gotto-hando/assets"
)

// optionsRowRE matches the first line of one OPTIONS table row (help.txt
// `:47-116`): a 2-space indent, an optional "-x, " short-flag prefix, a
// "--long-flag" token, an optional " METAVAR" and then either end of
// line or a >=2-space gap before the description text starts.
//
// The >=2-space-gap requirement (instead of just "starts with -") is
// load-bearing: the OPTIONS section's closing paragraph ("Forwarded to
// the remote process...", help.txt:110-115) is prose wrapped at the same
// 2-space indent and happens to contain lines that themselves start with
// "--inline-captures" / "--delay," etc. (help.txt:111,113) - those are
// single-spaced prose, not column-aligned table rows, so this pattern
// does not match them.
var optionsRowRE = regexp.MustCompile(`^  (?:(-[a-zA-Z]), )?(--[A-Za-z][A-Za-z0-9-]*)(?: [A-Z][A-Za-z0-9_]*)?(?:|\s{2,}\S.*)$`)

// extractOptionsTableFlags isolates the OPTIONS section of help and
// returns every short/long flag token found in its table rows.
func extractOptionsTableFlags(help string) []string {
	var flags []string
	inSection := false
	for _, line := range strings.Split(help, "\n") {
		if strings.HasPrefix(line, "== OPTIONS ==") {
			inSection = true
			continue
		}
		if inSection && strings.HasPrefix(line, "== ") {
			break
		}
		if !inSection {
			continue
		}
		m := optionsRowRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if m[1] != "" {
			flags = append(flags, m[1])
		}
		flags = append(flags, m[2])
	}
	return flags
}

// tableTrackedParserFlags returns every short/long flag token the option
// parser recognizes as belonging to the OPTIONS table (excludes the
// SYNOPSIS-only --help* flags).
func tableTrackedParserFlags() []string {
	var flags []string
	for _, d := range optDefs {
		if !d.tableTracked {
			continue
		}
		flags = append(flags, d.long)
		if d.short != "" {
			flags = append(flags, d.short)
		}
	}
	return flags
}

// TestOptionsTableDriftAgainstParser is drift test (g): the OPTIONS table
// in assets/help.txt and the option parser's table-tracked flag set must
// be identical in both directions. --help/--help-macos/--help-windows/
// --help-remote are SYNOPSIS-only (no OPTIONS row) and excluded from
// both sides.
func TestOptionsTableDriftAgainstParser(t *testing.T) {
	tableFlags := extractOptionsTableFlags(assets.Help)
	if len(tableFlags) == 0 {
		t.Fatal("extractor found zero OPTIONS rows; the extractor or the OPTIONS section markers are broken")
	}
	sort.Strings(tableFlags)

	parserFlags := tableTrackedParserFlags()
	sort.Strings(parserFlags)

	if !reflect.DeepEqual(tableFlags, parserFlags) {
		t.Fatalf("OPTIONS table vs parser flag set drift:\nOPTIONS table: %v\nparser table-tracked: %v",
			tableFlags, parserFlags)
	}
}
