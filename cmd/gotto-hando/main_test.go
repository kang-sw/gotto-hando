package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kang-sw/gotto-hando/assets"
)

var binPath string

// dryrunBinPath is the `-tags dryrun` "remote" binary TestMain builds
// (same one fakessh's default passthrough spawns). remote_test.go's T1
// capture-relay test invokes it directly, unwrapped by ssh, as the
// "local dry-run" byte-identity baseline to diff the ssh-wrapped run
// against - both runs exercise the identical dryrun.Backend shape, so a
// direct comparison is meaningful (unlike diffing against binPath's own
// `local`, which drives a completely different, real OS backend).
var dryrunBinPath string

// exeName appends the platform executable suffix.
func exeName(base string) string {
	if runtime.GOOS == "windows" {
		return base + ".exe"
	}
	return base
}

// buildGo runs `go build [tags...] -o outPath pkgDir`, failing the whole
// test binary (os.Exit, matching the existing binPath build's own
// failure handling) on error - every artifact TestMain builds is required
// by some test, so there is no useful partial-failure mode.
func buildGo(outPath, pkgDir string, tags ...string) {
	args := []string{"build"}
	if len(tags) > 0 {
		args = append(args, "-tags", strings.Join(tags, ","))
	}
	args = append(args, "-o", outPath, pkgDir)
	cmd := exec.Command("go", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintln(os.Stderr, "go build failed:", err)
		fmt.Fprintln(os.Stderr, string(out))
		os.Exit(1)
	}
}

