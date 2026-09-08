# Plan: 260907-feat-darwin-backend — Phase 3: exec and open

## Relevant Ticket Contract

- Goals (ticket Phase 3): `exec` (argv split with `"..."` only — already done
  by the parser, `shell` login-shell path, `timeout` kill, `noerr`,
  stdout/stderr capture capped at 64 KiB each, exit code in the result line
  and JSONL); `open` (`wait=` polling for a window of the app through the
  Phase 2 window list and the shared 100 ms `pollForWindow` loop,
  `E_NOWINDOW` after the duration).
- Decision (ticket): `exec[shell]` runs `$SHELL -lc` (fallback `/bin/zsh -lc`
  when `$SHELL` is empty); `open` uses `/usr/bin/open -a <name>` or
  `/usr/bin/open <path>`.
- help.txt `exec` (:363-398): argv split without `shell` (no escapes/glob),
  `$SHELL -lc <payload>` with `shell`, cwd/env = the running process, stdin
  closed, stdout/stderr each capped at 64 KiB with `truncated=1` when
  exceeded, exit != 0 = `E_EXEC` unless `noerr` (then ok, exit=N reported,
  run continues), timeout kill = `E_TIMEOUT` and `noerr` never softens it,
  no default delay (`d=` still applies).
- help.txt `open` (:363-368): `open -a <name>` / `open <path>`, payload
  trimmed, any path allowed, launch failure = `E_EXEC`, `wait=DUR` waits for
  a window of the app (`E_NOWINDOW` after DUR), no default delay.
