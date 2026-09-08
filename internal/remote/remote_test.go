package remote

import (
	"reflect"
	"testing"
)

// TestBuildForwardArgsFixedOrder asserts the forwarded argv exactly
// matches help-remote.txt HOW IT WORKS step 2's fixed order: local
// --jsonl --inline-captures --expect-version <ver> [-q] [--delay D]
// [--timeout D] [-k] [--cap-on-error] [--request-perms] -f -
func TestBuildForwardArgsFixedOrder(t *testing.T) {
	got := BuildForwardArgs(ForwardOptions{
		LocalVersion: "0.1.0",
		Quiet:        true,
		Delay:        "10ms",
		Timeout:      "5s",
		KeepGoing:    true,
		CapOnError:   true,
		RequestPerms: true,
	})
	want := []string{
		"local", "--jsonl", "--inline-captures", "--expect-version", "0.1.0",
		"-q", "--delay", "10ms", "--timeout", "5s", "-k", "--cap-on-error",
		"--request-perms", "-f", "-",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BuildForwardArgs = %v, want %v", got, want)
	}
}

// TestBuildForwardArgsMinimal asserts every optional flag is omitted when
// unset, leaving only the mandatory prefix and trailing -f -.
func TestBuildForwardArgsMinimal(t *testing.T) {
	got := BuildForwardArgs(ForwardOptions{LocalVersion: "0.1.0"})
	want := []string{
		"local", "--jsonl", "--inline-captures", "--expect-version", "0.1.0",
		"-f", "-",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BuildForwardArgs = %v, want %v", got, want)
	}
}

// TestEscapeTextRoundTrip asserts escapeText is the exact inverse of the
// three TEXT ESCAPES specials applyEscapes decodes (help.txt:275-284): a
// payload built from decoding an escaped string, then re-encoded, comes
// back byte-identical.
func TestEscapeTextRoundTrip(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"hello", "hello"},
		{"a\\b", `a\\b`},
		{"line1\nline2", `line1\nline2`},
		{"a\tb", `a\tb`},
		{"back\\slash\nand\ttab", `back\\slash\nand\ttab`},
		{"unicode: 한글 ✓", "unicode: 한글 ✓"},
	}
	for _, c := range cases {
		if got := escapeText(c.raw); got != c.want {
			t.Errorf("escapeText(%q) = %q, want %q", c.raw, got, c.want)
		}
	}
}
