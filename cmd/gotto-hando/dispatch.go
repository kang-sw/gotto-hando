package main

import (
	"fmt"
	"io"
	"time"

	"github.com/kang-sw/gotto-hando/assets"
	"github.com/kang-sw/gotto-hando/internal/output"
)

// run implements the Phase 1 priority-ordered decision tree (CONCEPT.md
// ch.12 puts arg parsing / dest resolution / --bridge / --help branching
// directly in cmd/gotto-hando). See the Phase 1 plan
// (ai-docs/.plans/2026-09/08-0843-260907-feat-cli-core-phase1.md) for the
// exact ordering this follows:
//  1. any --help* flag (leftmost wins) -> print it verbatim, ignore
//     everything else, exit 0.
//  2. --version (and no help flag) -> print the version, exit 0.
//  3. --expect-version mismatch -> stderr message, exit 3.
//  4. --bridge -> not implemented, abort E_VALIDATE, exit 2.
//  5. --remote-bin / --inline-captures / --request-perms -> not
//     implemented, abort E_VALIDATE, exit 2.
//  6. -f and [line ...] both given -> abort E_VALIDATE, exit 2.
//  7. --check / --ir -> collect lines, then parse/inline/validate them
//     (analyze): on failure, empty stdout + E_SYNTAX/E_VALIDATE
//     diagnostics on stderr, exit 2; --check success prints "ok <n>
//     lines" and --ir success prints the IR JSON, both exit 0. Never
//     connects (bypasses dest resolution).
//  8. dest == "local" -> "platform backend not implemented"; any other
//     dest (including none) -> "remote destinations not implemented".
//     Both abort E_VALIDATE, exit 2.
//
// Phase 2 adds the parser/IR/validator behind --check/--ir only; the
// engine and backends exist but are not CLI-wired (no real backend), so
// every dest path still exits 2. Options that remain inert (-q,
// --timeout, -k, --cap-on-error, --out) are accepted and stored but
// otherwise unused; --delay seeds the IR defaults for --ir.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	for _, a := range args {
		switch a {
		case "--help":
			fmt.Fprint(stdout, assets.Help)
			return output.ExitOK
		case "--help-macos":
			fmt.Fprint(stdout, assets.HelpMacos)
			return output.ExitOK
		case "--help-windows":
			fmt.Fprint(stdout, assets.HelpWindows)
			return output.ExitOK
		case "--help-remote":
			fmt.Fprint(stdout, assets.HelpRemote)
			return output.ExitOK
		}
	}

	opts, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "usage error: %v\n", err)
		return output.ExitValidation
	}

	if opts.Version {
		fmt.Fprintln(stdout, version())
		return output.ExitOK
	}

	if opts.HasExpectVersion && opts.ExpectVersion != version() {
		fmt.Fprintf(stderr, "version mismatch: remote %s, expected %s\n", version(), opts.ExpectVersion)
		return output.ExitConnect
	}

	abort := func(code output.ErrorCode, msg string) int {
		outDir := output.DefaultOutDir(opts.Out, opts.Dest, newRunID())
		if opts.JSONL {
			_ = output.WriteAbort(stdout, true, opts.Dest, outDir, code, msg)
		} else {
			_ = output.WriteAbort(stderr, false, opts.Dest, outDir, code, msg)
		}
		return output.AbortExit(code)
	}

	if opts.Bridge {
		return abort(output.EValidate, "session bridge not implemented")
	}

	if opts.HasRemoteBin {
		return abort(output.EValidate, "--remote-bin not implemented")
	}
	if opts.InlineCaptures {
		return abort(output.EValidate, "--inline-captures not implemented")
	}
	if opts.RequestPerms {
		return abort(output.EValidate, "--request-perms not implemented")
	}

	if opts.HasFile && len(opts.Lines) > 0 {
		return abort(output.EValidate, "-f and [line ...] cannot be mixed")
	}

	if opts.Check || opts.IR {
		lines, err := collectLines(opts.File, opts.HasFile, opts.Lines, stdin)
		if err != nil {
			fmt.Fprintf(stderr, "usage error: %v\n", err)
			return output.ExitValidation
		}
		return analyze(opts, lines, stdout, stderr)
	}

	if opts.Dest == "local" {
		// Covers the bare-TTY-to-qinfo shortcut and --ping too: neither
		// has a backend yet.
		return abort(output.EValidate, "platform backend not implemented, nothing ran")
	}
	return abort(output.EValidate, "remote destinations not implemented")
}

// newRunID generates the "<run-id>" path component used only to populate
// the JSONL start/abort object's "out" field in Phase 1 - no directory is
// created, so any reasonably distinct string works.
func newRunID() string {
	return time.Now().UTC().Format("20060102T150405.000Z")
}
