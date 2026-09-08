package syntax

import (
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/kang-sw/gotto-hando/assets"
)

// This file lives in package syntax (not syntax_test) because drift tests
// (b) and (c) need the unexported commands and keyNames package vars
// directly (Phase 3 plan, Implementation Plan step 4).

// --- (c) KEY NAMES vs keyNames -------------------------------------------

// extractKeyNamesTokens returns the raw KEY NAMES token list (help.txt
// `:448-454`): every whitespace-separated token from the section's opening
// paragraph, up to the blank line inserted (this same commit, step 1) to
// separate the token list from the following prose sentence.
func extractKeyNamesTokens(t *testing.T, help string) []string {
	t.Helper()
	var tokens []string
	inSection := false
	for _, line := range strings.Split(help, "\n") {
		if strings.HasPrefix(line, "== KEY NAMES ==") {
			inSection = true
			continue
		}
		if !inSection {
			continue
		}
		if strings.TrimSpace(line) == "" {
			break
		}
		tokens = append(tokens, strings.Fields(line)...)
	}
	if len(tokens) == 0 {
		t.Fatal("extractor found zero KEY NAMES tokens; the extractor, the section marker, or the blank-line separator is broken")
	}
	return tokens
}

var (
	letterRangeRE = regexp.MustCompile(`^([a-z])-([a-z])$`)
	numRangeRE    = regexp.MustCompile(`^([a-zA-Z]*)(\d+)-([a-zA-Z]*)(\d+)$`)
)

// expandKeyToken expands one KEY NAMES token into its member key names: a
// `|`-alias group (`alt|option`) becomes one entry per member; a letter
// range (`a-z`) or a numeric range with an optional shared prefix (`0-9`,
// `f1-f24`, `numpad0-numpad9`) becomes one entry per member; anything else
// is a single literal token.
func expandKeyToken(t *testing.T, tok string) []string {
	t.Helper()
	if strings.Contains(tok, "|") {
		return strings.Split(tok, "|")
	}
	if m := letterRangeRE.FindStringSubmatch(tok); m != nil {
		var out []string
		for c := m[1][0]; c <= m[2][0]; c++ {
			out = append(out, string(rune(c)))
		}
		return out
	}
	if m := numRangeRE.FindStringSubmatch(tok); m != nil {
		prefix1, n1s, prefix2, n2s := m[1], m[2], m[3], m[4]
		if prefix1 != prefix2 {
			t.Fatalf("KEY NAMES range token %q has mismatched prefixes %q/%q", tok, prefix1, prefix2)
		}
		n1, err1 := strconv.Atoi(n1s)
		n2, err2 := strconv.Atoi(n2s)
		if err1 != nil || err2 != nil {
			t.Fatalf("KEY NAMES range token %q: not parseable as a numeric range", tok)
		}
		var out []string
		for n := n1; n <= n2; n++ {
			out = append(out, prefix1+strconv.Itoa(n))
		}
		return out
	}
	return []string{tok}
}

// TestKeyNamesDriftAgainstKeyNames is drift test (c): the KEY NAMES token
// list, expanded, must match keyNames's key set exactly (both canonical
// names and aliases - keyNames maps every alias to its canonical form, and
// the KEY NAMES prose lists every alias too, so the raw key count is the
// right comparison, not just the canonical-name count).
func TestKeyNamesDriftAgainstKeyNames(t *testing.T) {
	tokens := extractKeyNamesTokens(t, assets.Help)

	helpSet := map[string]bool{}
	for _, tok := range tokens {
		for _, k := range expandKeyToken(t, tok) {
			helpSet[k] = true
		}
	}

	codeSet := map[string]bool{}
	for k := range keyNames {
		codeSet[k] = true
	}

	if len(helpSet) == 0 || len(codeSet) == 0 {
		t.Fatalf("empty key set: help=%d code=%d", len(helpSet), len(codeSet))
	}

	helpList, codeList := setToSortedSlice(helpSet), setToSortedSlice(codeSet)
	if !reflect.DeepEqual(helpList, codeList) {
		t.Fatalf("KEY NAMES drift:\nhelp.txt tokens (expanded): %v\nkeyNames keys: %v", helpList, codeList)
	}
}

