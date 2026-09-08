---
title: "Windows backend: input, windows, capture, session checks, exec/open"
parent: 260907-epic-gotto-hando-v1
related:
  260907-feat-cli-core: prerequisite
  260907-feat-darwin-backend: reference implementation of the same interface
sage-review-design: completed
sage-review-design-reviewed: 55bf13435baadcbb
sage-review-completeness: completed
sage-review-completeness-reviewed: 0ae58bfdce4b9bd2
---

# Windows backend: input, windows, capture, session checks, exec/open

## Background

Implements the `Backend` interface for windows/amd64 so the same sequences
run on a Windows desktop. Normative contract: `assets/help.txt` COMMANDS,
EXECUTION, STATE MACHINE, ERROR POLICY, CAVEATS and all of
`assets/help-windows.txt`. The darwin ticket's landed code is the reference
for engine expectations; nothing platform-specific is added to the command
set.

## Decisions

- No cgo: `golang.org/x/sys/windows` plus `LazyDLL` procs for `SendInput`,
  `GetAsyncKeyState`, `EnumWindows`/`DwmGetWindowAttribute` (cloaked
  filter, extended frame bounds), `SetForegroundWindow`/`ShowWindow`,
  `EnumDisplayMonitors`, `SetProcessDpiAwarenessContext`, GDI `BitBlt` for
  desktop/display/rect capture and `PrintWindow` for window capture (per
  help-windows.txt DPI AND COORDINATES), `OpenClipboard` family,
  `WTSGetActiveConsoleSessionId` / session-lock detection,
  `GetConsoleOutputCP`, `ShellExecuteExW`.
- Coordinates are physical pixels; the process declares per-monitor-v2 DPI
  awareness so window and monitor rectangles are physical. This ticket owns
  the application manifest (per-monitor-v2 DPI awareness, UTF-8 active code
  page) embedded as `cmd/gotto-hando/rsrc_windows_amd64.syso` (generated,
  checked in), plus a runtime `SetProcessDpiAwarenessContext` call in `init`
  as fallback; its failure is ignored when the manifest already applied.
- Keyboard uses scan codes with the extended-key flag for the keys listed in
  help-windows.txt; `txt` uses `KEYEVENTF_UNICODE`. `volup`/`voldown`/`mute`
  are sent through `SendInput` like any other key (every documented key name
  is supported on Windows, so preflight check 5 never fails here).
- Preflight runs all five checks of help.txt EXECUTION step 4 (exit 4,
  nothing runs), subject to the per-command gating below:
  1. GUI session - the process's own interactive session, or a reachable
     bridge when the process is not in it - unlocked: E_SESSION. Bridge
     forwarding itself lands in `260908-feat-remote-ssh`; this ticket
     provides the detection (process session id vs
     `WTSGetActiveConsoleSessionId`, `WinSta0` access, input desktop name
     for the lock/secure-desktop state).
  2. permissions: Windows has none (`perms=n/a`), E_PERMISSION never occurs.
  3. no key or mouse button physically held down (`GetAsyncKeyState` over
     every virtual key, mouse buttons included): E_INPUT, never E_VALIDATE
     or E_SESSION (help-windows.txt CHECK). This check is input-injection
     gating: it runs only when the run contains an input-injecting command,
     so a query-only run (`qinfo`/`qdisp`/`qmouse`/`sleep`/`set` and
     comments) is exempt and never aborts on a stuck key - matching the
     landed darwin backend and the clarified help.txt EXECUTION step 4, and
     keeping the `qinfo`/`--ping` recovery loop alive while a key is held.
     On Windows `GetAsyncKeyState` has no permission coupling, so this
     exemption is an explicit gating decision, not a side effect of a
     permission check (gate it on the same "run injects input" condition as
     macOS Accessibility, even though Windows itself needs no permission).
  4. absolute and `disp=` coordinates inside the virtual desktop: E_BOUNDS.
  5. every key name supported on this platform: E_INPUT (always passes on
     Windows).
- Session gating is per command: a run whose lines are only `qinfo`,
  `qdisp`, `qmouse`, `sleep`, `set` and comments never fails the session
  check (nor the held-key check, per check 3 above) and reports the state
  instead (`qinfo session=inactive|locked`), which keeps the recovery loop
  (`qinfo`, `--ping`) alive; every other command requires an unlocked
  interactive session (own or bridge).
