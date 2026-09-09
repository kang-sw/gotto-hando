# Plan: 260908-feat-remote-ssh — Phase 2: Session bridge (macOS and Windows)

## Relevant Ticket Contract

- `--bridge` gets a shared core and a per-platform listener: macOS unix
  socket `$HOME/Library/Application Support/gotto-hando/bridge.sock` (file
  0600, dir 0700); Windows named pipe (already built, Phase 0).
- Single-run gating; `local` ssh-session detection with the
  qinfo/qdisp/qmouse/sleep/set/comments-only forwarding exception; no bridge
  reachable = `abort` E_SESSION after `start`, exit 4; IR `"v"` mismatch =
  `abort` E_CONNECT.
- `qinfo` through the bridge reports `session=bridge`.
- `--request-perms` executed by the bridge: wire shape
  `{"v":1,"request_perms":true}` in, `{"event":"perms","accessibility":
  "ok|missing","screen":"ok|missing"}` out (help-remote.txt:122-129,
  help.txt:628-631).
- Abort-and-release when the caller disconnects; bridge.log at
  `~/Library/Logs/gotto-hando/bridge.log`.
- LaunchAgent (macOS) and Task Scheduler (Windows) recipes verified end to
  end from a fresh login (recipes already documented in help-macos.txt:155-
  180 and help-windows.txt:64-100 — no new text needed).
- Verification boundary (ticket text): from an ssh session with the bridge
  running, `qinfo`→`session=bridge` and EXAMPLES run end to end with
  captures on the agent's machine and nothing on the remote; bridge stopped
  → `start`+`abort` E_SESSION, exit 4, while `<dest> qinfo` still answers
  `session=inactive`; GUI-terminal `qinfo` never touches the socket/pipe;
  two-direction ssh-env detection test; disconnect-mid-run releases a held
  key; fresh signing identity `--request-perms` raises the prompt.
- Spec Impact: none — help-macos.txt/help-remote.txt already document the
  full macOS bridge contract (socket path, perms, LaunchAgent, request-perms
  procedure); confirmed no `== SECTION ==` needs adding.

## Out of Scope

- Real over-ssh acceptance against a live second Mac/Windows box, and live
  LaunchAgent/Task Scheduler login verification — inherently interactive,
  deferred non-blocking exactly as Phase 1's own "no real over-ssh run"
  deferral (mental-model + ticket Result sections already establish this
  pattern).
- `--timeout`/`deadline_ms` enforcement over the bridge wire — pre-existing,
  explicitly-deferred gap (`internal/bridge/wire.go`'s `RunEnvelope` doc
  comment), not reopened here.
- `local` direct `qclip[f]<path>` file write and the unconsulted `--ping`
  option — Phase 1 Out of Scope / Unresolved notes, unrelated to the bridge.
- Any `assets/help*.txt` change — Spec Impact is none.
- Windows-side new code beyond what the shared `internal/bridge` fixes
  already carry — the Windows named-pipe listener, `IsRemoteSession`,
  `RequiresSession`, `NewBridge`/`bridgeSessionProbe` are already built
  (Phase 0, e18b19a) and untouched here except for benefiting from the
  `internal/bridge` capture-inlining fix below.
- A `WaitNamedPipe`-style busy-retry loop for the macOS dial path — a Unix
  domain `SOCK_STREAM` listener's kernel accept backlog already queues a
  concurrent `connect()` while `Handle` is busy (unlike a single-instance
  named pipe, which fails `CreateFile` with `ERROR_PIPE_BUSY` immediately),
  so `DialBridge` needs no retry loop to get "a second caller waits" for
  free.

## Codebase Findings

- `cmd/gotto-hando/dispatch_windows.go` (whole file) — the template to
  mirror: `runBridge` (:47-88), `openBridgeLog` (:97-118),
  `shouldForwardToBridge` (:128-130), the `dialBridge` test seam (:135),
  `forwardToBridge` (:145-230), and the wire-decode/relay helpers
  `decodeAbortEvent`/`wireDone`/`decodeDoneEvent`/`relayResult`/`relayDone`
  (:232-311).
- `cmd/gotto-hando/dispatch_darwin.go:42-66` — the three stubs to replace
  (`runBridge`, `shouldForwardToBridge`, `forwardToBridge`), and `:30-40`
  `requestPerms` (currently always calls `darwin.RequestPerms()` in-process
  and always prints only the plain `perms=...` line, ignoring `opts.JSONL`
  and never checking whether this process itself is ssh-started).
