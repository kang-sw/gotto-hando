# Plan: 260907-feat-windows-backend — Phase 2: Windows, monitors, capture, exec/open

## Relevant Ticket Contract

- Goals (ticket Phase 2): `win` (selectors, `wait=`, restore/foreground),
  `qwin` (visible, not cloaked, non-empty title, no tool windows; DWM
  extended-frame bounds), `qdisp` (already fully implemented by Phase 1 —
  nothing to do), `cap` (full/`w`/`disp=`/`rect=`, `scale`, `n`/`ms` frame
  series, `label`; PNG only, black `PrintWindow` = ok; JPEG/`q=`/cursor stay
  post-v1), `exec` (argv/`cmd /C`, code-page conversion, `noerr`, timeout
  kill that leaves no orphaned `cmd /C` child — Job Object suggested), `open`
  via `ShellExecuteExW` + the shared `wait=` poll loop.
- Verification boundary (ticket, verbatim): `qwin` excludes cloaked UWP
  windows; `cap[w]` at `scale=1` WxH == DWM extended-frame bounds `qwin`
  reports for that id; `win[wait=5s]` succeeds on a window appearing 2s
  later, `E_NOWINDOW` after ~5s when it never appears; `open[wait=5s]notepad`
  + `qwin[]app:notepad`; `open[wait=]` handoff resolves via the
  image-basename fallback; `exec[shell]dir` output is valid UTF-8 on a
  Korean-locale machine; `exec[timeout=1s]ping -n 10 localhost` is killed
  with `E_TIMEOUT` (no orphan); then QUICK START/EXAMPLES against Notepad.
- Decisions this phase must implement (ticket, not to relitigate):
  `EnumWindows`+`DwmGetWindowAttribute` (cloaked filter,
  `DWMWA_EXTENDED_FRAME_BOUNDS`) for window enumeration;
  `SetForegroundWindow`/`ShowWindow` for focus, with the CAVEATS retry
  (input-thread attach + synthetic Alt tap, `E_NOWINDOW` if still refused);
  GDI `BitBlt` for desktop/display/rect capture and `PrintWindow` for window
  capture; `exec[shell]` = `cmd /C`; code page from `GetConsoleOutputCP`
  (fallback `GetACP`) → UTF-8 with U+FFFD; `open` via `ShellExecuteExW` +
  `SEE_MASK_NOCLOSEPROCESS`; a Job Object as the suggested exec-timeout kill
  mechanism (no orphaned `ping.exe`).
- Constraint: UIPI silently drops input to elevated windows — stays a
  documented caveat, not a detectable error; not this phase's concern beyond
  not attempting to detect it.
- Constraint: capture of a headless/disconnected session returns
  `E_CAPTURE`; held-release/clipboard/capture-bytes rules match the darwin
  ticket (already satisfied by the shared `backend.Image` raw-pixel
  contract and engine-side PNG encode).
- `Backend.Preflight`'s five-check gate, the session/held-key exemption
  list, and `RequiresSession` are **already correct for every Phase 2 kind**
  with zero changes needed (verified by reading
  `internal/backend/windows/preflight.go` in full — see Codebase Findings).

## Out of Scope

- Phase 1 (landed, Result 4d523dc): keyboard/mouse/scroll/clipboard/text,
  session/lock detection, five-check preflight, `dispatch_windows.go`, the
  DPI manifest `.syso` + `init` fallback — all already checked in and not to
  be touched.
- `internal/engine/*` and `internal/ir/*`: `win`/`qwin`/`cap`/`exec`/`open`
  dispatch, the `win[wait=]`/`open[wait=]` shared 100ms poll loop, PNG
  encoding, capture path building, `openAppSelector`, and all OUTPUT/JSONL
  formatting for these commands are **already fully implemented and
  GOOS-agnostic** by the landed darwin backend's phases — confirmed by
  reading `internal/engine/run.go`, `capture.go`, `query.go`, `exec.go` in
  full. Phase 2 only implements the five stubbed `backend.Backend` methods
  in `internal/backend/windows/`.
