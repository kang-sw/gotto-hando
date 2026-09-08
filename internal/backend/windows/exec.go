//go:build windows

package windows

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
	"unicode/utf8"
	"unsafe"

	"github.com/kang-sw/gotto-hando/internal/backend"
	"golang.org/x/sys/windows"
)

// execOutputCap is exec's per-stream output cap, applied independently to
// stdout and stderr (help.txt exec OUTPUT :591-593, LIMITS :756-757: "exec
// stdout/stderr <= 64 KiB each"). Identical to darwin's exec.go.
const execOutputCap = 64 * 1024

// cappedWriter accumulates up to a fixed byte budget, silently dropping
// anything past it and marking Truncated. It never fails the underlying
// Write - a truncation must not make cmd.Wait report a spurious process
// error. Duplicated verbatim from darwin's exec.go.
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

// buildExecArgv resolves exec's argv (help-windows.txt "exec ON WINDOWS"):
// with the shell flag, %ComSpec% (falling back to cmd.exe) /C <payload> -
// pipes, redirection and built-ins all go through cmd; otherwise the
// parser's already argv-split Argv runs directly.
func buildExecArgv(req backend.ExecReq) []string {
	if req.Shell {
		comspec := os.Getenv("ComSpec")
		if comspec == "" {
			comspec = "cmd.exe"
		}
		return []string{comspec, "/C", req.Cmd}
	}
	return req.Argv
}

// Exec runs one exec line (help-windows.txt "exec ON WINDOWS"). It reserves
// a non-nil error for spawn failure or an unexpected Wait error only: a
// timeout kill and a non-zero exit are both reported through ExecResult
// with a NIL error, the same contract darwin's exec.go documents -
// internal/engine/run.go's doExec checks err != nil before inspecting
// r.TimedOut/r.Exit.
//
// Unlike darwin (exec.CommandContext), the process is started with plain
// exec.Command so the kill path is explicit: after Start(), the child is
// assigned to a Job Object with JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE, so a
// timeout (or a canceled ctx, e.g. a dropped bridge connection) can
// TerminateJobObject the whole tree - including a `cmd /C`-spawned
// grandchild like ping.exe - with one call, leaving no orphan (ticket
// Decision: "a Job Object as the suggested exec-timeout kill mechanism").
func (b *Backend) Exec(ctx context.Context, req backend.ExecReq) (backend.ExecResult, error) {
	argv := buildExecArgv(req)
	cmd := exec.Command(argv[0], argv[1:]...)
	// cmd.Stdin left nil: Go already reads from the null device for a nil
	// Stdin, matching help.txt exec's "stdin is closed".
	cmd.Stdin = nil
	// Suppress the console flash a `cmd /C` (or any console-subsystem
	// child) would otherwise pop up (help-windows.txt "exec ON WINDOWS").
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	stdout := &cappedWriter{limit: execOutputCap}
	stderr := &cappedWriter{limit: execOutputCap}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		// Spawn failure (help.txt E_EXEC "spawn failure"); the engine's
		// existing err != nil -> E_EXEC path handles this unchanged.
		return backend.ExecResult{}, err
	}

	job := assignToKillOnCloseJob(cmd.Process.Pid)
	if job != 0 {
		// Closing the job handle after the wait releases it; with
		// KILL_ON_JOB_CLOSE set this would also kill anything still
		// assigned, but by the time this defer runs Wait() has already
		// returned (the process, and any grandchild sharing the job, is
		// already gone or is about to be explicitly terminated below).
		defer windows.CloseHandle(job)
	}

	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()

	var timeoutC <-chan time.Time
	if req.Timeout > 0 {
		timer := time.NewTimer(req.Timeout)
		defer timer.Stop()
		timeoutC = timer.C
	}

	var waitErr error
	var timedOut bool
	select {
	case waitErr = <-waitCh:
	case <-timeoutC:
		timedOut = true
		killJob(job, cmd)
		waitErr = <-waitCh
	case <-ctx.Done():
		// Not a timeout (req.Timeout may be unset) - a safety net so a
		// canceled run context (e.g. the bridge's connection-dropped
		// cancel, internal/bridge/session.go) still reaps the child instead
		// of leaking it; reported through the generic waitErr path below,
		// same as darwin's ctx-cancel behavior via CommandContext.
		killJob(job, cmd)
		waitErr = <-waitCh
	}

	truncated := stdout.truncated || stderr.truncated
	cp := consoleOutputCodePage()
	outStr := decodeCodePage(stdout.buf.Bytes(), cp)
	errStr := decodeCodePage(stderr.buf.Bytes(), cp)

	if timedOut {
		return backend.ExecResult{
			TimedOut:  true,
			Stdout:    outStr,
			Stderr:    errStr,
			Truncated: truncated,
		}, nil
	}

	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) {
		return backend.ExecResult{
			Exit:      exitErr.ExitCode(),
			Stdout:    outStr,
			Stderr:    errStr,
			Truncated: truncated,
		}, nil
	}
	if waitErr != nil {
		// An unexpected Wait error - not an *exec.ExitError - falls into
		// E_EXEC generically via the returned error.
		return backend.ExecResult{}, waitErr
	}
	return backend.ExecResult{
		Exit:      0,
		Stdout:    outStr,
		Stderr:    errStr,
		Truncated: truncated,
	}, nil
}

