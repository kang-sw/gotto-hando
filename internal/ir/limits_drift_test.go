package ir

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/kang-sw/gotto-hando/assets"
)

// limitRowRE matches one LIMITS row's leading "name" cell (help.txt
// `:743-767`, regularized in this same commit into one-number-per-row
// form): a 2-space indent, a name of non-space-padded words (no internal
// double space), then a >=2-space gap before the value cell. A
// continuation line (25-space indent, wrapped value text) starts with
// spaces immediately after the 2-space anchor and so never matches this
// pattern - it is folded into the preceding row's value instead.
var limitRowRE = regexp.MustCompile(`^  (\S.*?)  +(\S.*)$`)

// extractLimitsRows returns the LIMITS section as name -> concatenated
// value text (continuation lines joined onto the row that owns them with
// a single space, restoring the wrapped sentence).
func extractLimitsRows(t *testing.T, help string) map[string]string {
	t.Helper()
	rows := map[string]string{}
	var order []string
	inSection := false
	curName := ""
	for _, line := range strings.Split(help, "\n") {
		if strings.HasPrefix(line, "== LIMITS ==") {
			inSection = true
			continue
		}
		if inSection && strings.HasPrefix(line, "== ") {
			break
		}
		if !inSection {
			continue
		}
		if m := limitRowRE.FindStringSubmatch(line); m != nil {
			curName = m[1]
			if _, dup := rows[curName]; dup {
				t.Fatalf("LIMITS row name %q appears twice", curName)
			}
			rows[curName] = m[2]
			order = append(order, curName)
			continue
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if curName == "" {
			t.Fatalf("LIMITS continuation line with no owning row: %q", line)
		}
		rows[curName] = rows[curName] + " " + trimmed
	}
	if len(rows) == 0 {
		t.Fatal("extractor found zero LIMITS rows; the extractor or the section marker is broken")
	}
	return rows
}

var (
	leadingIntRE   = regexp.MustCompile(`^(\d+)`)
	leadingRangeRE = regexp.MustCompile(`^(\d+)\.\.(\d+)`)
	leExactRE      = regexp.MustCompile(`^<=\s*(\d+)s`)
	leadingKiBRE   = regexp.MustCompile(`^(\d+)\s*KiB`)
	leCharsRE      = regexp.MustCompile(`^<=\s*(\d+)\s*chars`)
	leBareRE       = regexp.MustCompile(`^<=\s*(\d+)$`)
	scaleRangeRE   = regexp.MustCompile(`^([\d.]+)\.\.([\d.]+)`)
)

func mustInt(t *testing.T, re *regexp.Regexp, s, row string) int {
	t.Helper()
	m := re.FindStringSubmatch(s)
	if m == nil {
		t.Fatalf("LIMITS row %q: value %q does not match %s", row, s, re.String())
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatalf("LIMITS row %q: parse %q: %v", row, m[1], err)
	}
	return n
}

// TestLimitsDriftAgainstConstants is drift test (e): every numeric LIMITS
// row with a corresponding Go constant (internal/ir/limits.go) must match
// it exactly.
//
// Three help.txt rows are deliberately excluded (Out of Scope, per the
// Phase 3 plan and limits.go's own doc comment: "coordinate bounds,
// platform key support and exec output caps are preflight/runtime, out of
// scope here"): "exec stdout/stderr" (a runtime cap, target-dependent, no
// static constant), "--timeout" (the CLI's own default, not enforced as a
// limit by this package), and "argv" (an OS limit, not a gotto-hando
// constant). Every other row must be recognized below; an unrecognized row
// fails loudly instead of silently passing.
func TestLimitsDriftAgainstConstants(t *testing.T) {
	rows := extractLimitsRows(t, assets.Help)

	excluded := map[string]bool{
		"exec stdout/stderr": true,
		"--timeout":          true,
		"argv":               true,
	}

	check := func(row string, got, want int) {
		t.Helper()
		delete(rows, row)
		if got != want {
			t.Errorf("LIMITS row %q: help.txt says %d, constant says %d", row, got, want)
		}
	}
	checkRange := func(row string, gotLo, gotHi, wantLo, wantHi int) {
		t.Helper()
		delete(rows, row)
		if gotLo != wantLo || gotHi != wantHi {
			t.Errorf("LIMITS row %q: help.txt says %d..%d, constants say %d..%d", row, gotLo, gotHi, wantLo, wantHi)
		}
	}

	if v, ok := rows["lines per run"]; ok {
		check("lines per run", mustInt(t, leadingIntRE, v, "lines per run"), MaxLinesPerRun)
	} else {
		t.Error("LIMITS row \"lines per run\" not found")
	}

	if v, ok := rows["line length"]; ok {
		kib := mustInt(t, leadingKiBRE, v, "line length")
		check("line length", kib*1024, MaxLineBytes)
	} else {
		t.Error("LIMITS row \"line length\" not found")
	}

	if v, ok := rows["k keys sequential"]; ok {
		check("k keys sequential", mustInt(t, leadingIntRE, v, "k keys sequential"), MaxKeysSeq)
	} else {
		t.Error("LIMITS row \"k keys sequential\" not found")
	}

	if v, ok := rows["k keys per chord"]; ok {
		check("k keys per chord", mustInt(t, leadingIntRE, v, "k keys per chord"), MaxChordKeys)
	} else {
		t.Error("LIMITS row \"k keys per chord\" not found")
	}

	if v, ok := rows["scroll ticks"]; ok {
		m := leadingRangeRE.FindStringSubmatch(v)
		if m == nil {
			t.Errorf("LIMITS row \"scroll ticks\": value %q does not match A..B", v)
		} else {
			lo, _ := strconv.Atoi(m[1])
			hi, _ := strconv.Atoi(m[2])
			checkRange("scroll ticks", lo, hi, MinScrollTicks, MaxScrollTicks)
		}
	} else {
		t.Error("LIMITS row \"scroll ticks\" not found")
	}

	if v, ok := rows["sleep"]; ok {
		s := mustInt(t, leExactRE, v, "sleep")
		check("sleep", s*1000, MaxSleepMS)
	} else {
		t.Error("LIMITS row \"sleep\" not found")
	}

	if v, ok := rows["d=, ms="]; ok {
		s := mustInt(t, leExactRE, v, "d=, ms=")
		check("d=, ms=", s*1000, MaxDelayMS)
	} else {
		t.Error("LIMITS row \"d=, ms=\" not found")
	}

	if v, ok := rows["wait="]; ok {
		s := mustInt(t, leExactRE, v, "wait=")
		check("wait=", s*1000, MaxWaitMS)
	} else {
		t.Error("LIMITS row \"wait=\" not found")
	}

	if v, ok := rows["exec timeout="]; ok {
		s := mustInt(t, leExactRE, v, "exec timeout=")
		check("exec timeout=", s*1000, MaxExecTimeoutMS)
	} else {
		t.Error("LIMITS row \"exec timeout=\" not found")
	}

	if v, ok := rows["drag points"]; ok {
		m := leadingRangeRE.FindStringSubmatch(v)
		if m == nil {
			t.Errorf("LIMITS row \"drag points\": value %q does not match A..B", v)
		} else {
			lo, _ := strconv.Atoi(m[1])
			hi, _ := strconv.Atoi(m[2])
			checkRange("drag points", lo, hi, MinDragPoints, MaxDragPoints)
		}
	} else {
		t.Error("LIMITS row \"drag points\" not found")
	}

	if v, ok := rows["drag steps"]; ok {
		m := leadingRangeRE.FindStringSubmatch(v)
		if m == nil {
			t.Errorf("LIMITS row \"drag steps\": value %q does not match A..B", v)
		} else {
			lo, _ := strconv.Atoi(m[1])
			hi, _ := strconv.Atoi(m[2])
			checkRange("drag steps", lo, hi, MinDragSteps, MaxDragSteps)
		}
	} else {
		t.Error("LIMITS row \"drag steps\" not found")
	}

	if v, ok := rows["label="]; ok {
		check("label=", mustInt(t, leCharsRE, v, "label="), MaxLabelLen)
	} else {
		t.Error("LIMITS row \"label=\" not found")
	}

	if v, ok := rows["cap n="]; ok {
		check("cap n=", mustInt(t, leBareRE, v, "cap n="), MaxCapN)
	} else {
		t.Error("LIMITS row \"cap n=\" not found")
	}

	if v, ok := rows["cap captures per run"]; ok {
		check("cap captures per run", mustInt(t, leBareRE, v, "cap captures per run"), MaxCapturesTotal)
	} else {
		t.Error("LIMITS row \"cap captures per run\" not found")
	}

	if v, ok := rows["scale="]; ok {
		m := scaleRangeRE.FindStringSubmatch(v)
		if m == nil {
			t.Errorf("LIMITS row \"scale=\": value %q does not match A..B", v)
		} else {
			lo, errLo := strconv.ParseFloat(m[1], 64)
			hi, errHi := strconv.ParseFloat(m[2], 64)
			if errLo != nil || errHi != nil {
				t.Fatalf("LIMITS row \"scale=\": parse %q: %v / %v", v, errLo, errHi)
			}
			delete(rows, "scale=")
			if lo != MinScale || hi != MaxScale {
				t.Errorf("LIMITS row \"scale=\": help.txt says %v..%v, constants say %v..%v", lo, hi, MinScale, MaxScale)
			}
		}
	} else {
		t.Error("LIMITS row \"scale=\" not found")
	}

	for name := range excluded {
		delete(rows, name)
	}
	if len(rows) > 0 {
		var leftover []string
		for name := range rows {
			leftover = append(leftover, name)
		}
		t.Errorf("LIMITS row(s) neither checked against a constant nor excluded: %v", leftover)
	}
}
