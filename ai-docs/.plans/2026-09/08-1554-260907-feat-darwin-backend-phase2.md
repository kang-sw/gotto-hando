# Plan: 260907-feat-darwin-backend — Phase 2

## Relevant Ticket Contract

- Goals (ticket Phase 2): `win` (WINDOW SELECTORS matching, `wait=DUR` polling
  at 100ms with E_NOWINDOW after DUR, unminimize/raise/focus via AX,
  current-window state), `qwin` (window-list rule from Decisions, OUTPUT line
  format), `cap` (full/`w`/`disp=`/`rect=`, `scale`, `n`/`ms` frame series with
  absolute deadlines and slippage report, `label`; backend returns raw
  pixels, engine encodes PNG via `image/png`), `w`-frame coordinates and
  E_BOUNDS for `w`, `scroll by=page` using the current window's height,
  Screen Recording gating for `cap` only, `--request-perms` (local, darwin)
  calling the two request APIs, `scripts/codesign-dev.sh` + Makefile target.
- Decision (capture API, already settled — do not relitigate): use
  `CGDisplayCreateImage` / `CGWindowListCreateImage`, knowingly deprecated
  since macOS 14.4; ScreenCaptureKit stays post-v1
  (`260907-feat-post-v1-extensions`). **Recommendation: follow the ticket
  as written — CGDisplayCreateImage/CGWindowListCreateImage, no
  ScreenCaptureKit.** Rationale: it's a synchronous call purego can bind
  like every other Phase 1 CG function; ScreenCaptureKit is
  delegate/async (`SCStreamOutput` callback across the objc runtime) and
  would be a materially different integration shape for no v1 benefit —
  the ticket already rejected it explicitly.
- Decision: capture pixels never encoded/written by the backend
  (`backend.Image` raw pixels only); PNG encoding is Go `image/png`,
  platform-independent, lives in `internal/engine` (or `internal/output`
  — survey recommends `internal/engine`, see Codebase Findings). The CLI
  layer (`cmd/gotto-hando`) writes the file or, with `--inline-captures`,
  base64-encodes it into JSONL.
- Decision: window list = `kCGWindowListOptionAll` filtered to layer-0
  windows of apps with the regular activation policy; `min` = not
  on-screen with `AXMinimized` true; `hidden` = owning app `isHidden`;
  `win` searches the same list (a minimized match is found and restored).
- Decision: `win[wait=DUR]` polls the window list every 100ms,
  `E_NOWINDOW` after DUR — same loop shape `open[wait=]` will reuse later
  (Phase 3, out of scope here but keep the loop reusable).
- Decision: `scroll by=page` = current window's height in pixel units
  (display-height fallback already shipped Phase 1; this phase adds the
  current-window-height variant).
- Decision: Screen Recording gates `cap` only; without it `qwin`/`win`
  still run (status ok) with empty titles, `qinfo` reports
  `screen:missing`. Accessibility gates `win` (already in Phase 1's
  `accessibilityGated` map — confirm no change needed there).
- Decision: `--request-perms` calls `AXIsProcessTrustedWithOptions(prompt:
  true)` and `CGRequestScreenCaptureAccess()`, prints
  `perms=accessibility:ok|missing,screen:ok|missing`, exit 0 when both ok
  else 4; Windows: exit 2 (already handled generically, confirm).
- Decision: stable signing identity script + Makefile target
  (`scripts/codesign-dev.sh` runs `codesign -s "gotto-hando-dev" --force`
  then `codesign -dv`).
- Constraint: preflight order/gating from Phase 1 stays authoritative —
  Screen Recording is a NEW (6th, cap-only) gate layered onto the existing
  5-check order, not a reordering.
- Constraint: multi-display origin/negative-coordinate handling from
  Phase 1 (`disp=N` follows `qdisp` order) extends to `w`-frame bounds
  the same way.

## Out of Scope

- `exec`, `open` real behavior (Phase 3, depends on this phase but not
  authorized here).
- ScreenCaptureKit (post-v1 ticket `260907-feat-post-v1-extensions`).
- Session bridge / remote ssh perms request path (`260908-feat-remote-ssh`
  owns the bridge-side `--request-perms` and `session=bridge` detection;
  this phase only wires the `local` darwin path).
- Windows Phase 2 (separate ticket) — this phase's engine-level additions
  (qwin/cap formatting, frame-series scheduling, PNG encode, `w`-frame
  bounds) must stay GOOS-agnostic so that ticket only adds backend FFI,
  but implementing it is not this phase's job.