- **Risk signal — missed contract**: `cmd/gotto-hando/dispatch_darwin.go`'s
  `requestPerms` never emits the JSONL `start`+`{"event":"perms",...}` pair
  that help.txt JSONL (`:628-631`) documents for `local --request-perms
  --jsonl`, and never routes to a bridge when the process is itself
  ssh-started. `cmd/gotto-hando/remote.go`'s `runRemoteRequestPerms`
  (`:249-307`) already decodes exactly that JSONL shape via
  `internal/remote.DecodePerms` when relaying a `<dest> --request-perms`
  run — meaning today `gotto-hando <dest> --request-perms` against a macOS
  target is broken end to end (the remote-side `local --request-perms
  --jsonl` invocation never prints the object `runRemoteRequestPerms`
  expects). `cmd/gotto-hando/main_test.go:178-202`'s
  `TestRequestPermsPhase2` never exercises `--jsonl`, so this gap is
  unexercised. This is required by Phase 2's own contract (`<dest>
  --request-perms` over ssh to a mac target, always forwarding `--jsonl`
  per Decisions), not new scope.
- **Risk signal — public contract violation**: `internal/bridge/session.go`
  `Handle` (:83-99) builds `engine.RunOptions{KeepGoing, CapOnError,
  OnResult}` without `InlineCaptures: true` and without `OutDir`. A `cap` op
  run through the bridge today falls into `internal/engine/capture.go`'s
  `!st.inlineCaptures` branch (:137-141) and calls `WriteCaptureFile` with
  `CapturePath("", ...)` — a relative path in the bridge process's own
  working directory. This directly violates help-remote.txt SESSION
  BRIDGE's "the bridge writes nothing else to disk" (`:135`) and Phase 2's
  own verification bullet ("captures landing on the agent's machine and
  nothing written on the remote"). `internal/bridge` is GOOS-agnostic, so
  this bug affects the already-shipped Windows bridge too; fixing it here
  is in scope because Phase 2 is the first phase whose verification
  boundary actually exercises `cap` through a bridge.
- `internal/backend/windows/bridge_pipe.go` (whole file) — the pipe
  transport pattern (`ListenBridge`/`Accept`/`Close`, `ErrBridgeAlreadyRunning`/
  `ErrListenerClosed`, `DialBridge` with its `WaitNamedPipe` busy-retry
  loop, `PipeName`/`CurrentUsername`). The macOS unix-socket equivalent is
  simpler: no per-user name needed (the socket path is already under
  `$HOME`), and no busy-retry loop (see Out of Scope).
- `internal/backend/windows/remote.go` (`IsRemoteSession`, :7-30) — checks
  `SSH_CONNECTION`/`SSH_TTY` OR a non-console session id/WinSta0
  inaccessibility. help-remote.txt SESSION BRIDGE `:109-117` ("Detection")
  confirms the extra session-id/WinSta0 checks are Windows-only; macOS
  needs only the env-var check.
- `internal/backend/windows/backend.go:49-62` (`NewBridge`) +
  `internal/backend/windows/probes.go:56-65` (`bridgeSessionProbe`) — the
  pattern for making `qinfo` report `session=bridge`: swap only the
  session probe, keep every other probe real. Darwin's
  `internal/backend/darwin/backend.go:53-68` (`New`) and
  `internal/backend/darwin/probes.go:55-59` (`realSessionProbe`) are the
  exact fields to mirror into a new `NewBridge()`/`bridgeSessionProbe{}`
  pair; darwin's `New()` also calls `initFFI()` first (windows's lazy-DLL
  `New()` has no such step) — `NewBridge()` must call it too.
- `internal/backend/windows/preflight.go:116-129` (exported
  `RequiresSession`) — darwin's `internal/backend/darwin/preflight.go:15-21`
  already has the identical `exemptFromSession` map (and Preflight already
  accepts `session=="bridge"` at `:65-66`, so Preflight itself needs no
  change) but no exported `RequiresSession` function for
  `shouldForwardToBridge` to call.
- `internal/backend/darwin/session.go:26-30` — the `sessionState()` doc
  comment explicitly says the `"bridge"` branch "has no real detection
  logic yet ... stays reserved ... per the ticket's Out of Scope", which
  becomes stale once `NewBridge()`/`bridgeSessionProbe{}` wire it (the
  wiring happens via a separate probe, not by editing `sessionState()`
  itself) — update on contact per AGENTS.md Context Window Discipline.
- `internal/bridge/wire.go:36-93` — `decodeRequest`/`EncodeRequest`, the
  wire framing to extend: `decodeRequest` needs to also report a
  `request_perms` bool (a third field alongside the IR sequence and the run
  envelope — `ir.Unmarshal` already tolerates a request body with no `ops`
  and ignores the unknown `request_perms` key, so no `ir` package change is
  needed), and a new `EncodeRequestPerms() []byte` building
  `{"v":<ir.SchemaVersion>,"request_perms":true}` for
  `cmd/gotto-hando/dispatch_darwin.go`'s bridge-forwarding
  `--request-perms` path to reuse (mirrors `EncodeRequest`'s existing
  shape/doc pattern, :71-93).
- `internal/output/writer.go:207-215` (`WriteAbortEvent`) — the pattern to
  copy for a new `WritePermsEvent(w io.Writer, accessibility, screen bool)
  error`, using the same `writeJSONObject`/`KV` helpers (:18, :240) to
  build `{"event":"perms","accessibility":"ok|missing","screen":
  "ok|missing"}` (help.txt:628-631). This is the natural GOOS-agnostic home
  (alongside `WriteStart`/`WriteAbortEvent`/`WriteDone`), reused by both
  `internal/bridge/session.go`'s new request-perms branch and
  `cmd/gotto-hando/dispatch_darwin.go`'s `requestPerms`.
- `internal/remote/decode.go:52-67` (`PermsEvent`/`DecodePerms`) — the
  exact wire shape `WritePermsEvent` must produce; already imported by
  `cmd/gotto-hando` (via `remote.go`), so darwin's bridge-request-perms
  forwarder should decode the bridge's perms response with
  `remote.DecodePerms` rather than declaring a third perms struct.
- **Reuse opportunity**: `decodeAbortEvent`/`wireDone`/`decodeDoneEvent`/
  `relayResult`/`relayDone` (`dispatch_windows.go:232-311`) are private to
  a `windows`-build-tagged file, invisible to `dispatch_darwin.go`. The
  Phase 0 review already flagged `runBridge`/`forwardToBridge` duplication
  across `dispatch_darwin.go`/`dispatch_other.go` as minor/record-only debt
  when darwin's stubs were one-liners; giving darwin a full
  `forwardToBridge` now would double it into ~80 duplicated lines instead.
  Extracting these five symbols into a new build-tag-free
  `cmd/gotto-hando/bridge_relay.go` (same package, compiled on every GOOS)
  is a mechanical, low-risk move that both platforms then share — resolves
  the flagged debt instead of growing it.
- `internal/bridge/session_test.go` (`TestHandleWriteFailureStopsRunAndReleasesHeldKey`,
  :321-378) and `ai-docs/mental-model/remote-transport.md`'s second Domain
  Rule — confirm the caller-disconnect → held-key-release path is
  **already correct by construction** for the bridge: `OnResult`'s
  write-failure `cancel()` only stops further op execution, and the
  engine's unconditional end-of-run `releaseAll` (`internal/engine/run.go:192`,
  `held.go:60`) releases regardless of cancellation or whether the
  disconnect was ever detected mid-run (e.g. a disconnect during a `sleep`
  with nothing streaming is invisible until the run's natural end, but
  `releaseAll` still fires then). No code change needed here beyond what
  darwin's new transport already gets for free from the shared
  `internal/bridge` package; only new darwin-specific tests are needed to
  exercise it over a real unix socket.
- `cmd/gotto-hando/dispatch_windows_test.go` (whole file) — the
  `withBridgeSession`/`dialBridge`-seam + `net.Pipe()` + `dryrun.Backend`
  test pattern to mirror in a new `dispatch_darwin_test.go`
  (`//go:build darwin`, no `dryrun` tag needed — this dev host is macOS, so
  these tests run natively, unlike the windows file which only
  compile-verifies here).
