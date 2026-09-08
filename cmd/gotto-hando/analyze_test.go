package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestIRSuccess: --ir prints a parseable IR JSON document and exits 0 with
// empty stderr (help.txt EXECUTION failure contract, IR JSON).
func TestIRSuccess(t *testing.T) {
	out, errOut, code := runBin(t, "", "local", "--ir", "k[c]v")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr=%q)", code, errOut)
	}
	if errOut != "" {
		t.Errorf("stderr = %q, want empty", errOut)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("--ir output is not valid JSON: %v\n%s", err, out)
	}
	if doc["v"].(float64) != 1 {
		t.Errorf("v = %v, want 1", doc["v"])
	}
}

// TestCheckSuccess: --check prints "ok <n> lines" counting every input line
// (blank/comment included) and exits 0.
func TestCheckSuccess(t *testing.T) {
	out, errOut, code := runBin(t, "", "local", "--check", "k[]a", "# note", "k[]b")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr=%q)", code, errOut)
	}
	if out != "ok 3 lines\n" {
		t.Errorf("stdout = %q, want %q", out, "ok 3 lines\n")
	}
}

// TestSyntaxFailure: a parse error yields empty stdout, an E_SYNTAX
// diagnostic with the indented source line on stderr, and exit 2.
func TestSyntaxFailure(t *testing.T) {
	out, errOut, code := runBin(t, "", "local", "--check", "m 1,2")
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
	if !strings.HasPrefix(errOut, "E_SYNTAX line 1 col 2: ") {
		t.Errorf("stderr missing E_SYNTAX prefix:\n%s", errOut)
	}
	if !strings.Contains(errOut, "\n  m 1,2\n") {
		t.Errorf("stderr missing two-space-indented source line:\n%s", errOut)
	}
}

// TestValidateFailureJSONL: a validation error uses the E_VALIDATE code and
// keeps stdout empty even with --jsonl (help.txt failure contract).
func TestValidateFailureJSONL(t *testing.T) {
	out, errOut, code := runBin(t, "", "local", "--jsonl", "--ir", "ku[]shift")
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty (even with --jsonl)", out)
	}
	if !strings.HasPrefix(errOut, "E_VALIDATE line 1 col 1: ") {
		t.Errorf("stderr missing E_VALIDATE prefix:\n%s", errOut)
	}
}

// TestReportsAllErrorsCLI: every bad line is reported before exit.
func TestReportsAllErrorsCLI(t *testing.T) {
	_, errOut, code := runBin(t, "", "local", "--check", "m 1,2", "foo[]x")
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if strings.Count(errOut, "E_SYNTAX line") != 2 {
		t.Errorf("want 2 diagnostics, got:\n%s", errOut)
	}
}

// TestCheckFromStdin: --check reads -f - and counts lines from stdin.
func TestCheckFromStdin(t *testing.T) {
	out, errOut, code := runBin(t, "k[]a\nk[]b\n", "local", "--check", "-f", "-")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr=%q)", code, errOut)
	}
	if out != "ok 2 lines\n" {
		t.Errorf("stdout = %q, want %q", out, "ok 2 lines\n")
	}
}

// TestBadCoordFailure: a malformed coordinate is an E_SYNTAX parse error
// with the exact "<CODE> line n col c:" format, the indented source line,
// empty stdout and exit 2 (help.txt failure contract).
func TestBadCoordFailure(t *testing.T) {
	out, errOut, code := runBin(t, "", "local", "--check", "m[]abc,def")
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
	if !strings.HasPrefix(errOut, "E_SYNTAX line 1 col 4: ") {
		t.Errorf("stderr missing E_SYNTAX prefix:\n%s", errOut)
	}
	if !strings.Contains(errOut, "\n  m[]abc,def\n") {
		t.Errorf("stderr missing two-space-indented source line:\n%s", errOut)
	}
}

// TestBadSelectorFailure: a non-numeric id: selector is an E_VALIDATE error
// with the exact format, empty stdout and exit 2.
func TestBadSelectorFailure(t *testing.T) {
	out, errOut, code := runBin(t, "", "local", "--check", "win[]id:abc")
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
	if !strings.HasPrefix(errOut, "E_VALIDATE line 1 col 1: ") {
		t.Errorf("stderr missing E_VALIDATE prefix:\n%s", errOut)
	}
	if !strings.Contains(errOut, "\n  win[]id:abc\n") {
		t.Errorf("stderr missing two-space-indented source line:\n%s", errOut)
	}
}

// TestScaleNaNRejected: cap[scale=NaN] must NOT validate as ok (the guard
// closes the strconv.ParseFloat NaN/Inf loophole); it is an exit-2 error
// with empty stdout, not "ok 1 lines".
func TestScaleNaNRejected(t *testing.T) {
	out, _, code := runBin(t, "", "local", "--check", "cap[scale=NaN]")
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (NaN scale must be rejected)", code)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty (must not print ok)", out)
	}
}
