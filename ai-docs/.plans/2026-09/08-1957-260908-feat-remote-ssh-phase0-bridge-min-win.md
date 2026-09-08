# Plan: 260908-feat-remote-ssh — Phase 0: Minimal Windows session bridge (verification enabler)

## Relevant Ticket Contract

- `gotto-hando --bridge`: resident process on named pipe `\\.\pipe\gotto-hando-<username>`
  (current-user ACL), one run at a time, a second instance for the same user exits 2,
  one line per run/error logged to `%LOCALAPPDATA%\gotto-hando\bridge.log` (no sequence
  text, no images). Protocol: one IR JSON document with the extra top-level `run`
  object in, the same JSONL out (`start`, per-line objects, `done`/`abort`); IR `"v"`
  mismatch = abort E_CONNECT (ticket Decisions; Phase 0 Goals).
- `gotto-hando local` ssh-session detection (process session != active console session,
  or `SSH_CONNECTION`/`SSH_TTY` set): forward the IR to the bridge and never execute
  input in-process; no bridge reachable = abort E_SESSION after `start`, exit 4, with
  the help.txt hint (`"start `gotto-hando --bridge` in the logged-on GUI session"`,
  help-windows.txt:72-73). The `qinfo`/`qdisp`/`qmouse`/`sleep`/`set`/comments-only run
  is the exception: answered in-process without a bridge, reporting `session=inactive`.
- `qinfo` through the bridge reports `session=bridge` (help-windows.txt CHECK :17-19;
  preflight.go already accepts `session=="bridge"`).
