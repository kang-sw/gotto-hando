// Command fakessh stands in for the real `ssh` binary in
// 260908-feat-remote-ssh Phase 1's integration test harness
// (cmd/gotto-hando/remote_test.go): it is built once, placed on a temp
// PATH ahead of any real ssh, and invoked by the gotto-hando binary under
// test exactly as it would invoke real ssh: `ssh <dest> <remote-bin>
// local ...`. Its own argv[1] (<dest>) selects a scripted failure
// scenario; every other <dest> passes through to a REAL spawn of
// <remote-bin> (found on PATH, normally the `-tags dryrun` gotto-hando
// binary the harness also builds) with inherited stdio, so most test
// scenarios exercise the genuine remote binary end to end.
package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "fakessh: need <dest> <remote-bin> [args...]")
		return 2
	}
	dest, remoteBin, rest := args[0], args[1], args[2:]

	switch dest {
	case "connectfail":
		// Simulates ssh itself failing before ever reaching a remote
		// process: nothing on stdout, one stderr line, exit 255 (a
		// typical ssh client failure code).
		fmt.Fprintln(os.Stderr, "ssh: connect to host connectfail port 22: Connection refused")
		return 255

	case "abortsession":
		// Scripts the remote's own JSONL directly (no real remote binary
		// involved) so the wrapper's "abort after start" relay path is
		// exercised deterministically: start, then abort E_SESSION.
		fmt.Print(`{"event":"start","out":"/remote/out/","dest":"local","target":{"os":"windows"}}` + "\n")
		fmt.Print(`{"event":"abort","code":"E_SESSION","msg":"start ` + "`gotto-hando --bridge`" + ` in the logged-on GUI session"}` + "\n")
		return 1

	case "abortconnect":
		// Same shape, but E_CONNECT (the bridge's own IR-version check
		// failing, help.txt DESTINATIONS) - a DIFFERENT wrapper exit code
		// (3) than the generic pre-start connect failure above, since
		// this one arrives AFTER a start object.
		fmt.Print(`{"event":"start","out":"/remote/out/","dest":"local","target":{"os":"windows"}}` + "\n")
		fmt.Print(`{"event":"abort","code":"E_CONNECT","msg":"IR schema version mismatch"}` + "\n")
		return 1

	case "killmidrun":
		// Prints a start object and exactly one ok result, then exits
		// without a done/abort - simulating an ssh connection dropped
		// mid-run (ERROR POLICY "Connection loss"), AFTER the wrapper has
		// already committed to printing its own start object.
		fmt.Print(`{"event":"start","out":"/remote/out/","dest":"local","target":{"os":"darwin"}}` + "\n")
		fmt.Print(`{"line":1,"status":"ok","cmd":"qinfo","os":"darwin","osver":"","arch":"","ver":"0.1.0","primary":"0","desktop_x":0,"desktop_y":0,"desktop_w":0,"desktop_h":0,"displays":0,"session":"local","perms":"n/a","t_ms":1}` + "\n")
		return 1

	case "killbeforeresult":
		// Prints ONLY a start object, then exits without a single result
		// or a done/abort - the connection-loss case BEFORE the wrapper
		// has printed its own start (remote.go's pre-first-result EOF
		// site, as opposed to killmidrun's post-commit case above) - the
		// distinction review NEW-1 is about: the wrapper must still print
		// exactly one start of its own here, but the mid-run sites above
		// must not print a SECOND one when they have already committed.
		fmt.Print(`{"event":"start","out":"/remote/out/","dest":"local","target":{"os":"darwin"}}` + "\n")
		return 1

	case "badusage":
		// Real-spawns the remote binary but with an injected unknown
		// option, so the remote's own parseArgs fails before it ever gets
		// near --expect-version or a start line (a "pre---expect-version
		// usage error").
		return spawnPassthrough(remoteBin, append(rest, "--totally-bogus-option-xyz"), nil)

	case "versionmismatch":
		// Real-spawns the remote binary with an EXTRA --expect-version
		// appended after the wrapper's own correct one (parseArgs' last
		// occurrence wins, cmd/gotto-hando/options.go) - the remote's own
		// pre-existing version-mismatch check (dispatch.go, unchanged by
		// this ticket) then genuinely fires and prints "version mismatch:
		// remote <x>, expected <y>" to its OWN stderr and exits 3 before
		// ever printing a start line (help-remote.txt HOW IT WORKS step 3,
		// TROUBLESHOOTING) - a real remote-side mismatch, not a fabricated
		// stdout script (review T2).
		return spawnPassthrough(remoteBin, append(rest, "--expect-version", "9.9.9"), os.Stdin)

	case "winrelay":
		// A scripted successful run (no real remote binary involved)
		// whose target is windows regardless of the local test host's own
		// GOOS - proof that a relayed "start" object carries the REMOTE's
		// real target.os, not the local wrapper's runtime.GOOS (review
		// I1). Deterministic on every host, including a darwin CI runner
		// where target.os would otherwise coincidentally match by
		// accident.
		fmt.Print(`{"event":"start","out":"/remote/out/","dest":"local","target":{"os":"windows"}}` + "\n")
		fmt.Print(`{"line":1,"status":"ok","cmd":"qinfo","os":"windows","osver":"","arch":"","ver":"0.1.0","primary":"ctrl","desktop_x":0,"desktop_y":0,"desktop_w":0,"desktop_h":0,"displays":0,"session":"bridge","perms":"n/a","t_ms":1}` + "\n")
		fmt.Print(`{"event":"done","ok":1,"err":0,"skip":0,"elapsed_ms":2,"held_released":0}` + "\n")
		return 0

	case "pastefail":
		// A scripted run (no real remote binary involved) whose one line
		// is a genuine "err" result for a paste command - exercising the
		// wrapper's err-src-restoration through the REAL runRemote spawn/
		// relay loop end to end, not just internal/remote/relay_test.go's
		// unit-level Relay.Process coverage (review I-test). The wire
		// "src" is deliberately the INLINED form a real remote would see
		// after [f] rewrite, so the test can assert the wrapper restores
		// the caller's original "paste[f]./cmd.py" from its own local
		// parse, not this wire value.
		fmt.Print(`{"event":"start","out":"/remote/out/","dest":"local","target":{"os":"darwin"}}` + "\n")
		fmt.Print(`{"line":1,"status":"err","cmd":"paste","src":"paste[]inlined cmd.py contents","code":"E_CLIPBOARD","msg":"clipboard set failed"}` + "\n")
		fmt.Print(`{"event":"done","ok":0,"err":1,"skip":0,"elapsed_ms":3,"held_released":0}` + "\n")
		return 0
	}

	return spawnPassthrough(remoteBin, rest, os.Stdin)
}

// spawnPassthrough is the default behavior: find remoteBin on PATH and run
// it with the given args, stdio connected straight through - mirroring
// what real ssh does once it has connected and started the remote
// command. stdin, when nil, is left disconnected (the caller has nothing
// to send, e.g. --request-perms).
func spawnPassthrough(remoteBin string, args []string, stdin io.Reader) int {
	path, err := exec.LookPath(remoteBin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bash: %s: command not found\n", remoteBin)
		return 127
	}
	cmd := exec.Command(path, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if stdin != nil {
		cmd.Stdin = stdin
	}
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		fmt.Fprintln(os.Stderr, "fakessh: spawn failed:", err)
		return 255
	}
	return 0
}