- JPEG / `fmt=` / `cursor` capture modifiers (post-v1, parser already
  rejects them as E_SYNTAX).

## Codebase Findings

### Shared engine infra to build (GOOS-agnostic, windows Phase 2 will reuse)

- `internal/engine/run.go#L240-244` — `KindQueryWindows` case currently
  discards `Windows()`'s result and sets only `AlwaysShow`; no
  `res.Extra`/`res.JSON` formatting exists. Needs a `formatQueryWindows`
  (analogous to `internal/engine/query.go`'s `formatQueryDisp`) building
  the OUTPUT `qwin` line format (`assets/help.txt:590`):
  `  <id>\t<pid>\t<app>\t<x>,<y> <w>x<h>\t<flags>\t<title>` with
  `flags` = `*` focused / `min` minimized / `hidden`.
- `internal/engine/run.go#L298-316` (`doFocus`) — `res.Detail` is
  currently `"id=%d app=%s matched=%d"`, missing the `<x>,<y> <w>x<h>
  "<title>"` tail the OUTPUT spec requires
  (`assets/help.txt:600`: `win id=<id> app=<app> matched=<N> <x>,<y>
  <w>x<h> "<title>"`). Fix the format string; this is a small but
  concrete correctness gap, not new logic.
- `internal/engine/run.go#L251-257` (`KindCapture`) — currently a single
  `Capture()` call with no PNG encode, no file write, no frame-series
  loop, no `n=`/`ms=` handling despite `op.CapCount`/`op.CapInterval`
  already existing in the IR (`internal/ir/ops.go#L169-170`,
  `internal/ir/validate.go#L122-146` already validates `CapCount<=120`,
  total captures `<=121`, `CapInterval<=MaxDelayMS`, label regex). This
  phase must add: frame-series scheduling (n frames every ms, absolute
  deadlines from run start — not accumulated per-frame sleep, to avoid
  drift — with slippage = actual-scheduled per help.txt CAVEAT
  `:799-800`), PNG encoding via `image/png` from `backend.Image.Pixels`,
  capture path building (`assets/help.txt:601-606`: `<out>/<NNNN>-
  <label>-<UTC timestamp>.png`, `-00`/`-01` suffixes for n>1) and file
  writing — but see path-building placement note below.
- **Capture path building / writing placement**: `assets/help.txt:594` +
  Constraints ("the CLI layer writes the file or, with
  `--inline-captures`, emits it as base64") says file writing is a CLI
  concern, not engine. But `cmd/gotto-hando/dispatch.go` currently has
  no per-result post-processing hook — `engine.Run` returns a finished
  `Summary` before any output is written (`dispatch.go#L134-146`). Two
  options: (a) engine returns raw `backend.Image`(s) in `output.Result`
  (extend `Result` with an unexported-from-JSON field) and
  `cmd/gotto-hando` encodes+writes; (b) engine does PNG-encode +
  path-build + write itself (still GOOS-agnostic, `os.WriteFile` +
  `image/png` are both stdlib) and only `--inline-captures` base64
  happens in the CLI layer by having the engine hand back bytes instead
  of a path when a flag says so. **Recommendation: (b)** — keeps
  `dispatch.go` a thin dispatcher (matches its current shape: it has zero
  post-processing today) and keeps the path-building logic in one place
  next to the frame-series loop that already needs to know `label`/`n`/
  run-start-time. `engine.RunOptions` needs new fields: `OutDir string`,
  `InlineCaptures bool` (dispatch.go already computes `outDir` via
  `output.DefaultOutDir` before calling `engine.Run` — thread it through).
  This is a design call within the ticket's stated boundary (backend
  never encodes/writes — satisfied either way); flagged for the executor
  to confirm against `output.DefaultOutDir`'s existing signature
  (`cmd/gotto-hando/dispatch.go#L80`, `#L133`) before committing.
- `internal/engine/compose.go#L133-192` (`resolve`) — `case "window"` at
  L136-140 sets origin/size from `st.window` but line 186 explicitly
  excludes `p.Frame != "window"` from the run-time `checkBounds` call
  (comment: "`st.window` is always nil in Phase 1... Phase 2"). Phase 2
  must (1) make `checkBounds` handle `Frame == "window"` (bounds = the
  current window's rect, `E_NOWINDOW`-vs-`E_BOUNDS` distinction: no
  current window at all should probably be a distinct error — check
  whether `assets/help.txt:254` ("else the OS-focused window") implies a
  fallback to the OS-focused window when no `win` has run yet, which
  needs a new `Backend` query or `Windows()` call with an empty/current
  selector), (2) remove line 186's exclusion for `window`. This is the
  single trickiest resolve() change — the "else the OS-focused window"
  fallback needs a concrete source (see Escalations).
- `internal/engine/run.go#L216-218` (`KindScroll`) — passes
  `backend.ScrollUnit(op.ScrollBy)` straight through; `by=page` sizing
  (display height vs current-window height) is entirely inside the
  darwin backend's `Scroll` (`internal/backend/darwin/mouse.go`, not
  read this pass but named in the Phase 1 Result note as already doing
  the display-height fallback) — Phase 2 needs the backend to know the
  current window, which today it does not (only `engine.doFocus` tracks
  `st.window`; the backend has no window state). **This means `Scroll`'s
  signature or a new backend method must carry the current window height
  in**, OR the engine resolves the page height itself and passes ticks in
  a pre-scaled unit. Needs a concrete signature decision — flagged in
  Escalations only if it turns out `Scroll`'s interface must change
  (public `Backend` interface change = "ask first" per AGENTS.md Approval
  Protocol, cross-module interface).

