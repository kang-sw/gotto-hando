package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These tests drive the real binPath binary against testdata/fakessh (put
// on PATH ahead of any real ssh by TestMain) to exercise the <dest> ssh
// wrapper (cmd/gotto-hando/remote.go, internal/remote) end to end: argv
// building, the local [f] rewrite, the start/abort/done relay and its
// exit-code mapping, all without touching a real network or OS backend.
// fakessh's own doc comment (testdata/fakessh/main.go) lists every
// scripted <dest> scenario.

// TestRemoteConnectFailure asserts an ssh-level failure before any start
// line (help-remote.txt TROUBLESHOOTING "Connection refused") is exit 3,
// empty stdout, and ssh's own diagnostic plus the wrapper's own abort line
// on stderr.
func TestRemoteConnectFailure(t *testing.T) {
	out, errOut, code := runBin(t, "", "connectfail", "qinfo")
	if code != 3 {
		t.Fatalf("exit = %d, want 3 (stderr=%q)", code, errOut)
	}
	if out != "" {
		t.Fatalf("stdout = %q, want empty", out)
	}
	if !strings.Contains(errOut, "Connection refused") {
		t.Errorf("stderr = %q, want it to contain ssh's own diagnostic", errOut)
	}
	if !strings.Contains(errOut, "abort: remote process ended before its start line (E_CONNECT)") {
		t.Errorf("stderr = %q, want the wrapper's own abort line", errOut)
	}
}

// TestRemoteMissingRemoteBin asserts a --remote-bin naming a binary absent
// from the remote PATH is exit 3 (help-remote.txt TROUBLESHOOTING "command
// not found"), nothing on stdout.
func TestRemoteMissingRemoteBin(t *testing.T) {
	out, errOut, code := runBin(t, "", "devbox", "--remote-bin", "no-such-gotto-hando-binary", "qinfo")
	if code != 3 {
		t.Fatalf("exit = %d, want 3 (stderr=%q)", code, errOut)
	}
	if out != "" {
		t.Fatalf("stdout = %q, want empty", out)
	}
	if !strings.Contains(errOut, "command not found") {
		t.Errorf("stderr = %q, want it to contain the remote shell's own diagnostic", errOut)
	}
}

// TestRemoteBadUsage asserts a usage error from the remote process before
// its start line (an option gotto-hando local rejects, help-remote.txt
// TROUBLESHOOTING "usage error, exit 2") is exit 3 locally - nothing ran -
// with the remote's own usage error relayed on stderr.
func TestRemoteBadUsage(t *testing.T) {
	out, errOut, code := runBin(t, "", "badusage", "qinfo")
	if code != 3 {
		t.Fatalf("exit = %d, want 3 (stderr=%q)", code, errOut)
	}
	if out != "" {
		t.Fatalf("stdout = %q, want empty", out)
	}
	if !strings.Contains(errOut, "usage error:") {
		t.Errorf("stderr = %q, want the remote's own usage error relayed", errOut)
	}
}

// TestRemoteAbortSessionPlain asserts an abort object arriving right after
// start (E_SESSION here) is relayed as help.txt's "abort: <message>
// (<E_CODE>)" on stderr, with stdout staying completely EMPTY - this is
// the 260908-feat-remote-ssh Phase 1 byte-identity guardrail's abort-path
// evidence: the wrapper's own "start" line must never reach stdout when
// the very next line after the remote's start is an abort.
func TestRemoteAbortSessionPlain(t *testing.T) {
	out, errOut, code := runBin(t, "", "abortsession", "qinfo")
	if code != 4 {
		t.Fatalf("exit = %d, want 4 (stderr=%q)", code, errOut)
	}
	if out != "" {
		t.Fatalf("stdout = %q, want completely EMPTY (byte-identity guardrail: no early \"out\" line on abort)", out)
	}
	want := "abort: start `gotto-hando --bridge` in the logged-on GUI session (E_SESSION)\n"
	if errOut != want {
		t.Fatalf("stderr = %q, want %q", errOut, want)
	}
}

// TestRemoteAbortSessionJSONL asserts the same abort, in --jsonl, prints
// exactly one start object (LOCAL dest/out, not the remote's own) followed
// by the abort object on stdout, nothing on stderr.
func TestRemoteAbortSessionJSONL(t *testing.T) {
	out, errOut, code := runBin(t, "", "--jsonl", "abortsession", "qinfo")
	if code != 4 {
		t.Fatalf("exit = %d, want 4 (stderr=%q)", code, errOut)
	}
	if errOut != "" {
		t.Fatalf("stderr = %q, want empty", errOut)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("stdout lines = %d, want 2 (start, abort): %q", len(lines), out)
	}
	var start struct {
		Event string `json:"event"`
		Dest  string `json:"dest"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &start); err != nil {
		t.Fatalf("decode start line: %v (%q)", err, lines[0])
	}
	if start.Event != "start" || start.Dest != "abortsession" {
		t.Errorf("start = %+v, want event=start dest=abortsession", start)
	}
	var abort struct {
		Event string `json:"event"`
		Code  string `json:"code"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &abort); err != nil {
		t.Fatalf("decode abort line: %v (%q)", err, lines[1])
	}
	if abort.Event != "abort" || abort.Code != "E_SESSION" {
		t.Errorf("abort = %+v, want event=abort code=E_SESSION", abort)
	}
}

// TestRemoteAbortConnect asserts an E_CONNECT abort after start (the
// bridge's own IR-schema-version mismatch, help-remote.txt HOW IT WORKS
// step 3/4) is a DIFFERENT exit code (3) than E_SESSION above, despite
// both being abort objects arriving after start.
func TestRemoteAbortConnect(t *testing.T) {
	out, errOut, code := runBin(t, "", "abortconnect", "qinfo")
	if code != 3 {
		t.Fatalf("exit = %d, want 3 (stderr=%q)", code, errOut)
	}
	if out != "" {
		t.Fatalf("stdout = %q, want empty", out)
	}
	want := "abort: IR schema version mismatch (E_CONNECT)\n"
	if errOut != want {
		t.Fatalf("stderr = %q, want %q", errOut, want)
	}
}

// TestRemoteKillMidRun asserts a connection dropped mid-run (no done/abort
// after some results streamed) synthesizes a done object with
// state=unknown and exits 5 (ERROR POLICY "Connection loss"), after
// relaying whatever results DID arrive.
func TestRemoteKillMidRun(t *testing.T) {
	out, errOut, code := runBin(t, "", "killmidrun", "qinfo")
	if code != 5 {
		t.Fatalf("exit = %d, want 5 (stderr=%q)", code, errOut)
	}
	if errOut != "" {
		t.Fatalf("stderr = %q, want empty", errOut)
	}
	if !strings.HasPrefix(out, "out ") {
		t.Fatalf("stdout = %q, want it to start with the \"out <dir>\" line", out)
	}
	if !strings.Contains(out, "1 ok qinfo") {
		t.Errorf("stdout = %q, want the one relayed qinfo result", out)
	}
	if !strings.Contains(out, "done ok=1 err=0 skip=0") || !strings.Contains(out, "state=unknown") {
		t.Errorf("stdout = %q, want a done line with state=unknown", out)
	}
}

// TestRemoteRequestPermsAbort asserts --request-perms forwarded to a
// non-macOS (here: the dryrun harness's fake remote, dispatch_dryrun.go)
// remote aborts E_VALIDATE right after start, exit 2 - same abort-after-
// start contract as a normal run (help.txt OPTIONS --request-perms).
func TestRemoteRequestPermsAbort(t *testing.T) {
	out, errOut, code := runBin(t, "", "devbox", "--request-perms")
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (stderr=%q)", code, errOut)
	}
	if out != "" {
		t.Fatalf("stdout = %q, want empty", out)
	}
	want := "abort: --request-perms is macOS only (E_VALIDATE)\n"
	if errOut != want {
		t.Fatalf("stderr = %q, want %q", errOut, want)
	}
}

