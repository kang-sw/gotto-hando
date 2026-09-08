package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

// TestNotImplementedOptionsExitTwo covers every later-ticket option
// (ticket Decisions: --bridge, --remote-bin, --inline-captures,
// --request-perms) and the local/--check/--ir/remote-dest paths, each
// asserting exit 2 with its exact stderr message.
func TestNotImplementedOptionsExitTwo(t *testing.T) {
	tmp := writeTempFile(t, "k[]a\n")
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"bridge", []string{"--bridge"}, "abort: session bridge not implemented (E_VALIDATE)\n"},
		{"remote-bin", []string{"local", "--remote-bin", "/opt/gotto-hando"}, "abort: --remote-bin not implemented (E_VALIDATE)\n"},
		{"inline-captures", []string{"local", "--inline-captures"}, "abort: --inline-captures not implemented (E_VALIDATE)\n"},
		{"request-perms", []string{"local", "--request-perms"}, "abort: --request-perms not implemented (E_VALIDATE)\n"},
		{"check", []string{"--check", "-f", tmp}, "abort: parser not implemented (E_VALIDATE)\n"},
		{"ir", []string{"--ir", "-f", tmp}, "abort: parser not implemented (E_VALIDATE)\n"},
		{"local-plain", []string{"local"}, "abort: platform backend not implemented, nothing ran (E_VALIDATE)\n"},
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