- A preflight failure is reported as the `abort` object (help.txt JSONL):
  `{"event":"abort","code":"E_...","msg":"..."}` with no `done` object; in
  the plain format nothing goes to stdout and stderr gets
  `abort: <message> (<E_CODE>)`. `Backend.Preflight` returns the code via
  `*backend.PreflightError` (the coded-error type and the `engine.Run`
  Preflight-call + abort-mapping are already landed by
  `260907-feat-darwin-backend`); a non-coded error becomes `E_UNKNOWN`. The
  CLI layer from `260907-feat-cli-core` prints the abort and maps the exit
  code (E_CONNECT 3, everything else 4). The windows backend only has to
  return `*backend.PreflightError` from its five checks; no engine change is
  needed.
- Local dispatch wiring: Phase 1 adds `cmd/gotto-hando/dispatch_windows.go`
  (`//go:build windows`) whose `newLocalBackend()` constructs the windows
  backend behind `runtime.GOOS == "windows"`, parallel to the landed
  `dispatch_darwin.go`. The existing `dispatch_other.go` build constraint
  must narrow from `//go:build !darwin` to `//go:build !darwin && !windows`
  so windows gets exactly one `newLocalBackend` (its own), and every other
  GOOS keeps the exit-2 stub. `backend.Info.DisplayList` and the engine's
  `qinfo`/`qdisp`/`qmouse` formatting already exist (landed by darwin); the
  windows backend only populates them.
- `win[wait=DUR]` is v1 and shares one polling loop with `open[wait=]`: poll
  every 100 ms, E_NOWINDOW after DUR.
- `open` launches via `ShellExecuteExW` with `SEE_MASK_NOCLOSEPROCESS`.
  `open[wait=]` then polls every 100 ms for a visible, non-tool, top-level
  window owned by the launched process id or, when the shell handed off
  (`hProcess` null, or the process exited without a window), by any process
  whose image basename equals the payload's basename (case-insensitive).
  Same E_NOWINDOW rule as `win[wait=]`.
- Capture is PNG only in v1: `cap` has no `fmt=`, `q=` or `cursor` modifier
  (E_SYNTAX from the parser in `260907-feat-cli-core`; no runtime path here);
  the inline JSONL object carries `"fmt":"png"`. A black `PrintWindow`
  result is status `ok` (documented in help-windows.txt DPI AND
  COORDINATES); E_CAPTURE only for an API failure or a zero-size bitmap.
- `exec[shell]` is `cmd /C`; output decoded from the console output code page
  (fallback ACP) to UTF-8 with U+FFFD replacement.
- `--request-perms` locally exits 2 (nothing to request); `qinfo` reports
  `perms=n/a elevated=0|1`. There is no dedicated `backend.Info` field for
  elevation: pack `elevated=0|1` into the existing free-form `Info.Perms`
  string (as darwin composes its `perms=` value in `permsString`), so
  `qinfo` prints `perms=n/a elevated=0|1` from that one field.

## Constraints

- Input to an elevated (UAC) window is silently dropped by UIPI; the backend
  cannot detect the target's elevation reliably, so this stays a documented
  caveat, not an error.
- Capture of a headless/disconnected session returns E_CAPTURE.
- Same held-release, clipboard and capture-bytes rules as the darwin ticket.

## Spec Impact

None expected. `ai-docs/spec/help-windows.md` (one pointer anchor per
`== SECTION ==` of `assets/help-windows.txt`) is created by
`260907-feat-cli-core` together with `help.md`; this ticket adds an anchor
there only if it adds a new `== SECTION ==` to `assets/help-windows.txt`,
and never touches other anchors.

## Phases

### Phase 1: Keyboard, mouse, scroll, clipboard, text, session preflight

Goals: all KEYBOARD/MOUSE/CLIPBOARD commands, `qmouse`, `qinfo` (the
complete line including `desktop=`/`displays=` from `EnumDisplayMonitors`
and `elevated=`), delays, held-state, all five preflight checks with the
per-command gating rule and the `abort` result (held key = E_INPUT), the DPI
manifest `.syso` plus the `init` fallback call, key-name to scan-code table.
This phase also wires `local` dispatch: `Preflight` returns
`*backend.PreflightError`, and a new `dispatch_windows.go` factory (behind
`runtime.GOOS == "windows"`, with `dispatch_other.go` narrowed to
`!darwin && !windows`) runs the windows backend through the real CLI, so
`gotto-hando local ...` executes its Phase-1 command set end to end.
Verification: keyboard/mouse/clipboard commands against Notepad plus
`qinfo`/`qmouse`; `qinfo desktop=`/`displays=` match a two-monitor layout
with different scaling percentages; unit tests for scan-code mapping
(extended keys, numpad) with an injectable `SendInput`; unit tests with
injectable `GetAsyncKeyState` and session probes that a held key yields
`abort` E_INPUT for an input-injecting run, that a `qinfo`-only run passes
preflight in a locked or non-console session AND while a key is physically
held (check 3 is input-gated, so the recovery loop survives a stuck key),
and that a run containing `k` fails E_SESSION there;
cross-compile windows/amd64 from macOS and run on a Windows machine.