- help.txt OUTPUT multi-line `exec` format (:591-598): header
  `<n> ok exec exit=0 ms=41 stdout=34B stderr=0B`, then one
  `  <1|2>\t<text>` line per output line (1=stdout, 2=stderr, "in order of
  appearance, best effort"); output lines also follow err results
  (non-zero exit, timeout); with `noerr` a non-zero exit is still ok.
- help.txt JSONL example (:614-616): `{"line":6,"status":"ok","cmd":"exec",
  "exit":0,"stdout":"...\n","stderr":"","truncated":false,"t_ms":41}` — err
  objects never carry command-specific fields (generic `src`/`code`/`msg`
  only; confirmed by `internal/output/writer.go`'s `writeResultJSON`, which
  only appends `r.JSON` on the non-err branch).
- ERROR CODES (:718, :724): `E_NOWINDOW` = "win or open[wait=] found no
  window, or the OS refused to bring it to the front"; `E_EXEC` = "exec exit
  != 0 without noerr, spawn failure, open failure" — both branches of the
  Key Context's flagged question are already settled by the spec text, not
  ambiguous.
- help-macos.txt CAVEAT (:247-248): "`open[]Name` uses `open -a Name`
  (bundle name, case-insensitive); a path uses `open <path>`. A wrong name
  is E_EXEC." — does not state the app-name-vs-path heuristic; that is a
  binding decision this plan makes (see Escalations).
- LIMITS (:756-757): `exec timeout=` <= 60s, default 10s (already enforced
  by `internal/ir/validate.go` and `internal/syntax/build.go`'s
  `durVal(..., "timeout", 10000)`); `exec stdout/stderr` <= 64 KiB each.
- Verification boundary (ticket): `exec[]true`, `exec[]false` (E_EXEC),
  `exec[noerr]false`, `exec[shell]echo $PATH` under a LaunchAgent bridge,
  `exec[timeout=1s]sleep 5` (E_TIMEOUT), `open[wait=5s]TextEdit` then
  `qwin[]app:TextEdit` — all runnable except the LaunchAgent-PATH case and
  anything needing a real GUI window, which move to the HOME-VERIFICATION
  CHECKLIST (mirrors Phase 2's pattern).

## Out of Scope

- Windows exec/open (a later ticket; `internal/backend/windows/stubs.go`
  keeps `errNotThisPhase` for both — do not touch).
- Remote/ssh dispatch (`260908-feat-remote-ssh`).
- Any change to `backend.ExecReq`/`backend.ExecResult`'s field set — both
  already carry everything this phase needs (Key Context confirmed; see
  Escalations for the one place this constrains the design: stdout/stderr
  interleaving).
- Global `--timeout` (300s) wiring — `cmd/gotto-hando/dispatch.go:131` still
  passes `context.Background()`; exec's own `timeout=` is the only deadline
  this phase deals with.
- Re-litigating Phase 1/2 command sets (`k`/`m`/`win`/`cap`/etc.) beyond the
  one shared-infra fix this phase's own goal depends on (see Codebase
  Findings: HasWait).
- The manual checklist run of help.txt QUICK START/EXAMPLES against
  TextEdit — GUI-only, HOME-VERIFICATION CHECKLIST.

## Codebase Findings

- `internal/backend/darwin/stubs.go:20-26` — `Exec`/`Open` are the only two
  stubs left; both return `errNotThisPhase`. Delete the file once both are
  implemented (mirrors Phase 2's note that `win`/`qwin`/`cap` "are no longer
  stubbed").
- `internal/backend/backend.go:138-181` — `ExecReq{Argv,Cmd,Shell,Timeout}`
  and `ExecResult{Exit,Stdout,Stderr,Truncated,TimedOut}` already exist;
  `Open(ctx, target string) error` takes no wait parameter — wait= polling
  is entirely an engine-level concern (matches `win`'s split: `Focus` also
  takes no wait).
- `internal/engine/run.go:283-286` — current `KindOpen` case: `st.be.Open(ctx,
  op.Target)`, fail `E_EXEC` on error, otherwise silently ok. No wait=
  handling at all yet, confirming the Key Context.
- `internal/engine/run.go:366-384` (`doExec`) — already wired to darwin
  Preflight/dispatch (Phase 2 infra) but only a **skeleton**: on ok it sets
  `res.Detail = "exit=%d"` only — no `ms=`/`stdout=`/`stderr=` header, no
  `res.Extra` (no per-line stdout/stderr output), no `res.JSON` (no
  `exit`/`stdout`/`stderr`/`truncated` JSONL fields). This is a real gap
  against the OUTPUT/JSONL contract above, not just a darwin-backend
  wiring task — the engine-side formatting is unfinished Phase-3 work.
- **Risk signal (fail-closure value-capture footgun):** `execute()` builds
  `res := output.Result{...}` and `fail := func(code, msg) output.Result {
  res.Status="err"; ...; return res }` (run.go:180-186), then calls
  `st.doExec(ctx, op, res, fail)` — `res` is passed to `doExec` **by value**
  but `fail` still closes over `execute()`'s own `res` variable, not
  `doExec`'s local copy. `doFocus` never hits this because it only mutates
  its local `res` on the success path (already `return res` directly, never
  through `fail`, after mutating — see run.go:354-363). `doExec` **will**
  hit it once it needs to attach `Extra`/`JSON` to the `E_TIMEOUT` and
  non-zero-exit-without-`noerr` paths (help.txt: "Output lines also follow
  err results (non-zero exit, timeout)") — calling `fail(...)` after
  mutating a local `res` copy silently drops that data. `doExec` must set
  `res.Status/ErrCode/ErrMsg` directly on those two paths instead of routing
  through the passed-in `fail` closure (the true spawn-failure `err != nil`
  path is unaffected: no local mutation happens before it, so `fail(...)`
  stays correct there).
- **Confirmed latent bug — `op.HasWait` is never set:** `ir/ops.go:128`
  documents `HasWait` as "win/open/qwin: wait= was given"; it is read in
  `internal/engine/run.go:336` (`doFocus`'s `waitMS` gate) and in
  `internal/backend/darwin/preflight.go:56` /
  `internal/backend/windows/preflight.go:52` (Accessibility gating for
  `open[wait=]`) — but `grep -rn "HasWait\s*="` across the repo returns
  **zero assignments**. `internal/syntax/build.go`'s `plSelector` case
  (win, lines ~133-141) and `plTarget` case (open, lines ~159-168) both call
  `durVal(..., "wait", 0)` and set `op.WaitMS` but never `op.HasWait`. Net
  effect today: `win[wait=5s]` never actually polls (`doFocus`'s `waitMS`
  stays 0 regardless of the `wait=` value) and `open[wait=]` never triggers
  the Accessibility preflight gate. No test catches this: `grep -rn
  "HasWait"` across `*_test.go` is empty, and
  `internal/engine/window_test.go`'s only `win` tests
  (`TestFocusWindowDetail`, `TestFocusNoMatchIsNoWindow`) use `win[]...`
  with no `wait=`. Phase 2's own Result section deferred the interactive
  `win[wait=5s]` check to the (not-yet-run) HOME-VERIFICATION CHECKLIST, so
  this was never actually exercised end-to-end — it is being caught here,
  before that checklist runs, not after a false-positive pass. Phase 3's
  own goal ("open ... wait= polling ... through the shared 100 ms loop")
  cannot work correctly without fixing this, and fixing it also fixes
  `win[wait=]` as a side effect — both are the same shared mechanism the
  ticket explicitly says `open[wait=]` reuses. In scope for this phase
  (shared infra this phase's own goal depends on), not a Phase 1/2
  relitigation.
- `internal/syntax/build.go:133-141` (win `plSelector`), `:159-168` (open
  `plTarget`) — fix site: read `_, has := m.kvs["wait"]` before/alongside
  `durVal` and set `op.HasWait = has`.
- `internal/backend/darwin/preflight.go:23-33,56` — `accessibilityGated`
  map + `(op.Kind == ir.KindOpen && op.HasWait)` — once `HasWait` is fixed,
  this becomes reachable and correctly requires Accessibility for
  `open[wait=]`, matching the ticket Decision list. No test exists for this
  yet (`preflight_test.go` has no `KindOpen` case).
- `internal/engine/schedule.go:38-58` (`pollForWindow`) — reusable as-is;
  `waitMS <= 0` is already a single lookup with no polling, so `open`
  without `wait=` should simply never call it (see next finding).
- `internal/engine/run.go:331-364` (`doFocus`) — reference pattern for
  `doOpen`: `waitMS := 0; if op.HasWait { waitMS = op.WaitMS }`, then
  `pollForWindow(ctx, waitMS, realClock{}, func() ([]backend.Window, error)
  { return st.be.Windows(ctx, sel) })`. Key difference for `open`: `win`
  **always** looks up + focuses (waitMS may be 0, single lookup); `open`
  must **only** call `Windows`/poll when `op.HasWait` is true — without
  `wait=`, `open` does nothing after `Open()` succeeds (help.txt: "No
  default delay ... use open[wait=5s] ... to wait for the UI"). `open` also
  must **not** call `Focus` and must **not** set `st.window` — only `win`
  "becomes the current window for w frames and cap[w]" (help.txt:356-357);
  open is never mentioned there.
- `internal/backend/darwin/windows.go:308-323` (`selectorMatches`) — `app`
  selector is a case-insensitive **substring** match against
  `w.App` (`kCGWindowOwnerName`). This is what any selector `doOpen` builds
  will be matched against.
- `internal/ir/ops.go:126-128` (`Selector`) is **not** populated by the
  parser for `KindOpen` (only `WaitMS`/`HasWait`/`Target`,
  `internal/syntax/build.go:159-168`) — the engine must derive a
  `ir.Selector{Kind:"app", Value:...}` itself from `op.Target` for the poll
  (the binding decision flagged in Escalations).
- `internal/output/writer.go:82-120` — confirms: `r.Extra` prints
  unconditionally regardless of `r.Status` (plain mode); `r.JSON` is
  appended only on the non-err branch (JSONL) — so JSONL err objects need
  no exec-specific handling at all, only plain-mode `Extra` needs to survive
  the err paths.
- `internal/engine/query.go:43-54,91-98` — existing per-command formatting
  pattern to mirror: 2-space-indented, tab-separated `Extra` lines
  (`"  %d\t%s", ...`) and a paired `...JSON(...) []output.KV` helper. Model
  the new exec formatting helpers on this file's shape.
- `internal/backend/dryrun/dryrun.go:28-30,111-119` — the engine-level test
  double already supports injecting `ExecResult` (`Stdout`/`Stderr`/
  `Truncated`/`Exit`/`TimedOut`) and `WindowsResult`/`Windows(sel)`
  (records `"Windows %s:%s", sel.Kind, sel.Value"`), so both `doExec`'s
  formatting and `doOpen`'s selector/poll logic are testable at the engine
  level without touching darwin — no dryrun changes needed.
- `internal/backend/darwin/*.go` file pairing (`windows.go`/
  `windows_test.go`, `capture.go`/`capture_test.go`, etc.) and Phase 2's
  "the function is the seam" pattern (pure `filterWindows`/`selectorMatches`
  extracted and unit-tested without FFI) — the model to follow for
  `exec.go`/`open.go`: keep `os/exec` plumbing thin, extract pure helpers
  (output capping, timeout/exit classification, open argv building) for
  direct unit tests.
- `internal/backend/darwin/preflight_test.go` — no `KindOpen` case exists
  yet; add one once `HasWait` is fixed.
- `internal/engine/timing.go:12-22` (`noDefaultDelay`) already includes
  `KindExec`/`KindOpen` — no change needed, confirms "No default delay" is
  already handled.
- `internal/ir/validate.go:114-121` — `exec timeout=` and `open wait=` LIMITS
  checks (<=60s) already exist; no IR/validate change needed.

## Implementation Plan

1. **Fix `HasWait` wiring** (`internal/syntax/build.go`): in the `plSelector`
   case (win) and the `plTarget` case (open), set `op.HasWait` from whether
   `"wait"` is present in `m.kvs`, e.g. `_, hasWait := m.kvs["wait"];
   op.HasWait = hasWait`, keeping the existing `durVal(..., "wait", 0)` call
   for `op.WaitMS`. Add a regression assertion in `internal/syntax/parse_test.go`
   (or wherever IR field assertions live) that `win[wait=5s]...` and
   `open[wait=5s]...` produce `HasWait == true` and a plain `win[]...` /
   `open[]...` produce `HasWait == false`.
2. **darwin `exec.go`** (new file, replaces the `Exec` stub in
   `internal/backend/darwin/stubs.go`): implement
   `func (b *Backend) Exec(ctx context.Context, req backend.ExecReq)
   (backend.ExecResult, error)` using `os/exec` (no FFI, `CGO_ENABLED=0`
   already satisfied):
   - Build argv: `req.Shell` → `[$SHELL or "/bin/zsh", "-lc", req.Cmd]`;
     else `req.Argv` directly (already split by the parser).
   - `if req.Timeout > 0 { ctx, cancel := context.WithTimeout(ctx,
     req.Timeout); defer cancel() }`, then `exec.CommandContext(ctx,
     argv[0], argv[1:]...)`.
   - `cmd.Stdin = nil` (default already reads from the null device per Go
     docs — matches "stdin is closed").
   - Two capped writers (`cmd.Stdout`, `cmd.Stderr`), each capping at
     65536 bytes and setting its own `truncated` flag, dropping excess
     silently (never erroring the underlying `Write` so `cmd.Wait` doesn't
     fail on truncation).
   - `cmd.Start()` failure → return `(backend.ExecResult{}, err)` (spawn
     failure; the engine's existing `err != nil → E_EXEC` path handles it
     unchanged, help.txt E_EXEC "spawn failure").
   - `cmd.Wait()`: if `ctx.Err() == context.DeadlineExceeded` → return
     `(ExecResult{TimedOut:true, Stdout:..., Stderr:..., Truncated:...},
     nil)` — **must return a nil error** here, since
     `internal/engine/run.go:370-372`'s `if err != nil { return
     fail(EExec,...) }` runs before the `r.TimedOut` check and would
     misreport `E_TIMEOUT` as `E_EXEC` otherwise.
   - Else if `errors.As(err, &exitErr)` (`*exec.ExitError`) → return
     `(ExecResult{Exit: exitErr.ExitCode(), Stdout:..., Stderr:...,
     Truncated:...}, nil)` — again nil error; a non-zero exit is not a Go
     error from this backend's point of view, only `r.Exit != 0` signals
     it, so `noerr` can soften it downstream.
   - Else (`err == nil`) → `Exit: 0`.
   - Any other unexpected `Wait` error (not an `*exec.ExitError`, not a
     deadline) → return it as the `error` (falls into `E_EXEC` generically).
3. **darwin `open.go`** (new file, replaces the `Open` stub): implement
   `func (b *Backend) Open(ctx context.Context, target string) error`:
   - Extract a pure `buildOpenArgv(target string) []string` helper (the
     "function is the seam" pattern): `if strings.Contains(target, "/") {
     return []string{"/usr/bin/open", target} } else { return
     []string{"/usr/bin/open", "-a", target} }` (mirrors help-macos.txt
     CAVEAT :247-248's `open -a Name` / `open <path>` split; a bare word
     with no `/` is treated as a bundle name).
   - `Open` itself: `exec.CommandContext(ctx, argv[0], argv[1:]...).Run()`;
     a non-nil error (nonzero exit or spawn failure — `/usr/bin/open`
     itself always exists, so this is effectively "wrong name"/"bad path")
     becomes the returned error, which the engine's existing `case
     ir.KindOpen` already maps to `E_EXEC` (help-macos.txt "A wrong name is
     E_EXEC").
   - Unit-test only `buildOpenArgv` (pure, no subprocess) — do not invoke
     the real `/usr/bin/open` in `go test` (headless-safe, matches the
     Phase 2 "pure function" pattern); real launch behavior is
     HOME-VERIFICATION only.
4. **Delete `internal/backend/darwin/stubs.go`** once both stubs are
   replaced (file would otherwise be left with just the `errNotThisPhase`
   var and no users).
5. **Engine: `doExec`** (`internal/engine/run.go:366-384`) — rewrite:
   - Time the `st.be.Exec` call locally (`start := time.Now()` before, `ms
     := time.Since(start).Milliseconds()` after) — the plain-mode header's
     `ms=` is a doc-specified field distinct from the generic `TMS`/`t_ms`
     (which `writeResultPlain` never prints; only `writeResultJSON` uses
     it), so it must be built into `Detail` here.
   - `err != nil` (spawn failure, no output captured yet) → unchanged:
     `return fail(output.EExec, err.Error())`.
   - Once `r` is back, build `res.Detail`, `res.Extra`, `res.JSON` from `r`
     (new helpers, step 6) **before** branching on `TimedOut`/`Exit`.
   - `r.TimedOut` → set `res.Status/ErrCode/ErrMsg` directly (**not** via
     `fail(...)`, per the Codebase Findings footgun) and `return res`.
   - `r.Exit != 0 && !op.Noerr` → same: set fields directly, `return res`.
   - Else → `res.AlwaysShow = true; return res`.
6. **New engine helpers** (new file `internal/engine/exec.go`, or appended
   to `query.go` if the executor prefers one fewer file — either is fine,
   mirror `query.go`'s shape either way):
   - `formatExecDetail(r backend.ExecResult, ms int64) string` → `"exit=%d
     ms=%d stdout=%dB stderr=%dB"`.
   - `formatExecOutputLines(r backend.ExecResult) []string` → split
     `r.Stdout` on `"\n"` (drop a trailing empty element from a final
     newline) tagged `1`, then `r.Stderr` lines tagged `2`, each rendered
     `"  %d\t%s"` (2-space indent + tab, matching `query.go`'s convention)
     — concatenated in that fixed stdout-then-stderr order (see
     Escalations: this is a simplification of "in order of appearance,
     best effort", not true chronological interleaving).
   - `execJSON(r backend.ExecResult) []output.KV` → `{"exit", r.Exit},
     {"stdout", r.Stdout}, {"stderr", r.Stderr}, {"truncated",
     r.Truncated}` (order matches the help.txt JSONL example).
7. **Engine: `doOpen`** (new method in `internal/engine/run.go`, replacing
   the inline `case ir.KindOpen:` at :283-286):
   ```go
   func (st *engineState) doOpen(ctx context.Context, op *ir.Op, res output.Result, fail func(output.ErrorCode, string) output.Result) output.Result {
       if err := st.be.Open(ctx, op.Target); err != nil {
           return fail(output.EExec, err.Error())
       }
       if !op.HasWait {
           return res
       }
       sel := openAppSelector(op.Target)
       wins, err := pollForWindow(ctx, op.WaitMS, realClock{}, func() ([]backend.Window, error) {
           return st.be.Windows(ctx, sel)
       })
       if err != nil {
           return fail(output.ENoWindow, err.Error())
       }
       if len(wins) == 0 {
           return fail(output.ENoWindow, fmt.Sprintf("no window of %q appeared", op.Target))
       }
       return res
   }
   ```
   Deliberately does **not** call `st.be.Focus` and does **not** set
   `st.window` (see Codebase Findings — only `win` does either).
   `case ir.KindOpen:` in `execute()` becomes `return st.doOpen(ctx, op,
   res, fail)`.
8. **`openAppSelector` helper** (same new file as step 6, or `run.go`):
   ```go
   func openAppSelector(target string) ir.Selector {
       name := target
       if strings.Contains(target, "/") {
           name = filepath.Base(strings.TrimSuffix(strings.TrimRight(target, "/"), ".app"))
       }
       return ir.Selector{Kind: "app", Value: name}
   }
   ```
   (see Escalations for the rationale and the known limitation for a
   non-`.app` file/URL path).
9. **Tests**:
   - `internal/backend/darwin/exec_test.go`: `exec[]true` → exit 0;
     `exec[]false`/`exec[]/bin/nonexistent-xyz` → nonzero exit / spawn
     error; `noerr` handling is the engine's job, not this backend's, so
     just assert `ExecResult.Exit`/error shape; a `timeout=` case using
     something like `sleep 2` with a 200ms timeout asserts `TimedOut==true`
     and `err==nil`; a large-output case (e.g. `yes | head -c 200000`
     equivalent via a small helper script or `printf` loop) asserts
     `Truncated==true` and `len(Stdout)==65536`; `shell` case asserts
     `$SHELL -lc` argv shape (can stub via a fake `$SHELL` env var pointing
     at a test script, or just assert the built argv via a small internal
     helper if `exec.go` factors argv-building out similarly to `open.go`).
   - `internal/backend/darwin/open_test.go`: table test for
     `buildOpenArgv` (bundle name → `-a`, absolute path → direct,
     `.app` path → direct, path with spaces, empty already rejected by the
     parser so not a case here).
   - `internal/engine/exec_test.go` (or extend `engine_test.go`): using
     `dryrun.Backend{ExecResult: ...}`, assert `Detail`/`Extra`/`JSON` for
     ok/err(exit)/err(noerr-softened)/err(timeout) cases, and specifically
     that `Extra` (stdout/stderr lines) survives the two err paths (this is
     the regression test for the fail-closure footgun in Codebase
     Findings).
   - `internal/engine/open_test.go` (or extend `window_test.go`): using
     `dryrun.Backend{WindowsResult: ...}`, assert `open[]Name` never calls
     `Windows` (no wait=), `open[wait=50ms]Name` with a populated
     `WindowsResult` succeeds without polling delay, `open[wait=50ms]Name`
     with `WindowsResult: nil` returns `E_NOWINDOW` after ~50ms (small
     value to keep the test fast — this really sleeps via `realClock{}`
     like `doFocus`'s existing untested wait path; keep it short), and a
     table test for `openAppSelector` (bare name unchanged, `.app` path →
     basename minus suffix, generic file path → best-effort basename).
   - `internal/backend/darwin/preflight_test.go`: add
     `TestPreflightOpenWaitRequiresAccessibility` (open with `wait=` and no
     Accessibility → `E_PERMISSION`) and confirm `open[]Name` (no `wait=`)
     needs no Accessibility (mirrors the existing
     `TestPreflightWindowOpsPassWithoutScreenRecording`-style setup).
   - `internal/ir` / `internal/syntax`: the `HasWait` regression assertion
     from step 1.

## Verification Plan

- `go build ./...` (all GOOS, especially `GOOS=windows go build ./...` stays
  green — the darwin files are `//go:build darwin`; windows stubs
  untouched).
- `go vet ./...`.
- `go test ./... -race` — fully runnable headless/locked on this dev Mac:
  `exec` uses only `os/exec` subprocess calls (no GUI/session needed); the
  new engine-level `doExec`/`doOpen` tests use `dryrun`, no darwin FFI.
  Specifically confirm: `exec[]true`/`exec[]false`/`exec[noerr]false`/
  `exec[timeout=1s]sleep 5` (or a shorter timeout for test speed) match the
  ticket's own verification list; `openAppSelector` and `buildOpenArgv`
  table tests; the `HasWait` parser regression; the fail-closure `Extra`
  survival regression; the new `preflight_test.go` open-wait case.
- Cross-compile `darwin/amd64` + `darwin/arm64` clean (matches Phase 1/2
  convention).
- `gotto-hando local 'exec[]true'` / `'exec[]false'` /
  `'exec[noerr]false'` / `'exec[timeout=1s]sleep 5'` end-to-end through the
  real CLI on this dev Mac (exec needs no GUI session — confirm the
  per-command Preflight gating in `exemptFromSession`/`accessibilityGated`
  still lets a pure-`exec` run proceed even when `session=locked`, per the
  ticket Decision "`clip`, `qclip`, `exec`, `open` (without `wait=`) need no
  permission" — this is existing Phase 1 logic, just confirm it still holds
  with the new real `Exec`/`Open`).
- HOME-VERIFICATION CHECKLIST addition (run together with the Phase 2
  checklist, unlocked GUI session, Accessibility granted):
  - `exec[shell]echo $PATH` under a LaunchAgent-started `gotto-hando
    --bridge` (or equivalent) shows the login-shell PATH, not the reduced
    launchd PATH.
  - `open[wait=5s]TextEdit` followed by `qwin[]app:TextEdit` succeeds and
    the window is visible.
  - `open[wait=2s]<an app name that never launches>` returns `E_NOWINDOW`
    after ~2s.
  - `open[]<a bogus bundle name>` (no `wait=`) returns `E_EXEC` immediately.
  - The manual checklist run of help.txt QUICK START and EXAMPLES against
    TextEdit (ticket's own closing verification item, now that every
    command they use is implemented).
  - Confirm `win[wait=5s]<title>` (Phase 2, previously deferred) now
    actually waits ~5s instead of returning instantly, now that `HasWait`
    is fixed.

## Escalations

- Confidence: high overall; the two items below are binding decisions this
  plan makes (not contract ambiguities blocking implementation) — recorded
  here for lead visibility as the delegation asked, not as a research
  hand-off.
- **Binding decision — open target → window selector correlation.** The
  ticket does not specify how `open[wait=]`'s app-window match is derived
  from `Target` (a free-form `app | path` string with no parser-level
  distinction, `internal/syntax/build.go:159-168`). Resolution used in this
  plan (`openAppSelector`, step 8): a bare name (no `/`) is used verbatim as
  an `app`-kind selector value (matches the ticket's own verification
  example, `open[wait=5s]TextEdit`); a path is reduced to its base name with
  a trailing `.app` stripped (`/Applications/TextEdit.app` → `TextEdit`).
  **Known limitation:** a plain non-`.app` file or URL target (e.g.
  `open[wait=5s]./notes.txt`) has no statically-derivable app name — the
  basename-based guess will usually not match the real default-handler
  app's window, so `open[wait=]` on such a target will likely hit
  `E_NOWINDOW` even after the file opens successfully. A correct fix would
  need a runtime "what app opens this file" lookup (e.g. AppKit's
  `NSWorkspace URLForApplicationToOpenURL:`), which is materially more FFI
  surface than this phase's "no cgo, os/exec only" scope and isn't asked
  for by the ticket text or its verification list (which only exercises the
  bare-app-name case). Recommend accepting this limitation for v1 and
  documenting it as a `help-macos.txt` CAVEAT follow-up if the lead wants it
  spec'd (ticket's own Spec Impact says "None expected"; this plan does not
  add one, since it wasn't requested).
- **Confirmed, not ambiguous — E_NOWINDOW vs E_EXEC split.** The Key
  Context asked to flag "whether open[wait=] without a match after DUR is
  E_NOWINDOW while open without wait= that fails to launch is E_EXEC" — this
  is already settled by help.txt ERROR CODES verbatim (:718 "win or
  open[wait=] found no window"; :724 "spawn failure, open failure"), not a
  judgment call. Recorded here for completeness only.
- **Design simplification — exec stdout/stderr interleaving.**
  `backend.ExecResult` carries two flat strings (`Stdout`, `Stderr`), not an
  ordered per-chunk log, so true "in order of appearance" interleaving
  across streams (help.txt OUTPUT :591-593) is not reconstructable at the
  engine layer without a `backend.ExecResult`/`ExecReq` shape change (a
  Backend interface change, which the Key Context said is not expected this
  phase — "ExecReq/ExecResult already exist"). This plan renders all stdout
  lines before all stderr lines (`formatExecOutputLines`, step 6), reading
  "best effort" as license for this grouping rather than true
  chronological interleaving. If the lead wants true interleaving, that
  needs a `backend.ExecResult` field addition (e.g. an ordered
  `[]ExecOutputLine{Src int; Text string}`) plumbed through darwin's
  `exec.go` via a mutex-guarded recorder shared between the stdout/stderr
  writers — flagging as a scope question, not implementing it speculatively.
