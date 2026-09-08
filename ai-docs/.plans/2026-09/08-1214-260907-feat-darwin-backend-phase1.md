# Plan: 260907-feat-darwin-backend — Phase 1

## Relevant Ticket Contract

- Phase 1 goal command set (`ai-docs/tickets/ready/260907-feat-darwin-backend.md#L154-172`):
  `k kd ku txt m c md mu drag scroll clip paste qclip qmouse qdisp qinfo sleep set`,
  session/lock detection, all five preflight checks with per-command gating
  and the `abort` result, runtime `E_BOUNDS` for `r`/`%`, Secure Input as a
  run-time `E_INPUT`, the KEY NAMES table mapped to virtual key codes with
  `volup`/`voldown`/`mute` rejected in preflight. `w`-frame commands and
  `E_BOUNDS` for `w` are explicitly Phase 2.
- Phase 1 also defines `backend.PreflightError` (carries `output.ErrorCode`,
  placed so `internal/output` never imports `internal/backend`), wires
  `engine.Run` to call `Preflight` before any op and map its coded error to
  the `abort` object (non-coded error → `E_UNKNOWN`, exit 4), and replaces
  the `dest=="local"` stub in `cmd/gotto-hando/dispatch.go` with the darwin
  backend behind `runtime.GOOS=="darwin"` (`ai-docs/tickets/...#L94-107`,
  `#L168-172`).
- No-cgo constraint: every macOS API goes through `purego` dlopen; exact
  framework/function inventory in `ai-docs/tickets/...#L24-38`.
- Preflight order and per-command gating (`ai-docs/tickets/...#L79-93`,
  `#L115-127`):
  1. GUI session unlocked (own or bridge) — every command except
     `qinfo qdisp qmouse sleep set` (comments too) — `E_SESSION`.
  2. Accessibility — `k kd ku txt m c md mu drag scroll paste win open[wait=]`
     — `E_PERMISSION`. Screen Recording — `cap` only (Phase 2). No
     permission: `clip qclip exec open` (no `wait=`).
  3. No key/button physically held (`CGEventSourceKeyState` over every
     virtual key code + `CGEventSourceButtonState`) — `E_INPUT`.
  4. Absolute and `disp=` coordinates inside the desktop — `E_BOUNDS`.
  5. Every key name supported on darwin (`volup`/`voldown`/`mute` excluded)
     — `E_INPUT`.
  Secure Input is NOT a preflight check — it fails the affected line at run
  time with `E_INPUT` (`ai-docs/tickets/...#L126-127`).
- Coordinate bounds split (`assets/help.txt:245-268`): absolute/`disp=`
  points are checked in **preflight** against the real desktop (abort,
  exit 4); `r`/`w`/`%` points depend on run-time state and are checked
  **right before the line runs** (line `err`, exit 1). Phase 1 implements
  only the `r`/non-window `%` half; `w`-frame runtime bounds stay Phase 2.
- `primary`, `volup`/`voldown`/`mute` are IR-level canonical key symbols
  already produced by the (unexported) parser table at
  `internal/syntax/keys.go:12-79`; the backend receives these symbol
  strings verbatim via `Backend.KeyDown/KeyUp(ctx, key string)`
  (`internal/backend/backend.go:122-123`) and must resolve `primary` to
  the macOS Command key itself.
- `paste` semantics, `scroll by=page` display-height fallback, `qinfo`/
  `qdisp`/`qmouse` field/line formats: `assets/help.txt:334-380`,
  `:431-447`, `:576-586`.

## Out of Scope

- Phase 2: `win`, `qwin`, `cap`, `w`-frame coordinate resolution and its
  `E_BOUNDS` check, Screen Recording gating, `--request-perms`,
  `scripts/codesign-dev.sh` / Makefile signing target.
- Phase 3: real `exec`/`open` backend behavior (their `Backend` methods
  exist only as compile-satisfying stubs in Phase 1 — `exec`/`open` are not
  in the Phase 1 goal list).
- `--bridge`, `--remote-bin`, `--inline-captures`, ssh/session-bridge
  behavior (260908-feat-remote-ssh); `session=bridge`/`inactive` detection
  logic stays in the darwin session probe but is exercised for real only by
  that later ticket.
