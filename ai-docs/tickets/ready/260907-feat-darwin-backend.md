---
title: "macOS backend: input, windows, capture, permissions, exec/open"
parent: 260907-epic-gotto-hando-v1
related:
  260907-feat-cli-core: prerequisite
sage-review-design: completed
sage-review-design-reviewed: 43788a8194458500
sage-review-completeness: completed
sage-review-completeness-reviewed: 89b668c89f2826db
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
- Capture encoding boundary: the backend returns raw pixels in
  `backend.Image` (never an encoded file); the engine/local side encodes PNG
  with Go's standard `image/png`. PNG encoding is platform-independent, so it
  stays out of the darwin backend and no ImageIO is used.
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
- Preflight coded-error contract: on failure `Backend.Preflight` returns a
  coded error (`backend.PreflightError` carrying an `output.ErrorCode` and a
  message, placed so it introduces no import cycle) rather than a bare
  `error`. The engine's `Run` calls `Preflight(ctx, seq)` before executing
  any op and, on that coded error, emits the `abort` object with its code and
  runs nothing; a non-coded error becomes `E_UNKNOWN` (exit 4). Defining the
  type and wiring the `Preflight` call + abort mapping into
  `internal/engine`'s `Run` is Phase 1 work - cli-core built `Run` against the
  dry-run backend and deliberately left EXECUTION stages 3-6 unwired.
- Local dispatch wiring: Phase 1 replaces the `dest == "local"` exit-2 stub in
  `cmd/gotto-hando/dispatch.go` with construction of the darwin backend
  (guarded by `runtime.GOOS == "darwin"`; other GOOS keep the exit-2 stub) and
  a call to `engine.Run`, streaming plain/`--jsonl` output, so
  `gotto-hando local ...` runs its Phase-1 command set for real.
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
  `abort: <message> (<E_CODE>)`. `Backend.Preflight` returns the code via
  `backend.PreflightError`; the engine emits the `abort`, and the CLI layer
  from `260907-feat-cli-core` prints it and maps the exit code (E_CONNECT 3,
  everything else 4).
- Held keys and buttons are released in reverse order on any failure and at
  the end of a run, with a `warn` line.
- Capture pixels are returned to the engine and encoded to PNG there (Go
  `image/png`), never encoded or written by the backend; the CLI layer writes
  the file or, with `--inline-captures`, emits it as base64 in JSONL (this
  keeps the ssh path identical).
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
`set`, default and explicit delays, session detection
(`CGSessionCopyCurrentDictionary`/`CGSSessionScreenIsLocked`), all five
preflight checks with the per-command gating rule and the `abort` result,
runtime bounds check for `r`/`%` (E_BOUNDS), Secure Input surfacing as a
run-time `E_INPUT` on the affected keyboard-command line. Key name table
mapped to virtual key codes per help.txt KEY NAMES, with
`volup`/`voldown`/`mute` rejected in preflight. `w`-frame commands and
E_BOUNDS for `w` are Phase 2 (they need window geometry). This phase also
defines the `backend.PreflightError` coded error, wires `engine.Run` to call
`Preflight` and map it to the `abort` object, and replaces the `local`
dispatch stub in `cmd/gotto-hando/dispatch.go` with the darwin backend behind
`runtime.GOOS == "darwin"`, so `gotto-hando local ...` runs its Phase-1
command set end to end through the real CLI.
Verification splits into two subsets. Lock-independent (runs on a locked or
headless session, so it is done first): unit tests for key-code mapping and
held-release ordering via the engine's held tracking using an injectable
event sink; unit tests with injectable session, permission and key-state
probes that a physically held key yields `abort` E_INPUT, that a `qinfo`-only
run passes preflight with session=locked and perms missing, and that a run
containing `k` fails E_SESSION / E_PERMISSION respectively; end to end through
the real CLI, `gotto-hando local qinfo`/`qdisp`/`qmouse` print live values and
`gotto-hando local 'k[]a'` on a locked session aborts `E_SESSION`; `go vet`
and cross-compile for darwin/amd64 + darwin/arm64 from the dev machine.
Interactive-injection (requires an unlocked GUI session with Accessibility
granted): `k`/`txt`/`m`/`c`/`drag`/`scroll`/`clip`/`paste` against TextEdit,
plus `qinfo`/`qdisp`/`qmouse` output checked against System Settings >
Displays.