### Result (4d523dc) - 2026-09-08

`internal/backend/windows` (`golang.org/x/sys/windows` LazyDLL/LazyProc,
`CGO_ENABLED=0`) implements the Phase 1 command set: `k kd ku txt m c md mu
drag scroll clip paste qclip qmouse qdisp qinfo sleep set`, session/lock
detection (`ProcessIdToSessionId` vs `WTSGetActiveConsoleSessionId`, then
input-desktop name `Default`->active / `Winlogon`->locked / else->inactive;
`bridge` reserved-unwired for `260908-feat-remote-ssh`), the five-check
preflight gate in Constraints order with the input-injection gating on
checks 3 and 5 (a query-only run stays exempt even with a stuck key), `disp=`
display-relative bounds, and the full KEY NAMES scan-code table (every
canonical symbol supported, `volup`/`voldown`/`mute` included; extended-key
flag per help-windows.txt CAVEATS). `SendInput` drives keyboard (scan code +
extended flag; `txt` via `KEYEVENTF_UNICODE` UTF-16 units) and mouse
(absolute normalization to 0..65535 against the virtual-desktop metrics,
`MOUSEEVENTF_WHEEL`/`HWHEEL` `ticks*WHEEL_DELTA`); clipboard via
`OpenClipboard`/`GlobalAlloc`/`CF_UNICODETEXT`. `qinfo` packs `elevated=0|1`
into the free-form `Info.Perms` (`perms=n/a elevated=N`), so `formatQueryInfo`
prints it with no engine change; `Primary="ctrl"`, `OSVer` via `RtlGetVersion`,
`DisplayList`/`desktop=` from `EnumDisplayMonitors`+`GetMonitorInfoW`+
`GetDpiForMonitor`. Per-Monitor-V2 DPI awareness ships as a checked-in
`cmd/gotto-hando/rsrc_windows_amd64.syso` (generated from
`cmd/gotto-hando/windows.manifest` by `github.com/akavel/rsrc`, a build-time
tool that adds no `go.mod` require) plus a `SetProcessDpiAwarenessContext`
`init` fallback. `cmd/gotto-hando` dispatches `local` through the windows
backend behind `runtime.GOOS=="windows"` via `dispatch_windows.go`, with
`dispatch_other.go` narrowed to `!darwin && !windows`.

Verification (safe subset, run this phase): `go build`/`go vet`/`go test
./internal/engine/... ./assets/...` on the Mac green; `GOOS=windows
GOARCH=amd64 go build ./...`/`go vet` clean save two documented, unavoidable
`unsafe.Pointer` warnings on the cgo-free clipboard `GlobalLock` path
(`golang.org/x/sys/windows`'s own generated code carries the identical
pattern); `gofmt -l` clean. Cross-compiled `internal/backend/windows` unit
tests run on a real Windows 11 box (`sw.kang@192.168.100.2`, no Go installed,
via `go test -c` + scp) all pass, covering full scan-code coverage/extended
flags/numpad, the `INPUT` struct-size invariant, `scrollAmount` line==page
notches, `normalizeAbsolute`, and every preflight fake-probe gating scenario.
CLI smoke on the box: `local qinfo` -> `os=windows osver=10.0.26200 ...
session=inactive perms=n/a elevated=1` (the documented-correct result for an
ssh shell with no bridge), `local qdisp`/`qmouse` returned live values, and
`local 'k[]a'` aborted `E_SESSION` (exit 4).

Review: no Critical. Three Important (all test-quality: tautological
`scroll`/`normalizeAbsolute` tests that never called production code, and a
locked-session preflight test whose `held=false` contradicted its
check-1-before-check-3 claim) fixed by extracting pure `scrollAmount`/
`normalizeAbsoluteAgainst` seams and setting `held=true`, re-verified passing
against production code on the box; three Minor fixed (`ClipboardSet` now
`GlobalFree`s on both error paths, `gofmt`, a stale test doc comment).

