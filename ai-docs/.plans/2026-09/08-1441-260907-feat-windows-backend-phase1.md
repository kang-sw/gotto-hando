# Plan: 260907-feat-windows-backend — Phase 1

## Relevant Ticket Contract

- Phase 1 goal command set (`ai-docs/tickets/ready/260907-feat-windows-backend.md#L135-154`):
  all KEYBOARD/MOUSE/CLIPBOARD commands (`k kd ku txt m c md mu drag scroll
  clip paste qclip`), `qmouse`, `qinfo` (full line incl. `desktop=`/
  `displays=` from `EnumDisplayMonitors` and `elevated=`), delays, held
  state, all five preflight checks with per-command gating, the `abort`
  result (held key = `E_INPUT`), the DPI manifest `.syso` + `init`
  fallback, key-name→scan-code table, and the `local` dispatch wiring
  (`dispatch_windows.go` + narrowing `dispatch_other.go`).
- No-cgo: `golang.org/x/sys/windows` + `LazyDLL`/`LazyProc` bindings for
  `SendInput`, `GetAsyncKeyState`, `EnumDisplayMonitors`,
  `SetProcessDpiAwarenessContext`, `OpenClipboard` family,
  `WTSGetActiveConsoleSessionId`/session-lock detection,
  `GetConsoleOutputCP`, `ShellExecuteExW` (ticket Decisions `#L26-33`; the
  capture/window/exec-only APIs — `EnumWindows`/`DwmGetWindowAttribute`,
  `BitBlt`/`PrintWindow`, `ShellExecuteExW`'s `open` behavior — are Phase
  2/3, listed here only because Decisions inventories the whole ticket at
  once).
- Coordinates are physical pixels via Per-Monitor-V2 DPI awareness: an
  application manifest embedded as `cmd/gotto-hando/rsrc_windows_amd64.syso`
  (generated, checked in) PLUS a runtime `SetProcessDpiAwarenessContext`
  call in `init`, whose failure is ignored when the manifest already
  applied (ticket Decisions `#L34-39`).
- Keyboard uses scan codes with the extended-key flag per help-windows.txt
  CAVEATS; `txt` uses `KEYEVENTF_UNICODE`. Every documented KEY NAMES
  symbol is supported on Windows (`volup`/`voldown`/`mute` included via
  `SendInput`, unlike darwin) — preflight check 5 never fails
  (`#L40-43`).
- Preflight: five checks, literal order, short-circuit, per-command gating
  (ticket Decisions `#L44-73`):
  1. GUI session unlocked (own or bridge; bridge detection/forwarding is
     `260908-feat-remote-ssh` — this ticket only provides local
     active/locked/inactive detection) — every command except `qinfo qdisp
     qmouse sleep set` (and comments) — `E_SESSION`.
  2. Permissions: `perms=n/a`, never fails.
  3. No key/mouse button physically held (`GetAsyncKeyState` over every
     virtual key) — `E_INPUT`, **input-injection gated**: runs only when
     the sequence contains an input-injecting command, so a query-only run
     is exempt (matches darwin, keeps the `qinfo`/`--ping` recovery loop
     alive with a stuck key). This gating is explicit policy, not a
     permission-check side effect (Windows needs no permission).
  4. Absolute/`disp=` coordinates inside the virtual desktop — `E_BOUNDS`.
  5. Every key name supported — always passes on Windows.
- A preflight failure returns `*backend.PreflightError` only; `engine.Run`
  already calls `Preflight` and maps it to the `abort` object — **no
  engine change needed** (ticket Decisions `#L74-84`, confirmed against
  `internal/engine/run.go:71-81`, landed by `260907-feat-darwin-backend`).
- Local dispatch wiring (ticket Decisions `#L85-93`): new
  `cmd/gotto-hando/dispatch_windows.go` (`//go:build windows`) with
  `newLocalBackend()` behind `runtime.GOOS=="windows"`; narrow
  `cmd/gotto-hando/dispatch_other.go` from `//go:build !darwin` to
  `//go:build !darwin && !windows`.
- `elevated=0|1` has no dedicated `backend.Info` field: packed into the
  existing free-form `Info.Perms` string (ticket Decisions `#L109-113`),
  so `qinfo` prints `perms=n/a elevated=0|1` from that one field with zero
  `internal/engine/query.go` change (its `formatQueryInfo` already does
  `perms=%s` verbatim).