### Result (787b2f2) - 2026-09-08

`internal/backend/darwin` (purego, `CGO_ENABLED=0`) implements the Phase 1
command set: `k kd ku txt m c md mu drag scroll clip paste qclip qmouse
qdisp qinfo sleep set`, session/lock detection, the five-check preflight
gate in the Constraints order, `r`/`%`/`disp=` bounds handling, Secure Input
as a run-time `E_INPUT`, and the KEY NAMES keycode table (`volup`/`voldown`/
`mute` rejected). `backend.PreflightError` carries an `output.ErrorCode`;
`engine.Run` calls `Preflight` first and maps the coded error to the `abort`
object (non-coded -> `E_UNKNOWN`, exit 4). `cmd/gotto-hando` dispatches
`local` through the darwin backend behind `runtime.GOOS=="darwin"` via a
`dispatch_darwin.go`/`dispatch_other.go` factory pair; other GOOS keep the
exit-2 stub.

Verification (lock-independent subset - the dev Mac was screen-locked and
remote): `go test ./... -race` green, `go vet ./...` clean, cross-compile
`darwin/amd64`+`darwin/arm64` clean, and CLI end-to-end `local qinfo`/`qdisp`/
`qmouse` printed live values (`session=locked`) with `local 'k[]a'` aborting
`E_SESSION` (exit 4).

