package syntax_test

import (
	"strings"
	"testing"

	"github.com/kang-sw/gotto-hando/internal/ir"
	"github.com/kang-sw/gotto-hando/internal/output"
	"github.com/kang-sw/gotto-hando/internal/syntax"
)

var defaults = ir.Defaults{DelayMS: 100, TextIntervalMS: 0, KeyGapMS: 30}

// positiveLines are the extracted single-quoted args / heredoc bodies of
// every == EXAMPLES == and == SHELL QUOTING == invocation, the QUICK START
// lines, and every inline COMMANDS example (help.txt). Each must PARSE
// without a syntax error (held balance and [f] file reads are separate
// passes, so ku/kd and [f] lines are exercised here only for parseability).
var positiveLines = []string{
	// QUICK START (:118-131)
	"win[]Safari", "k[m]l", `txt[]example.com\n`, "sleep[]1s", "cap",
	"c[]312,140", "txt[ms=20]hello", "k[]enter",
	// COMMANDS inline examples (:311, :327, :342, :393-396, :415-416, :426)
	"k[c]v", "k[]enter enter", "k[n=3]tab", "k[]ctrl+shift+a", "k[p]s",
	"c[n=2]", "c[b=right]100,200", "c[s]", "c[w]50%,50%",
	"scroll[]down 2", "scroll[by=page]down",
	`exec[]git -C "C:\work dir" status`, "exec[shell]dir /b *.blend",
	"exec[timeout=60s]blender -b x.blend -f 1", "exec[noerr]grep -q TODO notes.txt",
	"cap", "cap[w]", "cap[rect=0:0:600:400]", "cap[w,scale=0.5]",
	"cap[n=3,ms=100,label=orbit]", "cap[]./shot.png", "set[delay=200]",
	// EXAMPLES (:853-888)
	"win[]Blender", "k[]a", "k[]g", "k[]x", "k[]1 period 5",
	"win[r]Python Console", "paste[f]./cmd.py", "cap[w]", "sleep[]500",
	"m[]960,540", "drag[b=middle,ms=300]960,540 1100,540",
	"win[]Finder", "c[w]120,200", "c[ws]120,240", "c[w,b=right]120,240",
	"m[r]0,-40", "sleep[]300", "cap[rect=0:0:600:400]",
	"m[]700,500", "scroll[by=page]down 2",
	"kd[]shift", "c[]100,100", "c[]300,100", "ku[]shift",
	"k[c]a", "k[c]c", "qclip[f]./selection.txt",
	"open[wait=10s]blender", "exec[]blender --version", `exec[shell]dir /b C:\work`,
	`exec[timeout=60s]blender -b C:\work\scene.blend -f 1`, `exec[shell]dir /b C:\work\render`,
	"exec[noerr,shell]grep -q ERROR ~/app.log",
	"qwin", "qdisp", "qmouse", "qinfo",
	// SHELL QUOTING (:788-799)
	"txt[]hello, world!", "txt[]it's", "m[]1,2",
}

func TestPositiveParse(t *testing.T) {
	for _, line := range positiveLines {
		_, diags := syntax.Parse([]string{line}, defaults)
		if len(diags) != 0 {
			t.Errorf("line %q should parse, got %+v", line, diags)
		}
	}
}

// TestExampleSequencesValidate checks that whole multi-line EXAMPLES
// sequences also pass static validation (balanced held keys, in-range).
func TestExampleSequencesValidate(t *testing.T) {
	seqs := [][]string{
		{"kd[]shift", "c[]100,100", "c[]300,100", "ku[]shift"},
		{"win[]Blender", "k[]a", "k[]g", "k[]x", "k[]1 period 5", "k[]enter", "cap"},
		{"m[]960,540", "drag[b=middle,ms=300]960,540 1100,540", "cap[n=3,ms=100,label=orbit]"},
		{"win[]Finder", "c[w]120,200", "c[ws]120,240", "c[w,b=right]120,240", "cap[w,scale=0.5]"},
	}
	for _, lines := range seqs {
		seq, diags := syntax.Parse(lines, defaults)
		diags = append(diags, ir.Validate(seq, len(lines))...)
		if len(diags) != 0 {
			t.Errorf("sequence %v should validate, got %+v", lines, diags)
		}
	}
}

