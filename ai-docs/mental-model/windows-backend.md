# Mental Model: windows-backend

`internal/backend/windows` — the `golang.org/x/sys/windows` LazyDLL backend
(`CGO_ENABLED=0`, no cgo). This file captures only what the code cannot make
obvious on its own — rules whose violation surfaces as an intermittent or
long-uptime runtime failure rather than a compile error or a failing test on
this Go-less-target project.

## Domain Rules

- **`windows.NewCallback` trampolines are process-global and never freed —
  build each one exactly once.** `windows.NewCallback` allocates a slot from a
  small, permanent, process-wide pool with no matching free API; a process
  that exhausts it panics with "too many callback functions". Any FFI that
  takes a Go callback (`EnumWindows`, `EnumDisplayMonitors`, and any future
  enumerator) must build its trampoline once via `sync.Once` and route each
  call's results through mutex-guarded package state, never through a
  per-call captured closure. This is not a micro-optimization: `Backend.Windows`
  is exactly what `internal/engine`'s `pollForWindow` loop calls every 100 ms
  for `win[wait=]`/`open[wait=]`, and the resident `--bridge` process serves
  many forwarded runs over its lifetime, so a per-call `NewCallback` leaks
  until the bridge crashes. The mutex is required regardless, because a single
  shared trampoline that appends to package state is not reentrant against
  concurrent callers. Existing instances: `windowEnum` in `windows.go`,
  `displayEnum` in `probes.go` — copy that shape for any new enumerator.

- **Foreground-lock bypass in `Focus` must run pinned to one OS thread.** The
  `AttachThreadInput` + synthetic-Alt-tap + `SetForegroundWindow` retry
  (mandated verbatim by `assets/help-windows.txt` CAVEATS) only works when the
  OS thread that calls `SetForegroundWindow` is the same one attached to the
  foreground window's input queue. Go's async preemption (on by default on
  Windows) can otherwise migrate the goroutine mid-retry and silently defeat
  the bypass — returning a spurious `E_NOWINDOW`. Wrap the retry in
  `runtime.LockOSThread()`/`defer runtime.UnlockOSThread()` and read the
  current thread id only after locking. A synchronous unit test cannot force
  this scheduler-timing race, so the rule lives here.

- **`GetDIBits` forbids its source bitmap being selected into the DC passed to
  it.** Pass a DC the bitmap was never selected into (the capture path passes
  `hdcScreen`, not the `hdcMem` the bitmap is selected into until the deferred
  `SelectObject` restore runs). `GetDIBits`'s `hdc` argument only resolves a
  device's default color format, which is irrelevant when an explicit
  `BITMAPINFOHEADER` is always supplied — so this costs nothing and avoids a
  driver-dependent, `E_CAPTURE`-surfacing contract violation that "often works
  on Windows 10" and thus hides on casual testing.

## Verification Reality

Go is not installed on the Windows target box, so windows-gated code is
verified here only by cross-compile (`CGO_ENABLED=0 GOOS=windows GOARCH=amd64
go build ./...` + `go vet` + `go test -c`) plus pure-logic unit tests off
synthetic fixtures; live FFI behavior (real `EnumWindows`, GDI capture, Job
Object tree-kill, `ShellExecuteExW`) is confirmed only over the session bridge
against a real GUI session. Treat the three rules above as load-bearing
precisely because no local test exercises the paths they govern.
