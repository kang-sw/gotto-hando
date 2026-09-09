package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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

// TestRemoteKillMidRunJSONLSingleStart is the NEW-1 regression test: the
// mid-run connection-loss path (results already relayed, so this
// wrapper's own start object was already committed and printed) must
// emit EXACTLY ONE {"event":"start"} object, never a second one. Relay
// #1's C1 fix folded WriteStartOS into the same closure reused by both
// this mid-run path and the pre-first-result-EOF path below, which
// caused a duplicate start here; asserting a COUNT (not prefix/contains,
// which TestRemoteKillMidRun above already does and which missed this)
// is what catches a regression like it recurring.
func TestRemoteKillMidRunJSONLSingleStart(t *testing.T) {
	out, errOut, code := runBin(t, "", "--jsonl", "killmidrun", "qinfo")
	if code != 5 {
		t.Fatalf("exit = %d, want 5 (stderr=%q)", code, errOut)
	}
	if errOut != "" {
		t.Fatalf("stderr = %q, want empty", errOut)
	}
	startCount := strings.Count(out, `"event":"start"`)
	if startCount != 1 {
		t.Fatalf("stdout has %d {\"event\":\"start\"} objects, want exactly 1: %q", startCount, out)
	}
	doneCount := strings.Count(out, `"event":"done"`)
	if doneCount != 1 {
		t.Errorf("stdout has %d {\"event\":\"done\"} objects, want exactly 1: %q", doneCount, out)
	}
	if !strings.Contains(out, `"state":"unknown"`) {
		t.Errorf("stdout = %q, want the done object to carry state=unknown", out)
	}
}

// TestRemoteKillBeforeResultJSONLSingleStart is NEW-1's companion case:
// the connection-loss path BEFORE this wrapper has ever printed its own
// start (fakessh's "killbeforeresult" - a start object with zero results,
// then EOF) must still print exactly one start, synthesizing a done right
// after it (mirroring runRemoteRequestPerms's own pre-result EOF branch).
func TestRemoteKillBeforeResultJSONLSingleStart(t *testing.T) {
	out, errOut, code := runBin(t, "", "--jsonl", "killbeforeresult", "qinfo")
	if code != 5 {
		t.Fatalf("exit = %d, want 5 (stderr=%q)", code, errOut)
	}
	if errOut != "" {
		t.Fatalf("stderr = %q, want empty", errOut)
	}
	startCount := strings.Count(out, `"event":"start"`)
	if startCount != 1 {
		t.Fatalf("stdout has %d {\"event\":\"start\"} objects, want exactly 1: %q", startCount, out)
	}
	doneCount := strings.Count(out, `"event":"done"`)
	if doneCount != 1 {
		t.Errorf("stdout has %d {\"event\":\"done\"} objects, want exactly 1: %q", doneCount, out)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("stdout lines = %d, want 2 (start, done): %q", len(lines), out)
	}
	var done struct {
		Event string `json:"event"`
		OK    int    `json:"ok"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &done); err != nil || done.Event != "done" || done.OK != 0 {
		t.Fatalf("done line = %q (err=%v), want event=done ok=0 (no results ever arrived)", lines[1], err)
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

// Field patterns whose values are expected to legitimately differ between
// a genuinely-local run and the same sequence relayed through the <dest>
// ssh wrapper: timestamps/elapsed time (never reproducible), and out/
// dest/path (which necessarily embed each run's own --out directory and
// destination name). Every other byte in a relayed JSONL line is required
// to be identical to a direct local dry-run of the same sequence (review
// T1) - masking exactly these five fields, and nothing else, is what
// makes that byte-identity claim meaningful rather than vacuous.
var (
	reTMS      = regexp.MustCompile(`"t_ms":\d+`)
	reElapsed  = regexp.MustCompile(`"elapsed_ms":\d+`)
	reOutField = regexp.MustCompile(`"out":"[^"]*"`)
	reDest     = regexp.MustCompile(`"dest":"[^"]*"`)
	rePath     = regexp.MustCompile(`"path":"[^"]*"`)
)

func normalizeJSONLForDiff(line string) string {
	line = reTMS.ReplaceAllString(line, `"t_ms":0`)
	line = reElapsed.ReplaceAllString(line, `"elapsed_ms":0`)
	line = reOutField.ReplaceAllString(line, `"out":"OUT"`)
	line = reDest.ReplaceAllString(line, `"dest":"DEST"`)
	line = rePath.ReplaceAllString(line, `"path":"PATH"`)
	return line
}

