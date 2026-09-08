//go:build darwin

package darwin

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kang-sw/gotto-hando/internal/backend"
)

// TestExecSuccessExitZero: a command that exits 0 reports Exit == 0 and a
// nil error (help.txt exec :370-398).
func TestExecSuccessExitZero(t *testing.T) {
	b := &Backend{}
	r, err := b.Exec(context.Background(), backend.ExecReq{Argv: []string{"/usr/bin/true"}})
	if err != nil {
		t.Fatalf("Exec() err = %v, want nil", err)
	}
	if r.Exit != 0 {
		t.Errorf("Exit = %d, want 0", r.Exit)
	}
	if r.TimedOut {
		t.Errorf("TimedOut = true, want false")
	}
}

// TestExecNonZeroExitIsNilError: a non-zero exit is reported via
// ExecResult.Exit with a NIL error (help.txt E_EXEC "exit != 0 without
// noerr" - noerr softening is the engine's job, not this backend's; this
// backend must never turn a plain non-zero exit into a Go error, or the
// engine could never soften it with noerr).
func TestExecNonZeroExitIsNilError(t *testing.T) {
	b := &Backend{}
	r, err := b.Exec(context.Background(), backend.ExecReq{Argv: []string{"/usr/bin/false"}})
	if err != nil {
		t.Fatalf("Exec() err = %v, want nil (non-zero exit is not a Go error)", err)
	}
	if r.Exit == 0 {
		t.Errorf("Exit = 0, want non-zero")
	}
	if r.TimedOut {
		t.Errorf("TimedOut = true, want false")
	}
}

// TestExecSpawnFailureIsError: a nonexistent binary is a spawn failure -
// this DOES return a non-nil error (help.txt E_EXEC "spawn failure").
func TestExecSpawnFailureIsError(t *testing.T) {
	b := &Backend{}
	_, err := b.Exec(context.Background(), backend.ExecReq{Argv: []string{"/bin/nonexistent-xyz"}})
	if err == nil {
		t.Fatal("Exec() err = nil, want a spawn-failure error")
	}
}

// TestExecTimeout: timeout= kills the process and reports TimedOut == true
// with a NIL error (help.txt exec :390-391; the engine checks err != nil
// before r.TimedOut, so a non-nil error here would misreport E_EXEC instead
// of E_TIMEOUT).
func TestExecTimeout(t *testing.T) {
	b := &Backend{}
	start := time.Now()
	r, err := b.Exec(context.Background(), backend.ExecReq{
		Argv:    []string{"/bin/sleep", "2"},
		Timeout: 200 * time.Millisecond,
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Exec() err = %v, want nil (timeout is reported via ExecResult, not an error)", err)
	}
	if !r.TimedOut {
		t.Errorf("TimedOut = false, want true")
	}
	if elapsed > 1500*time.Millisecond {
		t.Errorf("Exec() took %s, want well under the 2s sleep duration (timeout should have killed it)", elapsed)
	}
}

// TestExecOutputTruncated: output beyond the 64 KiB per-stream cap is
// silently dropped and Truncated is set (LIMITS :756-757).
func TestExecOutputTruncated(t *testing.T) {
	b := &Backend{}
	// A piped `yes | head -c` produces 300000 bytes of stdout, well past
	// the 65536-byte cap, without hitting ARG_MAX (unlike a giant argv).
	r, err := b.Exec(context.Background(), backend.ExecReq{
		Shell: true,
		Cmd:   "yes x | head -c 300000",
	})
	if err != nil {
		t.Fatalf("Exec() err = %v, want nil", err)
	}
	if !r.Truncated {
		t.Fatalf("Truncated = false, want true")
	}
	if len(r.Stdout) != execOutputCap {
		t.Errorf("len(Stdout) = %d, want %d", len(r.Stdout), execOutputCap)
	}
}

// TestBuildExecArgvShell: the shell flag routes through $SHELL -lc <cmd>,
// falling back to /bin/zsh when $SHELL is unset (ticket Decision).
func TestBuildExecArgvShell(t *testing.T) {
	t.Run("uses $SHELL", func(t *testing.T) {
		t.Setenv("SHELL", "/opt/homebrew/bin/fish")
		got := buildExecArgv(backend.ExecReq{Shell: true, Cmd: "echo hi"})
		want := []string{"/opt/homebrew/bin/fish", "-lc", "echo hi"}
		if !strSliceEqual(got, want) {
			t.Errorf("argv = %v, want %v", got, want)
		}
	})
	t.Run("falls back to /bin/zsh when $SHELL is empty", func(t *testing.T) {
		t.Setenv("SHELL", "")
		got := buildExecArgv(backend.ExecReq{Shell: true, Cmd: "echo hi"})
		want := []string{"/bin/zsh", "-lc", "echo hi"}
		if !strSliceEqual(got, want) {
			t.Errorf("argv = %v, want %v", got, want)
		}
	})
	t.Run("non-shell uses Argv directly", func(t *testing.T) {
		got := buildExecArgv(backend.ExecReq{Argv: []string{"blender", "--version"}})
		want := []string{"blender", "--version"}
		if !strSliceEqual(got, want) {
			t.Errorf("argv = %v, want %v", got, want)
		}
	})
}

// TestExecShellRunsLoginShell confirms the shell path actually executes
// through the resolved $SHELL -lc, not just that buildExecArgv looks right.
func TestExecShellRunsLoginShell(t *testing.T) {
	b := &Backend{}
	r, err := b.Exec(context.Background(), backend.ExecReq{Shell: true, Cmd: "echo hello-from-shell"})
	if err != nil {
		t.Fatalf("Exec() err = %v, want nil", err)
	}
	if r.Exit != 0 {
		t.Fatalf("Exit = %d, want 0 (stdout=%q stderr=%q)", r.Exit, r.Stdout, r.Stderr)
	}
	if !strings.Contains(r.Stdout, "hello-from-shell") {
		t.Errorf("Stdout = %q, want it to contain %q", r.Stdout, "hello-from-shell")
	}
}

func strSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
