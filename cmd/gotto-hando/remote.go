package main

import (
	"bufio"
	"context"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/kang-sw/gotto-hando/internal/ir"
	"github.com/kang-sw/gotto-hando/internal/output"
	"github.com/kang-sw/gotto-hando/internal/remote"
	"github.com/kang-sw/gotto-hando/internal/syntax"
)

// defaultTimeoutMS is --timeout's default (help.txt OPTIONS :56-59,
// LIMITS "--timeout default 300s").
const defaultTimeoutMS = 300_000

// scannerMaxBuf bounds one relayed JSONL line's size (a capture's inline
// base64 can be far larger than the 64 KiB INPUT line limit) - matches
// dispatch_windows.go's bridge forwarder buffer exactly.
const scannerMaxBuf = 16 * 1024 * 1024

// remoteBinName resolves --remote-bin (default "gotto-hando", help.txt
// OPTIONS :64-67).
func remoteBinName(opts parsedOptions) string {
	if opts.HasRemoteBin && opts.RemoteBin != "" {
		return opts.RemoteBin
	}
	return "gotto-hando"
}

// sshDeadline is the local kill deadline: --timeout (default 300s) + 5s
// grace (help.txt ERROR POLICY "Connection loss", help-remote.txt HOW IT
// WORKS "the local side additionally kills ssh at deadline + 5s").
func sshDeadline(opts parsedOptions) time.Duration {
	ms := defaultTimeoutMS
	if opts.Timeout != "" {
		if v, ok := syntax.ParseDurationMS(opts.Timeout); ok {
			ms = v
		}
	}
	return time.Duration(ms)*time.Millisecond + 5*time.Second
}

// forwardOptionsFrom adapts parsedOptions into internal/remote's
// ForwardOptions (the two packages cannot share a type: internal/remote
// must stay import-free of cmd/gotto-hando).
func forwardOptionsFrom(opts parsedOptions, requestPerms bool) remote.ForwardOptions {
	return remote.ForwardOptions{
		LocalVersion: version(), Quiet: opts.Quiet, Delay: opts.Delay,
		Timeout: opts.Timeout, KeepGoing: opts.KeepGoing,
		CapOnError: opts.CapOnError, RequestPerms: requestPerms,
	}
}

// sshArgs builds the full `ssh <dest> <remote-bin> local ...` argv.
func sshArgs(opts parsedOptions, fo remote.ForwardOptions) []string {
	args := []string{opts.Dest, remoteBinName(opts)}
	return append(args, remote.BuildForwardArgs(fo)...)
}

// startSSH spawns `ssh <dest> <remote-bin> local ...`, wiring stderr
// directly through (so ssh's own diagnostics and the remote's own
// pre-start stderr - including "version mismatch: ..." and usage errors -
// reach the caller verbatim, help-remote.txt TROUBLESHOOTING) and stdin
// from the given reader. It returns a scanner over the child's stdout.
func startSSH(ctx context.Context, args []string, stdin io.Reader, stderr io.Writer) (cmd *exec.Cmd, scanner *bufio.Scanner, err error) {
	cmd = exec.CommandContext(ctx, "ssh", args...)
	cmd.Stderr = stderr
	cmd.Stdin = stdin
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	sc := bufio.NewScanner(stdoutPipe)
	sc.Buffer(make([]byte, 4096), scannerMaxBuf)
	return cmd, sc, nil
}

// waitForStart reads and discards lines until it sees a "start" event
// (help-remote.txt HOW IT WORKS step 4: "wait for the start object; until
// then any failure - ssh, binary not found, usage error or version
// mismatch from the remote binary - is exit 3 and nothing ran"). ok is
// false on EOF or a non-start first line.
func waitForStart(scanner *bufio.Scanner) bool {
	if !scanner.Scan() {
		return false
	}
	_, ok := remote.DecodeStart(scanner.Bytes())
	return ok
}