- JPEG / `fmt=` / `cursor` capture modifiers (`260907-feat-post-v1-extensions`).
- Remote/bridge forwarding wiring (`260908-feat-remote-ssh`) — `remote.go`,
  `bridge_pipe.go` stay untouched; the bridge only needs `Windows`, `Focus`,
  `Capture`, `Exec`, `Open` to exist and work once forwarding lands.
- DPI manifest / `rsrc_windows_amd64.syso` generation — **already done in
  Phase 1** (`cmd/gotto-hando/rsrc_windows_amd64.syso`,
  `cmd/gotto-hando/windows.manifest`, `internal/backend/windows/init.go` all
  exist and are checked in; confirmed by reading them). Not a Phase 2 task.
- `--request-perms` (Windows: exit 2, "nothing to request") — already
  generic/handled per ticket Decisions; not touched by this phase.

## Codebase Findings

- `internal/backend/windows/stubs.go#L20-38` — the five methods Phase 2
  must implement: `Windows`, `Focus`, `Capture`, `Exec`, `Open`. This file
  is deleted incrementally as each is implemented (darwin's own
  `stubs.go` history is the precedent for partial deletion across phases).
- `internal/backend/backend.go#L162-186` — the `Backend` interface these
  five methods must satisfy; `Image`/`CaptureReq`/`ExecReq`/`ExecResult`/
  `Window` types are already shared, GOOS-agnostic value types.
- `internal/backend/darwin/windows.go` (324 lines) — reference shape for
  `Windows`/`Focus`: `filterWindows`/`selectorMatches` (pure, package-local,
  ~35 lines) implement the four WINDOW SELECTOR kinds
  (`assets/help.txt:286-297`) and are **duplicated per-backend by design**
  (no shared package exists yet — same pattern as `unionBounds` already
  duplicated between darwin's and windows' `backend.go`); windows Phase 2
  should copy this logic verbatim rather than extracting a shared package
  (surgical-changes doctrine, matches existing precedent).
- `internal/backend/darwin/capture.go` (302 lines) — reference shape for
  `Capture`: `captureRect` (pure frame+rect→region resolution with
  `backend.ErrBounds`/`backend.ErrNoWindow` wrapping), `focusedWindow`
  helper (calls `b.Windows(ctx, ir.Selector{Kind:"title",Value:""})` and
  picks the `Focused` one — reused verbatim by `engine/compose.go:243` too),
  `scaledDims`/`resizeRGBANearest`/`bgraToRGBA` (pure pixel-format
  conversion to the tightly-packed RGBA `backend.Image` shape). Windows
  needs its own `captureRect`-equivalent and its own GDI→RGBA converter (DIB
  rows may be bottom-up unless requested top-down via a negative
  `biHeight`) — same duplication precedent as above.
- `internal/backend/darwin/exec.go` (126 lines) / `open.go` (31 lines) —
  reference shape: `cappedWriter`/`execOutputCap` (64 KiB/stream cap, pure,
  duplicate into windows), `buildExecArgv` analog, and the critical
  contract in `Exec`'s doc comment: **a timeout or non-zero exit must
  return via `ExecResult` with a `nil` error** — only spawn failure /
  unexpected `Wait` error is a non-nil error (`internal/engine/run.go`'s
  `doExec` depends on this to distinguish `E_TIMEOUT` from `E_EXEC`).
- `internal/backend/windows/probes.go#L92-110` (`activeDisplays`) — the
  `windows.NewCallback(...)` + `procEnumDisplayMonitors.Call(...)` pattern
  is the template `EnumWindows` enumeration should mirror exactly (a Go
  closure callback appending to a captured slice, `return 1` to continue).