### darwin-backend-specific FFI (new purego bindings, `internal/backend/darwin/ffi.go`)

- Not yet bound (confirmed by reading `ffi.go` in full): `CGWindowListCopyWindowInfo`,
  `CGWindowListCreateImage`, `CGDisplayCreateImage`, `CGRequestScreenCaptureAccess`,
  `AXIsProcessTrustedWithOptions`, and the full `AXUIElement*` family
  (`AXUIElementCreateApplication`, `AXUIElementCopyAttributeValue`,
  `AXUIElementSetAttributeValue`, `AXUIElementPerformAction` for raise,
  `kAXWindowsAttribute`/`kAXTitleAttribute`/`kAXMinimizedAttribute`/
  `kAXPositionAttribute`/`kAXSizeAttribute`/`kAXRaiseAction` string
  constants recreated via `cfString`, same pattern as `session.go`'s
  `sessionKeyScreenIsLocked`). Only `AXIsProcessTrusted` (no-prompt query)
  and `cgPreflightScreenCaptureAccess` exist today
  (`internal/backend/darwin/ffi.go#L83`, `#L141`).
- `internal/backend/darwin/probes.go#L21-24,63-66` — `permissionProbe`
  interface **already has `ScreenRecording() bool`**, and
  `realPermissionProbe.ScreenRecording()` **already calls
  `cgPreflightScreenCaptureAccess()`**. This is Phase 1 having
  pre-built the seam Phase 2 needs — Preflight's Screen Recording gate is
  almost free: add a `capGated` kind check (only `KindCapture`) in
  `preflight.go` next to the existing `accessibilityGated` map
  (`internal/backend/darwin/preflight.go#L23-33`) calling
  `b.perm.ScreenRecording()`.
- `internal/backend/darwin/probes.go` needs new probe interfaces for the
  test seam (matching the existing pattern exactly): a `windowProbe`
  (backs `CGWindowListCopyWindowInfo` enumeration + AX cross-reference)
  and possibly folding capture into `permissionProbe`'s existing shape —
  no new capture probe needed since `Capture()` itself is the seam (no
  preflight-time capture behavior beyond the permission check already
  covered).