Deviations: `golang.org/x/sys v0.47.0` added as a direct dependency; the
initial DPI manifest invented a non-existent `<dpiAwarenessContext>` element
that broke process startup (SxS error, reproduced live on the box) and was
corrected to the real `<dpiAwareness>` (2016 ns) + `<activeCodePage>` (2019
ns) elements; `GetProcessWindowStation` from the plan's ffi list was dropped
as redundant (session-id comparison already identifies session-0/service/
disconnected-RDP before `OpenInputDesktop`). `scroll[by=page]`: Windows has
no pixel/page-unit wheel injection API (only `WHEEL_DELTA` notches), so
`by=page` sends the same notches as `by=line`, documented as a new
`assets/help-windows.txt` CAVEATS bullet inside the already-anchored
`== CAVEATS ==` section (no new `== SECTION ==`, no new spec anchor).

Deferred to an unlocked interactive GUI/console session (not run this phase,
an ssh shell is not one): the real-`SendInput`-injection subset
(`k`/`txt`/`m`/`c`/`drag`/`scroll`/`clip`/`paste` against Notepad), the
two-monitor `qinfo desktop=`/`displays=` visual cross-check with differing
scaling, and a genuinely physically-held key exercising the real
`GetAsyncKeyState` path (covered by fake-probe unit tests until then).

### Phase 2: Windows, monitors, capture, exec/open

Depends on Phase 1. Goals: `win` (selectors, `wait=`, restore/foreground),
`qwin` (visible, not cloaked, non-empty title, no tool windows; DWM
extended-frame bounds), `qdisp`, `cap` (full/`w`/`disp=`/`rect=`, `scale`,
`n`/`ms` frame series, `label`; PNG only, black `PrintWindow` = ok; JPEG
and cursor overlay stay in `260907-feat-post-v1-extensions` as on darwin),
`exec` (argv/`cmd /C`, code page conversion, `noerr`, timeout kill that
leaves no child of `cmd /C` running so the verification below actually stops
`ping.exe`; a Job Object is the suggested mechanism), `open` with the
`ShellExecuteExW` + `wait=` mechanism from Decisions.
Verification: `qwin` excludes cloaked UWP windows; `cap[w]` at `scale=1`
produces an image whose WxH equals the DWM extended-frame bounds `qwin`
reports for that id; `win[wait=5s]` on a window that appears 2 s later
succeeds and on one that never appears returns E_NOWINDOW after about 5 s;
`open[wait=5s]notepad` followed by `qwin[]app:notepad`, and `open[wait=]` on
an app whose launcher hands off to an already-running instance resolves the
window through the image-basename fallback; `exec[shell]dir` output is
valid UTF-8 on a Korean-locale machine; `exec[timeout=1s]ping -n 10 localhost`
is killed with E_TIMEOUT; then the QUICK START and EXAMPLES sections against
Notepad, now that every command they use is implemented.

### Result (46376b1) - 2026-09-08

Windows backend Phase 2 lands the five previously stubbed `backend.Backend`
methods in `internal/backend/windows/` (`Windows`, `Focus`, `Capture`,
`Exec`, `Open`); `stubs.go` is deleted. The engine dispatch, PNG encode,
`win[wait=]`/`open[wait=]` shared 100 ms poll loop, capture path building,
and per-command session preflight were already GOOS-agnostic from the darwin
phases and needed zero change (verified). No new `go.mod` dependency: most
FFI is reused directly from `golang.org/x/sys/windows` v0.47.0; only the
user32 focus/enum-text/`PrintWindow`, gdi32 capture family, and
`ShellExecuteExW` (the Ex variant x/sys lacks) are new LazyDLL procs.

Behavioral delta:
- `win`/`qwin` — `EnumWindows` enumeration with the qwin filter (visible,
  not cloaked via `DWMWA_CLOAKED`, non-empty title, not a tool window);
  frame is DWM extended-frame bounds (`DWMWA_EXTENDED_FRAME_BOUNDS`), not the
  raw window rect. `Focus` does `ShowWindow(SW_RESTORE)` + `SetForegroundWindow`
  with the mandated `AttachThreadInput` + synthetic-Alt-tap retry, returning
  `E_NOWINDOW` if still refused.
- `cap` — GDI `BitBlt` for desktop/display/rect frames, `PrintWindow`
  (`PW_RENDERFULLCONTENT`) for a window frame (black result = ok);
  `GetDIBits` into a top-down 32-bit BGRA DIB, converted to tightly-packed
  RGBA. `nativeScale` is always 1 on Windows (Per-Monitor-V2 DPI already
  yields physical pixels).