- `internal/backend/windows/ffi.go` (185 lines, full read) — confirms
  **no** `EnumWindows`, `DwmGetWindowAttribute`, `SetForegroundWindow`,
  `ShowWindow`, GDI, `ShellExecuteEx`, or Job Object procs exist yet.
  Verified via `go doc`/source inspection of `golang.org/x/sys/windows
  v0.47.0` (the already-pinned, only dependency — **no new `go.mod`
  entry needed for anything in this phase**):
  - **Already available directly as Go functions (no new LazyDLL binding
    needed)**: `windows.EnumWindows`, `windows.IsWindowVisible`,
    `windows.GetWindowThreadProcessId`, `windows.GetForegroundWindow`,
    `windows.DwmGetWindowAttribute`/`DwmSetWindowAttribute` (with
    `windows.DWMWA_EXTENDED_FRAME_BOUNDS = 9`, `windows.DWMWA_CLOAKED = 14`
    already defined constants), `windows.ShellExecute` (basic, non-Ex),
    `windows.GetClassName`, `windows.GetCurrentThreadId`,
    `windows.OpenProcess`, `windows.QueryFullProcessImageName`,
    `windows.GetProcessId`, `windows.GetExitCodeProcess`,
    `windows.MultiByteToWideChar`, `windows.GetConsoleOutputCP`,
    `windows.GetACP`, `windows.CreateJobObject`,
    `windows.AssignProcessToJobObject`, `windows.SetInformationJobObject`,
    `windows.TerminateJobObject`,
    `windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION`,
    `windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE = 0x2000`,
    `windows.PROCESS_SET_QUOTA`/`PROCESS_TERMINATE`,
    `windows.CREATE_NO_WINDOW`. This is a significant reuse surface — most
    of the ticket's FFI list needs no manual binding at all.
  - **Needs a new `ffi.go` LazyDLL proc** (confirmed absent from
    `x/sys/windows`): `SetForegroundWindow`, `ShowWindow`,
    `GetWindowTextW`/`GetWindowTextLengthW`, `GetWindowLongPtrW` (for
    `WS_EX_TOOLWINDOW` filtering), `AttachThreadInput`, `IsIconic`,
    `PrintWindow` (user32.dll); `GetDC`/`ReleaseDC`/`CreateCompatibleDC`/
    `CreateCompatibleBitmap`/`SelectObject`/`DeleteDC`/`DeleteObject`/
    `BitBlt`/`GetDIBits` (gdi32.dll); `ShellExecuteExW` +
    `SHELLEXECUTEINFOW` struct + `SEE_MASK_NOCLOSEPROCESS` const
    (shell32.dll) — `x/sys/windows` only wraps the simpler `ShellExecuteW`,
    not the `Ex` variant the ticket requires for `hProcess`.
- `assets/help-windows.txt` CAVEATS section — **non-obvious constraint**:
  "`SetForegroundWindow` may only flash the taskbar button. `win` retries
  with input-thread attachment and a synthetic Alt tap and reports
  `E_NOWINDOW` if the window still did not come to the front." This is a
  specific, mandatory retry mechanism for `Focus`, not a generic
  best-effort call — missing it is a risk signal (a naive
  `SetForegroundWindow`-only `Focus` will intermittently fail silently on a
  locked-foreground-window OS restriction).
- `assets/help-windows.txt` DPI AND COORDINATES — `qwin` filter is
  precisely: visible (`IsWindowVisible`) + not cloaked
  (`DwmGetWindowAttribute DWMWA_CLOAKED == 0`) + non-empty title
  (`GetWindowTextLengthW > 0`) + not a tool window
  (`GetWindowLongPtrW(GWL_EXSTYLE) & WS_EX_TOOLWINDOW == 0`). Frame = DWM
  extended-frame bounds (excludes the invisible drop-shadow border), not
  the raw window rect — this is what makes the `cap[w]` WxH ==
  `qwin`-reported-bounds verification bullet true.