// capturePathFromLine extracts a cap result's "path" field value from a
// raw (unnormalized) JSONL line, for reading back the actual PNG file it
// names.
func capturePathFromLine(t *testing.T, line string) string {
	t.Helper()
	m := rePath.FindStringSubmatch(line)
	if m == nil {
		t.Fatalf("no \"path\" field in cap line: %q", line)
	}
	// m[0] is `"path":"<value>"`; decode it directly as a one-field object.
	var v struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte("{"+m[0]+"}"), &v); err != nil {
		t.Fatalf("decode path field %q: %v", m[0], err)
	}
	return v.Path
}

// runDryrunBinDirect runs the harness's own `-tags dryrun` "remote" binary
// directly, unwrapped by ssh/fakessh - the T1 "local dry-run" baseline: it
// binds `local` to the exact same dryrun.Backend shape
// (dispatch_dryrun.go) the ssh wrapper's fakessh ultimately spawns, so
// diffing its output against a wrapped run is meaningful (unlike diffing
// against binPath's own `local`, which would drive a completely different,
// real OS backend on darwin/windows).
func runDryrunBinDirect(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.Command(dryrunBinPath, args...)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("run %v: %v", args, err)
		}
		code = ee.ExitCode()
	}
	return outBuf.String(), errBuf.String(), code
}

// TestRemoteCaptureRelayByteIdentical is T1: it drives the same multi-op
// sequence (drawn from help.txt EXAMPLES/DESTINATIONS - the "hold a
// modifier across several actions" line plus the DESTINATIONS section's
// own "'win[]Blender' 'k[]a' 'cap' # PNG on THIS machine" capture
// illustration, combined and adapted to ops the dryrun backend always
// succeeds at) through two paths: (a) directly against the -tags dryrun
// binary (the "local dry-run" baseline, no ssh/relay involved) and (b)
// through the real <dest> ssh wrapper via fakessh's default passthrough
// to that SAME binary. Every JSONL byte other than timestamps/elapsed
// time/out/dest/path (necessarily run-specific) must be identical between
// the two - including the --inline-captures wire relay's decoded cap
// object (path/w/h/origin/scale field shape and order) - and the two
// runs' actual written PNG capture files must be byte-for-byte identical,
// proving the base64-over-the-wire -> local-file pipeline is lossless.
func TestRemoteCaptureRelayByteIdentical(t *testing.T) {
	seq := []string{"kd[]shift", "c[]100,100", "c[]300,100", "ku[]shift", "cap"}

	dirA := filepath.Join(t.TempDir(), "outA")
	baseArgs := append([]string{"--jsonl", "--out", dirA, "local"}, seq...)
	baseOut, baseErr, baseCode := runDryrunBinDirect(t, baseArgs...)
	if baseCode != 0 {
		t.Fatalf("baseline exit = %d, want 0 (stderr=%q, stdout=%q)", baseCode, baseErr, baseOut)
	}
	if baseErr != "" {
		t.Fatalf("baseline stderr = %q, want empty", baseErr)
	}

	dirB := filepath.Join(t.TempDir(), "outB")
	wrapArgs := append([]string{"--jsonl", "--out", dirB, "devbox"}, seq...)
	wrapOut, wrapErr, wrapCode := runBin(t, "", wrapArgs...)
	if wrapCode != 0 {
		t.Fatalf("wrapped exit = %d, want 0 (stderr=%q, stdout=%q)", wrapCode, wrapErr, wrapOut)
	}
	if wrapErr != "" {
		t.Fatalf("wrapped stderr = %q, want empty", wrapErr)
	}

	baseLines := strings.Split(strings.TrimRight(baseOut, "\n"), "\n")
	wrapLines := strings.Split(strings.TrimRight(wrapOut, "\n"), "\n")
	if len(baseLines) != len(wrapLines) {
		t.Fatalf("line count: baseline=%d wrapped=%d\nbaseline=%q\nwrapped=%q",
			len(baseLines), len(wrapLines), baseOut, wrapOut)
	}

	var basePNGPath, wrapPNGPath string
	for i := range baseLines {
		nb, nw := normalizeJSONLForDiff(baseLines[i]), normalizeJSONLForDiff(wrapLines[i])
		if nb != nw {
			t.Errorf("line %d differs after normalization:\n  baseline=%q\n  wrapped =%q\n  (raw baseline=%q)\n  (raw wrapped =%q)",
				i, nb, nw, baseLines[i], wrapLines[i])
		}
		if strings.Contains(baseLines[i], `"cmd":"cap"`) {
			basePNGPath = capturePathFromLine(t, baseLines[i])
			wrapPNGPath = capturePathFromLine(t, wrapLines[i])
		}
	}

	if basePNGPath == "" || wrapPNGPath == "" {
		t.Fatal("no cap result line found in either run's output")
	}
	baseBytes, err := os.ReadFile(basePNGPath)
	if err != nil {
		t.Fatalf("read baseline PNG %q: %v", basePNGPath, err)
	}
	wrapBytes, err := os.ReadFile(wrapPNGPath)
	if err != nil {
		t.Fatalf("read wrapped PNG %q: %v", wrapPNGPath, err)
	}
	if !bytes.Equal(baseBytes, wrapBytes) {
		t.Errorf("capture PNG bytes differ: baseline %d bytes (%s), wrapped %d bytes (%s)",
			len(baseBytes), basePNGPath, len(wrapBytes), wrapPNGPath)
	}
}