- `--timeout` deadline enforcement: stays inert, as it already is per
  `cmd/gotto-hando/dispatch.go:36-38`; Phase 1 passes `context.Background()`
  to `engine.Run`.
- PNG encoding, capture paths, `--out` directory creation.

## Codebase Findings

- `internal/backend/backend.go:118-143` — the `Backend` interface Phase 1
  must fully satisfy (incl. `Windows/Focus/Capture/Exec/Open`, which are
  Phase 2/3 in behavior but must compile in Phase 1).
- `internal/backend/backend.go` has no `PreflightError` type yet (confirmed
  via repo-wide grep — only the ticket text mentions it); `internal/output`
  imports nothing project-internal (`internal/output/writer.go:1-12`), so
  `internal/backend` can safely import `internal/output` with no cycle.
- `internal/engine/run.go:62-113` — `Run` never calls `Preflight` today
  (cli-core built it against the dryrun backend only); `Summary` has no
  abort field. This is the exact wiring gap Phase 1 must close.
- `internal/engine/run.go:246-260` — `KindQueryInfo`/`KindQueryDisp`/
  `KindQueryMouse` already call `be.Info()`/`be.MousePos()` but set only
  `res.AlwaysShow = true`; `res.Detail`/`res.Extra`/`res.JSON` are never
  populated for these three kinds. Since `qinfo qdisp qmouse` are the
  Phase 1 goal's "complete line" queries, this formatting is Phase 1 work,
  not a later phase's — `doFocus` (`run.go:265-283`) is the existing
  pattern to follow (build `res.Detail`, return `res`).
- `assets/help.txt:576-586` — `qwin/qdisp/qmouse/exec/cap/win` results are
  a one-line summary plus TAB-separated, two-space-indented continuation
  lines (`output.Result.Extra`, `internal/output/writer.go:44-47`); `qdisp`
  is one line per display, `qmouse` is `"  x,y"` alone — both use the same
  `Extra` mechanism as `qwin`'s example even for a single line.
- `internal/output/writer.go:150-161` — `writeStart` (the `out <dir>` /
  `{"event":"start",...}` line) is unexported and today is called only
  from inside `WriteAbort`. Nothing in `cmd/gotto-hando` currently prints a
  normal (non-abort) start line — Phase 1's local dispatch is the first
  caller that needs one, so it must be exported (e.g. `WriteStart`).
- `cmd/gotto-hando/analyze.go:26-67` — the parse → `syntax.Inline` →
  `ir.Validate` → diagnostic-printing pipeline the local-run path must
  reuse verbatim (same failure contract: empty stdout, sorted diagnostics
  to stderr, exit 2). Currently private to `analyze`; extracting a shared
  helper avoids duplicating this glue in the new local-run path (risk
  signal: duplicated glue if not extracted).
- `cmd/gotto-hando/dispatch.go:110-115` — exact stub to replace; `abort`
  closure (`dispatch.go:73-81`) already implements the abort-object
  contract and should be reused unchanged for both parse-failure and
  `Preflight`-failure paths.
- `cmd/gotto-hando/version.go:10-33` — `version()` parses `ver=` from
  `assets.Help`'s first line but is private to `package main`; `qinfo`'s
  `ver=` field needs the same string from inside `internal/backend/darwin`,
  which cannot import `cmd/gotto-hando` (wrong direction). Moving the parse
  into `assets` (e.g. `assets.Version()`) lets both sides call one function
  instead of duplicating the regex (risk signal: duplicated glue if not
  extracted).