// runRemote implements the <dest> ssh wrapper (help-remote.txt HOW IT
// WORKS, help.txt DESTINATIONS :830-864): rewrite [f] payloads locally,
// spawn ssh, relay the remote JSONL stream in the caller's chosen format,
// decoding captures/qclip[f] to local files and restoring err src from
// the local parse.
func runRemote(opts parsedOptions, seq *ir.Sequence, lines []string, stdout, stderr io.Writer, abort func(output.ErrorCode, string) int) int {
	rewritten, qclips, diags := remote.Rewrite(lines, seq)
	if len(diags) > 0 {
		writeDiagnostics(stderr, diags)
		return output.ExitValidation
	}

	args := sshArgs(opts, forwardOptionsFrom(opts, false))
	ctx, cancel := context.WithTimeout(context.Background(), sshDeadline(opts))
	defer cancel()

	stdinText := strings.Join(rewritten, "\n")
	if len(rewritten) > 0 {
		stdinText += "\n"
	}
	cmd, scanner, err := startSSH(ctx, args, strings.NewReader(stdinText), stderr)
	if err != nil {
		return abort(output.EConnect, "ssh spawn failed: "+err.Error())
	}

	if !waitForStart(scanner) {
		_ = cmd.Wait()
		return abort(output.EConnect, "remote process ended before its start line")
	}

	outDir := output.DefaultOutDir(opts.Out, opts.Dest, newRunID())
	qclipPaths := make(map[int]string, len(qclips))
	for _, q := range qclips {
		qclipPaths[q.Line] = q.FilePath
	}
	rl := &remote.Relay{OutDir: outDir, LocalSeq: seq, QClipPaths: qclipPaths}

	localStart := time.Now()
	var ok, errN, skip int
	stateUnknown := func() int {
		_ = cmd.Wait()
		_ = output.WriteDone(stdout, opts.JSONL, remote.SynthesizeDone(ok, errN, skip, localStart))
		return output.ExitStateUnknown
	}

	// A preflight-style "abort" (help.txt OUTPUT "Abort") can only ever be
	// the very first thing after "start" - engine.Run only ever sets
	// Summary.Aborted from Preflight, which always runs before any line
	// executes - so the first post-start line decides the outcome exactly
	// like dispatch_windows.go's forwardToBridge: an abort here must route
	// through the generic abort() closure (matching a direct local run's
	// own preflight-abort contract - plain: nothing on stdout, one stderr
	// line; jsonl: start+abort together) WITHOUT this wrapper's own local
	// start ever reaching stdout. Only once this first line is confirmed
	// NOT an abort does the wrapper commit to printing its own start and
	// begin streaming.
	if !scanner.Scan() {
		return abort(output.EConnect, "remote process ended before its start line")
	}
	first := append([]byte(nil), scanner.Bytes()...)
	if a, isAbort := remote.DecodeAbort(first); isAbort {
		_ = cmd.Wait()
		return abort(output.ErrorCode(a.Code), a.Msg)
	}

	_ = output.WriteStart(stdout, opts.JSONL, opts.Dest, outDir)

	line := first
	for {
		if d, isDone := remote.DecodeDone(line); isDone {
			_ = output.WriteDone(stdout, opts.JSONL, output.Done{
				OK: d.OK, Err: d.Err, Skip: d.Skip, ElapsedMS: d.ElapsedMS,
				HeldReleased: d.HeldReleased, StateUnknown: d.State == "unknown",
			})
			_ = cmd.Wait()
			if d.State == "unknown" {
				return output.ExitStateUnknown
			}
			if d.Err > 0 {
				return output.ExitRuntimeFailure
			}
			return output.ExitOK
		}

		res, rawForJSONL, perr := rl.Process(line)
		if perr != nil {
			return stateUnknown()
		}
		switch res.Status {
		case "err":
			errN++
		case "skip":
			skip++
		default:
			ok++
		}
		if opts.JSONL && rawForJSONL != nil {
			if _, werr := stdout.Write(append(rawForJSONL, '\n')); werr != nil {
				return stateUnknown()
			}
		} else {
			_ = output.WriteResult(stdout, opts.JSONL, opts.Quiet, res)
		}

		if !scanner.Scan() {
			// scanner ended without a done after start: connection lost
			// mid-run, or the local --timeout+5s deadline killed ssh
			// (ERROR POLICY "Connection loss").
			return stateUnknown()
		}
		line = append([]byte(nil), scanner.Bytes()...)
	}
}

// runRemoteRequestPerms implements `<dest> --request-perms`
// (help.txt OPTIONS --request-perms, help-remote.txt REMOTE MAC / REMOTE
// WINDOWS): same spawn/start-wait shape as runRemote, but with no stdin
// sequence and a "perms" terminal event instead of "done"
// (help.txt JSONL "--request-perms" :617-619).
func runRemoteRequestPerms(opts parsedOptions, stdout, stderr io.Writer, abort func(output.ErrorCode, string) int) int {
	args := sshArgs(opts, forwardOptionsFrom(opts, true))
	ctx, cancel := context.WithTimeout(context.Background(), sshDeadline(opts))
	defer cancel()

	cmd, scanner, err := startSSH(ctx, args, strings.NewReader(""), stderr)
	if err != nil {
		return abort(output.EConnect, "ssh spawn failed: "+err.Error())
	}

	if !waitForStart(scanner) {
		_ = cmd.Wait()
		return abort(output.EConnect, "remote process ended before its start line")
	}

	outDir := output.DefaultOutDir(opts.Out, opts.Dest, newRunID())

	// Same peek-before-commit contract as runRemote: a request-perms
	// abort (e.g. E_VALIDATE for a non-macOS remote, help.txt OPTIONS
	// --request-perms) is the first thing after "start" and must never
	// let this wrapper's own local start reach stdout in plain mode.
	if !scanner.Scan() {
		_ = cmd.Wait()
		_ = output.WriteStart(stdout, opts.JSONL, opts.Dest, outDir)
		_ = output.WriteDone(stdout, opts.JSONL, remote.SynthesizeDone(0, 0, 0, time.Now()))
		return output.ExitStateUnknown
	}
	line := append([]byte(nil), scanner.Bytes()...)

	if a, isAbort := remote.DecodeAbort(line); isAbort {
		_ = cmd.Wait()
		return abort(output.ErrorCode(a.Code), a.Msg)
	}

	_ = output.WriteStart(stdout, opts.JSONL, opts.Dest, outDir)

	if p, isPerms := remote.DecodePerms(line); isPerms {
		_ = cmd.Wait()
		if opts.JSONL {
			_, _ = stdout.Write(append(line, '\n'))
		} else {
			_, _ = io.WriteString(stdout, "perms=accessibility:"+p.Accessibility+",screen:"+p.Screen+"\n")
		}
		if p.Accessibility == "ok" && p.Screen == "ok" {
			return output.ExitOK
		}
		return output.ExitPreflight
	}

	_ = cmd.Wait()
	_ = output.WriteDone(stdout, opts.JSONL, remote.SynthesizeDone(0, 0, 0, time.Now()))
	return output.ExitStateUnknown
}