Review: one Critical (disp=N ignored the display origin in both `resolve()`
and preflight check 4) fixed and cleared by a Critical-scoped re-review;
three Important (horizontal-scroll arm64 C-variadic ABI, `scroll by=page`
pixel units, `query.go` formatting coverage) fixed; two Minor fixed
(`ClipboardSet` now surfaces `setString:forType:` failure so the `E_CLIPBOARD`
paste path is reachable; `go mod tidy`), two Minor accepted as-is
(`displayScale`'s unused Phase-2 param, `UniCharCount` width).

Deviations: the `go` directive moved 1.23 -> 1.25.0 (forced by
`github.com/ebitengine/purego`); `backend.Info` gained `DisplayList
[]DisplayGeom` (needed for `qdisp` lines and `disp=` bounds); horizontal
scroll required an arm64-specific register-exhausting `...any` binding (a
plain `...any` binding was empirically proven insufficient - purego still
routes the value to a register), covered by a real-ABI regression test.
`scroll by=page` reconciliation: the ticket's pixel-units Decision is
authoritative; CONCEPT.md ch.8's line-unit statement describes the `by=line`
default. Phase 1 emits the display-height pixel page; the current-window
height variant stays Phase 2.

Deferred to an unlocked GUI session with Accessibility granted (not run this
phase): the interactive-injection subset (`k`/`txt`/`m`/`c`/`drag`/`scroll`/
`clip`/`paste` against TextEdit; `qinfo`/`qdisp`/`qmouse` cross-checked
against System Settings > Displays). Real multi-display `disp=` cursor
placement and the horizontal-scroll live effect are covered only by unit and
real-ABI tests until then.

### Phase 2: Windows, capture, permissions request, signing script

Depends on Phase 1. Goals: `win` (selector matching per WINDOW SELECTORS,
`wait=DUR` polling at 100 ms with E_NOWINDOW after DUR, unminimize/raise/
focus via AX, current-window state), `qwin` (window list rule from
Decisions, line format per OUTPUT), `cap` (full/`w`/`disp=`/`rect=`,
`scale`, `n`/`ms` frame series with absolute deadlines and slippage report,
`label`; the backend returns raw pixels and the engine encodes PNG via Go
`image/png`), `w` frame coordinates and E_BOUNDS for `w`,
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

### Result (a5e3dba) - 2026-09-08

Phase 2 implements `win`, `qwin`, `cap`, `w`-frame bounds, `scroll by=page`
at the current window's height, the Screen-Recording-gated `cap` preflight
check, `--request-perms` (darwin), and the dev codesigning script/Makefile.
Window enumeration (`internal/backend/darwin/windows.go`) uses
`CGWindowListCopyWindowInfo` filtered to layer-0 regular-activation-policy
windows, correlated per-pid to the Accessibility API
(`AXUIElementCreateApplication` + `kAXWindows`/`kAXMinimized`/`kAXPosition`/
`kAXSize`) by title+position for the `min` flag and for raise/unminimize/
focus; `Focus` sets `kAXMinimized=false`, performs `kAXRaise`, and activates
the app frontmost. Capture (`capture.go`) grabs raw pixels via
`CGDisplayCreateImage` (desktop/`disp=`/`rect=`) and `CGWindowListCreateImage`
with `kCGWindowImageBoundsIgnoreFraming` (`cap[w]`, tight to window bounds),
converts BGRA->RGBA in a pure `bgraToRGBA` helper, and returns
`backend.Image` raw pixels only. Screen Recording is a cap-only 6th preflight
check layered onto Phase 1's five (via the Phase-1 `permissionProbe.
ScreenRecording()` seam), `E_PERMISSION` on failure; `qwin`/`win` still run
with empty titles when it is missing.

Shared engine infra landed here is GOOS-agnostic so the later windows Phase 2
reuses it without a fork: `internal/engine/schedule.go` (clock seam,
`pollForWindow` 100ms->`E_NOWINDOW`, `runFrameSeries` absolute-deadline burst
+ slippage), `internal/engine/capture.go` (`encodePNG` via `image/png`,
capture-path building, file / `--inline-captures` base64 write, cap-on-error),
`internal/engine/compose.go` (`w`-frame bounds, `pageHeight`, `currentWindow`
via `Windows()` Focused==true), and `internal/engine/query.go`
(`formatQueryWindows`/`queryWindowsJSON`/`focusJSON`). Per the ticket
Decision the backend returns raw pixels and never encodes/writes; PNG
encoding + path/write live in the engine, `--inline-captures`/`OutDir` thread
through `engine.RunOptions` from the CLI. One authorized `Backend` interface
change: `Scroll` gained `pageHeightPixels int` (resolved once in
`engine.pageHeight()`: focused-window height, else primary display, else a
900px fallback), updated in lockstep across darwin, the windows stub
(accepted-unused; Windows scrolls in wheel notches), and dryrun.

Deliberate pattern note (fit review): the plan's step-14 `windowProbe`
test-seam was not added; instead the FFI-free logic (`filterWindows`/
`selectorMatches` in `windows.go`, `captureRect`/`scaledDims`/`bgraToRGBA` in
`capture.go`) was extracted into pure functions and unit-tested directly,
which is a lighter seam than mocking `CGWindowListCopyWindowInfo`'s
CFDictionary-array shape - the same "the function is the seam" reasoning the
plan itself accepted for the capture-permission gate. A future windows Phase
2 mirrors this pure-function split.

Verification (safe subset, run this phase; interactive subset deferred to the
HOME-VERIFICATION CHECKLIST below): `go build ./...`, `go vet ./...`, and
`go test ./... -race` all green natively on the Mac; new pure-logic unit
tests cover selector matching (id/pid/app/title-substring/regex, z-order),
qwin format + `win`/`E_NOWINDOW`, `cap` rect/`w`/`disp=` geometry + `E_BOUNDS`,
frame-series absolute-deadline no-drift + slippage (fake clock),
`pollForWindow` cadence/timeout (fake clock), `bgraToRGBA` color-order/stride
(synthetic BGRA incl. a premultiplied pixel), the PNG encode->decode round
trip, `doCapture` orchestration (file written, JSONL path/data+fmt/frames,
`E_NOWINDOW`/`E_BOUNDS`/`E_CAPTURE` mapping), the `resolve()` window-frame +
`currentWindow` fallback + `pageHeight` threading, and the Screen-Recording
preflight gate (cap-only). `local qdisp` and `local --request-perms` smoke
run without crashing (this dev Mac is headless/screen-locked, so
`CGGetActiveDisplayList` returns none and perms read missing - the
documented environment, matching Phase 1). The codesign script + `make
dev-sign` build and run, but signing fails with `gotto-hando-dev: no identity
found` (no such self-signed identity in this Keychain) - an environment
prerequisite (help-macos.txt STABLE SIGNING IDENTITY step 1), not a code
defect.

Review: no Critical. One correctness Important (`cap[w]` included drop-shadow
framing so its dims/origin/nativeScale did not match `qwin`) fixed with
`kCGWindowImageBoundsIgnoreFraming`; four test-coverage Important (BGRA->RGBA
swap, `doCapture` orchestration, `resolve()` window/`pageHeight`, the Screen-
Recording gate - all new GOOS-agnostic logic testable headless) closed with
17 new tests; one Minor fixed (`cap[w]` with no window now maps to
`E_NOWINDOW`, consistent with `c[w]`/`m[w]`, via a new `backend.ErrNoWindow`
sentinel). Minors left as record-only: forced-opaque premultiply darkening on
rounded window corners (v1-acceptable), inline-burst frame0 header/continuation
duplication (matches the qwin/exec convention), `-%02d` suffix width for
n>=100 (cosmetic), `--request-perms` test's {0,4} tolerance (non-deterministic
env).

Deviations: `internal/backend/darwin/ffi.go` reformatted by `gofmt` (drift
from an earlier edit); `.gitignore` gained `/gotto-hando` (the new Makefile
emits it); `internal/syntax/build.go` `buildCap` now wires the explicit-path
payload (`cap[]./shot.png`) into `op.FilePath`, which `doCapture` already
honored (a latent gap surfaced this phase).

HOME-VERIFICATION CHECKLIST (run once, in an unlocked GUI session with Screen
Recording + Accessibility granted; a self-signed `gotto-hando-dev` identity in
Keychain for the signing step):
- `gotto-hando local 'cap'` / `cap[w]` / `cap[disp=1]` / `cap[rect=...]`:
  output dimensions/origin match `qdisp`/`qwin` on a Retina + external display
  (verifies the `kCGWindowImageBoundsIgnoreFraming` tightening and nativeScale).
- `gotto-hando local 'win[]<minimized-app-title>'`: restores (unminimize) +
  raises + activates it frontmost.
- `gotto-hando local 'win[wait=5s]<title>'` on a window appearing ~2s later
  succeeds; on one that never appears returns `E_NOWINDOW` after ~5s.
- Revoke Screen Recording: `qwin`/`win` run with empty titles (status ok);
  `cap` fails preflight with `abort` `E_PERMISSION`; `qinfo` shows
  `screen:missing`.
- `gotto-hando local --request-perms` on a fresh identity triggers the
  Accessibility + Screen Recording system prompts; check the printed `perms=`
  line and exit code before and after granting.
- Two windows of the same app with identical titles: confirm the AX/CG
  title+position correlation reports `matched=2` (degrades gracefully, no
  silent misbehavior) and `qwin`'s `min`/`hidden` flags stay correct.
- `scroll[by=page]` inside a focused window scrolls one window-height page
  (not the display height).
- `scripts/codesign-dev.sh` (or `make dev-sign`): `codesign -dv` shows
  `Authority=gotto-hando-dev`.

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