- `assets/help-macos.txt:132-208` (SESSION BRIDGE FOR SSH (REMOTE MAC)) and
  `assets/help-remote.txt:99-138` (SESSION BRIDGE) — the normative contract
  already fully written: socket path/perms, LaunchAgent plist, log path,
  `--request-perms` procedure, one-run-at-a-time, disconnect behavior.
  Confirms Spec Impact is none.

## Implementation Plan

1. `internal/backend/darwin/remote.go` (new file, `//go:build darwin`) —
   add `IsRemoteSession() bool` checking only `SSH_CONNECTION`/`SSH_TTY`
   (mirror `internal/backend/windows/remote.go`'s doc-comment shape but
   drop the console-session-id/WinSta0 checks, which are windows-only per
   help-remote.txt `:109-117`).
2. `internal/backend/darwin/preflight.go` — add exported `RequiresSession(seq
   *ir.Sequence) bool` reusing the existing `exemptFromSession` map
   (mirror `internal/backend/windows/preflight.go:116-129` verbatim).
3. `internal/backend/darwin/probes.go` — add `bridgeSessionProbe struct{}`
   with `State() string { return "bridge" }` (mirror
   `internal/backend/windows/probes.go:56-65`).
4. `internal/backend/darwin/backend.go` — add `NewBridge() (*Backend,
   error)`: same as `New()` (`:53-68`, including `initFFI()` and the
   `evtSource`/`clickState`/`lastButtonUp` fields) except `session:
   bridgeSessionProbe{}`.