- `exec` — direct argv or `%ComSpec% /C` for `shell`; a Job Object with
  `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` provides the timeout/cancel tree-kill
  so no orphaned `cmd /C` grandchild (e.g. `ping.exe`) survives; output bytes
  decoded from `GetConsoleOutputCP` (fallback `GetACP`) to UTF-8 with U+FFFD
  substitution, DBCS lead+trail decoded as one unit.
- `open` — `ShellExecuteExW` + `SEE_MASK_NOCLOSEPROCESS`; the launched PID and
  target basename are recorded on `*Backend` so `Windows`'s app-selector
  prefers that PID's window before the generic image-basename fallback
  (satisfies the `open[wait=]` handoff resolution).

Deviations from plan (both additive, no contract change):
- `Exec`'s wait race also selects on `ctx.Done()` alongside the timeout timer,
  restoring the ctx-cancel-reaps-child parity darwin gets from
  `CommandContext` (matters when the bridge cancels `runCtx` on a dropped
  connection); falls through the existing generic exit path, never reported as
  `TimedOut`.
- `decodeCodePage` does a strict whole-buffer `MB_ERR_INVALID_CHARS` decode
  first, then an ascending 1–4-byte window scan so DBCS pairs decode as one
  unit rather than shredding into two U+FFFD (the plan left the substitution
  strategy as an implementation-time detail).

Review: partitioned correctness/fit/test. Fit and test came back clean;
correctness returned three Important findings, all fixed in 46376b1 —
(1) `windows.NewCallback` was rebuilt per `Windows()`/`activeDisplays()` call,
leaking from the process-global never-freed trampoline pool and eventually
panicking a long-lived `--bridge` on the 100 ms poll loop (now built once via
`sync.Once` with mutex-guarded package state); (2) `GetDIBits` was called with
the DC the source bitmap was still selected into, an MSDN contract violation
(now passes `hdcScreen`); (3) `Focus`'s foreground-lock retry ran without
`runtime.LockOSThread`, so async preemption could migrate the goroutine and
silently defeat the bypass (now pinned). New windows-backend modification
guidelines recorded in `ai-docs/mental-model/windows-backend.md`.

Verification done here (macOS host, Go absent on the target box): native
`go build`/`go vet`/`gofmt -l`/`go test ./... -race` clean; `CGO_ENABLED=0
GOOS=windows GOARCH=amd64` build + vet clean (only the two pre-existing
documented clipboard `unsafe.Pointer` warnings); `go test -c
./internal/backend/windows/...` compiles; four-GOOS build matrix
(darwin/amd64, darwin/arm64, windows/amd64, linux/amd64) green. Pure-logic
units added: `filterWindows`/`selectorMatches`, capture rect resolution
(incl. `E_BOUNDS`/`E_NOWINDOW`), `buildExecArgv`, `cappedWriter` truncation,
CP949 decode with a U+FFFD byte + DBCS re-sync, and `SHELLEXECUTEINFOW` /
`BITMAPINFOHEADER` struct-size ABI checks.

Pending — GUI acceptance over the session bridge (deferred, needs the
operator to relaunch `gotto-hando --bridge` from this branch's build in the
console GUI session; the currently-resident bridge is a Phase-1 build whose
`win`/`cap`/`exec`/`open` are still stubs): `qwin` excludes cloaked UWP
windows; `cap[w]` at `scale=1` WxH == the DWM extended-frame bounds `qwin`
reports; `win[wait=5s]` succeeds on a window appearing ~2 s later and returns
`E_NOWINDOW` after ~5 s when it never appears; `open[wait=5s]notepad` +
`qwin[]app:notepad`; `open[wait=]` handoff via image-basename fallback;
`exec[shell]dir` output valid UTF-8 on the Korean-locale box;
`exec[timeout=1s]ping -n 10 localhost` killed with `E_TIMEOUT` and no
surviving `ping.exe` (confirm via `tasklist`); then QUICK START / EXAMPLES
against Notepad. `cap` PNGs are astra-vision-dogfood candidates.

Deferred follow-ups (not this phase): JPEG / `fmt=` / `cursor` capture
modifiers stay in `260907-feat-post-v1-extensions`. Minor record-only
findings left as-is: `PrintWindow` content offset by the drop-shadow margin
(under the v1 black-`PrintWindow`-is-ok simplification); `MB_ERR_INVALID_CHARS`
failing for a few exotic console code pages (949 is fine); the Job-Object
Start-then-assign race the plan itself pins as the sequence to use.
