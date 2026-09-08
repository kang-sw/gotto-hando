//go:build darwin

package darwin

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"

	"github.com/kang-sw/gotto-hando/internal/backend"
)

// execOutputCap is exec's per-stream output cap, applied independently to
// stdout and stderr (help.txt exec OUTPUT :591-593, LIMITS :756-757: "exec
// stdout/stderr <= 64 KiB each").
const execOutputCap = 64 * 1024

// cappedWriter accumulates up to a fixed byte budget, silently dropping
// anything past it and marking Truncated. It never fails the underlying
// Write - a truncation must not make cmd.Wait report a spurious process
// error.
type cappedWriter struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (w *cappedWriter) Write(p []byte) (int, error) {
	remain := w.limit - w.buf.Len()
	if remain <= 0 {
		if len(p) > 0 {
			w.truncated = true
		}
		return len(p), nil
	}
	if len(p) > remain {
		w.buf.Write(p[:remain])
		w.truncated = true
	} else {
		w.buf.Write(p)
	}
	return len(p), nil
}

// buildExecArgv resolves exec's argv (help.txt exec :370-398, ticket
// Decision): with the shell flag, $SHELL -lc <payload> (falling back to
// /bin/zsh when $SHELL is empty, the login-shell path); otherwise the
// parser's already argv-split Argv runs directly (no further
// escapes/glob).
func buildExecArgv(req backend.ExecReq) []string {
	if req.Shell {
		sh := os.Getenv("SHELL")
		if sh == "" {
			sh = "/bin/zsh"
		}
		return []string{sh, "-lc", req.Cmd}
	}
	return req.Argv
}

// Exec runs one exec line via os/exec (help.txt exec :370-398). It reserves
// a non-nil error for spawn failure or an unexpected Wait error only: a
// timeout kill and a non-zero exit are both reported through ExecResult
// with a NIL error, because internal/engine/run.go's doExec checks err !=
// nil before inspecting r.TimedOut/r.Exit - a non-nil error here would
// misclassify a timeout as E_EXEC instead of E_TIMEOUT, and would prevent
// noerr from softening a non-zero exit.
func (b *Backend) Exec(ctx context.Context, req backend.ExecReq) (backend.ExecResult, error) {
	argv := buildExecArgv(req)
	runCtx := ctx
	if req.Timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, req.Timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(runCtx, argv[0], argv[1:]...)
	// cmd.Stdin left nil: Go already reads from the null device for a nil
	// Stdin, matching help.txt exec's "stdin is closed".
	cmd.Stdin = nil
	stdout := &cappedWriter{limit: execOutputCap}
	stderr := &cappedWriter{limit: execOutputCap}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		// Spawn failure (help.txt E_EXEC "spawn failure"); the engine's
		// existing err != nil -> E_EXEC path handles this unchanged.
		return backend.ExecResult{}, err
	}
	waitErr := cmd.Wait()
	truncated := stdout.truncated || stderr.truncated

	if runCtx.Err() == context.DeadlineExceeded {
		return backend.ExecResult{
			TimedOut:  true,
			Stdout:    stdout.buf.String(),
			Stderr:    stderr.buf.String(),
			Truncated: truncated,
		}, nil
	}

	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) {
		return backend.ExecResult{
			Exit:      exitErr.ExitCode(),
			Stdout:    stdout.buf.String(),
			Stderr:    stderr.buf.String(),
			Truncated: truncated,
		}, nil
	}
	if waitErr != nil {
		// An unexpected Wait error - not an *exec.ExitError, not a deadline
		// - falls into E_EXEC generically via the returned error.
		return backend.ExecResult{}, waitErr
	}
	return backend.ExecResult{
		Exit:      0,
		Stdout:    stdout.buf.String(),
		Stderr:    stderr.buf.String(),
		Truncated: truncated,
	}, nil
}