func setToSortedSlice(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// --- (b) COMMANDS vs commands ---------------------------------------------

// cmdLineRE matches one COMMANDS command-name line (help.txt `:299-447`): a
// 2-space indent, a lowercase-or-`#` name, an optional " [bracket]", then
// either end of line or a >=2-space gap before the payload-grammar token.
// The lowercase-vs-uppercase leading character is load-bearing: it is what
// excludes ALL-CAPS group headers (KEYBOARD, MOUSE, ...) at the same
// 2-space indent, and the >=2-space trailing gap (not just any whitespace)
// is what excludes the section's own lowercase-starting intro-prose
// continuation lines ("payload. Common modifiers...", "under MODIFIERS and
// repeated here...") which run single-spaced at the same indent.
var cmdLineRE = regexp.MustCompile(`^  (#|[a-z][a-zA-Z]*)(?:\s\[([^\]]*)\])?(?:\s{2,}\S.*)?$`)

// extractCommandBrackets returns, for each COMMANDS command-name line, the
// command name and its raw bracket text (empty when the command has no
// bracket). `#` (COMMANDS' comment line) is intentionally included here and
// dropped by the caller: the line-shape dispatcher, not cmdSpec, handles
// it, so it has no commands map entry to compare against.
func extractCommandBrackets(t *testing.T, help string) map[string]string {
	t.Helper()
	rows := map[string]string{}
	inSection := false
	for _, line := range strings.Split(help, "\n") {
		if strings.HasPrefix(line, "== COMMANDS ==") {
			inSection = true
			continue
		}
		if inSection && strings.HasPrefix(line, "== ") {
			break
		}
		if !inSection {
			continue
		}
		m := cmdLineRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		name := m[1]
		if _, dup := rows[name]; dup {
			t.Fatalf("COMMANDS command name %q appears twice", name)
		}
		rows[name] = m[2]
	}
	if len(rows) == 0 {
		t.Fatal("extractor found zero COMMANDS command-name lines; the extractor or the section marker is broken")
	}
	return rows
}

// acceptedByRE matches the "Accepted by: ..." sentence shared, after this
// commit's regularization (Phase 3 plan step 1), verbatim by MODIFIERS
// (`:209`) and MODIFIER KEYS (`:235`).
var acceptedByRE = regexp.MustCompile(`Accepted by: ([a-z ]+)\.`)

// extractAcceptedByCommands returns the command names in the common
// modifiers' (c s a m p) "Accepted by" list.
func extractAcceptedByCommands(t *testing.T, help string) map[string]bool {
	t.Helper()
	m := acceptedByRE.FindStringSubmatch(help)
	if m == nil {
		t.Fatal(`"Accepted by: ..." sentence not found; the regularization edit or the extractor regex is broken`)
	}
	set := map[string]bool{}
	for _, name := range strings.Fields(m[1]) {
		set[name] = true
	}
	return set
}

// parseBracketItem classifies one top-level bracket item (help.txt
// `:305-446`, comma-separated) into the single-char flag, word flag, or kv
// key it contributes. Two literal tokens are special-cased as the
// mutually-exclusive coordinate-frame group (help.txt MODIFIERS `:220-224`,
// COORDINATES `:245-268`): `r|w|disp=` (m/c/md/drag: relative, window
// frame, or display N) and `w|disp=N` (cap: window frame or display N, no
// relative frame) - these are the only two `|`-groups in COMMANDS brackets
// that are not a single enum-valued modifier (`b=left|right|middle`,
// `by=line|page`, where `=` binds the FIRST token instead of the last).
func parseBracketItem(item string, single, word, kv map[string]bool) {
	switch item {
	case "r|w|disp=":
		single["r"] = true
		single["w"] = true
		kv["disp"] = true
		return
	case "w|disp=N":
		single["w"] = true
		kv["disp"] = true
		return
	}
	if idx := strings.IndexByte(item, '='); idx >= 0 {
		kv[item[:idx]] = true
		return
	}
	// A bare flag word, optionally followed by an inline description
	// (e.g. "r regex" on win/qwin, where "regex" describes what r means
	// for that command).
	name := item
	if sp := strings.IndexByte(item, ' '); sp >= 0 {
		name = item[:sp]
	}
	if len(name) == 1 {
		single[name] = true
	} else {
		word[name] = true
	}
}

// extractBracketModifiers parses a command's raw bracket text into its
// single-char flag, word flag, and kv-key sets.
func extractBracketModifiers(bracket string) (single, word, kv map[string]bool) {
	single, word, kv = map[string]bool{}, map[string]bool{}, map[string]bool{}
	if bracket == "" {
		return
	}
	for _, item := range strings.Split(bracket, ", ") {
		parseBracketItem(strings.TrimSpace(item), single, word, kv)
	}
	return
}

// TestCommandsDriftAgainstCommands is drift test (b): the COMMANDS
// command-name set must match commands' key set (minus `#`, which the
// line-shape dispatcher handles instead of cmdSpec), and each command's
// accepted-modifier set - its COMMANDS bracket UNION `{c,s,a,m,p}` when the
// command's name is in the common modifiers' "Accepted by" list - must
// match its cmdSpec's single+word+kv sets, excluding "d": every command
// accepts d= (the universal delay modifier, help.txt MODIFIERS `:210-212`)
// without it appearing in any bracket or the Accepted-by list, so kvkeys()
// auto-adds it on the code side (commands.go kvkeys) and it must be
// excluded here before comparing, or every command would false-fail.
func TestCommandsDriftAgainstCommands(t *testing.T) {
	rows := extractCommandBrackets(t, assets.Help)
	delete(rows, "#")

	var tableNames []string
	for name := range rows {
		tableNames = append(tableNames, name)
	}
	sort.Strings(tableNames)

	var codeNames []string
	for name := range commands {
		codeNames = append(codeNames, name)
	}
	sort.Strings(codeNames)

	if !reflect.DeepEqual(tableNames, codeNames) {
		t.Fatalf("COMMANDS name set drift:\nhelp.txt (minus '#'): %v\ncommands: %v", tableNames, codeNames)
	}

	acceptedBy := extractAcceptedByCommands(t, assets.Help)

	for _, name := range tableNames {
		single, word, kv := extractBracketModifiers(rows[name])
		if acceptedBy[name] {
			for _, r := range []string{"c", "s", "a", "m", "p"} {
				single[r] = true
			}
		}
		var helpMods []string
		for k := range single {
			helpMods = append(helpMods, k)
		}
		for k := range word {
			helpMods = append(helpMods, k)
		}
		for k := range kv {
			helpMods = append(helpMods, k)
		}
		sort.Strings(helpMods)

		spec := commands[name]
		var codeMods []string
		for r := range spec.single {
			codeMods = append(codeMods, string(r))
		}
		for w := range spec.word {
			codeMods = append(codeMods, w)
		}
		for k := range spec.kv {
			if k == "d" {
				continue
			}
			codeMods = append(codeMods, k)
		}
		sort.Strings(codeMods)

		if !reflect.DeepEqual(helpMods, codeMods) {
			t.Errorf("command %q modifier-set drift:\nhelp.txt (bracket UNION Accepted-by): %v\ncmdSpec (single+word+kv, minus d): %v",
				name, helpMods, codeMods)
		}
	}
}