- `internal/syntax/keys.go:8-79` — the canonical KEY NAMES table (all
  aliases → canonical symbol) is unexported and parser-only; the darwin
  backend's keycode table must enumerate the same symbol set independently
  (it cannot import an unexported map from another package). This is
  intentional duplication across two different concerns (parse-time
  canonicalization vs. platform keycode mapping) but the two tables must be
  kept in sync by hand — flag with a cross-reference comment in both files,
  not a shared table (no clean import path exists without exporting parser
  internals, which is out of this ticket's scope).
- `internal/backend/dryrun/dryrun.go:49-51` — `Preflight` just records the
  call and returns `FailOn`'s error; no change needed there. Engine-level
  Preflight-wiring tests can inject a `*backend.PreflightError` (or a plain
  `error`) via `FailOn` without touching the dryrun package. This also
  confirms the "injectable event sink" the ticket's verification text asks
  for already exists — no new engine-level test seam is needed for the
  held-release-ordering tests; `dryrun.Backend` is that seam.
- `internal/engine/compose.go:111-140` — `resolve` (frame + percent →
  `backend.Point`) never checks bounds today; it is the natural home for
  the new `r`/non-window-`%` runtime `E_BOUNDS` check (desktop/display size
  from `st.getInfo(ctx)`, run.go:115-121). `window`-frame resolution exists
  in `resolve` already but `st.window` is always `nil` in Phase 1 (no `win`
  yet) — leave it as-is; it is inert until Phase 2, not a Phase 1 bug.
- No `go:build`/`+build` platform-tagged file exists anywhere in this repo
  yet (`grep -rl "go:build\|+build"` only matches test files that check
  `runtime.GOOS` at run time, not compile-time tags) — Phase 1 is the first
  build-tag split; there is no local precedent to imitate, only the
  standard Go idiom (a build-tag package plus a `_darwin.go`/`_other.go`
  pair of thin factory functions at the call site).
- `go.mod` has zero dependencies today — `github.com/ebitengine/purego`
  must be added (`go get`).

## Implementation Plan

1. `assets/version.go` (new): move the `ver=` regex-parse out of
   `cmd/gotto-hando/version.go` into an exported `assets.Version() string`;
   update `cmd/gotto-hando/version.go`'s `version()` to call it. No
   behavior change, just relocation so `internal/backend/darwin` can read
   the tool version without importing `cmd/gotto-hando`.
2. `internal/backend/preflight_error.go` (new, package `backend`): define
   `PreflightError{ Code output.ErrorCode; Msg string }` with `Error() string`;
   import `internal/output` (no cycle — confirmed above).
3. `go get github.com/ebitengine/purego` (pin an exact version, per the
   ticket's no-cgo decision).
4. `internal/backend/darwin/` (new package, every file `//go:build darwin`):
   - `ffi.go` — purego dlopen/registration plumbing for the frameworks
     listed in the ticket (CoreGraphics, HIToolbox, ApplicationServices/AX,
     AppKit via the `objc` runtime).
   - `keys.go` — canonical symbol → `CGKeyCode` table (same symbol set as
     `internal/syntax/keys.go`, independently enumerated per the finding
     above), omitting `volup`/`voldown`/`mute` so a lookup miss on those
     three is the "unsupported key name" signal Preflight check 5 uses.
     `primary` resolves to the Command keycode directly here (not a
     separate indirection).
   - `probes.go` — small interfaces for the test seam: `sessionProbe`
     (session dict + `CGSSessionScreenIsLocked`), `permissionProbe`
     (`AXIsProcessTrusted`, `CGPreflightScreenCaptureAccess`),
     `keyStateProbe` (`CGEventSourceKeyState` over the full keycode table +
     `CGEventSourceButtonState`), `secureInputProbe`
     (`IsSecureEventInputEnabled`). Real purego-backed implementations plus
     the `Backend` struct holding each as an interface-typed field
     (defaulted to the real impl in `New()`, directly settable in
     same-package `_test.go` files — no exported override API needed since
     tests live in `package darwin`).
   - `backend.go` — `Backend` struct, `New() (*Backend, error)` (dlopens
     everything once), `Info(ctx)` (os/osver/arch via `runtime.GOOS`/
     `runtime.GOARCH`/a small sysctl or `syscall.Sysctl`, `ver` via
     `assets.Version()`, `primary="cmd"`, desktop bounds via
     `CGGetActiveDisplayList`/`CGDisplayBounds`, `session`/`perms` via the
     probes, formatted exactly as `assets/help-macos.txt:11-14`).
   - `preflight.go` — `Preflight(ctx, seq *ir.Sequence) error` implementing
     the 5 checks in the literal order from the ticket's Constraints list,
     short-circuiting on first failure, returning `*backend.PreflightError`
     with the matching `output.ErrorCode`; per-command gating computed by
     scanning `seq.Ops` once for: any non-exempt kind (session), any
     Accessibility-gated kind (permission + held-key-state + key-name
     checks — these three only make sense when the run injects input, so
     they run only when the Accessibility-gated set is present), any point
     with `Frame` in `{"desktop","display"}` and no percent flags
     (bounds — Phase 1 desktop/`disp=` half only).
   - `keyboard.go` — `KeyDown/KeyUp/TypeText` via
     `CGEventCreateKeyboardEvent`/`CGEventKeyboardSetUnicodeString`/
     `CGEventPost`; Secure Input surfaced as a run-time error here (not in
     Preflight), tagged so the engine reports `E_INPUT` (it already does —
     `run.go:134-137` maps every `KeyDown`/`KeyUp`/`TypeText` error to
     `E_INPUT`, so a plain `error` from this file is enough, no coded type
     needed here).
   - `mouse.go` — `MouseMove/ButtonDown/ButtonUp/Scroll/MousePos` via
     `CGEventCreateMouseEvent`/`CGEventCreateScrollWheelEvent`; `by=page`
     uses the display height (no current-window fallback needed yet since
     `win` is Phase 2 and `st.window` is always nil — matches the ticket's
     "the display's height when no current window" clause exactly).
   - `clipboard.go` — `ClipboardGet/ClipboardSet` via `NSPasteboard`
     (objc runtime).
   - `stubs.go` — `Windows/Focus/Capture/Exec/Open` return a plain
     `errors.New("not implemented in Phase 1")`-style error so the type
     compiles; not reachable through any Phase 1 verification path (a line
     using them would simply fail at run time as `err`, which is
     acceptable since these commands are out of Phase 1 scope).
   - `session.go` — session/lock detection via
     `CGSessionCopyCurrentDictionary`/`CGSSessionScreenIsLocked` (private
     SkyLight framework symbol, per the ticket's own inventory).
5. `internal/engine/run.go`:
   - Add `Aborted bool`, `AbortCode output.ErrorCode`, `AbortMsg string` to
     `Summary`.
   - At the top of `Run`, call `be.Preflight(ctx, seq)`; on error, use
     `errors.As` against `*backend.PreflightError` (code/msg from it) else
     fall back to `output.EUnknown`; set `Aborted`/`AbortCode`/`AbortMsg`/
     `Exit: output.AbortExit(code)` and return immediately (no ops run, no
     `Results`/`Done`).
   - Populate `res.Detail`/`res.Extra` (plain) for `KindQueryInfo` (single
     line per `assets/help.txt:431-434`/`help-macos.txt:13`),
     `KindQueryDisp` (one `Extra` line per display,
     `assets/help.txt:582`), `KindQueryMouse` (`Extra = ["  x,y"]`,
     `assets/help.txt:583`); populate `res.JSON` for `--jsonl` with a
     reasonable structured shape (`qinfo`: flat KV pairs mirroring the
     plain fields; `qdisp`: a `"displays"` array field) — help.txt's JSONL
     section does not show a verbatim example for these three, so this
     shape is a spec-consistent inference, not a literal quote; flag for
     confirmation if a later spec pass adds an explicit example.
   - Add lightweight timing: `time.Now()`/`time.Since` around each
     `execute` call for `res.TMS` and around the whole loop for
     `sum.Done.ElapsedMS` (both are always zero today — small, low-risk,
     pattern-following addition, not a new policy).
6. `internal/engine/compose.go` — extend `resolve` to bounds-check the
   result for `Frame == "pointer"` (`r`) and `Frame != "window"` with a
   percent flag set, against `st.getInfo(ctx)`'s desktop bounds (or the
   named display's bounds when `Frame == "display"`), returning a plain
   `error` (the caller sites in `run.go` already map `doMove`/`doClick`/
   `doDrag` errors to `E_INPUT`... **note**: those call sites must map an
   `E_BOUNDS`-flavored resolve error to `output.EBounds`, not `E_INPUT` —
   this needs a typed/sentinel error out of `resolve` so `execute` can
   branch on it, e.g. `errBounds` returned as `fmt.Errorf("%w: ...",
   errBounds)` and checked with `errors.Is` at each of the three call
   sites (`run.go:162-190`) before falling back to `E_INPUT`).
7. `internal/output/writer.go` — export `writeStart` as `WriteStart` (same
   signature/behavior); update `WriteAbort`'s internal call site
   accordingly.
8. `cmd/gotto-hando/analyze.go` — extract `parseAndValidate(opts
   parsedOptions, lines []string) (*ir.Sequence, []ir.Diagnostic)` (the
   `ir.DefaultState()`/`--delay`/`syntax.Parse`/`syntax.Inline`/
   `ir.Validate` block, `analyze.go:27-36`) and `writeDiagnostics(stderr
   io.Writer, diags []ir.Diagnostic)` (the sort + print block,
   `analyze.go:38-51`); have `analyze` call both; the new local-run path
   reuses both unchanged.
9. `cmd/gotto-hando/dispatch_darwin.go` (new, `//go:build darwin`) and
   `cmd/gotto-hando/dispatch_other.go` (new, `//go:build !darwin`): each
   defines `newLocalBackend() (backend.Backend, error)` — darwin calls
   `darwin.New()`; other returns the current stub's error text
   (`"platform backend not implemented, nothing ran"`) so the abort message
   is unchanged for non-darwin builds.
10. `cmd/gotto-hando/dispatch.go` — replace the `opts.Dest == "local"`
    block (`dispatch.go:110-114`) with: `collectLines` (same call already
    used for `--check`/`--ir`, `dispatch.go:102`) → `parseAndValidate` →
    on diagnostics, `writeDiagnostics` + `return output.ExitValidation`
    (identical contract to `analyze`'s failure path) → `newLocalBackend()`,
    on error `return abort(output.EValidate, err.Error())` → compute
    `outDir := output.DefaultOutDir(opts.Out, opts.Dest, newRunID())` →
    `sum := engine.Run(context.Background(), be, seq,
    engine.RunOptions{KeepGoing: opts.KeepGoing, CapOnError:
    opts.CapOnError})` → if `sum.Aborted`, `return abort(sum.AbortCode,
    sum.AbortMsg)` (reuses the existing closure verbatim) → else
    `output.WriteStart(stdout, opts.JSONL, opts.Dest, outDir)`, loop
    `output.WriteResult(stdout, opts.JSONL, opts.Quiet, r)` over
    `sum.Results`, `output.WriteDone(stdout, opts.JSONL, sum.Done)`,
    `return sum.Exit`.

## Verification Plan

**Lock-independent (run now, on the locked/remote dev Mac — native
GOOS=darwin, so no cross-GOOS build-tag issue):**
- `go test ./internal/engine/...` — existing fail-fast/-k/held tests plus
  new Preflight-wiring tests (dryrun `FailOn` returns a
  `*backend.PreflightError` → `Summary.Aborted`/`AbortCode`/`Exit==4`; a
  non-coded error → `E_UNKNOWN`/exit 4) and new resolve()-bounds tests
  (an out-of-bounds `r`/`%` point → line `err` `E_BOUNDS`, exit 1, not an
  abort).
- `go test ./internal/backend/darwin/...` (native darwin) — key-code table
  completeness/aliases against the KEY NAMES list, `volup`/`voldown`/`mute`
  absent; `Preflight` tests with the injected fake probes: (a) session
  locked + only `qinfo`/`qdisp`/`qmouse`/`sleep`/`set` lines → preflight
  passes; (b) session locked + a `k` line → `E_SESSION`; (c) session ok,
  Accessibility missing + a `k` line → `E_PERMISSION`; (d) a held key
  reported by the fake `keyStateProbe` + a `k` line → `E_INPUT`.
- `go test ./...` (full suite, existing tests unaffected).
- End-to-end through the built CLI (native darwin, real syscalls, locked
  session): `go build ./cmd/gotto-hando && ./gotto-hando local qinfo` /
  `qdisp` / `qmouse` print live values (session should read `locked`);
  `./gotto-hando local 'k[]a'` on the locked session aborts (stderr
  `abort: ... (E_SESSION)`, exit 4).
- `go vet ./...` (native darwin).
- `GOOS=darwin GOARCH=amd64 go build ./...` and
  `GOOS=darwin GOARCH=arm64 go build ./...` (cross-compile check called out
  by the ticket).

**Interactive-injection (DEFERRED — requires an unlocked GUI session with
Accessibility granted; NOT run this phase):**
- `k`/`txt`/`m`/`c`/`drag`/`scroll`/`clip`/`paste` exercised against
  TextEdit.
- `qinfo`/`qdisp`/`qmouse` output cross-checked against System Settings >
  Displays.
This subset is explicitly out of this phase's verification pass per the
ticket; the executor should stop after the lock-independent subset and
report the interactive subset as pending until the Mac is unlocked with
permissions granted.

## Escalations

- None.
