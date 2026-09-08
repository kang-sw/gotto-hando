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

// TestMain builds the binary once (the ticket explicitly asks for a test
// that "runs the binary") and shares it across every subprocess test
// below.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "gotto-hando-test-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, "mkdtemp:", err)
		os.Exit(1)
	}
	binPath = filepath.Join(dir, "gotto-hando")
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", binPath, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		os.RemoveAll(dir)
		fmt.Fprintln(os.Stderr, "go build failed:", err)
		fmt.Fprintln(os.Stderr, string(out))
		os.Exit(1)
	}
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

// TestNotImplementedOptionsExitTwo covers the still-later-ticket options
// (--bridge, --remote-bin) and the remote-dest path, each asserting exit 2
// with its exact stderr message. --check/--ir are implemented (Phase 2) and
// covered by analyze_test.go; the local-dest path is covered by
// TestLocalDestDispatch below; --inline-captures/--request-perms are
// implemented in Phase 2 and covered by the two tests above.
func TestNotImplementedOptionsExitTwo(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"bridge", []string{"--bridge"}, "abort: session bridge not implemented (E_VALIDATE)\n"},
		{"remote-bin", []string{"local", "--remote-bin", "/opt/gotto-hando"}, "abort: --remote-bin not implemented (E_VALIDATE)\n"},
		{"remote-dest", []string{"winbox", "qinfo"}, "abort: remote destinations not implemented (E_VALIDATE)\n"},
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
// (260907-feat-darwin-backend Phase 1, dispatch.go step 10): on darwin it
// now runs for real through the darwin backend; on every other GOOS
// dispatch_other.go's newLocalBackend stub still aborts E_VALIDATE with
// the same message the pre-Phase-1 stub printed.
func TestLocalDestDispatch(t *testing.T) {
	out, errOut, code := runBin(t, "", "local")
	if runtime.GOOS != "darwin" {
		if code != 2 {
			t.Fatalf("exit = %d, want 2 (stderr=%q)", code, errOut)
		}
		want := "abort: platform backend not implemented, nothing ran (E_VALIDATE)\n"
		if errOut != want {
			t.Fatalf("stderr = %q, want %q", errOut, want)
		}
		return
	}
	// darwin: a run with zero lines never touches Preflight's session/
	// permission checks (help-macos.txt CHECK: an empty run has nothing to
	// fail on, same rule as the qinfo/qdisp/qmouse/sleep/set exemption) -
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
	if out != "0.1.0\n" {
		t.Fatalf("stdout = %q, want %q", out, "0.1.0\n")
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
	want := "version mismatch: remote 0.1.0, expected 9.9.9\n"
	if errOut != want {
		t.Fatalf("stderr = %q, want %q", errOut, want)
	}
}

// TestExpectVersionMatch asserts a matching --expect-version does not
// itself produce output and processing continues past it (falling
// through, here, to dest resolution with no dest given).
func TestExpectVersionMatch(t *testing.T) {
	_, errOut, code := runBin(t, "", "--expect-version", "0.1.0")
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (stderr=%q)", code, errOut)
	}
	want := "abort: remote destinations not implemented (E_VALIDATE)\n"
	if errOut != want {
		t.Fatalf("stderr = %q, want %q", errOut, want)
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
