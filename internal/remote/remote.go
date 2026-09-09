// Package remote implements the GOOS-agnostic, pure/testable pieces of the
// <dest> ssh wrapper (help-remote.txt HOW IT WORKS, help.txt DESTINATIONS
// :830-864): the forwarded-argv builder, the local [f]-rewrite pass, the
// JSONL relay-event decoders, and the local per-line rewrite (captures,
// qclip[f] write-back, err src restoration). cmd/gotto-hando/remote.go is
// the thin CLI glue on top of this package: it owns the os/exec ssh
// process and stdout/stderr wiring; everything here is a pure function or
// small struct so it can be unit-tested without spawning anything.
package remote

import "strings"

// ForwardOptions is the subset of parsedOptions the ssh wrapper forwards
// to the remote process (OPTIONS "Forwarded to the remote process...",
// help.txt:47-64). LocalVersion is always forwarded as --expect-version;
// --out and --remote-bin are local-only and never appear here.
type ForwardOptions struct {
	LocalVersion string
	Quiet        bool
	Delay        string
	Timeout      string
	KeepGoing    bool
	CapOnError   bool
	RequestPerms bool
}

// BuildForwardArgs builds the remote command line's own argv, in the fixed
// order help-remote.txt HOW IT WORKS step 2 documents:
//
//	local --jsonl --inline-captures --expect-version <local version> \
//	    [-q] [--delay D] [--timeout D] [-k] [--cap-on-error] \
//	    [--request-perms] -f -
//
// The caller prepends "<dest> <remote-bin>" (the ssh destination and the
// remote binary name) - this function only knows about the remote
// process's own flags, not how it gets invoked over ssh.
func BuildForwardArgs(o ForwardOptions) []string {
	args := []string{"local", "--jsonl", "--inline-captures", "--expect-version", o.LocalVersion}
	if o.Quiet {
		args = append(args, "-q")
	}
	if o.Delay != "" {
		args = append(args, "--delay", o.Delay)
	}
	if o.Timeout != "" {
		args = append(args, "--timeout", o.Timeout)
	}
	if o.KeepGoing {
		args = append(args, "-k")
	}
	if o.CapOnError {
		args = append(args, "--cap-on-error")
	}
	if o.RequestPerms {
		args = append(args, "--request-perms")
	}
	args = append(args, "-f", "-")
	return args
}

// escapeText encodes a [f]-inlined payload back into TEXT ESCAPES form
// (help.txt:275-284) for the rewritten txt[]/paste[]/clip[] line: only the
// three specials applyEscapes decodes need re-escaping (\ -> \\, LF -> \n,
// TAB -> \t); everything else, including non-ASCII, passes through
// literally, matching applyEscapes' own decode contract exactly (no
// \uXXXX encoder exists or is needed - applyEscapes never requires it on
// decode for characters outside those three specials).
func escapeText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
