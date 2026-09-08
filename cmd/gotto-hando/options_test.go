package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestParseArgsUnknownOption asserts that an unrecognized flag produces the
// documented parseArgs error (options.go:113) and that run() surfaces it as
// a usage error, exit 2, with the exact stderr text (dispatch.go:53-57).
// This path had zero test coverage before (review finding
// cli-core-p1-test.md Important #1).
func TestParseArgsUnknownOption(t *testing.T) {
	args := []string{"--not-real", "value"}

	_, err := parseArgs(args)
	if err == nil {
		t.Fatal("parseArgs: want error for an unknown option, got nil")
	}
	wantErr := "unknown option: --not-real"
	if err.Error() != wantErr {
		t.Fatalf("parseArgs error = %q, want %q", err.Error(), wantErr)
	}

	var stdout, stderr bytes.Buffer
	code := run(args, strings.NewReader(""), &stdout, &stderr)
	if code != 2 {
		t.Fatalf("run() exit = %d, want 2", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("run() stdout = %q, want empty", stdout.String())
	}
	wantStderr := "usage error: unknown option: --not-real\n"
	if stderr.String() != wantStderr {
		t.Fatalf("run() stderr = %q, want %q", stderr.String(), wantStderr)
	}
}

// TestParseArgsMissingValue asserts that an option which consumes a value
// (options.go:119) errors when none is supplied - e.g. a bare -f or
// --delay at the end of argv - and that run() reports it as a usage
// error, exit 2, with the exact stderr text. This path had zero test
// coverage before (review finding cli-core-p1-test.md Important #1).
func TestParseArgsMissingValue(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"short-flag-at-end", []string{"local", "-f"}, "option -f requires a value"},
		{"long-flag-at-end", []string{"local", "--delay"}, "option --delay requires a value"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := parseArgs(c.args)
			if err == nil {
				t.Fatalf("parseArgs(%v): want error, got nil", c.args)
			}
			if err.Error() != c.want {
				t.Fatalf("parseArgs error = %q, want %q", err.Error(), c.want)
			}

			var stdout, stderr bytes.Buffer
			code := run(c.args, strings.NewReader(""), &stdout, &stderr)
			if code != 2 {
				t.Fatalf("run() exit = %d, want 2", code)
			}
			if stdout.Len() != 0 {
				t.Fatalf("run() stdout = %q, want empty", stdout.String())
			}
			wantStderr := "usage error: " + c.want + "\n"
			if stderr.String() != wantStderr {
				t.Fatalf("run() stderr = %q, want %q", stderr.String(), wantStderr)
			}
		})
	}
}

// TestParseArgsInterleavedOptionsAndLines asserts that the manual argv
// scanner accepts options in any position relative to <dest> and
// [line ...], per SYNOPSIS (help.txt:44-45: "Options start with '-';
// command lines start with a lowercase letter, '#' or whitespace, so
// options and lines may be given in any order") - the plan's own Codebase
// Findings cite this as the specific reason a manual scanner replaced the
// stdlib flag package (which stops scanning at the first non-flag
// argument). No prior test put an option between <dest> and a line, or
// after a line (review finding cli-core-p1-test.md Important #2).
func TestParseArgsInterleavedOptionsAndLines(t *testing.T) {
	// Mirrors the review's own example: an option between <dest> and a
	// line, and another option after the line.
	o, err := parseArgs([]string{"local", "--jsonl", "k[]a", "--keep-going"})
	if err != nil {
		t.Fatalf("parseArgs: unexpected error: %v", err)
	}
	if o.Dest != "local" {
		t.Fatalf("Dest = %q, want %q", o.Dest, "local")
	}
	if !o.JSONL {
		t.Fatal("JSONL = false, want true (option between <dest> and a line must be recognized as an option)")
	}
	if !o.KeepGoing {
		t.Fatal("KeepGoing = false, want true (option after a line must be recognized as an option, not collected as a line)")
	}
	if len(o.Lines) != 1 || o.Lines[0] != "k[]a" {
		t.Fatalf("Lines = %v, want [%q]", o.Lines, "k[]a")
	}

	// A value-consuming option interleaved between two lines must
	// consume only its own value, and neither the option nor its value
	// may be misclassified as a line.
	o2, err := parseArgs([]string{"local", "first line", "--out", "/tmp/x", "second line"})
	if err != nil {
		t.Fatalf("parseArgs: unexpected error: %v", err)
	}
	if o2.Out != "/tmp/x" {
		t.Fatalf("Out = %q, want %q", o2.Out, "/tmp/x")
	}
	if len(o2.Lines) != 2 || o2.Lines[0] != "first line" || o2.Lines[1] != "second line" {
		t.Fatalf("Lines = %v, want [%q %q]", o2.Lines, "first line", "second line")
	}
}