5. `internal/backend/darwin/session.go:26-30` — update the stale
   `sessionState()` doc comment: the `"bridge"` branch is now reachable via
   `NewBridge()`'s probe swap, not via `sessionState()` itself.
6. `internal/backend/darwin/bridge_socket.go` (new file, `//go:build
   darwin`) — unix-socket transport, no new dependency (`net`/`os`/
   `path/filepath` stdlib only):
   - `bridgeSocketPath() (string, error)`: `os.UserHomeDir()` +
     `Library/Application Support/gotto-hando/bridge.sock`.
   - `ListenBridge() (*SocketListener, error)`: `os.MkdirAll(dir, 0o700)`
     then `os.Chmod(dir, 0o700)` (explicit chmod — `MkdirAll` is subject to
     umask); detect an already-running bridge by first attempting
     `net.DialTimeout("unix", path, ...)` — success means live (return
     `ErrBridgeAlreadyRunning`), `ECONNREFUSED`/`ENOENT` means a stale or
     absent socket file (safe to `os.Remove` and proceed), anything else is
     a genuine error; then `net.ListenUnix` and `os.Chmod(path, 0o600)`.
   - `(*SocketListener).Accept()`/`Close()`: thin wrappers translating
     `net.ErrClosed` to a package `ErrListenerClosed` (mirror
     `PipeListener`'s shape); `Close()` also removes the socket file
     best-effort.
   - `DialBridge() (io.ReadWriteCloser, error)`: a single `net.Dial("unix",
     path)` — no retry loop (see Out of Scope); a dial failure is the
     caller's "no bridge reachable" signal.
7. `internal/bridge/wire.go` — extend `decodeRequest` to also decode a
   `request_perms bool` (a third field on the same anonymous `wrapped`
   struct, alongside `Run`) and return it; add `EncodeRequestPerms()
   []byte` building `{"v":<ir.SchemaVersion>,"request_perms":true}` via
   `json.Marshal` (mirrors `EncodeRequest`'s doc pattern, help-remote.txt
   `:122-129`).
8. `internal/output/writer.go` — add `WritePermsEvent(w io.Writer,
   accessibility, screen bool) error` (JSONL-only, mirrors
   `WriteAbortEvent`'s shape at `:207-215`, using `writeJSONObject`/`KV`)
   producing `{"event":"perms","accessibility":"ok|missing","screen":
   "ok|missing"}` (help.txt:628-631).
9. `internal/bridge/session.go`:
   - Add a `RequestPerms func() (accessibility, screen bool, err error)`
     field to `Session` (nil-safe, like `Log`; never invoked on Windows
     since its bridge never receives this message).
   - `Handle` (:39-114): after decoding and the existing `v`-mismatch
     check, branch on the new `request_perms` flag to a new
     `handleRequestPerms(conn)` method: if `s.RequestPerms == nil`, write
     an abort (`E_VALIDATE`, "request_perms not supported by this
     bridge") and log; else call it, write an abort on error, else
     `output.WritePermsEvent(conn, acc, scr)` and log the outcome.
   - Add `InlineCaptures: true` to the `engine.RunOptions` built in the
     normal-run branch (:85-99) — fixes the disk-write violation found
     above; benefits both platforms since this file is GOOS-agnostic.
10. `cmd/gotto-hando/bridge_relay.go` (new file, no build tag) — move
    `decodeAbortEvent`, `wireDone`, `decodeDoneEvent`, `relayResult`,
    `relayDone` out of `dispatch_windows.go:232-311` verbatim; update
    `dispatch_windows.go` to drop the moved definitions (same package,
    call sites unaffected). Also add a shared `writePermsResult(opts
    parsedOptions, stdout io.Writer, accessibility, screen bool) int`
    helper here: JSONL → `output.WriteStart` + `output.WritePermsEvent`;
    plain → the existing `perms=accessibility:%s,screen:%s\n` line; return
    `output.ExitOK` when both are true else `output.ExitPreflight` — used
    by both darwin's direct and bridge-forwarded `requestPerms` paths so
    they stay byte-identical.
11. `cmd/gotto-hando/dispatch_darwin.go`:
    - `runBridge`: `darwin.ListenBridge()` (map
      `darwin.ErrBridgeAlreadyRunning` to the existing `EValidate` abort,
      same message as windows's), a new `openBridgeLog` (below),
      `darwin.NewBridge()`, `&bridge.Session{Backend: be, Log: logLine,
      RequestPerms: darwin.RequestPerms}`, the same `signal.Notify`
      accept loop as `dispatch_windows.go:66-88`.
    - New private `openBridgeLog(stderr io.Writer) (func(string), func())`
      — `~/Library/Logs/gotto-hando/bridge.log` via `os.UserHomeDir()`,
      otherwise identical shape to `dispatch_windows.go:97-118`.
    - `shouldForwardToBridge(seq)`: `darwin.IsRemoteSession() &&
      darwin.RequiresSession(seq)`.
    - `var dialBridge = darwin.DialBridge` (test seam, mirrors
      `dispatch_windows.go:135`; darwin's `DialBridge` takes no name
      argument, so the seam's function type differs from windows's — fine,
      the two files never compile together).
    - `forwardToBridge`: mirror `dispatch_windows.go:145-230`, reusing the
      relocated `bridge_relay.go` helpers and `bridge.EncodeRequest`.
    - `requestPerms`: branch on `darwin.IsRemoteSession()`: false → call
      `darwin.RequestPerms()` then `writePermsResult`; true → dial the
      bridge (dial failure → `abort(output.ESession, "start
      \`gotto-hando --bridge\` in the logged-on GUI session")`, same hint
      text as `forwardToBridge`'s dial-failure path), write
      `bridge.EncodeRequestPerms()+"\n"`, discard the bridge's own `start`
      line, decode the next line via `decodeAbortEvent` (abort) or
      `remote.DecodePerms` (perms → `writePermsResult`).

## Verification Plan

- `go build ./...` native (macOS, `CGO_ENABLED=0`) and `GOOS=windows go
  build ./...` clean; `gofmt -l .` empty; `go vet ./...` no new warnings;
  `git diff -- go.mod go.sum` empty (net/os/path/filepath stdlib only, no
  new dependency).
- `go test ./... -race -count=1` on this macOS host exercises the real
  code paths (not just compile-checks), since darwin is native here:
  - `internal/bridge`: extend `wire_test.go` for `EncodeRequestPerms`
    round-tripping through `decodeRequest`'s new return; extend
    `session_test.go` with a request-perms-callback success case, a
    nil-`RequestPerms` defensive-abort case, and a capture-inlining
    regression test (a `cap` op through `Handle` with `dryrun.Backend`
    must return `"data"` inline and write no file — proves the
    `InlineCaptures: true` fix).
  - `internal/backend/darwin`: new `remote_test.go` (`IsRemoteSession`
    env-var cases), extend `preflight_test.go` for `RequiresSession`, new
    `bridge_socket_test.go` exercising a real unix-domain socket round
    trip in a temp `$HOME` (`t.Setenv`/`t.TempDir()`):
    `ListenBridge`→`DialBridge`→write/read, `ErrBridgeAlreadyRunning` on a
    second `ListenBridge` while the first is live, and stale-socket-file
    recovery (a leftover `.sock` file with nothing listening must not
    block a fresh `ListenBridge`).
  - `cmd/gotto-hando`: new `dispatch_darwin_test.go` (`//go:build darwin`)
    mirroring `dispatch_windows_test.go`'s three `forwardToBridge` cases
    via the `dialBridge` seam + `net.Pipe()` + `dryrun.Backend`, plus
    request-perms-over-bridge success/no-bridge-abort cases; extend
    `main_test.go`'s `TestRequestPermsPhase2` (or add a sibling) to cover
    `local --request-perms --jsonl` printing a `start`+`perms` JSONL pair.
- `GOOS=windows go test -c ./...` (cmd/gotto-hando, internal/backend/windows,
  internal/bridge) compiles clean — confirms the shared `internal/bridge`
  changes (the `RequestPerms` field, `InlineCaptures: true`) don't disturb
  the already-shipped Windows bridge; Windows's own existing suite is
  unaffected since `Session.RequestPerms` stays nil there.
- Manual/deferred, matching Phase 1's own verification boundary (recorded,
  not blocking): real over-ssh acceptance against a live second Mac and
  Windows box; the LaunchAgent and Task Scheduler recipes started from a
  fresh login; a fresh macOS signing identity raising the `--request-perms`
  system prompt. These are inherently interactive/live-session checks the
  ticket itself defers past this survey's execution boundary.

## Escalations

- None.