// analyze runs the full parse -> validate pipeline (no [f] inline) and
// returns the diagnostics, mirroring the --check/--ir path.
func analyze(t *testing.T, lines ...string) []ir.Diagnostic {
	t.Helper()
	seq, diags := syntax.Parse(lines, defaults)
	return append(diags, ir.Validate(seq, len(lines))...)
}

func TestNegativeClasses(t *testing.T) {
	cases := []struct {
		name string
		line string
		code output.ErrorCode
	}{
		{"space form", "m 1,2", output.ESyntax},
		{"unknown flag", "k[z]a", output.ESyntax},
		{"duplicate flag", "k[cc]a", output.ESyntax},
		{"unknown kv", "k[foo=1]a", output.ESyntax},
		{"duplicate kv", "k[n=1,n=2]a", output.ESyntax},
		{"bad escape", `txt[]C:\Users`, output.ESyntax},
		{"percent with r", "m[r]50%,50%", output.ESyntax},
		{"two frame mods", "m[r,w]1,2", output.ESyntax},
		{"unknown command", "foo[]x", output.ESyntax},
		{"unknown key", "k[]notakey", output.ESyntax},
		{"negative duration", "sleep[]-5", output.ESyntax},
		{"missing bracket close", "k[c a", output.ESyntax},
		{"no space form for k", "k enter", output.ESyntax},
		{"sleep out of range", "sleep[]61s", output.EValidate},
		{"d= out of range", "k[d=11s]a", output.EValidate},
		{"unbalanced ku", "ku[]shift", output.EValidate},
		{"over-long chord", "k[csamp]a+b+c+d", output.EValidate},
		{"scroll ticks range", "scroll[]down 99", output.EValidate},
		{"exec timeout range", "exec[timeout=61s]x", output.EValidate},
		// Malformed coordinate (coord.go parseCoord / parsePoint).
		{"bad coord non-numeric", "m[]abc,def", output.ESyntax},
		{"bad coord single value", "m[]1", output.ESyntax},
		{"coord NaN", "m[]NaN,0", output.ESyntax},
		{"coord Inf", "m[]Inf,0", output.ESyntax},
		{"coord scientific", "m[]1e3,0", output.ESyntax},
		// NaN/Inf/scientific scale (build.go buildCap, validate grammar).
		{"scale NaN", "cap[scale=NaN]", output.ESyntax},
		{"scale Inf", "cap[scale=Inf]", output.ESyntax},
		// Malformed selector (validate.go validateSelector).
		{"non-numeric id selector", "win[]id:abc", output.EValidate},
		{"non-numeric pid selector", "win[]pid:xyz", output.EValidate},
		{"invalid regex selector", "win[r](", output.EValidate},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			diags := analyze(t, tc.line)
			if len(diags) == 0 {
				t.Fatalf("expected a diagnostic for %q", tc.line)
			}
			d := diags[0]
			if d.Code != tc.code {
				t.Fatalf("code = %s, want %s (%q -> %s)", d.Code, tc.code, tc.line, d.Msg)
			}
			if d.Line != 1 {
				t.Errorf("line = %d, want 1", d.Line)
			}
			if d.Col < 1 {
				t.Errorf("col = %d, want >= 1", d.Col)
			}
		})
	}
}

// TestReportsAllErrors: every bad line yields a diagnostic before exit
// (help.txt:464). Blank and comment lines are counted but produce none.
func TestReportsAllErrors(t *testing.T) {
	lines := []string{"m 1,2", "# comment", "", "k[z]a", "foo[]x"}
	diags := analyze(t, lines...)
	if len(diags) != 3 {
		t.Fatalf("expected 3 diagnostics, got %d: %+v", len(diags), diags)
	}
	gotLines := []int{diags[0].Line, diags[1].Line, diags[2].Line}
	want := []int{1, 4, 5}
	for i := range want {
		if gotLines[i] != want[i] {
			t.Fatalf("diagnostic lines = %v, want %v", gotLines, want)
		}
	}
}

