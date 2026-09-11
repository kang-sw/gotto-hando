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
//  5. `local --remote-bin ...` -> not implemented, abort E_VALIDATE, exit
//     2 (--remote-bin only matters for dest != "local", step 8 below).
//  6. --request-perms -> dest == "local": the platform's own
//     requestPerms (darwin: real prompts; every other GOOS: usage error);
//     dest != "local": runRemote.go's runRemoteRequestPerms (260908-feat-
//     remote-ssh Phase 1).
//  7. -f and [line ...] both given -> abort E_VALIDATE, exit 2.
//  8. --check / --ir -> collect lines, then parse/inline/validate them
//     (analyze): on failure, empty stdout + E_SYNTAX/E_VALIDATE
//     diagnostics on stderr, exit 2; --check success prints "ok <n>
//     lines" and --ir success prints the IR JSON, both exit 0. Never
//     connects (bypasses dest resolution).
//  9. collect lines and parse/inline/validate them for every remaining
//     invocation (both dest == "local" and dest != "local" need the same
//     local parse - the failure contract is identical to step 8's).
//     dest == "local": build the platform backend (darwin; every other
//     GOOS still aborts E_VALIDATE, dispatch_other.go/dispatch_dryrun.go)
//     and run engine.Run against it, printing Preflight aborts or the
//     normal start/result/done stream (--jsonl streams each result as
//     computed; plain buffers until the run finishes, unchanged).
//     dest != "local" (including "", now that a missing dest is no
//     longer special-cased): internal/remote's ssh wrapper
//     (cmd/gotto-hando/remote.go's runRemote, 260908-feat-remote-ssh
//     Phase 1) - help-remote.txt HOW IT WORKS, help.txt DESTINATIONS
//     :830-864.
//
// Phase 3 (260907-feat-darwin-backend Phase 1) wired the darwin backend
// behind dest=="local"; --timeout deadline enforcement for a direct local
// run stays inert (context.Background() is passed to engine.Run) and
// -q/--out/-k/--cap-on-error are read from opts but otherwise as
// documented; --delay seeds the IR defaults. 260908-feat-remote-ssh Phase
// 1 enforces --timeout (+5s grace) for the dest != "local" ssh wrapper
// only (remote.go's sshDeadline).
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

	// --remote-bin only matters once ssh is actually spawned (dest !=
	// "local"); `local --remote-bin ...` keeps its pre-260908-feat-
	// remote-ssh usage-error message (help.txt "--remote-bin PATH <dest>
	// only").
	if opts.HasRemoteBin && opts.Dest == "local" {
		return abort(output.EValidate, "--remote-bin not implemented")
	}
	if opts.RequestPerms {
		if opts.Dest != "local" {
			return runRemoteRequestPerms(opts, stdout, stderr, abort)
		}
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

	if opts.Dest == "local" {
		// On macOS and Windows, an ssh-started (or otherwise non-console)
		// session forwards every run to `gotto-hando --bridge` instead of
		// touching the ssh-side backend. This includes query-only diagnostics,
		// which must describe the same GUI session later operations will use.
		// shouldForwardToBridge is false on every other GOOS, so this branch is
		// a no-op there (help-remote.txt SESSION BRIDGE "Detection").
		// forwardToBridge prints its own start object; it is unaffected by the
		// dest!="local" ssh transport.
		if shouldForwardToBridge(seq) {
			return forwardToBridge(opts, seq, stdout, stderr, abort)
		}

		outDir := output.DefaultOutDir(opts.Out, opts.Dest, newRunID())

		// --jsonl prints "start" right after parse/validate, before
		// Preflight/the backend even exists, and streams each result as
		// it is computed (engine.RunOptions.OnResult) - this is what lets
		// the ssh-spawned remote (this exact `local --jsonl ...` code
		// path) let its caller (runRemote) tell "process died before
		// start" (exit 3) apart from "aborted after start" (exit 4) or
		// "connection lost after start" (exit 5). Plain mode keeps its
		// original fully-buffered shape below (output.WriteAbort already
		// never prints an early "out" line on abort, so this restructure
		// changes only timing, never bytes, for a direct `local` run in
		// either mode - 260908-feat-remote-ssh Phase 1's byte-identity
		// guardrail).
		if opts.JSONL {
			_ = output.WriteStart(stdout, true, opts.Dest, outDir)
			be, err := newLocalBackend()
			if err != nil {
				_ = output.WriteAbortEvent(stdout, true, output.EValidate, err.Error())
				return output.AbortExit(output.EValidate)
			}
			// A cancelable ctx, cancelled the moment a result write to stdout
			// fails (broken pipe / EOF on the far side) - mirrors
			// internal/bridge/session.go's identical pattern exactly (Phase
			// 0). This is what makes stdout EOF mid-run on the ssh-spawned
			// remote (this exact `local --jsonl` path) stop the engine
			// promptly instead of blindly finishing every remaining op on a
			// dead connection: engine.Run's per-op ctx.Err() check skips
			// the rest as "skip", and its unconditional end-of-run
			// releaseAll still releases held keys/buttons either way
			// (260908-feat-remote-ssh Phase 1 review T3). Plain mode is
			// untouched - it has no per-result write to fail against until
			// the whole run has already finished.
			runCtx, cancel := context.WithCancel(context.Background())
			defer cancel()
			sum := engine.Run(runCtx, be, seq, engine.RunOptions{
				KeepGoing: opts.KeepGoing, CapOnError: opts.CapOnError,
				OutDir: outDir, InlineCaptures: opts.InlineCaptures,
				OnResult: func(r output.Result) {
					if werr := output.WriteResult(stdout, true, opts.Quiet, r); werr != nil {
						cancel()
					}
				},
			})
			if sum.Aborted {
				_ = output.WriteAbortEvent(stdout, true, sum.AbortCode, sum.AbortMsg)
				return output.AbortExit(sum.AbortCode)
			}
			_ = output.WriteDone(stdout, true, sum.Done)
			return sum.Exit
		}

		be, err := newLocalBackend()
		if err != nil {
			return abort(output.EValidate, err.Error())
		}
		sum := engine.Run(context.Background(), be, seq, engine.RunOptions{
			KeepGoing: opts.KeepGoing, CapOnError: opts.CapOnError,
			OutDir: outDir, InlineCaptures: opts.InlineCaptures,
		})
		if sum.Aborted {
			return abort(sum.AbortCode, sum.AbortMsg)
		}

		_ = output.WriteStart(stdout, false, opts.Dest, outDir)
		for _, r := range sum.Results {
			_ = output.WriteResult(stdout, false, opts.Quiet, r)
		}
		_ = output.WriteDone(stdout, false, sum.Done)
		return sum.Exit
	}

	// 260908-feat-remote-ssh Phase 1: <dest> != "local" runs the ssh
	// wrapper (help-remote.txt HOW IT WORKS, help.txt DESTINATIONS
	// :830-864) instead of the old "remote destinations not implemented"
	// stub.
	return runRemote(opts, seq, lines, stdout, stderr, abort)
}

// newRunID generates the "<run-id>" path component used only to populate
// the JSONL start/abort object's "out" field in Phase 1 - no directory is
// created, so any reasonably distinct string works.
func newRunID() string {
	return time.Now().UTC().Format("20060102T150405.000Z")
}