- **AX/CG window correlation** (not spelled out in the ticket beyond "AX
  raise/focus, AXMinimized, window wait"): `CGWindowListCopyWindowInfo`
  gives `kCGWindowNumber` (id), `kCGWindowOwnerPID`, `kCGWindowOwnerName`,
  `kCGWindowName` (title — empty without Screen Recording per
  help-macos.txt:21-24), `kCGWindowBounds`, `kCGWindowLayer`,
  `kCGWindowIsOnscreen`. It has **no minimized flag and no direct link to
  an `AXUIElementRef`** (no public CGWindowID -> AXUIElement API exists).
  The standard technique (used by comparable non-cgo/AX tools): per
  distinct `kCGWindowOwnerPID` in the candidate set, call
  `AXUIElementCreateApplication(pid)` + `AXUIElementCopyAttributeValue(...,
  kAXWindowsAttribute)` to get that app's AX windows, then correlate to
  the CG entry by title (+ position as tie-break) to read
  `kAXMinimizedAttribute` and to raise/focus. This is a heuristic, not a
  guaranteed 1:1 mapping — duplicate-title windows in the same app are
  the known failure mode, but the command's own `matched=N`/`warn`
  semantics already tolerate ambiguity, so it degrades gracefully rather
  than silently misbehaving. Confidence: medium — standard technique, but
  unverifiable without a GUI session this pass (see Escalations note;
  not escalated, since verification is explicitly deferred to home).
- Window "regular activation policy" filter (Decisions: "layer-0 windows
  of apps with the regular activation policy") needs `NSRunningApplication
  (processIdentifier:)`'s `activationPolicy` (`NSApplicationActivationPolicyRegular
  == 0`), which is an AppKit objc message send — same pattern as
  `internal/backend/darwin/clipboard.go#L21-45`'s `NSPasteboard` init
  (dlopen AppKit once, `objc.GetClass`/`objc.RegisterName`, cache in a
  `sync.Once`). Reuse that pattern in a new `internal/backend/darwin/windows.go`
  rather than duplicating the AppKit dlopen.
- `internal/backend/darwin/backend.go#L52-68` (`New`) — add new probe
  fields (`windows windowProbe` or fold into existing structure) the same
  way `session`/`perm`/`keys`/`secure`/`displays` were added, defaulting
  to a `real*Probe{}` in `New()`.
- `internal/backend/darwin/stubs.go` — delete the `Windows`/`Focus`/
  `Capture` stub methods (lines 22-32) once real implementations exist in
  new files (`windows.go`, `capture.go`); keep `Exec`/`Open` stubs
  (Phase 3, `errPhase1` — rename comment since it's no longer "Phase 1"
  scoped, still accurate as "not this phase").

### Scripts / build

- No `scripts/` directory and no `Makefile` exist yet (confirmed —
  `ls scripts` and `ls Makefile` both miss). Ticket: create
  `scripts/codesign-dev.sh` (`codesign -s "gotto-hando-dev" --force
  ./gotto-hando && codesign -dv ./gotto-hando`, matching
  `assets/help-macos.txt:82-84` verbatim) and a `Makefile` target that
  runs it after a build (`make build && make sign`, or a single
  `make dev-sign` — executor's call, doc-consistent either way).

## Implementation Plan

1. **darwin FFI additions** (`internal/backend/darwin/ffi.go`): add
   `CGWindowListCopyWindowInfo`, `CGWindowListCreateImage`,
   `CGDisplayCreateImage`, `CGRequestScreenCaptureAccess`,
   `AXIsProcessTrustedWithOptions`, `AXUIElementCreateApplication`,
   `AXUIElementCopyAttributeValue`, `AXUIElementSetAttributeValue`,
   `AXUIElementPerformAction` bindings in `registerCoreGraphics`/a new
   `registerApplicationServices` extension, following the existing
   `purego.RegisterLibFunc` pattern exactly.
2. **Window enumeration + selector matching**
   (`internal/backend/darwin/windows.go`, new file): `Windows(ctx,
   sel)` — `CGWindowListCopyWindowInfo(kCGWindowListOptionAll, 0)`,
   filter layer-0 + regular-activation-policy (AppKit objc, reuse
   clipboard.go's dlopen pattern), map each to `backend.Window`
   (`Minimized` via the AX title-correlation heuristic above, `Hidden`
   via `NSRunningApplication.isHidden`, `Focused` via
   `kCGWindowLayer==0 && frontmost` or AX focused-window comparison),
   apply `ir.Selector` matching (id/pid/app/title substring/title regex,
   case-insensitive per `assets/help.txt:288-293`).
3. **Focus** (`internal/backend/darwin/windows.go`): AX unminimize
   (`kAXMinimizedAttribute` -> false via `AXUIElementSetAttributeValue`),
   raise (`kAXRaiseAction` via `AXUIElementPerformAction`), and
   frontmost-app activation (`NSRunningApplication.activateWithOptions:`
   or `NSApplication.activateIgnoringOtherApps` equivalent via AX/objc).
4. **`win[wait=DUR]` polling loop**: implement in `internal/engine`
   (GOOS-agnostic, shared with future `open[wait=]`) — poll
   `be.Windows(ctx, sel)` every 100ms up to `WaitMS`, fail `E_NOWINDOW`
   after the deadline. Replace/extend `doFocus`
   (`internal/engine/run.go#L298-316`) to loop when `op.HasWait`.
5. **qwin formatting** (`internal/engine/query.go`): add
   `formatQueryWindows([]backend.Window) []string` and
   `queryWindowsJSON`, wire into `run.go`'s `KindQueryWindows` case the
   same way `qdisp`/`qmouse` are wired.
6. **`win` result-line fix** (`internal/engine/run.go#doFocus`): extend
   `res.Detail` to the full OUTPUT format (id/app/matched/x,y/WxH/title).
7. **`w`-frame bounds** (`internal/engine/compose.go`): resolve the
   "else the OS-focused window" fallback question (Escalations item
   below) then wire `checkBounds` for `Frame == "window"` and drop the
   L186 exclusion.
8. **`scroll by=page` current-window height**: resolve the `Scroll`
   interface question (Escalations item), thread current window height
   from `engine.doFocus`'s tracked `st.window` into the darwin backend's
   existing page-height logic in `internal/backend/darwin/mouse.go`
   (read that file before implementing — not read this survey pass).
9. **Capture** (`internal/backend/darwin/capture.go`, new file):
   `Capture(ctx, req)` resolves frame (`desktop`/`w`/`disp=N`) +
   `rect=` to a `CGRect`, calls `CGDisplayCreateImage`/
   `CGWindowListCreateImage` accordingly, reads back raw pixel bytes
   (via `CGImage` -> `CGDataProvider` -> `CFData` -> bytes, or
   `CGBitmapContext` re-render — needs concrete purego binding work not
   detailed here), returns `backend.Image{W,H,OriginX,OriginY,Scale,Pixels}`.
   No PNG encoding here (ticket Decision).
10. **Capture engine path** (`internal/engine`): frame-series scheduling
    with absolute deadlines (`runStart + i*interval`, not
    `sleep(interval)` per frame, to avoid drift accumulation), PNG encode
    via `image/png.Encode` from `backend.Image`, path building
    (`<out>/<NNNN>-<label>-<UTC-timestamp>.png`, `-00`/`-01` suffixes for
    n>1), slippage = `actual - scheduled` per frame reported in JSONL
    (spec-consistent inference, same category as `query.go`'s existing
    `queryInfoJSON` inference comment — plain format gets one summary
    line + indented per-frame continuation lines, matching the qwin/exec
    convention). Wire `CapOnError`/`--inline-captures` per Constraints.
11. **Preflight Screen Recording gate**
    (`internal/backend/darwin/preflight.go`): add a `capGated` map
    (`ir.KindCapture: true`) alongside `accessibilityGated`, checked with
    `b.perm.ScreenRecording()`, `E_PERMISSION` on failure — insert as a
    new check after the existing Accessibility check, per Constraints'
    "any failure is exit 4" ordering (exact insertion point: confirm
    against help.txt EXECUTION step 4's check ordering, not fully re-read
    this pass).
12. **`--request-perms`** (`cmd/gotto-hando/dispatch.go`): replace the
    unconditional `abort(EValidate, "--request-perms not implemented")`
    (`dispatch.go#L99-101`) with a darwin-only branch (build-tag'd
    factory like `dispatch_darwin.go`/`dispatch_other.go` already do for
    `newLocalBackend`) that calls the two request APIs, prints the
    `perms=` line, exits 0/4; non-darwin keeps exit 2.
13. **`scripts/codesign-dev.sh` + Makefile**: create both per Codebase
    Findings, verbatim to `assets/help-macos.txt:82-84`.
14. **probes.go / dryrun.go test seams**: extend
    `internal/backend/darwin/probes.go` with the new window/capture-
    adjacent probe interfaces used above; `internal/backend/dryrun`
    already satisfies `Windows`/`Focus`/`Capture` (no change needed
    there unless frame-series needs a new dryrun hook for engine tests).

## Verification Plan

Run this phase (safe, no unlocked GUI / Screen Recording / Accessibility
needed):
- `go build ./...`, `go vet ./...`, `go test ./... -race` — all
  GOOS-agnostic engine tests plus darwin unit tests native on this Mac.
- Unit tests for WINDOW SELECTORS matching predicates (id/pid/app/title
  substring/title regex, case-insensitive) against synthetic
  `backend.Window` lists — pure logic, no FFI.
- Unit tests for the qwin list-filtering/formatting rule and line format
  (`formatQueryWindows`).
- Unit tests for `cap` rect/`w`/`disp=` geometry resolution and
  `E_BOUNDS` against synthetic display/window geometry (same
  `displayProbe`-style injection Phase 1 used in `preflight_test.go`).
- Unit tests for frame-series absolute-deadline scheduling + slippage
  math with a fake clock (no real `sleep`/FFI).
- Unit test: PNG encode from a synthetic raw-pixel `backend.Image`
  buffer, decoded back with `image/png` in the test to confirm a
  structurally valid PNG.
- Unit test: `win[wait=]` polling loop against a fake window-probe/clock
  — window appears after N polls -> success; never appears ->
  `E_NOWINDOW` after the deadline.
- `qdisp` CLI smoke test (`gotto-hando local qdisp`) — display
  enumeration works without an unlocked session (already proven safe in
  Phase 1's own verification).
- `scripts/codesign-dev.sh` + Makefile target actually run on this Mac;
  `codesign -dv` shows `Authority=gotto-hando-dev` — **dependency**: only
  passes if a `gotto-hando-dev` self-signed identity already exists in
  this Mac's Keychain (help-macos.txt STABLE SIGNING IDENTITY step 1);
  note this as a possible blocker, not a code defect, if absent.

HOME-VERIFICATION CHECKLIST (unlocked GUI session, Screen Recording +
Accessibility granted — mirrors the ticket's own Phase 2 Verification
list):
- `gotto-hando local 'cap'` / `cap[w]` / `cap[disp=1]` output
  dimensions/origin match `qdisp`/`qwin` values on a Retina + external
  display setup.
- `gotto-hando local 'win[]<minimized app title>'` restores and focuses
  it (unminimize + raise + frontmost).
- `gotto-hando local 'win[wait=5s]<title>'` on a window appearing ~2s
  later succeeds; on one that never appears, returns `E_NOWINDOW` after
  ~5s.
- With Screen Recording revoked: `qwin`/`win` run with empty titles
  (status ok); `cap` fails preflight with `abort` `E_PERMISSION`.
- `gotto-hando local --request-perms` on a fresh signing identity
  triggers the system prompts; verify the printed `perms=` line and exit
  code both before and after granting.
- Duplicate-title-window case for the AX/CG correlation heuristic (two
  windows of the same app with identical titles): confirm `win`
  reports `matched=2`/`warn` rather than misbehaving silently, and
  `qwin`'s `min`/`hidden` flags are still correct for both.

## Escalations

- None. The ticket's own Decisions resolve the strategic questions this
  survey would otherwise flag (capture API is pinned to
  CGDisplayCreateImage/CGWindowListCreateImage, not ScreenCaptureKit;
  encoding boundary is pinned to engine-side `image/png`; the AX/CG
  window-correlation technique is a standard, well-understood pattern
  whose reliability is explicitly deferred to home verification per the
  lead's directive, not a design unknown). Two narrow implementation-time
  decisions remain open and are called out inline in the Implementation
  Plan (steps 7-8) rather than escalated:
  1. **`w`-frame with no current window** (`internal/engine/compose.go`
     `resolve()`, `case "window"`): `assets/help.txt:254`'s "else the
     OS-focused window" fallback needs a concrete source once `st.window`
     stops being permanently nil. Resolving this may add one small,
     additive `Backend` method (e.g., a frontmost-window query) — a
     cross-module interface change per AGENTS.md's Approval Protocol, so
     the executor should confirm the shape with the lead before adding it,
     but the survey does not consider this a research question: the
     answer is "look at `mouse.go` and existing `Windows()`, pick the
     smallest addition consistent with backend.go's package doc."
  2. **`scroll by=page` current-window height plumbing**: not yet
     confirmed how Phase 1's display-height fallback in
     `internal/backend/darwin/mouse.go` is structured (not read this
     survey pass) — the executor reads it first, then picks the
     least-invasive way to give it the current window's height (most
     likely: thread `st.window`'s height through `Scroll`'s existing
     call site in `internal/engine/run.go#L215-218`, or a small backend
     method addition, same approval note as above).
  Neither blocks starting implementation on the rest of Phase 2 (window
  enumeration, qwin, cap, Screen Recording gating, `--request-perms`,
  signing script), and neither is a strategy or contract unknown — both
  are ordinary "read one more file, make a small additive call" steps
  inside the executor's normal flow.