func TestCommentAndBlankIgnored(t *testing.T) {
	seq, diags := syntax.Parse([]string{"  # a comment", "", "\t", "k[]a"}, defaults)
	if len(diags) != 0 {
		t.Fatalf("unexpected diags: %+v", diags)
	}
	if len(seq.Ops) != 1 {
		t.Fatalf("expected 1 op, got %d", len(seq.Ops))
	}
	if seq.Ops[0].Line != 4 {
		t.Errorf("op line = %d, want 4 (line numbers count blanks/comments)", seq.Ops[0].Line)
	}
}

func TestPayloadBracketsAndVerbatim(t *testing.T) {
	// txt[]a[b]c types a[b]c (help.txt:173); leading spaces kept verbatim.
	seq, diags := syntax.Parse([]string{"txt[]a[b]c", "txt[]  two spaces"}, defaults)
	if len(diags) != 0 {
		t.Fatalf("unexpected diags: %+v", diags)
	}
	if got := seq.Ops[0].Text; got != "a[b]c" {
		t.Errorf("text = %q, want %q", got, "a[b]c")
	}
	if got := seq.Ops[1].Text; got != "  two spaces" {
		t.Errorf("text = %q, want %q", got, "  two spaces")
	}
}

func TestChordKeysTwoD(t *testing.T) {
	// k[]ctrl+shift+a b -> [["ctrl","shift","a"],["b"]] (help.txt:662).
	seq, _ := syntax.Parse([]string{"k[]ctrl+shift+a b"}, defaults)
	got := seq.Ops[0].Keys
	want := [][]string{{"ctrl", "shift", "a"}, {"b"}}
	if !equal2D(got, want) {
		t.Fatalf("keys = %v, want %v", got, want)
	}
}

func equal2D(a, b [][]string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if strings.Join(a[i], "+") != strings.Join(b[i], "+") {
			return false
		}
	}
	return true
}

// TestMouseButtonParse: md/mu (mouse button hold/release) parse positively,
// mirroring the k/kd/ku positive coverage. md defaults to the left button
// and takes an optional point; mu takes no payload (help.txt:322-331).
func TestMouseButtonParse(t *testing.T) {
	for _, line := range []string{"md[]", "mu[]", "md[b=right]", "md[]100,200", "md[w]50%,50%"} {
		seq, diags := syntax.Parse([]string{line}, defaults)
		if len(diags) != 0 {
			t.Errorf("line %q should parse, got %+v", line, diags)
			continue
		}
		if len(seq.Ops) != 1 {
			t.Errorf("line %q: got %d ops, want 1", line, len(seq.Ops))
		}
	}
	// A single md[]/mu[] pair balances and validates.
	if d := analyze(t, "md[]", "mu[]"); len(d) != 0 {
		t.Errorf("md[]/mu[] pair should validate, got %+v", d)
	}
}

// TestMouseHeldBalance mirrors the kd/ku held-balance rules for md/mu
// (validate.go:80-99, help.txt:468-469): a button held by md cannot be
// clicked or dragged, cannot be held again, and mu must release a held
// button.
func TestMouseHeldBalance(t *testing.T) {
	cases := []struct {
		name  string
		lines []string
	}{
		{"click button held by md", []string{"md[]", "c[]1,2"}},
		{"drag button held by md", []string{"md[]", "drag[]1,2 3,4"}},
		{"duplicate md hold", []string{"md[]", "md[]"}},
		{"mu without md", []string{"mu[]"}},
		{"mu wrong button", []string{"md[b=left]", "mu[b=right]"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			diags := analyze(t, tc.lines...)
			if len(diags) == 0 {
				t.Fatalf("expected a validation diagnostic for %v", tc.lines)
			}
			if diags[0].Code != output.EValidate {
				t.Fatalf("code = %s, want E_VALIDATE (%v -> %s)", diags[0].Code, tc.lines, diags[0].Msg)
			}
		})
	}
}