// TestRemoteOversizedFilePayloadNeverSpawnsSSH asserts a [f] payload whose
// escaped, rewritten line exceeds the 64 KiB limit is caught LOCALLY,
// exit 2, before ssh is ever spawned: the destination is "connectfail" (a
// scripted failure with its own distinct exit 3 and stderr message) - if
// the diagnostic path had a bug and spawned ssh anyway, this test would
// see exit 3 and "Connection refused" instead of the validation error.
func TestRemoteOversizedFilePayloadNeverSpawnsSSH(t *testing.T) {
	big := strings.Repeat(`\`, 40000) // escapes to 80000 bytes, over 64 KiB
	path := filepath.Join(t.TempDir(), "big.txt")
	if err := os.WriteFile(path, []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	out, errOut, code := runBin(t, "", "connectfail", "txt[f]"+path)
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (stderr=%q)", code, errOut)
	}
	if out != "" {
		t.Fatalf("stdout = %q, want empty", out)
	}
	if strings.Contains(errOut, "Connection refused") {
		t.Fatalf("stderr = %q, ssh must never have been spawned", errOut)
	}
	if !strings.Contains(errOut, "E_VALIDATE") || !strings.Contains(errOut, "64 KiB") {
		t.Errorf("stderr = %q, want a 64 KiB E_VALIDATE diagnostic", errOut)
	}
}

// TestRemoteSuccessfulRunPlain drives a real multi-command sequence
// through fakessh's default passthrough into the harness's -tags dryrun
// "remote" binary (dispatch_dryrun.go's dryrun.Backend), covering the
// happy path end to end: local [f] rewrite of txt[f]/qclip[f], the
// relayed per-line results, and a normal done line/exit 0.
func TestRemoteSuccessfulRunPlain(t *testing.T) {
	txtPath := filepath.Join(t.TempDir(), "payload.txt")
	if err := os.WriteFile(txtPath, []byte("hello\tworld\\!"), 0o644); err != nil {
		t.Fatal(err)
	}
	qclipOut := filepath.Join(t.TempDir(), "clip-out.txt")

	out, errOut, code := runBin(t, "", "devbox",
		"qinfo", "txt[f]"+txtPath, "qclip[f]"+qclipOut)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr=%q, stdout=%q)", code, errOut, out)
	}
	if errOut != "" {
		t.Fatalf("stderr = %q, want empty", errOut)
	}
	if !strings.HasPrefix(out, "out ") {
		t.Fatalf("stdout = %q, want it to start with the \"out <dir>\" line", out)
	}
	if !strings.Contains(out, "1 ok qinfo") {
		t.Errorf("stdout = %q, want a qinfo result line", out)
	}
	if !strings.Contains(out, "2 ok txt") {
		t.Errorf("stdout = %q, want the rewritten txt[f] line to run ok", out)
	}
	if !strings.Contains(out, "3 ok qclip") {
		t.Errorf("stdout = %q, want the rewritten qclip[f] line to run ok", out)
	}
	if !strings.Contains(out, "done ok=3 err=0 skip=0") {
		t.Fatalf("stdout = %q, want done ok=3 err=0 skip=0", out)
	}
	if strings.Contains(out, "state=unknown") {
		t.Errorf("stdout = %q, want no state=unknown on a clean run", out)
	}

	// The qclip[f] write-back must have happened on THIS machine
	// (help-remote.txt HOW IT WORKS step 1/4): the dryrun remote's own
	// clipboard is always empty, so the file must exist with empty
	// content - proving the local write-back side effect ran, not that
	// it carried real clipboard text (untestable through this harness,
	// covered directly instead by internal/remote/relay_test.go).
	got, err := os.ReadFile(qclipOut)
	if err != nil {
		t.Fatalf("qclip[f] write-back file was not created: %v", err)
	}
	if string(got) != "" {
		t.Errorf("qclip[f] file content = %q, want empty (dryrun clipboard is always empty)", got)
	}
}

// TestRemoteSuccessfulRunJSONL is the same run as
// TestRemoteSuccessfulRunPlain but in --jsonl, asserting the start/result/
// done event shapes and that no stray output ever reaches stderr.
func TestRemoteSuccessfulRunJSONL(t *testing.T) {
	txtPath := filepath.Join(t.TempDir(), "payload.txt")
	if err := os.WriteFile(txtPath, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, errOut, code := runBin(t, "", "--jsonl", "devbox", "qinfo", "txt[f]"+txtPath)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr=%q, stdout=%q)", code, errOut, out)
	}
	if errOut != "" {
		t.Fatalf("stderr = %q, want empty", errOut)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("stdout lines = %d, want 4 (start, qinfo, txt, done): %q", len(lines), out)
	}
	var start struct {
		Event string `json:"event"`
		Dest  string `json:"dest"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &start); err != nil || start.Event != "start" || start.Dest != "devbox" {
		t.Fatalf("start line = %q (err=%v), want event=start dest=devbox", lines[0], err)
	}
	var done struct {
		Event string `json:"event"`
		OK    int    `json:"ok"`
		Err   int    `json:"err"`
	}
	if err := json.Unmarshal([]byte(lines[3]), &done); err != nil || done.Event != "done" || done.OK != 2 || done.Err != 0 {
		t.Fatalf("done line = %q (err=%v), want event=done ok=2 err=0", lines[3], err)
	}
}