// TestMain builds every binary the subprocess tests below need, once, and
// shares them across the whole package:
//   - binPath: the real (no build tag) gotto-hando, run directly by every
//     runBin call.
//   - a `-tags dryrun` gotto-hando named "gotto-hando" (260908-feat-
//     remote-ssh Phase 1's fake "remote" copy of itself, dispatch_dryrun.go
//   - never selectable in a release build) and a fake `ssh`
//     (testdata/fakessh), both placed in directories prepended to PATH so
//     remote_test.go's <dest> ssh-wrapper tests drive binPath against
//     them without touching a real ssh or OS backend.
func TestMain(m *testing.M) {
	// Cross-compiled focused tests run on the Windows GUI host, where Go is
	// deliberately absent. The bridge-forwarding unit tests use only their
	// in-process net.Pipe seam, so they can skip subprocess fixtures there.
	if os.Getenv("GOTTO_HANDO_SKIP_SUBPROCESS_BUILDS") == "1" {
		os.Exit(m.Run())
	}

	dir, err := os.MkdirTemp("", "gotto-hando-test-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, "mkdtemp:", err)
		os.Exit(1)
	}
	binPath = filepath.Join(dir, exeName("gotto-hando"))
	buildGo(binPath, ".")

	remoteDir := filepath.Join(dir, "remote-bin")
	sshDir := filepath.Join(dir, "fake-ssh")
	if err := os.MkdirAll(remoteDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "mkdir:", err)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	if err := os.MkdirAll(sshDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "mkdir:", err)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	// The dryrun "remote" binary must be named exactly what
	// remoteBinName's default resolves to ("gotto-hando") so the fake-ssh
	// + real wrapper harness needs no --remote-bin override for its
	// default-case tests.
	dryrunBinPath = filepath.Join(remoteDir, exeName("gotto-hando"))
	buildGo(dryrunBinPath, ".", "dryrun")
	buildGo(filepath.Join(sshDir, exeName("ssh")), "./testdata/fakessh")

	// Prepend both to PATH for the whole test binary: the real `ssh`
	// binary (if any) is on the original PATH after them, but fakessh
	// intercepts every invocation this package makes (only remote_test.go
	// ever spawns "ssh").
	origPath := os.Getenv("PATH")
	_ = os.Setenv("PATH", sshDir+string(os.PathListSeparator)+remoteDir+string(os.PathListSeparator)+origPath)

	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func runBin(t *testing.T, stdin string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.Command(binPath, args...)
	if stdin != "" {
		cmd.Stdin = bytes.NewReader([]byte(stdin))
	}
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

// TestHelpFlagsByteIdentical asserts --help/--help-macos/--help-windows/
// --help-remote print the embedded asset byte-for-byte and exit 0
// (help.txt:40-42).
func TestHelpFlagsByteIdentical(t *testing.T) {
	cases := []struct {
		flag string
		want string
	}{
		{"--help", assets.Help},
		{"--help-macos", assets.HelpMacos},
		{"--help-windows", assets.HelpWindows},
		{"--help-remote", assets.HelpRemote},
	}
	for _, c := range cases {
		t.Run(c.flag, func(t *testing.T) {
			out, errOut, code := runBin(t, "", c.flag)
			if code != 0 {
				t.Fatalf("exit = %d, want 0 (stderr=%q)", code, errOut)
			}
			if out != c.want {
				t.Fatalf("stdout for %s is not byte-identical to the embedded asset (got %d bytes, want %d)",
					c.flag, len(out), len(c.want))
			}
		})
	}
}

// TestHelpIgnoresEverythingElse asserts a --help* flag wins and ignores
// other, even malformed, arguments (help.txt:40-42: "Standalone: no
// <dest>; other options or lines given together with it are ignored").
func TestHelpIgnoresEverythingElse(t *testing.T) {
	out, _, code := runBin(t, "", "--not-a-real-flag", "--help", "some line", "-f")
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if out != assets.Help {
		t.Fatal("stdout not byte-identical to assets.Help")
	}
}

// TestMixedFileAndArgvExitsTwo asserts -f and [line ...] together exit 2
// with the exact abort message (help.txt:26-28).
func TestMixedFileAndArgvExitsTwo(t *testing.T) {
	tmp := writeTempFile(t, "k[]a\n")
	_, errOut, code := runBin(t, "", "local", "-f", tmp, "someline")
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (stderr=%q)", code, errOut)
	}
	want := "abort: -f and [line ...] cannot be mixed (E_VALIDATE)\n"
	if errOut != want {
		t.Fatalf("stderr = %q, want %q", errOut, want)
	}
}

// TestRequestPermsPhase2 covers --request-perms, implemented on darwin in
// Phase 2 (help.txt --request-perms :91-108): on darwin `local
// --request-perms` prints a single perms= line to stdout and exits 0 (both
// granted) or 4 (either missing) - the exact code is environment-dependent
// (this dev Mac is typically screen-locked with the grants absent, so 4),
// so the test accepts either. On every other GOOS it stays a usage error,
// abort E_VALIDATE / exit 2 ("--request-perms is macOS only").
func TestRequestPermsPhase2(t *testing.T) {
	out, errOut, code := runBin(t, "", "local", "--request-perms")
	if runtime.GOOS == "darwin" {
		if code != 0 && code != 4 {
			t.Fatalf("exit = %d, want 0 or 4 (stderr=%q)", code, errOut)
		}
		if !strings.HasPrefix(out, "perms=accessibility:") {
			t.Fatalf("stdout = %q, want a perms=accessibility:... line", out)
		}
		return
	}
	if code != 2 {
		t.Fatalf("exit = %d, want 2 on %s (stderr=%q)", code, runtime.GOOS, errOut)
	}
	if want := "abort: --request-perms is macOS only (E_VALIDATE)\n"; errOut != want {
		t.Fatalf("stderr = %q, want %q", errOut, want)
	}
}

// TestRequestPermsJSONLEmitsStartPermsPair covers `local --request-perms
// --jsonl` on darwin (260908-feat-remote-ssh Phase 2 Codebase Findings
// "missed contract"): the JSONL start+perms pair help.txt JSONL
// "--request-perms" (:617-619) documents, not just the plain perms= line
// TestRequestPermsPhase2 already covers. This is what makes `<dest>
// --request-perms` work end to end at all - remote.go's
// runRemoteRequestPerms decodes exactly this shape via
// internal/remote.DecodePerms when relaying a `<dest> --request-perms`
// run, so a remote `local --request-perms --jsonl` invocation that never
// printed it would leave the wrapper hanging on a bogus "done" fallback.
func TestRequestPermsJSONLEmitsStartPermsPair(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("local --request-perms needs the darwin backend")
	}
	out, errOut, code := runBin(t, "", "local", "--request-perms", "--jsonl")
	if code != 0 && code != 4 {
		t.Fatalf("exit = %d, want 0 or 4 (stderr=%q)", code, errOut)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("stdout lines = %v, want exactly 2 (start, perms)", lines)
	}
	if !strings.Contains(lines[0], `"event":"start"`) {
		t.Errorf("first line = %q, want a start event", lines[0])
	}
	if !strings.Contains(lines[1], `"event":"perms"`) {
		t.Errorf("second line = %q, want a perms event", lines[1])
	}
	if !strings.Contains(lines[1], `"accessibility"`) || !strings.Contains(lines[1], `"screen"`) {
		t.Errorf("perms line = %q, want accessibility and screen fields", lines[1])
	}
}

// TestInlineCapturesAcceptedPhase2 confirms --inline-captures is accepted
// (Phase 2) rather than an exit-2 usage error: a query-only run with the
// flag completes normally (help.txt --inline-captures :72-77, "Also
// accepted with plain local").
func TestInlineCapturesAcceptedPhase2(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("local run needs the darwin backend; other GOOS has no backend yet")
	}
	_, errOut, code := runBin(t, "", "local", "--inline-captures", "qinfo")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr=%q)", code, errOut)
	}
}