- The bridge aborts the run and releases held keys when the caller disconnects
  (Decisions: "the bridge aborts the run when its caller disconnects"; STATE MACHINE
  help.txt:528-529 "released ... when the run ends, fails, times out or is
  interrupted").
- Wire protocol shape (help-remote.txt SESSION BRIDGE :122-132): caller writes ONE IR
  JSON document (help.txt IR JSON) plus a top-level `"run"` object
  `{"deadline_ms":N,"keep_going":b,"quiet":b,"cap_on_error":b}`, then half-closes; the
  bridge answers with the same JSONL it would print on stdout with
  `--inline-captures`, ending with `done` — or an `abort` object when preflight fails
  or the IR `"v"` is not the bridge's (abort E_CONNECT) — and closes. One run at a
  time: a second caller waits.
- Endpoint/ACL: `\\.\pipe\gotto-hando-<username>`, ACL current-user only, no network
  reachability (help-remote.txt SESSION BRIDGE :118-121).
- Verification boundary (ticket Phase 0 Verification paragraph, and prompt): unit/
  integration with an in-process or fake-pipe listener where a desktop is not
  required; then autonomous over ssh: `local qinfo` -> `session=bridge`; a
  `txt[]...`/`k[c]a`/`k[c]c`/`qclip` round-trip confirms injected text landed (no
  `cap` needed); bridge stopped -> `start` then `abort` E_SESSION, exit 4, while a
  `qinfo`-only run still answers `session=inactive`; a caller that drops the
  connection mid-run holding a key has it released (visible in bridge.log and a
  following `qinfo`).

## Out of Scope

- The ssh-wrapped `<dest>` transport and its whole forwarded-option /
  `--expect-version` / `[f]`-inlining / capture-rewrite / `start`-timing surface
  (Phase 1).
- The macOS unix-socket bridge, `--request-perms` over the bridge, LaunchAgent /
  Task Scheduler recipes, the byte-identical-output guarantee (Phase 1/2).
- `cap` (capture) through the bridge: no inline-PNG decode or local file writing is
  built this slice (matches "capture rewrite" being explicitly Phase 1/2, and the
  ticket's own verification text: "no `cap` needed"). A forwarded run that happens to
  contain `cap` still executes on the bridge machine (nothing crashes), but the local
  forwarder's plain/JSONL reconstruction of that one line is best-effort (generic
  line/status/cmd only) rather than a fully rewritten local path — acceptable per the
  above, and consistent with Phase 1 owning "capture rewrite".
- `--timeout` / `deadline_ms` enforcement: not wired into `engine.Run` anywhere in the
  codebase today (`cmd/gotto-hando/dispatch.go:41-44` — "`--timeout` deadline
  enforcement stays inert"). The bridge decodes `run.deadline_ms` for wire
  completeness but does not enforce it; this is a pre-existing gap, not a Phase 0
  regression.
- macOS/other-GOOS bridge listener: `--bridge` on those platforms keeps today's
  behavior unchanged (abort E_VALIDATE "session bridge not implemented", exit 2).

## Codebase Findings

- `internal/ir/json.go` (whole file) — `Marshal(seq *ir.Sequence)` renders the IR JSON
  document; **no `Unmarshal`/decode exists anywhere in the repo**
  (`grep -rn Unmarshal internal/ir` only matches a test). The bridge protocol needs a
  JSON -> `ir.Sequence` decoder; this is net-new, not reuse. It must mirror
  `opNode()`'s per-`Kind` field switch (`internal/ir/json.go:46-148`) field-for-field
  so `Marshal(Unmarshal(Marshal(seq))) == Marshal(seq)`.
- `internal/ir/ops.go:115-189` — `Op` is a wide, kind-tagged struct with no
  `encoding/json` struct tags (the whole package hand-builds ordered JSON via the
  `kv`/`node` encoder, `internal/ir/json.go:190-259`). The decoder must be written by
  hand against the same field names `opNode` emits, not derived from tags.
- `internal/engine/run.go:87-168` (`Run`) — **batches**: it appends every line's
  `output.Result` to `sum.Results` and returns one `Summary` only after the whole
  sequence finishes; nothing is streamed as lines complete, and `ctx` is threaded
  through every backend call but is **never checked** except inside
  `sleepCtx` (`internal/engine/timing.go:39-47`), which only stops an inter-line
  delay early — it does not stop the loop. Every existing call site passes
  `context.Background()` (`grep -rn "engine.Run(" internal engine` — one real
  call site, `cmd/gotto-hando/dispatch.go:131`, plus every `_test.go`). Streaming
  per-line JSONL to the bridge's pipe connection, and detecting "caller disconnected"
  from a failed write mid-run, both require a real per-line hook and a real
  cancellation check — see Implementation Plan step 2.
- `internal/engine/run.go:266-272` (`KindQueryClip` case) — sets `res.Detail = s` for
  plain output but **never sets `res.JSON`**, so `qclip`'s clipboard text is silently
  absent from JSONL output today. help.txt (`:632-636`, `--inline-captures`) says
  "qclip[f] likewise returns `'text'` and no path" — this is a pre-existing bug that
  blocks the ticket's own verification round-trip (`txt`->`k[c]a`->`k[c]c`->`qclip`
  read back through the JSONL-only bridge wire). Must fix as part of this slice
  (small, in-package, matches documented contract — not scope creep).
- `internal/output/writer.go:171-182` (`WriteAbort`) — always calls `WriteStart`
  internally before writing the abort object/line. The local forwarder needs to print
  its OWN `start` object as soon as it knows the bridge run began (using its own
  `dest`/`outDir`, not the bridge's — see below), then later relay a bridge-side
  `abort`; calling `WriteAbort` a second time would double-print `start`. Needs a
  small additive refactor: extract the "write just the abort object/line" half into an
  exported `WriteAbortEvent`, with `WriteAbort` becoming `WriteStart` +
  `WriteAbortEvent` (pure refactor, no behavior change for existing callers —
  `cmd/gotto-hando/dispatch.go:79-87`'s `abort` closure keeps working unchanged).
- `internal/output/codes.go:44-53` (`AbortExit`) — already implements exactly the
  E_VALIDATE->2, E_CONNECT->3, else->4 mapping the ticket specifies; reuse directly,
  no change needed.
- `internal/backend/backend.go` (`Backend` interface) + `internal/backend/
  preflight_error.go` (`PreflightError{Code, Msg}`) — the bridge runs IR through
  `engine.Run(ctx, be, seq, opt)` exactly like `cmd/gotto-hando/dispatch.go:131-137`
  does today; `Run` already turns a `*backend.PreflightError` into `Summary{Aborted:
  true, AbortCode, AbortMsg, Exit}` (`internal/engine/run.go:87-97`). Reuse unchanged.
- `internal/backend/windows/probes.go:22-27,50-53` (`sessionProbe` interface,
  `realSessionProbe`) — `Backend.session` is an unexported, package-private,
  directly-settable field (`internal/backend/windows/backend.go:20-37`), the same
  "probe is the seam" pattern used by darwin. `qinfo` through the bridge needs
  `Info().Session == "bridge"`; the clean fit is a new `bridgeSessionProbe{}`
  (`State() string { return "bridge" }`) and an exported `NewBridge() (*Backend,
  error)` constructor mirroring `New()` (`internal/backend/windows/backend.go:31-37`)
  but with `session: bridgeSessionProbe{}` — `keys`/`displays` stay the real probes
  since the bridge process genuinely IS in the interactive session.
- `internal/backend/windows/preflight.go:44-60` — check 1 already does
  `if session != "active" && session != "bridge"`, so `NewBridge()`'s Backend passes
  Preflight with zero changes there. `preflight.go:14-22` (`exemptFromSession`) is the
  exact kind-set ("qinfo qdisp qmouse sleep set") the ticket calls the "answered
  in-process without a bridge" exception; it is unexported and inline inside
  `Preflight`. `local`'s own routing decision (forward vs. run in-process) needs the
  identical kind-set check *before* even building a backend, so this should be
  factored into an exported helper (e.g. `RequiresSession(seq *ir.Sequence) bool`)
  that `Preflight` calls internally and `cmd/gotto-hando`'s dispatch also calls — one
  source of truth instead of duplicating the kind list.
- `internal/backend/windows/session.go:36-66` (`sessionState`) — already has every
  primitive the "is this process remote" check needs: `windows.
  ProcessIdToSessionId`, `windows.WTSGetActiveConsoleSessionId`,
  `openInputDesktop()`/`desktopName()` (WinSta0 accessibility). The routing check
  ("process session != active console session, OR SSH_CONNECTION/SSH_TTY set") is
  *not* the same predicate as `sessionState() == "locked"` (a locked LOCAL session
  must NOT route to the bridge) — needs a small new exported function, e.g.
  `IsRemoteSession() bool`, built from `os.Getenv("SSH_CONNECTION")`/`SSH_TTY` plus
  the existing session-id/WinSta0 primitives (not the "Default"/"Winlogon" desktop-
  name branch, which only matters once already known to be the console session).
- `internal/backend/dryrun/dryrun.go` (whole file) — a plain, no-build-tag,
  call-recording `Backend`, "NEVER selectable from the CLI ... only internal/engine
  and its tests ... import it" (file header comment). This is the exact fake needed
  to unit-test the bridge core (protocol framing, single-run gating, disconnect ->
  held-key release, `session=bridge` via a small `InfoResult.Session = "bridge"`
  override) without any real backend or GOOS constraint — importable from a new
  GOOS-agnostic `internal/bridge` package's tests.
- `cmd/gotto-hando/dispatch.go:89-91` — `--bridge` currently short-circuits to
  `abort(output.EValidate, "session bridge not implemented")` inline, unconditional
  on GOOS. Needs to become a per-GOOS dispatch (`runBridge(...)`), matching the
  existing `newLocalBackend()`/`requestPerms()` per-GOOS split
  (`dispatch_darwin.go`/`dispatch_windows.go`/`dispatch_other.go`).
- `cmd/gotto-hando/dispatch.go:113-145` (`dest == "local"` branch) — the exact place
  the forward-vs-in-process routing decision must be inserted, *before*
  `newLocalBackend()` is called (a remote-detected run must never construct/touch the
  real windows backend for non-exempt commands). `parseAndValidate`
  (`cmd/gotto-hando/analyze.go:17`) and `collectLines` (`cmd/gotto-hando/lines.go:17`)
  already produce the `*ir.Sequence` this slice needs to re-serialize via
  `ir.Marshal` and send to the bridge — no change needed there.
- `cmd/gotto-hando/options.go:46,94,152-153` — `--bridge` parsing (`opts.Bridge`)
  already exists and is wired into `parseArgs`; nothing to add there.
- `go.mod` — `golang.org/x/sys v0.47.0` is already a dependency. Confirmed present in
  the vendored module (`$(go env GOPATH)/pkg/mod/golang.org/x/sys@v0.47.0/windows`):
  `CreateNamedPipe`, `ConnectNamedPipe`, `DisconnectNamedPipe`, `CreateFile`,
  `SetNamedPipeHandleState`, `GetNamedPipeInfo` (`zsyscall_windows.go`);
  `SecurityDescriptorFromString(sddl string) (*SECURITY_DESCRIPTOR, error)` and
  `SecurityAttributes{Length, SecurityDescriptor, InheritHandle}`
  (`security_windows.go:924-928,1452-1460`); `Token.GetTokenUser()` and `SID.String()`
  (`security_windows.go:709,232`) to build the current-user SDDL ACL; pipe constants
  `PIPE_ACCESS_DUPLEX`, `FILE_FLAG_FIRST_PIPE_INSTANCE`, `PIPE_WAIT`
  (`types_windows.go:134,3473-3483`). **No new dependency (e.g.
  `github.com/Microsoft/go-winio`) is needed** — this resolves the prompt's primary
  open technical question.
- `internal/backend/windows/backend.go:76-90` (`elevatedBit`),
  `internal/backend/windows/session.go` — existing precedent for calling
  `golang.org/x/sys/windows`'s own high-level wrappers directly (not through the
  hand-rolled `ffi.go` LazyDLL proc table, which is reserved for APIs `x/sys/windows`
  doesn't wrap, e.g. `OpenInputDesktop`, `GetAsyncKeyState`, `EnumDisplayMonitors`).
  The new named-pipe code should follow the same convention: call
  `windows.CreateNamedPipe` etc. directly, no `ffi.go` addition needed.
- `cmd/gotto-hando/main.go` — `main()` has no signal handling at all today
  (`os.Exit(run(...))`). `--bridge` "run ... until interrupted (Ctrl-C) or killed ...
  Exit 0 on Ctrl-C" (help.txt:83-88) needs `signal.Notify(os.Interrupt)` wiring local
  to the bridge dispatch path (windows-only for this slice), not a `main()` change.

## Implementation Plan

1. **`internal/ir`: add a JSON decoder.** New file `internal/ir/json_decode.go`:
   `Unmarshal(data []byte) (*Sequence, error)`. Decode top-level `v`/`ops`/`defaults`
   generically (`ops` as `[]json.RawMessage`), then decode each op via a per-`op`
   (Kind) switch mirroring `opNode()` (`internal/ir/json.go:46-148`) field-for-field
   (selector/point/rect/keys sub-decoders mirror `selectorNode`/`pointNode`/
   `rectNode`/`keysNode`). Ignore unknown top-level keys (so the caller can pass the
   whole bridge request body, `run` object included, straight through). Add
   `internal/ir/json_decode_test.go`: round-trip every `Kind` through
   `Marshal(Unmarshal(Marshal(seq)))` and assert byte-equality; a malformed/missing
   `"v"` case; an unknown `"op"` value case (decode error, not panic).

2. **`internal/engine`: cancellation + streaming hook.** Edit
   `internal/engine/run.go`:
   - Add `OnResult func(output.Result)` to `RunOptions` (nil-safe, called
     synchronously right after each line's `Result` is computed, before the
     inter-line delay — this is also the fix for the pre-existing "results aren't
     streamed" gap noted in Codebase Findings, scoped here only to what the bridge
     needs).
   - At the top of the per-op loop (`internal/engine/run.go:111-117`), before the
     existing fail-fast skip check, add an unconditional `ctx.Err() != nil` check
     (independent of `-k`/`KeepGoing` — once the caller is gone, "keep going" is
     moot) that marks the rest as `skip` and stops, exactly like fail-fast already
     does. `releaseAll` at the end of `Run` (`internal/engine/run.go:150-158`) already
     runs unconditionally, so no other change is needed to satisfy "releases held
     keys when its caller disconnects."
   - Fix the `KindQueryClip` case (`internal/engine/run.go:266-272`) to also set
     `res.JSON = []output.KV{{Key: "text", Val: s}}`, matching help.txt's documented
     "qclip[f] ... returns `'text'`" contract. Add/extend a test in
     `internal/engine/query_test.go` asserting the JSON field is present.
   - Add `internal/engine/engine_test.go` cases: (a) a cancelled `ctx` (via
     `context.WithCancel`, cancel after N `dryrun.Backend` calls using `FailOn` as
     the trigger point, or a wrapping `OnResult` that cancels after line 1) stops
     before the remaining ops execute and releases a still-held key/button (assert
     via `dryrun.Backend.Calls` containing the matching `KeyUp`/`ButtonUp`); (b)
     `OnResult` is invoked once per completed line, in order, with the same `Result`
     that ends up in `Summary.Results`.
   - This is a public-API addition to a shared package (`engine.RunOptions`) used by
     `cmd/gotto-hando`'s existing local-run path too — backward compatible (nil
     `OnResult`, an uncancelled `context.Background()` never trips the new check), but
     flag it to the lead as a cross-module interface change per the project's
     Approval Protocol before landing.

3. **`internal/output`: split `WriteAbort`.** Refactor
   `internal/output/writer.go:171-182`: extract the "write just the abort
   object/line" half into an exported `WriteAbortEvent(w io.Writer, jsonl bool, code
   ErrorCode, msg string) error`; `WriteAbort` becomes `WriteStart` +
   `WriteAbortEvent`. No behavior change for existing callers
   (`cmd/gotto-hando/dispatch.go`'s `abort` closure). Extend
   `internal/output/writer_test.go` with a case calling `WriteAbortEvent` directly
   (no `start` line) in both plain and jsonl mode.

4. **`internal/backend/windows`: bridge-facing session support.**
   - `probes.go`: add `bridgeSessionProbe{}` with `State() string { return "bridge"
     }`, next to `realSessionProbe`.
   - `backend.go`: add `func NewBridge() (*Backend, error)` mirroring `New()` but
     `session: bridgeSessionProbe{}`.
   - `preflight.go`: export the exempt-kind check as `func RequiresSession(seq
     *ir.Sequence) bool` (returns `true` iff any op's `Kind` is outside
     `exemptFromSession`), and have `Preflight` call it instead of re-deriving
     `needsSession` inline. Behavior-identical refactor.
   - New `session.go` addition (or new file `remote.go`): `func IsRemoteSession()
     bool` — `true` when `SSH_CONNECTION`/`SSH_TTY` is set, OR the process's session
     id != `WTSGetActiveConsoleSessionId()`, OR `openInputDesktop()` fails (WinSta0
     not accessible). Reuses the existing primitives in `session.go:36-66`; does NOT
     reuse the "Default"/"Winlogon" branch (that only applies once already known to
     be the console session — a *locked local* session must stay locally-preflighted,
     not routed to the bridge).
   - Add `internal/backend/windows/preflight_test.go` / a new `remote_test.go` case
     for `RequiresSession` (exempt-only sequence -> false; mixed -> true) and for
     `IsRemoteSession()`'s env-var branch (`t.Setenv("SSH_CONNECTION", "...")`) — the
     WTS/session-id branch cannot be unit-tested without a real session and is left
     to the over-ssh acceptance pass.

5. **`internal/backend/windows`: named-pipe listener.** New file
   `internal/backend/windows/bridge_pipe.go` (`//go:build windows`):
   - `pipeName(username string) string` -> `` \\.\pipe\gotto-hando-<username> ``.
   - `currentUserSDDL() (string, error)`: `windows.GetCurrentProcessToken().
     GetTokenUser()` -> `sid.String()` -> `fmt.Sprintf("D:P(A;;GA;;;%s)", sidStr)`.
   - `func ListenBridge() (*PipeListener, error)`: resolve username (`os/user.
     Current()`, strip a `DOMAIN\` prefix if present), build the SDDL,
     `windows.SecurityDescriptorFromString`, wrap in `windows.SecurityAttributes`,
     call `windows.CreateNamedPipe` with `PIPE_ACCESS_DUPLEX|
     FILE_FLAG_FIRST_PIPE_INSTANCE` and `PIPE_TYPE_BYTE|PIPE_READMODE_BYTE|PIPE_WAIT`
     (byte-mode + newline-delimited framing, see step 6) and `maxInstances=1`. A
     failure here (already-owned pipe) is "a second instance for the same user exits
     2" — surface as a typed sentinel error the caller checks.
   - `func (*PipeListener) Accept() (io.ReadWriteCloser, error)`: blocking
     `ConnectNamedPipe`, wrap the handle in a small `pipeConn` (`Read`/`Write` via
     `windows.ReadFile`/`WriteFile`, `Close` via `DisconnectNamedPipe` — the same
     handle is reused for `ConnectNamedPipe` again on the next `Accept()`, per normal
     named-pipe idiom, no recreate).
   - `func (*PipeListener) Close() error`: unblocks a pending `ConnectNamedPipe` (via
     `CancelIoEx` or simply closing the handle) for clean Ctrl-C shutdown.
   - No new dependency; all calls go through `golang.org/x/sys/windows` directly
     (Codebase Findings — matches the existing convention).

6. **New package `internal/bridge` (GOOS-agnostic, no build tag).** This is the
   desktop-free-testable core; it imports `internal/ir`, `internal/engine`,
   `internal/output`, `internal/backend`, never anything windows-specific.
   - `wire.go`: `type RunEnvelope struct { DeadlineMS int; KeepGoing, Quiet,
     CapOnError bool }` (json tags `deadline_ms`/`keep_going`/`quiet`/
     `cap_on_error`); `func decodeRequest(data []byte) (*ir.Sequence, RunEnvelope,
     error)` — `ir.Unmarshal(data)` for the sequence, a second
     `json.Unmarshal(data, &struct{ Run RunEnvelope \`json:"run"\` }{})` for the run
     object (both read the same bytes; unknown keys ignored by each).
   - **Framing decision** (not literally spelled out by the ticket text, which only
     says "writes ONE IR JSON document ... then half-closes" — named pipes have no
     clean half-close primitive): the request is one JSON document terminated by
     `\n` (the caller always emits it compact, one line), read via
     `bufio.NewReader(conn).ReadBytes('\n')`. This matches the JSONL-everywhere
     convention already used for every other wire/file format in this codebase and
     is trivially fakeable with `net.Pipe()`/`io.Pipe()` in tests. Document this
     inline as a Phase-0-local protocol detail Phase 1/2 can revisit.
   - `session.go`: `type Session struct { mu sync.Mutex; Backend backend.Backend; Log
     func(string) }` — `Backend` is injected (the real `windows.NewBridge()` result
     in production, `*dryrun.Backend` in tests), `Log` is the one-line-per-run/error
     sink (production: append to `bridge.log`; tests: capture to a slice). "One run
     at a time: a second caller waits" -> plain `mu.Lock()`/`defer mu.Unlock()`
     around `Handle`.
   - `func (s *Session) Handle(ctx context.Context, conn io.ReadWriteCloser)`:
     1. Read the framed request; on read/JSON error, best-effort write a plain-text
        error and close (log it) — malformed input from a same-machine, same-user
        caller is not expected in normal operation.
     2. `output.WriteStart(conn, true, "", "")` (dest/out are locally-meaningless
        placeholders here — the forwarder never trusts these fields, see step 7).
     3. If `seq.V != ir.SchemaVersion`: `output.WriteAbortEvent(conn, true,
        output.EConnect, "IR schema version mismatch")`, log, return.
     4. `runCtx, cancel := context.WithCancel(ctx)`; build `engine.RunOptions{
        KeepGoing: env.KeepGoing, CapOnError: env.CapOnError, OnResult: func(r
        output.Result) { if err := output.WriteResult(conn, true, false, r); err !=
        nil { cancel() } }}` (jsonl always, `quiet=false` — JSONL is never
        quiet-filtered, help.txt JSONL section). A write failure (broken pipe = caller
        disconnected) cancels `runCtx`, which the step-2 `engine.Run` change turns
        into "skip the rest, release held keys."
     5. `sum := engine.Run(runCtx, s.Backend, seq, opt)`; if `sum.Aborted`,
        `output.WriteAbortEvent(conn, true, sum.AbortCode, sum.AbortMsg)`, else
        `output.WriteDone(conn, true, sum.Done)` (best-effort; a write error here just
        means the caller is already gone — log and return either way).
     6. `s.Log(...)` one line: run outcome (ok/err counts, aborted?, held_released) —
        explicitly no sequence text, no image data (per the log contract).
   - `internal/bridge/session_test.go`: build requests with `ir.Marshal` +
     hand-written `run` JSON, drive `Handle` over `net.Pipe()` against
     `*dryrun.Backend`, and assert: (a) full start/result.../done sequence for a
     normal run; (b) IR `v` mismatch -> `start` then `abort` E_CONNECT, no `done`;
     (c) two concurrent `Handle` calls on two `net.Pipe()`s serialize (second only
     starts writing its own `start` after the first's `done`, verified via a
     `dryrun.Backend.FailOn` hook that blocks until signaled); (d) a `conn` whose
     `Write` errors after the Nth call (simulating disconnect) causes the run to stop
     early and a still-held key (via a `kd`-only sequence) to appear released in
     `dryrun.Backend.Calls`.

7. **`cmd/gotto-hando`: `--bridge` dispatch.** Edit `dispatch.go:89-91`: replace the
   inline abort with `return runBridge(stdout, stderr)`, implemented per-GOOS:
   - `dispatch_windows.go` (real): resolve username, `windows.ListenBridge()`
     (surface "second instance" as `abort(output.EValidate, ...)` -> exit 2 per spec,
     or a bare `fmt.Fprintln(stderr, ...); return 2` if it's cleaner to keep this path
     outside the `abort()`/JSONL machinery, since `--bridge` never emits JSONL
     itself); open/append `%LOCALAPPDATA%\gotto-hando\bridge.log`
     (`os.MkdirAll` the dir first); `signal.Notify(os.Interrupt)` -> on signal, close
     the listener and return `output.ExitOK`; otherwise loop `Accept()` ->
     `bridge.Session{Backend: winbackend.NewBridge()}.Handle(context.Background(),
     conn)` -> log -> loop.
   - `dispatch_darwin.go` / `dispatch_other.go` (stub): `func runBridge(stdout,
     stderr io.Writer) int { fmt.Fprintln(stderr, "usage error: session bridge not
     implemented"); return output.ExitValidation }` — **unchanged behavior/exit code**
     from today's inline abort (verify the exact current message/exit against
     `dispatch.go:89-91` before finalizing so no observable regression on those
     GOOS's).

8. **`cmd/gotto-hando`: `local` forwarding routing.** Edit `dispatch.go`'s `dest ==
   "local"` branch (`dispatch.go:113-145`), inserted right after `parseAndValidate`
   succeeds and *before* `newLocalBackend()`:
   - Windows-only decision, exposed as a per-GOOS seam function `func
     shouldForwardToBridge(seq *ir.Sequence) bool` (windows: `winbackend.
     IsRemoteSession() && winbackend.RequiresSession(seq)`; every other GOOS:
     `false`, so this branch is a no-op there and existing darwin/other behavior is
     completely unaffected).
   - When `true` (windows-only): call a new windows-only `forwardToBridge(opts,
     seq, stdout, stderr) int` in `dispatch_windows.go` instead of
     `newLocalBackend()`/`engine.Run`. It:
     1. Dials the pipe (`windows.CreateFile` on `` \\.\pipe\gotto-hando-<username> ``,
        wrapped by the same `dialBridge func(name string) (io.ReadWriteCloser, error)`
        seam the tests override — default implementation is the real `CreateFile`
        dial). A dial failure (pipe missing/busy) = "no bridge reachable" ->
        `abort(output.ESession, "start `gotto-hando --bridge` in the logged-on GUI
        session")`, exit 4, matching help.txt's hint text verbatim
        (help-windows.txt:72-73).
     2. On success, marshals `ir.Marshal(seq)` plus the `run` envelope
        (`{"deadline_ms":..,"keep_going":opts.KeepGoing,"quiet":opts.Quiet,
        "cap_on_error":opts.CapOnError}`) into one JSON object (merge, or just
        append `"run":{...}` before the closing `}` of `ir.Marshal`'s output — either
        works since `Unmarshal`/`decodeRequest` only look at known top-level keys),
        writes it `+ "\n"`, then reads the response line-by-line (`bufio.Scanner`).
     3. First line is the bridge's `start` object — **discarded** (the forwarder
        prints its OWN `output.WriteStart(stdout, opts.JSONL, "local",
        output.DefaultOutDir(opts.Out, "local", newRunID()))` instead, since `out`/
        `dest` are local-only concerns the bridge's copy is never authoritative for,
        same reasoning Phase 1 documents for the *cross-machine* case — this is the
        same-machine case, so the forwarder already has the correct values without
        needing to trust the wire).
     4. Subsequent lines: an object with `"event":"abort"` -> render via
        `output.WriteAbortEvent(...)` (plain or jsonl per `opts.JSONL`) and return
        `output.AbortExit(code)`; `"event":"done"` -> decode into `output.Done` (flat
        field mapping: `ok`/`err`/`skip`/`elapsed_ms`/`held_released`/`state`) and
        `output.WriteDone(stdout, opts.JSONL, done)`, return the same exit-code
        formula `dispatch.go:143-144` already uses for local runs (err>0 -> 1, else
        0); otherwise a per-line result object -> reconstruct an `output.Result` and
        `output.WriteResult(stdout, opts.JSONL, opts.Quiet, r)`.
     5. **Result reconstruction** (needed only when `!opts.JSONL`, i.e. plain mode —
        in `--jsonl` mode the received object can be forwarded close to verbatim
        after `line`/`status`/`cmd` pass-through, since it already IS the documented
        JSONL shape): `line`/`status`/`cmd` always present; `status=="err"` ->
        `src`/`code`/`msg` map straight across (already flat per the JSONL err
        example, help.txt:620-621); for `status!="err"`, a small per-`cmd` table
        rebuilds `Detail`/`Extra`/`AlwaysShow` from the known JSON fields for the
        commands that carry structured data — `qinfo` (12 flat fields, `internal/
        engine/query.go:26-36`) -> `formatQueryInfo`; `qclip` (the `"text"` field
        added in step 2) -> `Detail=text, AlwaysShow=true`; `focus`/`qwin`/`qdisp`/
        `qmouse`/`exec` similarly, reusing the *existing* formatting functions in
        `internal/engine/query.go` and `exec.go`'s `formatExecDetail`/
        `formatExecOutputLines`. Since those are unexported (`package engine`),
        export a single new function from `internal/engine` (e.g.
        `ResultDetailFromJSON(cmd string, fields map[string]json.RawMessage) (detail
        string, extra []string, alwaysShow bool, ok bool)`) that the forwarder calls;
        `cap` is intentionally excluded (Out of Scope) and falls through to the
        generic no-`Detail` case. Every other command kind (`k`/`kd`/`ku`/`txt`/`m`/
        `c`/`md`/`mu`/`drag`/`scroll`/`clip`/`paste`/`open`/`sleep`/`set`) has no
        `Detail` in plain mode today either (`internal/engine/run.go`'s `execute()`
        switch never sets it for them), so the generic fallback is already correct
        for them — no table entry needed.
   - Add `dispatch_windows_test.go` (or extend an existing windows-only test file)
     covering `forwardToBridge` with `dialBridge` overridden to return one side of a
     `net.Pipe()` driven in-test by an `internal/bridge.Session{Backend:
     &dryrun.Backend{...}}` — desktop-free, no real named pipe needed.

## Verification Plan

- Desktop-free, any GOOS (`go test ./...`):
  - `internal/ir`: `Marshal`/`Unmarshal` round-trip per `Kind` (step 1).
  - `internal/engine`: cancellation-releases-held-keys and `OnResult`-per-line tests
    against `dryrun.Backend` (step 2); `qclip` JSON field test (step 2).
  - `internal/output`: `WriteAbortEvent` test (step 3).
  - `internal/bridge`: protocol/framing/single-run-gating/disconnect-releases/version-
    mismatch tests over `net.Pipe()` against `dryrun.Backend` (step 6) — this is the
    "in-process or fake-pipe listener" the ticket's own Verification paragraph asks
    for.
- Windows-only unit tests (still desktop-agnostic, same seam pattern as existing
  `preflight_test.go`): `RequiresSession`, `IsRemoteSession()`'s env-var branch,
  `bridgeSessionProbe.State() == "bridge"` (step 4); `forwardToBridge` over a fake
  `dialBridge` (step 8).
- Build matrix: `go build ./...` (native), `GOOS=windows CGO_ENABLED=0 go build
  ./...`, `GOOS=darwin CGO_ENABLED=0 go build ./...`, and whatever GOOS
  `dispatch_other.go`'s tag covers (e.g. `GOOS=linux`) — all must stay green, since
  `internal/bridge` carries no build tag and `dispatch_darwin.go`/`dispatch_other.go`
  only add a same-signature `runBridge` stub (step 7), never importing anything
  windows-specific.
- Existing drift tests (`internal/output/help_drift_test.go`,
  `cmd/gotto-hando/options_drift_test.go`, `internal/ir/limits_drift_test.go`) must
  keep passing unchanged — this slice adds no new help-text sections or option
  entries (`--bridge` is already documented and parsed).
- Autonomous over-ssh acceptance (requires the user to start `gotto-hando --bridge`
  in the console/RDP session on `sw.kang@192.168.100.2` first; the agent then drives
  everything else over `ssh sw.kang@192.168.100.2`):
  1. `gotto-hando local qinfo` -> plain output shows `session=bridge`.
  2. `gotto-hando local 'txt[]hello-bridge' 'k[c]a' 'k[c]c' 'qclip'` -> the `qclip`
     line's printed text is `hello-bridge` (confirms the JSONL wire carries the fixed
     `qclip` `"text"` field end-to-end through the bridge and the forwarder's plain
     reconstruction).
  3. Ask the user to stop the bridge (Ctrl-C); rerun step 2's sequence -> stderr
     `abort: start `gotto-hando --bridge` in the logged-on GUI session (E_SESSION)`,
     exit 4; `gotto-hando local qinfo` in the same state -> `session=inactive`, exit
     0 (answered in-process, no bridge contacted).
  4. Ask the user to restart the bridge; run `gotto-hando local 'kd[]a' 'sleep[]10s'`
     and kill the ssh connection ~2s in; reconnect and check
     `%LOCALAPPDATA%\gotto-hando\bridge.log` shows the aborted run and a released key,
     and a following `gotto-hando local qinfo` still works (bridge accepted the next
     caller — single-run gating recovered).

## Escalations

- None. Every open technical question the prompt flagged is resolved concretely
  above with file/line evidence: named-pipe IPC and the current-user ACL both use
  `golang.org/x/sys/windows` directly (no new dependency); the desktop-free testing
  seam is `internal/bridge` (no build tag) driven by `net.Pipe()` +
  `internal/backend/dryrun`, mirroring the project's existing probe-injection
  pattern; the local-forwarder JSONL relay only needs `ir.Marshal`/`Unmarshal` and a
  small `run`-envelope struct, never Phase 1's ssh-transport/`[f]`-inlining/capture-
  rewrite/version-negotiation machinery.
- Two additions are cross-module/public-API changes on shared packages
  (`engine.RunOptions` gains `OnResult`; `output.WriteAbort` is split into
  `WriteStart`+`WriteAbortEvent`) — both are backward-compatible and directly required
  by the ticket's own "streams the same JSONL" and "releases held keys on disconnect"
  contract, not invented scope, but per the project's Approval Protocol
  ("cross-module interfaces ... Ask first") the executor should confirm before
  landing rather than treat them as an ordinary single-module change.