- KEY NAMES contract (`assets/help.txt:448-458`): `a-z 0-9 f1-f24 ctrl
  shift alt meta primary enter tab esc space backspace delete insert home
  end pageup pagedown up down left right period comma minus equal slash
  backslash semicolon quote grave lbracket rbracket numpad0-numpad9
  decimal numadd numsub nummul numdiv numenter capslock printscreen
  scrolllock pause volup voldown mute` — the windows scan-code table must
  cover ALL of these (no documented omissions, unlike darwin's keys.go).
- `qinfo`/`qdisp`/`qmouse` field/line formats: `assets/help.txt:431-447`,
  reference example `assets/help-windows.txt` CHECK: `os=windows
  osver=10.0.22631 arch=amd64 ver=0.1.0 primary=ctrl desktop=0,0 2560x1440
  displays=1 session=active perms=n/a elevated=0`.
- Session states (`assets/help-windows.txt` CHECK, WHEN YOU NEED THIS):
  `active` (own console session, input desktop "Default"), `bridge`
  (reserved for `260908-feat-remote-ssh`, not wired here — mirrors
  darwin's `session.go` "bridge" branch), `locked` (input desktop
  "Winlogon"), `inactive` (not the console session, no bridge).

## Out of Scope

- Phase 2: `win`, `qwin`, `qdisp`'s window-adjacent pieces beyond
  `DisplayList`, `cap`, `exec`, `open` real behavior (`Backend` methods
  for these exist only as compile-satisfying stubs, mirroring darwin's
  `stubs.go`).
- `260908-feat-remote-ssh`: `--bridge`, session-bridge forwarding, named
  pipe protocol; the `bridge` session state stays a reserved-but-unwired
  branch.
- `--request-perms` real behavior (exits 2 locally; already handled by
  existing `cmd/gotto-hando/dispatch.go:99-101`, unchanged).
- `--timeout` deadline enforcement (stays inert, `context.Background()`
  passed to `engine.Run`, same as darwin Phase 1).
- PNG encoding, capture paths, `--out` directory creation.

## Codebase Findings

- `internal/backend/backend.go:135-160` — the `Backend` interface Phase 1
  must fully satisfy, including `Windows/Focus/Capture/Exec/Open` as
  Phase-2/3 stubs (mirror `internal/backend/darwin/stubs.go:1-40`
  verbatim in shape).
- `internal/backend/preflight_error.go:1-16` — `PreflightError` already
  landed; windows Preflight only constructs/returns it, same as darwin's
  `preflight.go`.
- `internal/engine/run.go:71-81` — `Run` already calls `be.Preflight` and
  maps `*backend.PreflightError`/plain error to the abort `Summary`; zero
  engine change required for Phase 1, confirming the ticket's own claim.
- `internal/engine/query.go:10-20` — `formatQueryInfo`'s `perms=%s` is
  format-agnostic; windows must produce the full `"n/a elevated=0"` /
  `"n/a elevated=1"` string itself in `Info.Perms`. Same for `Info.Primary
  = "ctrl"` (vs. darwin's `"cmd"`).
- `internal/backend/darwin/probes.go:1-107` — the probe-seam pattern to
  mirror: `sessionProbe`/`keyStateProbe`/`displayProbe` interfaces, one
  field per probe on `Backend`, defaulted to `real*Probe` in `New()`,
  directly settable from same-package `_test.go` files (no exported
  override API). Windows drops darwin's `permissionProbe` (perms=n/a,
  nothing to probe) and `secureInputProbe` (no Windows equivalent — UIPI
  silently drops input to elevated windows instead of a queryable
  session-wide flag, so it is a documented CAVEAT, not a probe).
- `internal/backend/darwin/preflight.go:1-156` and
  `preflight_test.go:1-200` — the exact structure and test pattern to
  mirror: `exemptFromSession` map (identical kind set works for windows:
  `KindQueryInfo, KindQueryDisp, KindQueryMouse, KindSleep, KindSet`), a
  second gating map for check-3 (windows: rename `accessibilityGated` to
  something like `injectsInputGated` since there is no permission
  involved — same kind set as darwin's, restricted to what Phase 1 wires:
  `KindKey, KindKeyDown, KindKeyUp, KindText, KindMove, KindClick,
  KindButtonDown, KindButtonUp, KindDrag, KindScroll, KindPaste`; `KindFocus`
  and `KindOpen[wait=]` stay in the set for forward-compat even though
  their `Backend` methods are Phase-2 stubs — matches the ticket's
  "gate it on the same run-injects-input condition" instruction), the
  `collectPreflightPoints`/`pointInRect`/display-relative-`disp=` bounds
  logic (reusable as-is, just backed by `EnumDisplayMonitors` geometry
  instead of `CGGetActiveDisplayList`), and the fake-probe test seam
  (`newTestBackend`, `preflightCode` helper).
- `internal/backend/darwin/keys.go:1-109` and `keys_test.go:1-105` — the
  keycode-table pattern to mirror, but INVERTED coverage: darwin
  documents omissions (`unsupportedOnDarwin`); windows must cover every
  canonical symbol with NO omissions (per ticket Decision "every
  documented key name is supported on Windows"), so the windows
  `keycodeFor`-equivalent test should assert `ok == true` for every
  symbol in the canonical KEY NAMES list, the mirror image of darwin's
  `TestKeycodeForCoversEverySupportedCanonicalName`.
- `internal/backend/darwin/keyboard.go:13-23,55-96` — darwin needs a
  `heldFlags` field (CGEventFlags bit-OR) applied to every posted event
  because CGEvent's modifier state travels on each event. Windows'
  `SendInput` has no equivalent shared-flags field — modifier state is
  entirely determined by the actual key-down/up events already sent by
  `kd`/`ku`/chord composition in the engine, so `heldFlags` has NO windows
  analog; omit it (simplification vs. darwin, not a gap).
- `internal/backend/darwin/mouse.go:37-53,116-184` — darwin's
  `clickState`/`lastButtonUp`/`doubleClickWindow` manual click-burst
  tracking (`CGMouseEventClickState` field) has no Windows equivalent:
  Windows detects double-clicks itself from `SendInput`-posted event
  timestamps against `GetDoubleClickTime()`, so `ButtonDown`/`ButtonUp`
  need no click-state bookkeeping (another simplification vs. darwin).
- `internal/backend/darwin/mouse.go:186-260`,
  `scrollUnitsAndAmount`/`pageHeightPixels` — darwin's `by=page` uses a
  real pixel-unit scroll event (`kCGScrollEventUnitPixel`). **Risk
  signal**: `SendInput`'s `MOUSEEVENTF_WHEEL`/`HWHEEL` (the API
  `CONCEPT.md:150` and the ticket both name for Windows scroll) has no
  pixel-unit or page-unit variant — a Windows wheel event is always a
  signed multiple of `WHEEL_DELTA` (120), interpreted by the receiving
  app/OS using the user's own "lines to scroll" setting; there is no
  wire-level way to say "scroll one page" distinct from "scroll N lines."
  The ticket's Decisions section (unlike the darwin ticket) does not
  reconcile this. Recommended default (low risk, reversible): send
  `ticks * WHEEL_DELTA` for `by=line` (matches darwin's line semantics
  1:1) and the SAME `ticks * WHEEL_DELTA` for `by=page` (Windows cannot
  natively distinguish them at the injection layer), documented as a new
  CAVEATS bullet in `assets/help-windows.txt` (e.g. "scroll[by=page] sends
  the same wheel notches as by=line — Windows has no pixel/page-unit wheel
  injection API, only WHEEL_DELTA notches"). This is a doc-only spec touch
  (no new `== SECTION ==`, so no new spec anchor per the ticket's Spec
  Impact clause) and does not block the rest of Phase 1.
- `internal/syntax/keys.go` — canonical symbol table Phase 1's scan-code
  table must independently enumerate the full set (same cross-reference
  discipline as darwin's `keys.go` header comment).
- `cmd/gotto-hando/dispatch_darwin.go:1-16` and `dispatch_other.go:1-18` —
  the exact factory-function pattern and build-tag values to mirror;
  `dispatch_other.go`'s current `//go:build !darwin` (line 1) is the one
  line to change to `//go:build !darwin && !windows`.
- `assets/assets.go:1-27` — `HelpWindows` is already embedded from
  `assets/help-windows.txt`; no assets.go change needed unless CAVEATS
  gets the scroll-page bullet above (still no new `== SECTION ==`).
- `go.mod` — `golang.org/x/sys` is not yet a dependency (only
  `github.com/ebitengine/purego`); confirmed reachable via the module
  proxy (`go list -m -versions golang.org/x/sys` resolved a full version
  list), so `go get golang.org/x/sys/windows` is safe to run from this
  Mac.
- `.syso` generation — no cgo, no existing precedent in this repo (darwin
  needed none). `github.com/akavel/rsrc` (confirmed on the module proxy,
  versions through v0.10.2) generates a `.syso` from a manifest XML alone
  (`-manifest app.manifest -o rsrc_windows_amd64.syso -arch amd64`) via
  `go run github.com/akavel/rsrc@v0.10.2 ...` — a build-time-only
  invocation that does not add a `go.mod` requirement (unlike
  `go get`), matching "generated, checked in." Risk is bounded: the
  ticket's own Decision says the runtime `SetProcessDpiAwarenessContext`
  fallback call's failure "is ignored when the manifest already applied,"
  i.e. correctness does not depend on the manifest being perfect — the
  `init`-time API call alone is sufficient for Per-Monitor-V2 awareness on
  Windows 10 1703+ even if the manifest step needs iteration.

## Implementation Plan

1. `go get golang.org/x/sys/windows` (pin an exact version) to add the
   dependency; `windows.NewLazySystemDLL`/`NewProc` supply the
   `LazyDLL`/`LazyProc` binding surface the ticket specifies, plus ready
   made Windows types (`windows.HANDLE`, `windows.POINT`, `windows.RECT`,
   etc.) so `internal/backend/windows/ffi.go` does not need to redeclare
   them.
2. `internal/backend/windows/` (new package, every file `//go:build
   windows`), mirroring `internal/backend/darwin/`'s file split:
   - `ffi.go` — centralizes `NewLazySystemDLL` handles (`user32.dll`,
     `kernel32.dll`, `advapi32.dll`, `shcore.dll`, `ntdll.dll`) and
     `NewProc` bindings for: `SendInput`, `GetAsyncKeyState`,
     `EnumDisplayMonitors`, `GetMonitorInfoW`, `GetDpiForMonitor`
     (shcore, for `qdisp` `scale=`), `SetProcessDpiAwarenessContext`,
     `GetCursorPos`, `OpenClipboard`/`EmptyClipboard`/`GetClipboardData`/
     `SetClipboardData`/`CloseClipboard`/`GlobalAlloc`/`GlobalLock`/
     `GlobalUnlock`, `WTSGetActiveConsoleSessionId`, `ProcessIdToSessionId`,
     `GetProcessWindowStation`, `OpenInputDesktop`,
     `GetUserObjectInformationW`, `OpenProcessToken`/`GetTokenInformation`
     (elevation), `RtlGetVersion` (ntdll, real build number regardless of
     compatibility-shim manifest lies), `GetConsoleOutputCP` (kept even
     though `exec` is Phase 2, since it is cheap to bind alongside the
     other kernel32 procs — optional, skip if it adds friction).
   - `keys.go` — canonical KEY NAMES symbol → `(scanCode uint16, extended
     bool)` table (PS/2 Set-1 scan codes per the Microsoft Keyboard Scan
     Code Specification / the extended-key list in
     `assets/help-windows.txt` CAVEATS), covering every symbol with no
     omissions (unlike darwin). `primary` resolves to VK equivalent of
     Ctrl directly (help.txt: Windows `primary=ctrl`).
   - `probes.go` — `sessionProbe{State() string}`,
     `keyStateProbe{AnyPhysicallyHeld([]uint8) bool}` (single method: a
     `GetAsyncKeyState` scan naturally covers mouse buttons in the same VK
     space, unlike darwin's separate key/button probes),
     `displayProbe{Active() []backend.DisplayGeom}`; real implementations
     plus one interface-typed field each on `Backend`, defaulted in
     `New()`, same-package-test-overridable — identical seam shape to
     darwin's `probes.go`.
   - `session.go` — `sessionState()`: compare this process's session id
     (`ProcessIdToSessionId`) against `WTSGetActiveConsoleSessionId()`;
     when equal, `OpenInputDesktop`/`GetUserObjectInformationW(UOI_NAME)`
     to distinguish `"Default"` (active) from `"Winlogon"` (locked); when
     not equal (or `WinSta0`/`OpenInputDesktop` inaccessible), `inactive`
     (service/session 0/disconnected RDP/ssh — no bridge probe wired yet,
     mirrors darwin's reserved `"bridge"` branch).
   - `backend.go` — `Backend` struct + `New()` (no dlopen-time failure
     mode expected, unlike darwin's framework dlopens — `NewLazySystemDLL`
     is lazy and only errors on first `Call()`); `Info(ctx)`: `OS =
     "windows"`, `OSVer` via `RtlGetVersion`, `Arch = runtime.GOARCH`,
     `Ver = assets.Version()`, `Primary = "ctrl"`, `DisplayList` from
     `EnumDisplayMonitors` callback + `GetMonitorInfoW` + `GetDpiForMonitor`
     (scale = dpiX/96.0), desktop union bounds via a small windows-local
     `unionBounds` helper (duplicate darwin's ~15-line pure function —
     no shared-package extraction needed, matches "surgical changes"),
     `Session` via the probe, `Perms = fmt.Sprintf("n/a elevated=%d",
     elevatedBit)` (elevation via `OpenProcessToken`+
     `GetTokenInformation(TokenElevation)`, read directly in `Info`, no
     probe seam needed since it is not preflight-gated). `MousePos` via
     `GetCursorPos`.
   - `preflight.go` — five checks in order, short-circuiting: (1) session
     gate using `exemptFromSession` (identical kind set to darwin); (2)
     always passes; (3) `needsInputInjection` gate (see Codebase Findings)
     — `b.keys.AnyPhysicallyHeld(allVKCodes)` → `E_INPUT`; (4) bounds via
     `collectPreflightPoints`/`pointInRect`, reusing darwin's exact
     display-relative `disp=` logic against `b.displays.Active()`; (5)
     `keycodeFor` lookup over `collectKeySymbols(seq)` — always passes
     given full table coverage, but keep the loop for defensive parity
     (a future KEY NAMES addition without a matching table entry should
     still surface `E_INPUT`, not panic or silently pass).
   - `keyboard.go` — `KeyDown`/`KeyUp` via one `SendInput` call each
     (`INPUT_KEYBOARD`, `KEYEVENTF_SCANCODE | (extended flag) |
     (KEYEVENTF_KEYUP for up)`); `TypeText` per-rune `KEYEVENTF_UNICODE`
     (surrogate pairs as two consecutive `SendInput` calls per
     help-windows.txt CAVEATS "txt sends UTF-16 units"), `\n`/`\t` press
     named `enter`/`tab` via the scan-code path (same pattern as darwin's
     `pressNamed`). No `heldFlags` field (see Codebase Findings).
   - `mouse.go` — `MouseMove` via `SendInput`
     `MOUSEEVENTF_MOVE|MOUSEEVENTF_ABSOLUTE|MOUSEEVENTF_VIRTUALDESK`
     (coordinates normalized to 0..65535 against
     `SM_XVIRTUALSCREEN`/`SM_YVIRTUALSCREEN`/`SM_CXVIRTUALSCREEN`/
     `SM_CYVIRTUALSCREEN`), interpolated the same way as darwin's
     `MouseMove` (step loop over `dur`/`moveInterval`); `ButtonDown`/
     `ButtonUp` via `MOUSEEVENTF_{L,R,M}{DOWN,UP}` (no click-state
     bookkeeping, see Codebase Findings); `Scroll` via
     `MOUSEEVENTF_WHEEL`/`HWHEEL` with `mouseData = ticks * WHEEL_DELTA`
     for both `by=line` and `by=page` per the recommended default above
     (flag the `assets/help-windows.txt` CAVEATS addition as a follow-up
     doc edit in the same change).
   - `clipboard.go` — `ClipboardSet`/`ClipboardGet` via
     `OpenClipboard(0)`/`EmptyClipboard`/`GlobalAlloc(GMEM_MOVEABLE,...)`/
     `GlobalLock`/UTF-16 copy/`GlobalUnlock`/`SetClipboardData(CF_UNICODETEXT,...)`
     / `CloseClipboard`, and the read-back mirror via `GetClipboardData`.
   - `stubs.go` — `Windows/Focus/Capture/Exec/Open` return a Phase-1
     "not implemented" error, verbatim structure of darwin's `stubs.go`.
3. `cmd/gotto-hando/rsrc_windows_amd64.syso` (generated, checked in):
   author `cmd/gotto-hando/windows.manifest` (Per-Monitor-V2 DPI
   awareness `<dpiAwareness>PerMonitorV2</dpiAwareness>` /
   `<dpiAwarenessContext>` per-monitor-v2 element, plus
   `<activeCodePage>UTF-8</activeCodePage>` per ticket Decisions), then
   `go run github.com/akavel/rsrc@v0.10.2 -manifest
   cmd/gotto-hando/windows.manifest -o
   cmd/gotto-hando/rsrc_windows_amd64.syso -arch amd64`; verify with
   `GOOS=windows GOARCH=amd64 go build ./cmd/gotto-hando` (the linker
   picks up a `rsrc_windows_amd64.syso` next to `main.go` automatically —
   no `//go:generate` wiring required, but adding one is a reasonable
   convenience).
4. `internal/backend/windows/init.go` (or inline in `backend.go`'s
   package-level `init()`): call `SetProcessDpiAwarenessContext` with
   `DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2`, ignore the error (manifest
   fallback per ticket Decisions).
5. `cmd/gotto-hando/dispatch_windows.go` (new, `//go:build windows`):
   `newLocalBackend()` returns `windows.New()`, mirroring
   `dispatch_darwin.go` exactly.
6. `cmd/gotto-hando/dispatch_other.go` — change line 1 from `//go:build
   !darwin` to `//go:build !darwin && !windows`; no other change (the stub
   body/message stay correct for every remaining GOOS).
7. `assets/help-windows.txt` CAVEATS — append the scroll `by=page`
   limitation bullet (see Codebase Findings); no new `== SECTION ==`, so
   no spec anchor change per the ticket's Spec Impact clause.

## Verification Plan

**Safe subset (lock-independent, runnable this phase from the Mac + the
reachable Windows box):**
- `go build ./...` and `go vet ./...` on the Mac (GOOS-agnostic packages
  unaffected by the new `//go:build windows` files).
- `GOOS=windows GOARCH=amd64 go build ./...` and `go vet` (same GOOS/ARCH)
  from the Mac — compiles the new package + `dispatch_windows.go` without
  running it.
- `go test ./internal/engine/...` on the Mac (existing GOOS-agnostic
  engine tests, unaffected — no engine change this phase per the ticket).
- Cross-compile the windows unit-test binaries on the Mac and run them on
  the real Windows box over ssh (BatchMode key auth to
  `sw.kang@192.168.100.2`, default shell `cmd`, no Go installed there):
  ```
  GOOS=windows GOARCH=amd64 go test -c ./internal/backend/windows/ -o windows_backend.test.exe
  scp windows_backend.test.exe sw.kang@192.168.100.2:C:/tools/
  ssh sw.kang@192.168.100.2 C:\tools\windows_backend.test.exe -test.v
  ```
  Covers: scan-code table completeness (every canonical KEY NAMES symbol
  resolves, extended-key flag correct for the CAVEATS-listed keys, numpad
  by scan code regardless of NumLock), and Preflight gating with injected
  fake `sessionProbe`/`keyStateProbe` — held key → `abort` `E_INPUT` for
  an input-injecting run; a `qinfo`-only run passes preflight in a locked
  or non-console session AND while a key is physically held (check 3 is
  input-gated); a run containing `k` fails `E_SESSION` in a locked/
  non-console session. These are all fake-probe-driven, so no real key/
  session state on the box is disturbed.
- Cross-compiled CLI read-only smoke on the box:
  ```
  GOOS=windows GOARCH=amd64 go build -o gotto-hando.exe ./cmd/gotto-hando
  scp gotto-hando.exe sw.kang@192.168.100.2:C:/tools/
  ssh sw.kang@192.168.100.2 C:\tools\gotto-hando.exe local qinfo
  ssh sw.kang@192.168.100.2 C:\tools\gotto-hando.exe local qmouse
  ```
  Expect `qinfo` to report `session=inactive` (an ssh shell is session 0
  / not the console session per help-windows.txt INTERACTIVE SESSION
  REQUIRED — this is the CORRECT, documented result over ssh with no
  bridge running, not a bug) with `elevated=` reflecting the ssh account's
  token, and `desktop=`/`displays=` reflecting the box's real monitor
  layout; `qmouse` should return without erroring (`GetCursorPos` needs no
  interactive desktop).

**Deferred (needs an unlocked interactive GUI/console session on the box,
which an ssh shell is not — NOT run this phase, mirrors darwin's TextEdit
subset):**
- Real `SendInput` injection: `k`/`txt`/`m`/`c`/`drag`/`scroll`/`clip`/
  `paste` exercised against Notepad from a console/RDP-console session.
- `qinfo desktop=`/`displays=` visual cross-check against a real two-
  monitor layout with different scaling percentages (needs physically or
  RDP-attached multiple monitors, not just the headless ssh view).
- Held-key `E_INPUT` abort proven against a REAL physically-held key
  (`GetAsyncKeyState`), as opposed to the fake-probe unit test above.

## Escalations

- None.