// TestNotImplementedOptionsExitTwo covers the still-not-implemented options
// (`local --remote-bin`), asserting exit 2 with its exact stderr message.
// --check/--ir are implemented (Phase 2) and covered by analyze_test.go;
// the local-dest path is covered by TestLocalDestDispatch below;
// --inline-captures/--request-perms are implemented in Phase 2 and covered
// by the two tests above; the <dest> ssh wrapper (260908-feat-remote-ssh
// Phase 1) is covered by remote_test.go. --bridge is no longer here
// (260908-feat-remote-ssh Phase 2): on darwin - this dev host's native
// GOOS, so binPath (built with no tags, TestMain) always exercises it -
// runBridge now really listens on the unix socket and blocks in its
// Accept() loop until interrupted, so a bare `runBin(t, "", "--bridge")`
// would hang this test rather than exit 2; darwin's real bridge machinery
// is instead covered by internal/backend/darwin's bridge_socket_test.go
// (ListenBridge/Accept/Close), internal/bridge's session_test.go
// (Session.Handle), and dispatch_darwin_test.go
// (forwardToBridge/requestPerms over the dialBridge seam) - the same depth
// windows's own Phase 0 bridge got (dispatch_windows_test.go never
// subprocess-drives the real blocking Accept() loop either).
func TestNotImplementedOptionsExitTwo(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"remote-bin", []string{"local", "--remote-bin", "/opt/gotto-hando"}, "abort: --remote-bin not implemented (E_VALIDATE)\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, errOut, code := runBin(t, "", c.args...)
			if code != 2 {
				t.Fatalf("exit = %d, want 2 (stderr=%q)", code, errOut)
			}
			if out != "" {
				t.Fatalf("stdout = %q, want empty (plain abort has no stdout)", out)
			}
			if errOut != c.want {
				t.Fatalf("stderr = %q, want %q", errOut, c.want)
			}
		})
	}
}

// TestLocalDestDispatch covers dispatch.go's dest=="local" path
// through the native backend on darwin and windows. Other platforms use
// dispatch_other.go's newLocalBackend stub and abort with E_VALIDATE.
func TestLocalDestDispatch(t *testing.T) {
	out, errOut, code := runBin(t, "", "local")
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		if code != 2 {
			t.Fatalf("exit = %d, want 2 (stderr=%q)", code, errOut)
		}
		want := "abort: platform backend not implemented, nothing ran (E_VALIDATE)\n"
		if errOut != want {
			t.Fatalf("stderr = %q, want %q", errOut, want)
		}
		return
	}
	// Supported platforms: a run with zero lines never touches Preflight's
	// session/permission checks: an empty run has nothing to fail on -
	// it always completes with a normal, empty done line, regardless of
	// the GUI session's lock state.
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr=%q)", code, errOut)
	}
	if errOut != "" {
		t.Fatalf("stderr = %q, want empty", errOut)
	}
	if !strings.HasPrefix(out, "out ") {
		t.Fatalf("stdout = %q, want it to start with the \"out <dir>\" line", out)
	}
	if !strings.Contains(out, "done ok=0 err=0 skip=0") {
		t.Fatalf("stdout = %q, want a done ok=0 err=0 skip=0 line", out)
	}
}

// TestVersion asserts --version prints the help.txt:1 version token and
// exits 0.
func TestVersion(t *testing.T) {
	out, errOut, code := runBin(t, "", "--version")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr=%q)", code, errOut)
	}
	wantVersion := assets.Version()
	if out != wantVersion+"\n" {
		t.Fatalf("stdout = %q, want %q", out, wantVersion+"\n")
	}
}

// TestExpectVersionMismatch asserts the exact stderr message, exit 3 and
// empty stdout ("nothing else printed", help.txt:77-82).
func TestExpectVersionMismatch(t *testing.T) {
	out, errOut, code := runBin(t, "", "--expect-version", "9.9.9")
	if code != 3 {
		t.Fatalf("exit = %d, want 3 (stderr=%q)", code, errOut)
	}
	if out != "" {
		t.Fatalf("stdout = %q, want empty", out)
	}
	want := "version mismatch: remote " + assets.Version() + ", expected 9.9.9\n"
	if errOut != want {
		t.Fatalf("stderr = %q, want %q", errOut, want)
	}
}

// TestExpectVersionMatch asserts a matching --expect-version does not
// itself produce output and processing continues past it - here, into the
// same dest=="local" path TestLocalDestDispatch exercises (an explicit
// "local" dest keeps this deterministic: 260908-feat-remote-ssh Phase 1
// turned a bare/absent dest into a real ssh spawn, which would make this
// test flaky/environment-dependent).
func TestExpectVersionMatch(t *testing.T) {
	out, errOut, code := runBin(t, "", "--expect-version", assets.Version(), "local")
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		if code != 2 {
			t.Fatalf("exit = %d, want 2 (stderr=%q)", code, errOut)
		}
		want := "abort: platform backend not implemented, nothing ran (E_VALIDATE)\n"
		if errOut != want {
			t.Fatalf("stderr = %q, want %q", errOut, want)
		}
		return
	}
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr=%q)", code, errOut)
	}
	if errOut != "" {
		t.Fatalf("stderr = %q, want empty", errOut)
	}
	if !strings.HasPrefix(out, "out ") {
		t.Fatalf("stdout = %q, want it to start with the \"out <dir>\" line", out)
	}
	if !strings.Contains(out, "done ok=0 err=0 skip=0") {
		t.Fatalf("stdout = %q, want a done ok=0 err=0 skip=0 line", out)
	}
}

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "gotto-hando-*.gh")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	return f.Name()
}
