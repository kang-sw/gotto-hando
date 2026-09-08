// Package syntax is the pure text->IR parser (CONCEPT.md ch.12). It takes
// UTF-8 input lines and produces ir.Ops plus ir.Diagnostics; it performs no
// OS calls (the Decision: "parser is pure, text in, IR out"). Reading [f]
// files is a separate post-parse pass (inline.go); static range/limit/held
// checks are ir.Validate. The single source of truth for every shape is
// assets/help.txt (SYNTAX/GRAMMAR/COMMANDS/... sections).
package syntax

import (
	"strconv"
	"strings"

	"github.com/kang-sw/gotto-hando/internal/ir"
	"github.com/kang-sw/gotto-hando/internal/output"
)

// Parse turns input lines (1-based line numbers = index+1, counting blank
// and comment lines per help.txt:564) into a Sequence. Every line that
// fails produces one Diagnostic; parsing continues so all bad lines are
// reported before exit (help.txt:464). defaults seeds the sequence's
// initial delay/txt/key values.
func Parse(lines []string, defaults ir.Defaults) (*ir.Sequence, []ir.Diagnostic) {
	seq := &ir.Sequence{V: ir.SchemaVersion, Defaults: defaults}
	var diags []ir.Diagnostic
	for idx, src := range lines {
		op, d := parseLine(idx+1, src)
		if d != nil {
			diags = append(diags, *d)
			continue
		}
		if op != nil {
			seq.Ops = append(seq.Ops, *op)
		}
	}
	return seq, diags
}

func syntaxDiag(line, col int, src, msg string) *ir.Diagnostic {
	return &ir.Diagnostic{Line: line, Col: col, Code: output.ESyntax, Msg: msg, Src: src}
}

func isLower(r rune) bool { return r >= 'a' && r <= 'z' }
func isDigit(r rune) bool { return r >= '0' && r <= '9' }

// parseLine parses one source line. It returns (nil, nil) for ignored
// empty/comment lines, (op, nil) for a statement, or (nil, diag) on error.
func parseLine(line int, src string) (*ir.Op, *ir.Diagnostic) {
	rs := []rune(src)
	i := 0
	for i < len(rs) && (rs[i] == ' ' || rs[i] == '\t') {
		i++
	}
	if i >= len(rs) { // empty / whitespace-only
		return nil, nil
	}
	if rs[i] == '#' { // comment
		return nil, nil
	}
	if !isLower(rs[i]) {
		return nil, syntaxDiag(line, i+1, src, "line must start with a command (lowercase letter), # or blank")
	}
	start := i
	i++
	for i < len(rs) && (isLower(rs[i]) || isDigit(rs[i])) {
		i++
	}
	name := string(rs[start:i])
	spec, ok := commands[name]
	if !ok {
		return nil, syntaxDiag(line, start+1, src, "unknown command "+strconv.Quote(name))
	}

	var modStr []rune
	var modStartCol int
	var payload string
	var payloadStartCol int
	switch {
	case i >= len(rs):
		// no-payload form
	case rs[i] == '[':
		close := -1
		for j := i + 1; j < len(rs); j++ {
			if rs[j] == ']' {
				close = j
				break
			}
		}
		if close == -1 {
			return nil, syntaxDiag(line, i+1, src, "unterminated modifiers: missing ']'")
		}
		modStr = rs[i+1 : close]
		modStartCol = i + 2
		payload = string(rs[close+1:])
		payloadStartCol = close + 2
	default:
		// anything but '[' or EOL after the command: allow trailing
		// whitespace as the no-payload form, else syntax error (no
		// space-separated form, help.txt:147-154).
		if strings.TrimSpace(string(rs[i:])) == "" {
			// trailing whitespace only
		} else {
			return nil, syntaxDiag(line, i+1, src, "expected '[' after command or end of line (there is no space-separated form)")
		}
	}

	ms, d := parseModifiers(line, src, modStartCol, modStr, spec)
	if d != nil {
		return nil, d
	}
	return buildOp(line, src, spec, ms, payload, payloadStartCol)
}

// modSet is the classified modifiers of one line.
type modSet struct {
	flags map[rune]bool
	words map[string]bool
	kvs   map[string]string
	// column of each modifier for diagnostics (keyed by flag rune string
	// or kv key or word).
	col map[string]int
}

func newModSet() modSet {
	return modSet{flags: map[rune]bool{}, words: map[string]bool{}, kvs: map[string]string{}, col: map[string]int{}}
}

func parseModifiers(line int, src string, startCol int, modStr []rune, spec cmdSpec) (modSet, *ir.Diagnostic) {
	m := newModSet()
	if len(modStr) == 0 {
		return m, nil
	}
	// split on ',' tracking columns
	tokStart := 0
	emit := func(tok []rune, tokCol int) *ir.Diagnostic {
		if len(tok) == 0 {
			return syntaxDiag(line, tokCol, src, "empty modifier")
		}
		s := string(tok)
		if eq := strings.IndexRune(s, '='); eq >= 0 {
			key := s[:eq]
			val := s[eq+1:]
			if !validKey(key) {
				return syntaxDiag(line, tokCol, src, "invalid modifier key "+strconv.Quote(key))
			}
			if !spec.kv[key] {
				return syntaxDiag(line, tokCol, src, "unknown modifier "+strconv.Quote(key)+" for "+spec.name)
			}
			if _, dup := m.kvs[key]; dup || m.col["kv:"+key] != 0 {
				return syntaxDiag(line, tokCol, src, "repeated modifier "+strconv.Quote(key))
			}
			m.kvs[key] = val
			m.col["kv:"+key] = tokCol
			return nil
		}
		// non-kv token: a word flag, or a group of single-letter flags.
		if spec.word[s] {
			if m.words[s] {
				return syntaxDiag(line, tokCol, src, "repeated modifier "+strconv.Quote(s))
			}
			m.words[s] = true
			m.col["word:"+s] = tokCol
			return nil
		}
		for _, r := range tok {
			if spec.single == nil || !spec.single[r] {
				return syntaxDiag(line, tokCol, src, "unknown modifier flag "+strconv.Quote(string(r))+" for "+spec.name)
			}
			if m.flags[r] {
				return syntaxDiag(line, tokCol, src, "repeated modifier flag "+strconv.Quote(string(r)))
			}
			m.flags[r] = true
			m.col["flag:"+string(r)] = tokCol
		}
		return nil
	}
	for j := 0; j <= len(modStr); j++ {
		if j == len(modStr) || modStr[j] == ',' {
			tok := modStr[tokStart:j]
			if d := emit(tok, startCol+tokStart); d != nil {
				return m, d
			}
			tokStart = j + 1
		}
	}
	return m, nil
}

func validKey(k string) bool {
	if k == "" {
		return false
	}
	for i, r := range k {
		if i == 0 && !isLower(r) {
			return false
		}
		if i > 0 && !isLower(r) && !isDigit(r) {
			return false
		}
	}
	return true
}

// orderedMods returns the flag modifier-key symbols in canonical press
// order (help.txt:228, :664).
func orderedMods(m modSet) []string {
	var out []string
	for _, r := range flagOrder {
		if m.flags[r] {
			out = append(out, flagSymbol[r])
		}
	}
	if out == nil {
		out = []string{}
	}
	return out
}
