---
title: "Windows backend: input, windows, capture, session checks, exec/open"
parent: 260907-epic-gotto-hando-v1
related:
  260907-feat-cli-core: prerequisite
  260907-feat-darwin-backend: reference implementation of the same interface
sage-review-design: completed
sage-review-design-reviewed: 55bf13435baadcbb
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
     or E_SESSION (help-windows.txt CHECK).
  4. absolute and `disp=` coordinates inside the virtual desktop: E_BOUNDS.
  5. every key name supported on this platform: E_INPUT (always passes on
     Windows).
- Session gating is per command: a run whose lines are only `qinfo`,
  `qdisp`, `qmouse`, `sleep`, `set` and comments never fails the session
  check and reports the state instead (`qinfo session=inactive|locked`),
  which keeps the recovery loop (`qinfo`, `--ping`) alive; every other
  command requires an unlocked interactive session (own or bridge).
- A preflight failure is reported as the `abort` object (help.txt JSONL):
  `{"event":"abort","code":"E_...","msg":"..."}` with no `done` object; in
  the plain format nothing goes to stdout and stderr gets
  `abort: <message> (<E_CODE>)`. The backend returns code and message; the
  CLI layer from `260907-feat-cli-core` prints it and maps the exit code
  (E_CONNECT 3, everything else 4).
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
  `perms=n/a elevated=0|1`.

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
Verification: keyboard/mouse/clipboard commands against Notepad plus
`qinfo`/`qmouse`; `qinfo desktop=`/`displays=` match a two-monitor layout
with different scaling percentages; unit tests for scan-code mapping
(extended keys, numpad) with an injectable `SendInput`; unit tests with
injectable `GetAsyncKeyState` and session probes that a held key yields
`abort` E_INPUT, that a `qinfo`-only run passes preflight in a locked or
non-console session, and that a run containing `k` fails E_SESSION there;
cross-compile windows/amd64 from macOS and run on a Windows machine.

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