// assignToKillOnCloseJob creates an anonymous Job Object with
// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE and assigns pid to it. Best effort: a
// failure anywhere in this chain (CreateJobObject, OpenProcess,
// AssignProcessToJobObject all need real privileges/handles that can
// theoretically fail) returns 0 rather than failing Exec outright - a
// timeout then falls back to killJob's plain cmd.Process.Kill(), which
// still stops the direct child even without Job Object tree-kill coverage.
func assignToKillOnCloseJob(pid int) windows.Handle {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(job)
		return 0
	}

	proc, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		windows.CloseHandle(job)
		return 0
	}
	defer windows.CloseHandle(proc)
	if err := windows.AssignProcessToJobObject(job, proc); err != nil {
		windows.CloseHandle(job)
		return 0
	}
	return job
}

// killJob terminates every process in job (the whole tree, including a
// `cmd /C`-spawned grandchild like ping.exe - the concrete regression the
// Job Object exists to prevent); when job is 0 (assignToKillOnCloseJob
// failed) it falls back to killing just the direct child.
func killJob(job windows.Handle, cmd *exec.Cmd) {
	if job != 0 {
		windows.TerminateJobObject(job, 1)
		return
	}
	if cmd.Process != nil {
		cmd.Process.Kill()
	}
}

// mbErrInvalidChars (winnls.h) - MultiByteToWideChar's dwFlags bit that
// fails the call instead of silently best-fit-substituting an undecodable
// byte, so decodeCodePage can detect the invalid span and substitute U+FFFD
// itself (help-windows.txt "exec ON WINDOWS": "Undecodable bytes become
// U+FFFD").
const mbErrInvalidChars = 0x00000008

// consoleOutputCodePage resolves the code page exec's stdout/stderr bytes
// are decoded with (help-windows.txt "exec ON WINDOWS"): the console output
// code page, falling back to the ANSI code page when the executing process
// has no console (e.g. a Task Scheduler-started bridge) - ticket Decision.
func consoleOutputCodePage() uint32 {
	if cp, err := windows.GetConsoleOutputCP(); err == nil && cp != 0 {
		return cp
	}
	return windows.GetACP()
}

// decodeCodePage decodes b from Windows code page cp to UTF-8, substituting
// U+FFFD for any byte span MultiByteToWideChar(MB_ERR_INVALID_CHARS)
// rejects (help-windows.txt "exec ON WINDOWS"). The common case (the whole
// buffer decodes cleanly) is one MultiByteToWideChar round trip; only on
// failure does it fall back to a short-window scan so a double-byte code
// page (CP949, CP932, ...) still decodes a valid lead+trail pair as one
// character instead of shredding it into two bogus replacement runes.
func decodeCodePage(b []byte, cp uint32) string {
	if len(b) == 0 {
		return ""
	}
	if s, ok := tryDecodeCodePage(b, cp); ok {
		return s
	}
	var sb strings.Builder
	i := 0
	for i < len(b) {
		matched := false
		for n := 1; n <= min(4, len(b)-i); n++ {
			if s, ok := tryDecodeCodePage(b[i:i+n], cp); ok {
				sb.WriteString(s)
				i += n
				matched = true
				break
			}
		}
		if !matched {
			sb.WriteRune(utf8.RuneError)
			i++
		}
	}
	return sb.String()
}

// tryDecodeCodePage decodes b as one MB_ERR_INVALID_CHARS-strict
// MultiByteToWideChar call; ok is false when the span contains an invalid
// or incomplete multi-byte sequence for cp.
func tryDecodeCodePage(b []byte, cp uint32) (string, bool) {
	if len(b) == 0 {
		return "", true
	}
	n, err := windows.MultiByteToWideChar(cp, mbErrInvalidChars, &b[0], int32(len(b)), nil, 0)
	if err != nil || n <= 0 {
		return "", false
	}
	buf := make([]uint16, n)
	if _, err := windows.MultiByteToWideChar(cp, mbErrInvalidChars, &b[0], int32(len(b)), &buf[0], n); err != nil {
		return "", false
	}
	return string(utf16.Decode(buf)), true
}
