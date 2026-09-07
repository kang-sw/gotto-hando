---
title: "macOS backend: input, windows, capture, permissions, exec/open"
parent: 260907-epic-gotto-hando-v1
related:
  260907-feat-cli-core: prerequisite
sage-review-design: completed
sage-review-design-reviewed: 43788a8194458500
---

# macOS backend: input, windows, capture, permissions, exec/open

## Background

Implements the `Backend` interface from `260907-feat-cli-core` for darwin so
that `gotto-hando local ...` works end to end on a Mac. Normative contract:
`assets/help.txt` sections COMMANDS (runtime semantics), EXECUTION (stages
3-6: connect, preflight, execute, finish), STATE MACHINE, ERROR POLICY,
CAVEATS, and all of `assets/help-macos.txt`.

## Decisions

- No cgo: every system API is reached through `purego` dlopen. Complete
  inventory, grouped by framework:
  - CoreGraphics: `CGEventCreateKeyboardEvent`, `CGEventCreateMouseEvent`,
    `CGEventCreateScrollWheelEvent`, `CGEventKeyboardSetUnicodeString`,
    `CGEventPost`, `CGEventSourceKeyState`, `CGEventSourceButtonState`,
    `CGGetActiveDisplayList`/`CGDisplayBounds`, `CGWindowListCopyWindowInfo`,
    `CGWindowListCreateImage`, `CGDisplayCreateImage`,
    `CGSessionCopyCurrentDictionary`/`CGSSessionScreenIsLocked`,
    `CGRequestScreenCaptureAccess`, `CGPreflightScreenCaptureAccess`.
  - HIToolbox: `IsSecureEventInputEnabled`.
  - ApplicationServices/AX: `AXIsProcessTrustedWithOptions`, `AXUIElement*`
    (raise/focus, `AXMinimized`, window wait).
  - ImageIO: PNG encoding of capture bitmaps.
  - Objective-C runtime via purego `objc` for AppKit: `NSPasteboard`,
    `NSWorkspace`, `NSRunningApplication`.
  Rejected: robotgo (cgo, breaks cross-compile).
- Coordinates are macOS points (logical); `cap` default `scale=1` writes an
  image whose pixels map 1:1 to input coordinates (Retina is downscaled);
  `scale=native` writes the backing-store pixels.
- Capture is PNG only in v1: `cap` has no `fmt=`, `q=` or `cursor` modifier
  (a `cap[fmt=jpg]` line is E_SYNTAX from the parser in
  `260907-feat-cli-core`; no runtime path exists here). The IR capture op
  carries the constant `"format":"png"`, the inline JSONL object
  `"fmt":"png"`, and the file suffix is `.png`. JPEG and cursor overlay stay
  in `260907-feat-post-v1-extensions`.
- Capture APIs: `CGDisplayCreateImage` / `CGWindowListCreateImage` are
  deprecated since macOS 14.4 in favour of ScreenCaptureKit. v1 uses them
  knowingly: the supported floor is macOS 13+, tested on macOS 15 and 26;
  ScreenCaptureKit stays post-v1 (`260907-feat-post-v1-extensions`).
- Window lists: `qwin` uses `kCGWindowListOptionAll` filtered to layer-0
  windows of apps with the regular activation policy; `min` = not on-screen
  with `AXMinimized` true, `hidden` = owning app `isHidden`. `win` searches
  the same list, so a minimized match is found and restored (help-macos.txt
  DISPLAYS AND COORDINATES states this rule).
- `win[wait=DUR]` is v1 and shares one polling loop with `open[wait=]`: poll
  the window list every 100 ms, E_NOWINDOW after DUR.
- Text typing uses `CGEventKeyboardSetUnicodeString` per character; `paste`
  sets NSPasteboard then sends primary+v; clipboard failure aborts without
  sending keys (E_CLIPBOARD).
