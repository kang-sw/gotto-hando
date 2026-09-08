package output

import (
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/kang-sw/gotto-hando/assets"
)

// sectionLines returns the body lines of one "== NAME ==" section (help.txt
// section-scoping idiom, see cmd/gotto-hando/options_drift_test.go).
func sectionLines(help, header string) []string {
	var lines []string
	inSection := false
	for _, line := range strings.Split(help, "\n") {
		if strings.HasPrefix(line, header) {
			inSection = true
			continue
		}
		if inSection && strings.HasPrefix(line, "== ") {
			break
		}
		if inSection {
			lines = append(lines, line)
		}
	}
	return lines
}

// errorCodeRowRE matches an ERROR CODES row's leading "E_CODE" token
// (help.txt:699-724): a 2-space indent then an all-caps "E_" identifier.
var errorCodeRowRE = regexp.MustCompile(`^  (E_[A-Z]+)`)

// exitCodeRowRE matches an EXIT CODES row's leading digit (help.txt:727-741):
// a 2-space indent then a single 0-5 digit.
var exitCodeRowRE = regexp.MustCompile(`^  ([0-5])\s`)

// TestErrorCodesDriftAgainstCodes is drift test (d), ERROR CODES half: the
// help text's E_* rows must match the ErrorCode consts exactly.
func TestErrorCodesDriftAgainstCodes(t *testing.T) {
	var tableCodes []string
	for _, line := range sectionLines(assets.Help, "== ERROR CODES ==") {
		if m := errorCodeRowRE.FindStringSubmatch(line); m != nil {
			tableCodes = append(tableCodes, m[1])
		}
	}
	if len(tableCodes) == 0 {
		t.Fatal("extractor found zero ERROR CODES rows; the extractor or the section marker is broken")
	}
	sort.Strings(tableCodes)

	codeCodes := []string{
		string(ESyntax), string(EValidate), string(EConnect), string(EPermission),
		string(ESession), string(EBounds), string(ENoWindow), string(EInput),
		string(ECapture), string(EClipboard), string(EExec), string(ETimeout),
		string(EUnknown),
	}
	sort.Strings(codeCodes)

	if !reflect.DeepEqual(tableCodes, codeCodes) {
		t.Fatalf("ERROR CODES drift:\nhelp.txt: %v\nErrorCode consts: %v", tableCodes, codeCodes)
	}
}

// TestExitCodesDriftAgainstCodes is drift test (d), EXIT CODES half: the
// help text's leading-digit rows must match the exit consts' values.
func TestExitCodesDriftAgainstCodes(t *testing.T) {
	var tableExits []int
	for _, line := range sectionLines(assets.Help, "== EXIT CODES ==") {
		if m := exitCodeRowRE.FindStringSubmatch(line); m != nil {
			n, err := strconv.Atoi(m[1])
			if err != nil {
				t.Fatalf("parse exit code %q: %v", m[1], err)
			}
			tableExits = append(tableExits, n)
		}
	}
	if len(tableExits) == 0 {
		t.Fatal("extractor found zero EXIT CODES rows; the extractor or the section marker is broken")
	}
	sort.Ints(tableExits)

	codeExits := []int{
		ExitOK, ExitRuntimeFailure, ExitValidation, ExitConnect,
		ExitPreflight, ExitStateUnknown,
	}
	sort.Ints(codeExits)

	if !reflect.DeepEqual(tableExits, codeExits) {
		t.Fatalf("EXIT CODES drift:\nhelp.txt: %v\nexit consts: %v", tableExits, codeExits)
	}
}
