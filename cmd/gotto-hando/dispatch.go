package main

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/kang-sw/gotto-hando/assets"
	"github.com/kang-sw/gotto-hando/internal/engine"
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
//  4. --bridge -> runs the session bridge (windows: the real named-pipe
//     listener, 260908-feat-remote-ssh Phase 0; every other GOOS: still
//     not implemented, abort E_VALIDATE, exit 2 - unchanged from before
//     this ticket).
//  5. --remote-bin / --inline-captures / --request-perms -> not
//     implemented, abort E_VALIDATE, exit 2.
//  6. -f and [line ...] both given -> abort E_VALIDATE, exit 2.
//  7. --check / --ir -> collect lines, then parse/inline/validate them
//     (analyze): on failure, empty stdout + E_SYNTAX/E_VALIDATE
//     diagnostics on stderr, exit 2; --check success prints "ok <n>
//     lines" and --ir success prints the IR JSON, both exit 0. Never
//     connects (bypasses dest resolution).
//  8. dest == "local" -> collect lines, parse/inline/validate them (same
//     contract as --check/--ir's failure path), build the platform backend
//     (darwin; every other GOOS still aborts E_VALIDATE, dispatch_other.go)
//     and run engine.Run against it, printing Preflight aborts or the
//     normal start/result/done stream. Any other dest (including none) ->
//     "remote destinations not implemented", abort E_VALIDATE, exit 2
//     (260908-feat-remote-ssh is a separate ticket).
//
// Phase 3 (260907-feat-darwin-backend Phase 1) wires the darwin backend
// behind dest=="local"; --timeout deadline enforcement stays inert
// (context.Background() is passed to engine.Run) and -q/--out/-k/
// --cap-on-error are read from opts but otherwise as documented; --delay
// seeds the IR defaults.
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
		return runBridge(opts, stdout, stderr, abort)
	}

	if opts.HasRemoteBin {
		return abort(output.EValidate, "--remote-bin not implemented")
	}
	if opts.RequestPerms {
		return requestPerms(opts, stdout, stderr, abort)
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
		lines, err := collectLines(opts.File, opts.HasFile, opts.Lines, stdin)
		if err != nil {
			fmt.Fprintf(stderr, "usage error: %v\n", err)
			return output.ExitValidation
		}
		seq, diags := parseAndValidate(opts, lines)
		if len(diags) > 0 {
			writeDiagnostics(stderr, diags)
			return output.ExitValidation
		}

		// Windows-only routing (260908-feat-remote-ssh Phase 0): an
		// ssh-started (or otherwise non-console) session forwards
		// non-exempt runs to `gotto-hando --bridge` instead of touching
		// the real local backend at all - shouldForwardToBridge is false
		// on every other GOOS, so this branch is a no-op there
		// (help-remote.txt SESSION BRIDGE "Detection").
		if shouldForwardToBridge(seq) {
			return forwardToBridge(opts, seq, stdout, stderr, abort)
		}

		be, err := newLocalBackend()
		if err != nil {
			return abort(output.EValidate, err.Error())
		}

		outDir := output.DefaultOutDir(opts.Out, opts.Dest, newRunID())
		sum := engine.Run(context.Background(), be, seq, engine.RunOptions{
			KeepGoing: opts.KeepGoing, CapOnError: opts.CapOnError,
			OutDir: outDir, InlineCaptures: opts.InlineCaptures,
		})
		if sum.Aborted {
			return abort(sum.AbortCode, sum.AbortMsg)
		}

		_ = output.WriteStart(stdout, opts.JSONL, opts.Dest, outDir)
		for _, r := range sum.Results {
			_ = output.WriteResult(stdout, opts.JSONL, opts.Quiet, r)
		}
		_ = output.WriteDone(stdout, opts.JSONL, sum.Done)
		return sum.Exit
	}
	return abort(output.EValidate, "remote destinations not implemented")
}

// newRunID generates the "<run-id>" path component used only to populate
// the JSONL start/abort object's "out" field in Phase 1 - no directory is
// created, so any reasonably distinct string works.
func newRunID() string {
	return time.Now().UTC().Format("20060102T150405.000Z")
}