- Key names `volup`, `voldown`, `mute`: darwin takes the documented escape
  hatch - preflight fails with E_INPUT (exit 4, "a name the platform cannot
  produce"), per help.txt EXECUTION step 4 and the one-line note in
  help-macos.txt.
- `scroll by=page` is emulated: one page = the current window's height
  (the display's height when no current window), sent in pixel units.
- `exec[shell]` runs `$SHELL -lc` (fallback `/bin/zsh -lc`); `open` uses
  `/usr/bin/open -a <name>` or `/usr/bin/open <path>`.
- Permissions are reported by `qinfo` as `perms=accessibility:ok|missing,screen:ok|missing`
  and requested by `--request-perms`; granting is a human action and stays
  documented as such in help-macos.txt. Over ssh the request is executed by
  the session bridge (`260908-feat-remote-ssh`), which is the process that
  holds the TCC grants.
- Permission and session gating is per command (help.txt EXECUTION step 4,
  help-macos.txt CHECK): preflight evaluates only what the run needs.
  - A run whose lines are only `qinfo`, `qdisp`, `qmouse`, `sleep`, `set`
    and comments never fails the session or permission checks; it reports
    them (`qinfo session=/perms=`). This keeps the documented recovery loop
    (`qinfo`, `--ping`) alive when session=inactive/locked or perms=missing.
  - Any other command requires an unlocked GUI session (own or bridge):
    E_SESSION otherwise.
  - Accessibility is required when the run contains any of `k kd ku txt m c
    md mu drag scroll paste win open[wait=]` (window raising and window-wait
    use AX): E_PERMISSION when absent.
  - Screen Recording is required only when the run contains `cap`. Without
    it `qwin`/`win` still run with empty titles (status ok); `qinfo` reports
    `screen:missing`.
  - `clip`, `qclip`, `exec`, `open` (without `wait=`) need no permission.
- A stable signing identity for rebuilds is scripted: `scripts/codesign-dev.sh`
  runs `codesign -s "gotto-hando-dev" --force` followed by `codesign -dv`,
  and a Makefile target runs it (help-macos.txt STABLE SIGNING IDENTITY
  points at the script).

## Constraints

- Preflight runs all five checks of help.txt EXECUTION step 4, subject to
  the per-command gating above; any failure is exit 4 and nothing runs:
  1. GUI session (own, or a running bridge for an ssh-started process),
     unlocked - E_SESSION;
  2. permissions the run needs - E_PERMISSION;
  3. no key or mouse button physically held down
     (`CGEventSourceKeyState` over every virtual key code plus
     `CGEventSourceButtonState`) - E_INPUT, never E_VALIDATE or E_SESSION;
  4. absolute and `disp=` coordinates inside the desktop - E_BOUNDS;
  5. every key name supported on this platform (`volup`/`voldown`/`mute`
     are not) - E_INPUT.
  Secure Input is not a preflight check: the affected keyboard-command line
  fails at run time with `err E_INPUT`.
- A preflight failure is reported as the `abort` object (help.txt JSONL):
  `{"event":"abort","code":"E_...","msg":"..."}` with no `done` object; in
  the plain format nothing goes to stdout and stderr gets
  `abort: <message> (<E_CODE>)`. The backend returns code and message; the
  CLI layer from `260907-feat-cli-core` prints it and maps the exit code
  (E_CONNECT 3, everything else 4).
- Held keys and buttons are released in reverse order on any failure and at
  the end of a run, with a `warn` line.
- Capture bytes are returned to the engine, never written by the backend;
  the CLI layer writes the file or, with `--inline-captures`, emits it as
  base64 in JSONL (this keeps the ssh path identical).
- Multi-display: origin at the main display's top-left, negative coordinates
  allowed, `disp=N` follows `qdisp` order.

## Spec Impact

None expected. `ai-docs/spec/help-macos.md` (one pointer anchor per
`== SECTION ==` of `assets/help-macos.txt`) is created by
`260907-feat-cli-core` together with `help.md`; this ticket adds an anchor
there only if it adds a new `== SECTION ==` to `assets/help-macos.txt`, and
never touches other anchors.

## Phases

### Phase 1: Keyboard, mouse, scroll, clipboard, text, queries, preflight

Goals: `k`, `kd`, `ku`, `txt`, `m`, `c`, `md`, `mu`, `drag`, `scroll`
(`by=page` with the display-height fallback), `clip`, `paste`, `qclip`,
`qmouse`, `qdisp`, `qinfo` (the complete line: os/osver/arch/ver/primary,
`desktop=`/`displays=` via `CGGetActiveDisplayList`, session, perms), `sleep`,
`set`, default and explicit delays, held-state tracking, session detection
(`CGSessionCopyCurrentDictionary`/`CGSSessionScreenIsLocked`), all five
preflight checks with the per-command gating rule and the `abort` result,
runtime bounds check for `r`/`%` (E_BOUNDS), Secure Input surfacing as a
run-time `E_INPUT` on the affected keyboard-command line. Key name table
mapped to virtual key codes per help.txt KEY NAMES, with
`volup`/`voldown`/`mute` rejected in preflight. `w`-frame commands and
E_BOUNDS for `w` are Phase 2 (they need window geometry).
Verification: `k`/`txt`/`m`/`c`/`drag`/`scroll`/`clip`/`paste` against
TextEdit plus `qinfo`/`qdisp`/`qmouse` output checked against System
Settings > Displays; unit tests for key-code mapping and held-state release
order using an injectable event sink; unit tests with injectable session,
permission and key-state probes that a physically held key yields `abort`
E_INPUT, that a `qinfo`-only run passes preflight with session=locked and
perms missing, and that a run containing `k` fails E_SESSION / E_PERMISSION
respectively; `go vet` and cross-compile for darwin/amd64 + darwin/arm64
from the dev machine.

### Phase 2: Windows, capture, permissions request, signing script

Depends on Phase 1. Goals: `win` (selector matching per WINDOW SELECTORS,
`wait=DUR` polling at 100 ms with E_NOWINDOW after DUR, unminimize/raise/
focus via AX, current-window state), `qwin` (window list rule from
Decisions, line format per OUTPUT), `cap` (full/`w`/`disp=`/`rect=`,
`scale`, `n`/`ms` frame series with absolute deadlines and slippage report,
`label`; PNG via ImageIO only), `w` frame coordinates and E_BOUNDS for `w`,
`scroll by=page` using the current window's height, Screen Recording gating
for `cap` only, `--request-perms` (local) that calls the two request APIs,
`scripts/codesign-dev.sh` and the Makefile target that runs it.
Verification: `cap` output dimensions/origin match `qdisp`/`qwin` values on
a Retina + external display setup; `win` on a minimized window restores and
focuses it; `win[wait=5s]` on a window that appears 2 s later succeeds and on
one that never appears returns E_NOWINDOW after about 5 s; with Screen
Recording missing, `qwin` and `win` run with empty titles and `cap` fails
preflight with `abort` E_PERMISSION; `--request-perms` on a fresh signing
identity triggers the system prompt; the Makefile target re-signs the binary
and `codesign -dv` shows `Authority=gotto-hando-dev`.

### Phase 3: exec and open

Depends on Phase 2. Goals: `exec` (argv split with `"..."` only, `shell`
login-shell path, `timeout` kill, `noerr`, stdout/stderr capture limited to
64 KiB each, exit code in the result line and JSONL), `open` (`wait=`
polling for a window of the app through the Phase 2 window list and the
shared 100 ms loop, E_NOWINDOW after the duration).
Verification: `exec[]true`, `exec[]false` (E_EXEC), `exec[noerr]false`,
`exec[shell]echo $PATH` under a LaunchAgent-started process showing the
login-shell PATH, `exec[timeout=1s]sleep 5` killed with E_TIMEOUT,
`open[wait=5s]TextEdit` followed by `qwin[]app:TextEdit`; then the manual
checklist run of the help.txt QUICK START and EXAMPLES sections against
TextEdit, now that every command they use is implemented.