// TestRemoteExpectVersionMismatch is T2: fakessh's "versionmismatch" dest
// real-spawns the dryrun remote binary but appends a conflicting
// --expect-version AFTER the wrapper's own correct one - parseArgs' last-
// occurrence-wins semantics (cmd/gotto-hando/options.go) make the remote's
// own genuine, pre-existing version-mismatch check
// (dispatch.go:89-92-equivalent) fire for real, printing "version
// mismatch: remote <x>, expected <y>" to ITS OWN stderr and exiting before
// any start line - relayed here as exit 3 (E_CONNECT), that exact message
// on stderr, empty stdout (help-remote.txt TROUBLESHOOTING).
func TestRemoteExpectVersionMismatch(t *testing.T) {
	out, errOut, code := runBin(t, "", "versionmismatch", "qinfo")
	if code != 3 {
		t.Fatalf("exit = %d, want 3 (stderr=%q)", code, errOut)
	}
	if out != "" {
		t.Fatalf("stdout = %q, want empty", out)
	}
	if !strings.Contains(errOut, "version mismatch: remote "+version()+", expected 9.9.9") {
		t.Errorf("stderr = %q, want the remote's own genuine version-mismatch message", errOut)
	}
}

// TestRemoteTargetOSRelayed is the I1 best-effort test: fakessh's
// "winrelay" dest scripts a full successful run whose start object's
// target.os is hardcoded "windows" - a value that can never coincidentally
// match the test host's own runtime.GOOS (this package's other scripted
// scenarios mostly hardcode darwin/windows too, but this is the one whose
// entire point is proving the relayed value is NOT locally regenerated).
// Before review I1, the wrapper printed its OWN runtime.GOOS here; this
// test fails against that old behavior on any host except an actual
// Windows one.
func TestRemoteTargetOSRelayed(t *testing.T) {
	out, errOut, code := runBin(t, "", "--jsonl", "winrelay", "qinfo")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr=%q)", code, errOut)
	}
	if errOut != "" {
		t.Fatalf("stderr = %q, want empty", errOut)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) == 0 {
		t.Fatalf("stdout = %q, want at least a start line", out)
	}
	var start struct {
		Event  string `json:"event"`
		Target struct {
			OS string `json:"os"`
		} `json:"target"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &start); err != nil {
		t.Fatalf("decode start line: %v (%q)", err, lines[0])
	}
	if start.Event != "start" || start.Target.OS != "windows" {
		t.Errorf("start = %+v, want event=start target.os=windows (relayed from the remote, not local runtime.GOOS)", start)
	}
}

// TestRemotePasteErrSrcRestored is the I-test best-effort integration
// test: fakessh's "pastefail" dest scripts a genuine "err" result for a
// paste command whose wire "src" is the INLINED form a real remote would
// see after local [f] rewrite - exercising internal/remote.Relay.Process's
// err-src-restoration (relay.go) through the REAL runRemote spawn/relay
// loop end to end, complementing (not replacing) the existing unit-level
// coverage in internal/remote/relay_test.go's
// TestRelayProcessErrRestoresLocalSrc.
func TestRemotePasteErrSrcRestored(t *testing.T) {
	scriptPath := filepath.Join(t.TempDir(), "cmd.py")
	if err := os.WriteFile(scriptPath, []byte("print('hi')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, errOut, code := runBin(t, "", "pastefail", "paste[f]"+scriptPath)
	if code != 1 {
		t.Fatalf("exit = %d, want 1 (a runtime err, stderr=%q)", code, errOut)
	}
	if errOut != "" {
		t.Fatalf("stderr = %q, want empty", errOut)
	}
	wantSrc := "paste[f]" + scriptPath
	if !strings.Contains(out, wantSrc) {
		t.Errorf("stdout = %q, want it to contain the caller's own original src %q (not the wire-inlined form)", out, wantSrc)
	}
	if strings.Contains(out, "inlined cmd.py contents") {
		t.Errorf("stdout = %q, want the wire's inlined src NOT to leak through", out)
	}
}