- `internal/engine/exec.go#L64-78` (`openAppSelector`) — `open`'s `wait=`
  poll calls `st.be.Windows(ctx, sel)` with `sel = {Kind:"app", Value:
  <basename>}` (no PID). The ticket's "owned by the launched process id
  or, when the shell handed off, by any process whose image basename
  equals the payload's basename" nuance therefore cannot be expressed
  through the selector alone — the windows `Backend` needs a small piece
  of state from `Open` (the launched PID, set when `ShellExecuteExW`
  returns a non-null `hProcess`) that `Windows`'s app-selector matching
  consults to prefer that PID's window before falling back to a plain
  image-basename substring match across all processes (which `Windows`
  already does generically once `Window.App` = image basename, satisfying
  the "handed-off" fallback with no extra logic). This is an ordinary
  implementation-time design call (add one or two fields to `*Backend`,
  set in `Open`, read in `Windows`), not a contract ambiguity: the ticket
  spells out the exact mechanism.
- `internal/backend/windows/preflight.go` (full read) — `exemptFromSession`
  (query-only kinds) and `injectsInputGated` **already include
  `ir.KindFocus`** (forward-compat comment: "stays in the set for
  forward-compat even though its Backend method is a Phase-2 stub").
  `KindQueryWindows`, `KindCapture`, `KindExec`, `KindOpen` are correctly
  **not** in `exemptFromSession` (every command but
  `qinfo/qdisp/qmouse/sleep/set` requires an unlocked session, matching the
  ticket). `collectPreflightPoints` deliberately does not cover `cap`/`win`
  (neither carries an absolute `ir.Point`: `win`'s payload is a selector,
  `cap`'s `rect=` is frame-relative and resolved/bounds-checked entirely
  inside the backend's own `Capture`, mirroring darwin). **Net finding: no
  change to `preflight.go` is needed for Phase 2.**
- `go.mod` — `golang.org/x/sys v0.47.0` is the only dependency; confirmed
  every new API this phase needs is either already wrapped by that module
  version or bindable via the existing `LazyDLL`/`LazyProc` pattern in
  `ffi.go` — no new `go.mod` entry, no cgo.
- Baseline cross-compile check (run this survey pass, no source changes):
  `CGO_ENABLED=0 GOOS={darwin,windows,linux} GOARCH={amd64,arm64} go build
  ./...` is clean for all six combinations today — confirms Phase 1 left a
  green baseline to build on.

## Implementation Plan

**Recommended structure: two ordered slices inside this one phase, not two
separate tickets/PRs.** Slice A (`win`/`qwin`/`cap`) and Slice B
(`exec`/`open`) touch disjoint FFI surfaces (user32/dwmapi/gdi32 window +
capture vs. kernel32 job-object + shell32 process launch) and disjoint new
files, with only one soft dependency: `open[wait=]`'s basename-fallback
relies on `Windows` (Slice A) already being correct. Implementing A fully,
checkpointing (build/vet/test), then B, keeps each commit reviewable and
matches AGENTS.md's "one commit per logical unit." A single undifferentiated
pass risks a large, hard-to-bisect diff across two genuinely unrelated FFI
domains for no benefit — recommend against it.

### Slice A — `win` / `qwin` / `cap`

1. **`internal/backend/windows/ffi.go`**: add the new LazyDLL procs listed
   in Codebase Findings for window enumeration/focus/capture
   (`SetForegroundWindow`, `ShowWindow`, `GetWindowTextW`,
   `GetWindowTextLengthW`, `GetWindowLongPtrW`, `AttachThreadInput`,
   `IsIconic`, `PrintWindow`, and the gdi32 GDI family), plus the
   `WS_EX_TOOLWINDOW` constant and a `PW_RENDERFULLCONTENT` constant for
   `PrintWindow`. Reuse `windows.EnumWindows`/`DwmGetWindowAttribute`/etc.
   directly (no rebinding) per the reuse list above.
2. **`internal/backend/windows/windows.go`** (new file, mirrors
   `internal/backend/darwin/windows.go`'s shape): `Windows(ctx, sel)` —
   `windows.EnumWindows` with a `windows.NewCallback` closure (pattern:
   `probes.go`'s `activeDisplays`), apply the qwin filter (visible, not
   cloaked, non-empty title, not tool window) from Codebase Findings, build
   `backend.Window{ID: int(hwnd), PID, App: <image basename via
   OpenProcess+QueryFullProcessImageName>, Title, X/Y/W/H: DWM extended
   frame bounds, Minimized: IsIconic, Focused: hwnd ==
   GetForegroundWindow(), Hidden: false}` (Hidden stays false because the
   qwin filter already excludes anything that would set it — document this
   choice in a comment), then apply a package-local
   `filterWindows`/`selectorMatches` duplicated from darwin's (same four
   selector kinds, same case-insensitive substring/regex rules).
3. **`internal/backend/windows/windows.go`**: `Focus(ctx, w)` —
   `ShowWindow(hwnd, SW_RESTORE)` then `SetForegroundWindow(hwnd)`; if the
   window is not the new foreground window, retry once via
   `AttachThreadInput(GetCurrentThreadId(), <foreground window's thread id
   from GetWindowThreadProcessId(GetForegroundWindow())>, true)` +
   a synthetic Alt key tap (reuse the Phase-1 `SendInput`/scan-code path
   from `keyboard.go` for `VK_MENU`) + `SetForegroundWindow` again, then
   `AttachThreadInput(..., false)`. Return an error (mapped to
   `E_NOWINDOW` by `internal/engine/run.go doFocus`) if still not
   foreground after the retry, per the help-windows.txt CAVEAT verbatim.
4. **`internal/backend/windows/capture.go`** (new file, mirrors darwin's
   `capture.go`): a `captureRect`-equivalent pure function resolving
   `backend.CaptureReq` (`desktop`/`window`/`display` frame + optional
   `rect=`) against `b.displays.Active()` (already exists) and a
   `focusedWindow` helper (calls this package's own `Windows` with an
   empty title selector, picks `Focused`), wrapping `backend.ErrBounds`/
   `backend.ErrNoWindow` exactly like darwin.
5. **`internal/backend/windows/capture.go`**: `Capture(ctx, req)` — for
   `desktop`/`display`/`rect` frames: `GetDC(0)` (or a specific monitor's
   DC is not needed since virtual-desktop coordinates already select the
   region) → `CreateCompatibleDC` → `CreateCompatibleBitmap` →
   `SelectObject` → `BitBlt(..., SRCCOPY)`; for `window` frame:
   `PrintWindow(hwnd, hdcMem, PW_RENDERFULLCONTENT)` (a black result is
   `ok`, per Decisions — only a GDI API failure or zero-size bitmap is
   `E_CAPTURE`). Read pixels via `GetDIBits` into a `BITMAPINFO` requesting
   a top-down (negative `biHeight`) 32-bit BGRA DIB, convert to
   tightly-packed RGBA (mirror darwin's `bgraToRGBA`, forcing alpha
   opaque), release every GDI handle via `defer` (`DeleteObject`/`DeleteDC`/
   `ReleaseDC`) on every path including early-return errors. `scale=`
   handling can reuse darwin's `scaledDims` logic conceptually but Windows
   has no backing-pixel/point distinction (Per-Monitor-V2 DPI already
   yields physical pixels) — `nativeScale` is always 1; document this
   simplification inline.
6. **`internal/backend/windows/stubs.go`**: delete the `Windows`, `Focus`,
   `Capture` stub methods; leave `Exec`/`Open` stubs in place for Slice B
   (same partial-deletion precedent as darwin's own phased `stubs.go`).
7. **Checkpoint**: `go build ./...`, `go vet ./...`, `gofmt -l .`,
   `go test ./... -race` (native, Mac); `CGO_ENABLED=0 GOOS=windows
   GOARCH=amd64 go build ./...` and `go vet ./...` clean (only the two
   already-documented `unsafe.Pointer` warnings on the clipboard path are
   acceptable); commit.

### Slice B — `exec` / `open`

8. **`internal/backend/windows/ffi.go`**: add `ShellExecuteExW`
   (shell32.dll) LazyDLL proc, a hand-rolled `SHELLEXECUTEINFOW` struct
   matching the Win32 layout, and `seeMaskNocloseprocess` +
   `swShownormal` constants.
9. **`internal/backend/windows/exec.go`** (new file): duplicate
   `cappedWriter`/`execOutputCap` from darwin's `exec.go` (per the
   per-backend duplication precedent); a `buildExecArgv`-equivalent
   (direct argv vs. `%ComSpec%`/`cmd.exe -C <payload>` for `shell`, per
   `assets/help-windows.txt exec ON WINDOWS`); `Exec(ctx, req)` via
   `exec.Command` (not `CommandContext`, to control the kill path
   explicitly) with `cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow:
   true}` (suppress the console flash for `cmd /C`); after `cmd.Start()`,
   open the child's process handle
   (`windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE,
   false, uint32(cmd.Process.Pid))`), `windows.CreateJobObject(nil, nil)`,
   `windows.SetInformationJobObject(job,
   windows.JobObjectExtendedLimitInformation, ...)` with
   `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` set, `windows.AssignProcessToJobObject(job,
   handle)`; race `cmd.Wait()` against `req.Timeout` on a timer — on
   timeout, `windows.TerminateJobObject(job, 1)` (kills the whole tree,
   including any `ping.exe` grandchild) then still call `cmd.Wait()` to
   reap and set `TimedOut: true`; preserve darwin's exact `nil`-error
   contract (timeout and non-zero exit both return via `ExecResult` with a
   `nil` error). Decode `stdout`/`stderr` bytes via
   `windows.GetConsoleOutputCP()` (fallback `windows.GetACP()` on error) →
   `windows.MultiByteToWideChar` → `utf16.Decode` (stdlib) → `string`,
   with U+FFFD substitution for undecodable bytes — the exact
   substitution strategy (e.g., `MB_ERR_INVALID_CHARS` + per-invalid-byte
   retry vs. a single best-fit pass) is an implementation-time detail, not
   a contract question.
10. **`internal/backend/windows/open.go`** (new file): `Open(ctx, target)`
    via `ShellExecuteExW` with `SEE_MASK_NOCLOSEPROCESS`, `nShow =
    SW_SHOWNORMAL`; on success with a non-null `hProcess`, record the
    launched PID (`windows.GetProcessId(hProcess)`) and the target's
    basename on `*Backend` (new small fields, e.g. `lastOpenPID
    uint32`/`lastOpenBasename string`, cleared or overwritten on each
    `Open` call — single-sequence execution, no concurrency concern). A
    non-zero `ShellExecuteExW` failure (`hInstApp <= 32`) maps to the
    existing generic `E_EXEC` path via a returned error, matching darwin's
    `Open` contract. Extend `Windows`'s app-selector branch (step 2) to
    prefer a window owned by `lastOpenPID` when the selector value matches
    `lastOpenBasename` (case-insensitive), before falling back to the
    already-generic basename-substring match — implements the ticket's
    "owned by the launched process id or ... basename fallback" exactly.
11. **`internal/backend/windows/stubs.go`**: delete the remaining `Exec`/
    `Open` stub entries (the file becomes empty — delete it, matching
    darwin's own eventual empty-`stubs.go` fate once Phase 3 landed there,
    per that ticket's reference).
12. **Checkpoint**: same build/vet/gofmt/test matrix as step 7; commit.

## Verification Plan

Run this phase (safe, this Mac, no Windows box needed):
- `go build ./...`, `go vet ./...`, `gofmt -l .`, `go test ./... -race` —
  GOOS-agnostic engine/assets/ir tests plus every native-buildable package
  green.
- `CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build ./...` and `go vet
  ./...` clean, accepting only the two pre-existing documented
  `unsafe.Pointer` clipboard warnings (Phase 1 Result).
- `CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go test -c ./internal/backend/windows/...`
  compiles (compile-only verification here — Go is not installed on the
  target Windows box, so windows-gated `_test.go` files are exercised for
  real only on the box, matching Phase 1's precedent).
- Four-GOOS build matrix (`darwin/amd64`, `darwin/arm64`, `windows/amd64`,
  `linux/amd64`, `CGO_ENABLED=0`) stays green — confirmed clean as a
  baseline before this phase's changes; re-run after each slice.
- Unit tests to add (pure logic, no live FFI, same style as darwin's
  `windows_test.go`/`exec_test.go`/`open_test.go`): `filterWindows`/
  `selectorMatches` across id/pid/app/title-substring/title-regex on a
  synthetic `[]backend.Window`; the capture `captureRect`-equivalent
  bounds/`E_BOUNDS`/`E_NOWINDOW` resolution against synthetic
  display/window geometry (the existing `displayProbe` fake-injection seam
  from `preflight_test.go` extends naturally); `buildExecArgv`-equivalent
  argv construction (direct vs. `cmd /C`); `cappedWriter` truncation;
  the code-page-decode helper against known CP949/CP437 byte fixtures
  including at least one undecodable byte (assert U+FFFD); the DWM
  extended-frame-bounds / `SHELLEXECUTEINFOW` struct sizes (a
  `binary.Size`-style sanity check, same spirit as Phase 1's `INPUT`
  struct-size assertion in `ffi_test.go`/`mouse_test.go`).
- GUI acceptance (run over the bridge against `sw.kang@192.168.100.2`,
  where a resident `gotto-hando.exe --bridge` is reported up in the GUI
  session; **deferred only if the bridge is found down** at verification
  time): `qwin` excludes cloaked UWP windows; `cap[w]` at `scale=1`
  produces WxH == the DWM extended-frame bounds `qwin` reports for that id;
  `win[wait=5s]` on a window appearing ~2s later succeeds, on one that
  never appears returns `E_NOWINDOW` after ~5s; `open[wait=5s]notepad`
  followed by `qwin[]app:notepad`; `open[wait=]` on an app whose launcher
  hands off to an already-running instance resolves via the
  image-basename fallback; `exec[shell]dir` output is valid UTF-8 on the
  Korean-locale box; `exec[timeout=1s]ping -n 10 localhost` is killed with
  `E_TIMEOUT` — verify via `tasklist` on the box that no `ping.exe`
  survives the kill (the concrete regression the Job Object exists to
  prevent); QUICK START and EXAMPLES sections against Notepad.

## Escalations

- None. The ticket's Decisions pin every architectural choice this phase
  needs (enumeration/focus/capture/exec/open mechanisms, the Job Object
  requirement, the `wait=` polling reuse, PNG-only v1 scope). Two
  implementation-time design calls are surfaced inline in the Implementation
  Plan rather than escalated, because the ticket already specifies the
  mechanism and only the small wiring detail is left to the executor:
  1. **`open[wait=]` PID-then-basename resolution** (step 10): the engine
     only passes an app-name selector (`internal/engine/exec.go
     openAppSelector`), so preferring the specific launched process over a
     same-named one requires a couple of small fields on `*Backend` set by
     `Open` and read by `Windows`. This is package-local state, not a
     `Backend` interface change, so it needs no cross-module approval.
  2. **U+FFFD substitution algorithm for the code-page decode** (step 9):
     the ticket specifies the codepage source (`GetConsoleOutputCP`
     fallback `GetACP`) and the target encoding (UTF-8 with U+FFFD for
     undecodable bytes) precisely; only the `MultiByteToWideChar` call
     pattern that achieves it (single best-fit pass vs. error-detect-and-
     retry) is left open, and either is a small, local, easily-tested
     function.
  Neither blocks starting or finishing Phase 2, and neither is a strategy
  or contract unknown.
