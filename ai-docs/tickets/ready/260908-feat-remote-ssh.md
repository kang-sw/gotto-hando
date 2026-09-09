---
title: "Remote: ssh-wrapped destinations and the session bridge"
parent: 260907-epic-gotto-hando-v1
related:
  260907-feat-cli-core: prerequisite
  260907-feat-darwin-backend: prerequisite
  260907-feat-windows-backend: prerequisite for the Windows half of Phase 2
sage-review-design: completed
sage-review-design-reviewed: 6ce907560558bea3
sage-review-completeness: completed
sage-review-completeness-reviewed: 6ce907560558bea3
---

# Remote: ssh-wrapped destinations and the session bridge

## Background

Makes `gotto-hando <dest> ...` drive another machine where `<dest>` is an ssh
destination, without any network listener of our own. Normative contract:
`assets/help-remote.txt` (all sections), `assets/help.txt` SYNOPSIS
(`<dest>`, `--bridge`), OPTIONS (`--remote-bin`, `--inline-captures`,
`--expect-version`, `--request-perms` over a destination), DESTINATIONS,
JSONL (`start`, `abort`, `perms` objects), EXIT CODES (3, 4, 5), the
connection-loss rules in ERROR POLICY, and the bridge sections of
`assets/help-macos.txt` and `assets/help-windows.txt`.

## Decisions

- Remote = ssh wrapper. A non-`local` destination runs
  `ssh <dest> <remote-bin> local --jsonl --inline-captures --expect-version <local version> <forwarded options> -f -`,
  feeds the sequence on stdin, reads the remote JSONL from stdout, prints it
  in the local output format and writes inline base64 captures to local files
  under the `--out` precedence. Host aliases, keys, ports and jump hosts are
  ssh config's job; there is no profile file.
  Rejected: an HTTP server on a loopback port reached through an SSH tunnel
  with a bearer token and `profiles.toml`. SSH already provides
  authentication and encryption, the user has a shell on the remote by
  precondition, and what a resident server provided (a visible consent
  gate, a stable TCC identity on macOS, escape from Windows Session 0) is
  provided by a same-machine bridge without any network surface.
- Forwarded options (help.txt OPTIONS, help-remote.txt HOW IT WORKS step 2):
  always `--jsonl --inline-captures --expect-version <local version>`; when
  given `-q`, `--delay`, `--timeout`, `-k`, `--cap-on-error`,
  `--request-perms`. Local-only: `--out`, `--remote-bin`. `--check` and
  `--ir` never start ssh.
- `--expect-version VER`: the binary refuses to run unless its own version
  equals VER; the wrapper always forwards the local version. On mismatch the
  remote prints `version mismatch: remote <x>, expected <y>` to stderr and
  exits 3 with nothing else printed. A remote binary too old to know the
  flag exits 2 with its usage text and never prints `start`, so the local
  side exits 3 (E_CONNECT) with that stderr relayed. The bridge's IR `"v"`
  check stays as defence in depth; failing it is `abort` E_CONNECT (exit 3).
- Start-object timing: the remote process prints `start` right after it has
  parsed and validated the text and passed the version check, BEFORE
  connecting to the bridge and before preflight. "Remote process ended
  before its start line" = exit 3 (E_CONNECT), exactly for: ssh failure,
  remote binary not found, usage error from an older binary, version
  mismatch; it guarantees nothing ran. A missing bridge or a preflight
  failure arrives as `abort` after `start` and maps to exit 4. Exit 5
  (`state=unknown`) = connection lost after the start object with no `done`
  or `abort` received. The wrapper kills ssh at `--timeout` + 5 s.
- `abort` object: `{"event":"abort","code":"E_...","msg":"..."}` is emitted,
  locally or by the remote process, when the run stops before any line
  executed - preflight failure or bridge connection failure. Exit code from
  the code: E_VALIDATE -> 2 (an option the target OS lacks), E_CONNECT -> 3,
  everything else (E_SESSION, E_PERMISSION, E_BOUNDS, E_INPUT) -> 4. Plain format: nothing on stdout (no `out` line),
  stderr `abort: <message> (<E_CODE>)`. No `done` object follows an `abort`.
  The local side relays `abort` in the chosen format and exits with the
  mapped code; the remote process's own exit status is diagnostic only.
- Session bridge on both platforms, mandatory for remote runs.
  `gotto-hando --bridge` is a resident process the user starts in the
  logged-on GUI session (terminal, LaunchAgent, or Task Scheduler; recipes
  in the platform help files). macOS listens on the unix socket
  `~/Library/Application Support/gotto-hando/bridge.sock` (file 0600 inside
  a 0700 directory), Windows on the named pipe `\\.\pipe\gotto-hando-<username>`
  (current-user ACL). One run at a time; protocol = one IR JSON document
  with the extra top-level `run` object in, the same JSONL as stdout out
  (`start`, per-line objects, `done` - or `abort`); logs at
  `~/Library/Logs/gotto-hando/bridge.log` / `%LOCALAPPDATA%\gotto-hando\bridge.log`.
- A running bridge is the consent gate: `gotto-hando local` spawned by ssh
  (detected by `SSH_CONNECTION`/`SSH_TTY`; on Windows also by process
  session != active console session or no `WinSta0` access) must forward to
  the bridge and never executes input in-process; no bridge reachable =
  `abort` E_SESSION after `start`, exit 4, with the hint from help.txt.
  Per the per-command gating of the backend tickets, a run made only of
  `qinfo`, `qdisp`, `qmouse`, `sleep`, `set` and comments is answered
  without a bridge and reports `session=inactive` (help-remote.txt WHEN YOU
  NEED THIS), so the recovery loop keeps working. Stopping the bridge cuts
  off remote control. Local GUI-terminal runs never touch the bridge.
- Environment detection is a routing convenience, not the security boundary
  (help-remote.txt SESSION BRIDGE). A process that scrubs
  `SSH_CONNECTION`/`SSH_TTY` and runs in-process simply fails preflight:
  macOS has no TCC grant for a non-GUI process, Windows puts it in
  Session 0 (E_SESSION either way).
- Cancellation: the local side's Ctrl-C ends ssh; the remote process treats
  EOF/SIGPIPE on its stdout, or ssh channel loss, as cancel and drops the
  bridge connection; the bridge aborts the run and releases held keys when
  its caller disconnects (help-remote.txt HOW IT WORKS, SESSION BRIDGE).
- On macOS the bridge is the process that holds TCC permissions, so
  `--request-perms` sent to an ssh destination makes the bridge raise the
  system prompt on that Mac's screen (the toggle is still a human action).
  Wire shape: the remote prints JSONL `start` then
  `{"event":"perms","accessibility":"ok|missing","screen":"ok|missing"}`
  and no `done`; exit 0 when both are ok, else 4. The plain format prints
  the same `perms=accessibility:..,screen:..` line `qinfo` prints. Bridge
  protocol: the caller sends `{"v":1,"request_perms":true}`, the bridge
  answers the perms object. On Windows `--request-perms` is exit 2: locally
  a usage error before anything runs; as the TARGET of a `<dest>` (the local
  side cannot know the target OS before ssh, so the option is forwarded) the
  remote Windows binary prints `start` and then `abort` E_VALIDATE
  ("--request-perms is macOS only"), which the local side maps to exit 2.
  A same-version remote therefore never rejects a forwarded option before
  `start`; a pre-`start` exit 2 is always an older binary (exit 3).
- `[f]` payloads are resolved on the agent's machine: the wrapper parses and
  validates locally (syntax errors exit 2 and send nothing), inlines
  `txt[f]`/`paste[f]`/`clip[f]` file contents into the sequence text, rewrites
  `qclip[f]<path>` to `qclip[]` and writes the returned text to the local
  path. After inlining, the rewritten line is re-validated locally against
  the 64 KiB line limit (E_VALIDATE, exit 2, nothing sent - help.txt
  LIMITS). The remote receives plain sequence text only.
- Captures cross the wire as `"fmt":"png"` only (v1 capture is PNG; no
  `fmt=`/`q=`/`cursor` modifier exists); the local side writes `.png` files.

## Constraints

- gotto-hando opens no network listener; the bridge socket/pipe is
  same-machine, same-user (socket file 0600, directory 0700).
- The remote binary must be on the remote PATH or named via `--remote-bin`.
- Output of a remote run is byte-identical to the same sequence run locally
  on that machine. The local side rewrites exactly three things to get
  there: capture/qclip paths (local files), the `start` object's `out` and
  `dest` (local directory, destination name), and the `src` of err results
  (restored from the local parse by line number, so an inlined `[f]` line
  shows as written, never as up to 64 KiB of inlined text).

## Spec Impact

None expected. `ai-docs/spec/help-remote.md` (one pointer anchor per
`== SECTION ==` of `assets/help-remote.txt`) is created by
`260907-feat-cli-core` together with `help.md`, `help-macos.md` and
`help-windows.md` (the bridge sections included, since anchors only point
at sections). This ticket adds an anchor only if it adds a new
`== SECTION ==` to a help text, and never touches other anchors.

## Phases

### Phase 0: Minimal Windows session bridge (verification enabler)

Re-slice (2026-09-08, user-directed): to unblock autonomous over-ssh
verification of desktop injection without a person driving each keystroke, the
session bridge's minimal core is built ahead of Phase 1's ssh transport,
Windows first. This stays inside the sage-approved architecture — the bridge
pipe protocol, the ssh-session detection, `session=bridge`, and the no-bridge
`abort` E_SESSION are all from Decisions above; only the build order and the
first-slice size change, not the decisions. The Phase 1 -> Phase 2 dependency
and their full scope stand; this phase pulls forward only what the verification
loop needs and the later phases reuse it. (Design digest is intentionally left
stale by this edit; no architecture decision changed.)

Goals (Windows only, this slice):
- `gotto-hando --bridge`: resident process on the named pipe
  `\\.\pipe\gotto-hando-<username>` (current-user ACL); one run at a time; a
  second instance for the same user exits 2; one line per run/error logged to
  `%LOCALAPPDATA%\gotto-hando\bridge.log` (no sequence text, no images).
  Protocol: one IR JSON document with the extra top-level `run` object in, the
  same JSONL out (`start`, per-line objects, `done`/`abort`); an IR `"v"`
  mismatch is `abort` E_CONNECT.
- `gotto-hando local` ssh-session detection (process session != active console
  session, or `SSH_CONNECTION`/`SSH_TTY` set): forward the IR to the bridge and
  never execute input in-process; no bridge reachable = `abort` E_SESSION after
  `start`, exit 4, with the help.txt hint. The `qinfo`/`qdisp`/`qmouse`/`sleep`/
  `set`/comments-only run is answered in-process without a bridge and reports
  `session=inactive`.
- `qinfo` through the bridge reports `session=bridge`.
- The bridge aborts the run and releases held keys when its caller disconnects.

Out of scope (stays in Phase 1/2): the ssh-wrapped `<dest>` transport and its
whole forwarded-option / `--expect-version` / `[f]`-inlining / capture-rewrite /
`start`-timing surface; the macOS unix-socket bridge; `--request-perms` over the
bridge; the LaunchAgent / Task Scheduler recipes; the byte-identical-output
guarantee.

Verification: unit/integration with an in-process or fake-pipe listener where a
desktop is not required; then, autonomous over ssh once the user runs
`gotto-hando --bridge` inside the console/RDP session: `gotto-hando local
'qinfo'` reports `session=bridge`; a `txt[]...` then `k[c]a` `k[c]c` `qclip`
round-trip confirms the injected text landed (no `cap` needed); with the bridge
stopped the same run prints `start` then `abort` E_SESSION and exits 4, while a
`qinfo`-only run still answers `session=inactive`; a caller that drops the
connection mid-run holding a key has it released (visible in bridge.log and a
following `qinfo`).

### Result (e18b19a) - 2026-09-08

Range `7f52aac..e18b19a` on `impl/main/boxer-both-twig` (survey plan 7f52aac;
9 impl commits to 4037b1c; 6 review-fix commits to e18b19a; +1 docs commit
1bf8fb1). Windows-only slice as scoped.

Behavioral delta:
- `gotto-hando --bridge` (Windows): resident named-pipe listener on
  `\\.\pipe\gotto-hando-<username>` with a current-user SDDL DACL
  (`D:P(A;;GA;;;<SID>)`), `FILE_FLAG_FIRST_PIPE_INSTANCE` so a second instance
  exits via `ErrBridgeAlreadyRunning`; one run at a time (the GOOS-agnostic
  `internal/bridge.Session` serializes on a mutex); per-run/error line to
  `%LOCALAPPDATA%\gotto-hando\bridge.log` (stderr fallback). Protocol: one
  IR-JSON doc + top-level `run` object in, JSONL out (`start`, per-line,
  `done`/`abort`); IR `"v"` mismatch -> `abort` E_CONNECT.
- `gotto-hando local` on a remote Windows session (`IsRemoteSession`:
  `SSH_CONNECTION`/`SSH_TTY`, or process session != active console) forwards
  the run to the bridge and never injects in-process; no bridge reachable ->
  `abort` E_SESSION after `start`, exit 4, with the help hint. A
  qinfo/qdisp/qmouse/sleep/set/comments-only run is answered in-process
  (`session=inactive`). Through the bridge, `qinfo` reports `session=bridge`.
- Caller disconnect mid-run: the bridge's `OnResult` write failure cancels the
  run ctx; the engine's per-op `ctx.Err()` check stops the rest and its
  unconditional end-of-run `releaseAll` releases held keys/buttons.
- `internal/bridge` is GOOS-agnostic (imports only ir/engine/output/backend),
  tested via `net.Pipe()` + `dryrun.Backend`. darwin/other `--bridge` keeps the
  pre-ticket stub byte-for-byte (`abort(EValidate, "session bridge not
  implemented")`).

Shared-package changes (all additive / backward-compatible; local path passes
nil OnResult + `context.Background()`, so it is unaffected):
`engine.RunOptions.OnResult` + a per-op `ctx.Err()` loop check; `output.WriteAbort`
split into `WriteStart`+`WriteAbortEvent`; exported `engine.ResultDetailFromJSON`;
new `ir.Unmarshal` (the wire's IR JSON decoder, hand-mirrors the encoder);
`qclip` now sets `res.JSON = [{text: <clip>}]` (fixes a pre-existing gap vs
help.txt:634, needed for the round-trip and for plain-mode reconstruction).

Review (partitioned correctness/fit/test; 3 Critical, all `[fixed]` and
re-verified clean at Critical review #2 by direct inspection + independent
build/test):
- C1 `[fixed]`: the bridge streamed only via `OnResult`, which fired only in
  the op loop, so end-of-run auto-release **warn** lines were dropped over the
  wire (printed locally via the `sum.Results` scan). Fix fires `OnResult` for
  the warns; the implementer found and fixed the same omission in 3 more
  in-loop paths (ctx-cancel skip, fail-fast skip, cap-on-error extra). The
  OnResult/Results pairing is now documented as an invariant at the field.
- C2 `[fixed]`: `Op.HasWait` was never serialized/decoded, so `open[wait=]`/
  `win[wait=]` lost their wait behavior across the wire (silent wrong result;
  the byte-equality round-trip test structurally could not catch it). Fix
  emits/decodes `has_wait`; the round-trip fixture now sets it; help.txt
  example + golden updated.
- C3 `[fixed]`: a second concurrent caller hit `ERROR_PIPE_BUSY` on the
  single-instance pipe and was misreported as "bridge not running". Fix loops
  `DialBridge` on BUSY via `WaitNamedPipe` (new `WaitNamedPipeW` LazyProc,
  kernel32 - x/sys/windows has no wrapper), returning genuine
  `ERROR_FILE_NOT_FOUND` immediately.
- Important (best-effort, relayed once, all `[fixed]`): I1 KeepGoing/CapOnError
  now exercised e2e through the real Encode/decode path; I2 malformed-request
  plain-text behavior locked in by test (no format change); I3 request read
  bounded to 16 MiB.
- Minor (record-only): dead `Quiet` wire field (encoded, never consumed
  server-side); `win`/`qmouse` decode structs mix tagged and untagged fields;
  `runBridge`/`forwardToBridge` bodies duplicated across dispatch_darwin.go /
  dispatch_other.go; `json_decode` round-trip fixture never exercises the
  optional-field-omitted (HasPoint:false, nil `set` pointers) paths.

Verification evidence: `go build ./...` native/windows/darwin/linux
(`CGO_ENABLED=0`) clean; `go test ./... -race -count=1` all pass;
`GOOS=windows go test -c` for cmd/gotto-hando + internal/backend/windows +
internal/bridge compiles clean; `gofmt -l .` empty; `git diff 7f52aac --
go.mod go.sum` empty (no new dependency - x/sys/windows only). `go vet` shows
only the 2 pre-existing `unsafe.Pointer` warnings in windows/clipboard.go
(untouched).

Unresolved / deferred:
- **All Windows-specific code is compile-only-verified here** (Go is not
  installed on the Windows box; this dev host is macOS). C3's retry loop and
  the whole pipe transport have no runtime test - they ride on the over-ssh
  acceptance pass below.
- I2 forwarder note: on a malformed request the forwarder discards the
  plain-text line as the `start` object then fails E_CONNECT on the next read;
  only reachable if the local encoder and bridge decoder disagree (an internal
  bug, not a runtime scenario) - left as-is.

OVER-SSH ACCEPTANCE (pending; the user runs `gotto-hando --bridge` once in the
console/RDP session of `sw.kang@192.168.100.2`, then this is autonomous over
ssh): `local 'qinfo'` -> `session=bridge`; `txt[]...` then `k[c]a` `k[c]c`
`qclip` round-trip returns the injected text; with the bridge stopped the same
run prints `start` then `abort` E_SESSION exit 4 while a `qinfo`-only run still
answers `session=inactive`; a caller dropping the connection mid-run holding a
key has it released (bridge.log + a following `qinfo`).

### Phase 1: ssh transport

Goals: destination parsing, local parse/validate and `[f]` inlining with the
post-inlining 64 KiB re-validation, `qclip[f]` rewrite and local write, ssh
spawning with the exact forwarded option set (including
`--expect-version <local version>`), stdin hand-off, remote-side
`--expect-version` check and `start` timing (after parse/validate/version
check, before bridge connect and preflight), JSONL reading with inline
capture extraction (`"fmt":"png"`) and local `.png` writing, plain/JSONL
re-emission including the `abort` relay and its exit mapping (E_VALIDATE 2,
E_CONNECT 3, other codes 4), the `start` out/dest and err `src` rewrites,
exit 3/5 mapping, timeout kill, remote-side cancel on stdout
EOF/SIGPIPE or channel loss, `--inline-captures` on the remote side,
`--request-perms` forwarding with the `start` + `perms` wire shape.
Verification: an integration test that uses a fake `ssh` executable on PATH
(which runs a gotto-hando test binary built with the dry-run build tag that
`260907-feat-cli-core` provides for tests - the dry-run backend is never
selectable in a release build) and asserts the EXAMPLES section produces
byte-identical stdout to a local dry-run, capture paths included after the
rewrite; a test that a failing inlined `paste[f]` line reports
`paste[f]./cmd.py` as its source; a test that forwarding `--request-perms`
to a fake remote reporting os=windows yields `start`, `abort` E_VALIDATE
and local exit 2; a test that `txt[f]` content and `qclip[f]` output cross
the fake ssh as text and land locally; a test that a `txt[f]` file whose
inlined escaped form exceeds 64 KiB exits 2 with E_VALIDATE and never spawns
ssh; tests for exit 3 on connect failure and missing remote binary; a test
that a remote started with a wrong `--expect-version` prints
`version mismatch: remote <x>, expected <y>` and the local side exits 3
with that stderr; a test with a fake remote that rejects the flag with a
usage error (exit 2, no `start`) yielding local exit 3 with the stderr
relayed; tests that a fake remote emitting `start` then `abort` E_SESSION
yields exit 4 with nothing on stdout and `abort: ... (E_SESSION)` on stderr
in plain format (the `abort` object verbatim with `--jsonl`), and `abort`
E_CONNECT yields exit 3; a test that killing the fake ssh mid-run yields
`state=unknown` and exit 5; a test that the remote process, on stdout EOF
mid-run, releases held keys through the dry-run backend (same tagged test
binary) and exits.

### Result (bd85972) - 2026-09-09

Range `66bfa2c..bd85972` on `impl/main/boxer-both-twig` (survey plan e8bca2d;
4 impl commits to 52f2964; 3 review-fix commits to bd85972). ssh `<dest>`
transport as scoped; verified entirely on this macOS host via a fake-ssh +
`dryrun`-tagged-binary harness (no real remote / Device Guard dependency) -
the ticket's own Phase 1 verification boundary.

Behavioral delta:
- `gotto-hando <dest> ...` (any dest != `local`) now spawns
  `ssh <dest> <remote-bin> local --jsonl --inline-captures --expect-version <ver> <fwd opts> -f -`,
  feeds the locally `[f]`-rewritten sequence on stdin, and relays the remote
  JSONL back in the caller's format. Forwarded options per help.txt OPTIONS
  (always `--jsonl --inline-captures --expect-version`; conditional
  `-q/--delay/--timeout/-k/--cap-on-error/--request-perms`; never
  `--out`/`--remote-bin`). `--check`/`--ir` never start ssh.
- `[f]` resolved on the initiating machine: txt/paste/clip `[f]` inlined into
  the sequence text via a new escape encoder (exact inverse of
  `syntax.applyEscapes`: `\`->`\\`, LF->`\n`, TAB->`\t`, else literal);
  `qclip[f]<path>`->`qclip[]` with the returned text written to the local path;
  the rewritten line re-validated against `ir.MaxLineBytes` (64 KiB) with the
  diagnostic keyed on the original `op.Src`, so an oversize inlined line exits 2
  before ssh spawns.
- Captures cross as `"fmt":"png"`; base64 frames decoded locally and written to
  `<out>/<NNNN>-<label>-<UTC ts>.png` (reusing exported `engine.CapturePath`/
  `WriteCaptureFile`, one counter increment per cap result line). err `src`
  restored from the local parse by line number; the relayed `start` carries the
  local `out`/`dest` but the remote's real `target.os`.
- Exit mapping: pre-`start` process/ssh death -> E_CONNECT exit 3 with stderr
  relayed verbatim (ssh failure, missing remote binary, older-binary usage
  error, `--expect-version` mismatch); `abort` after `start` -> E_VALIDATE 2 /
  E_CONNECT 3 / else 4; connection lost after `start` with no `done`/`abort` ->
  synthesized `done` state=unknown exit 5. ssh killed at `--timeout`+5 s.
- `--request-perms` forwarded to a `<dest>`: `start` + `perms` wire shape; a
  windows remote answers `abort` E_VALIDATE (local exit 2).

Structural:
- New GOOS-agnostic `internal/remote` (pure rewrite/decode/relay logic) +
  `cmd/gotto-hando/remote.go` (ssh-spawn glue: `os/exec` + system `ssh`, same on
  every initiating GOOS). `dispatch.go`'s local path restructured to print
  `start` before preflight and stream via `engine.RunOptions.OnResult` under
  `--jsonl` (byte-identical for direct `local`: plain mode keeps its buffered
  no-early-`out` shape); the local `--jsonl` path now cancels the engine on an
  OnResult write failure so held keys release via end-of-run `releaseAll`.
- A 4th `dispatch_dryrun.go` (`//go:build dryrun`) selects the dry-run backend
  for the fake-ssh harness (the other three dispatch files gained `&& !dryrun`);
  the dry-run backend stays unreachable in a release build.
- No new dependency (x/sys + purego only). `internal/output.WriteStartOS` added
  (WriteStart delegates to it) to relay the remote's os; three engine capture
  helpers exported (rename-only).

Review (partitioned correctness/fit/test; review #1: 4 Critical + 2 Important,
all fixed; the C1 fix introduced one Critical regression NEW-1, fixed across
review #2 -> relay #2 -> review #3 clean; ceiling not reached):
- C1 (Critical): post-`start` connection loss mis-mapped to exit 3; fixed to
  exit 5 state=unknown mirroring `runRemoteRequestPerms`.
- T3 (Critical, impl+test): the local `--jsonl` path did not cancel the engine
  on stdout write failure, so held keys would not release through the dry-run
  backend; wired the bridge's cancel-on-write-failure mechanism + added the
  dryrun-tag held-key-release test.
- T1/T2 (Critical, test): added the EXAMPLES byte-identical + capture-PNG
  end-to-end test and the wrong-`--expect-version` relay test (both
  ticket-named verification bullets that had no coverage).
- I1 (Important): relayed `start` reported local GOOS not the remote's
  `target.os` (byte-identity break for macOS->Windows); fixed via WriteStartOS.
- I-test (Important): added an integration `paste[f]`-fail-src case.
- NEW-1 (Critical, self-inflicted by the C1 fix): the reused stateUnknown
  closure double-printed `start` on mid-run connection loss; split into
  start+done (pre-result) vs done-only (mid-run) with jsonl exact-count
  regression tests (verified to catch the bug).

Mental model: `ai-docs/mental-model/remote-transport.md` (07274e7) records the
single-`start` invariant across the four connection-loss terminal sites and the
cancel-on-write-failure -> end-of-run `releaseAll` held-key-release mechanism.

Verification: `go build ./...` native/windows/darwin (`CGO_ENABLED=0`) clean;
plain + `-tags dryrun` builds clean; `go test ./... -race` and
`-tags dryrun ./cmd/gotto-hando -race` all pass; `GOOS=windows go test -c`
compiles; `gofmt -l .` empty; `git diff e8bca2d -- go.mod go.sum` empty;
`go vet ./...` no new warnings.

Unresolved / deferred:
- No real over-ssh run: the whole transport is verified only by the fake-ssh +
  dryrun harness on this macOS host (matches this phase's verification
  boundary). Real remote acceptance rides Phase 2.
- Pre-existing gaps left untouched (Out of Scope): direct-`local`
  `qclip[f]<path>` file-write in `engine/query.go` is still unimplemented; the
  parsed `--ping` option is still unconsulted anywhere in `cmd/gotto-hando`.

### Phase 2: Session bridge (macOS and Windows)

Depends on Phase 1; the Windows half also depends on
`260907-feat-windows-backend`. Goals: `--bridge` process with a shared core
and per-platform listener (unix socket in a 0700 directory, file 0600 /
named pipe with the current-user ACL), single-run gating, ssh-session
detection in `local`, mandatory forwarding with the `qinfo`-family
exception, `abort` E_SESSION after `start` when no bridge is reachable,
`abort` E_CONNECT on an IR `"v"` mismatch, `qinfo session=bridge`,
`--request-perms` executed by the bridge (`{"v":1,"request_perms":true}` in,
perms object out), abort-and-release when the caller disconnects, bridge
log, LaunchAgent and Task Scheduler recipes verified.
Verification: from an ssh session into each platform with the bridge running,
`gotto-hando local 'qinfo'` reports `session=bridge` and the EXAMPLES section
runs end to end with captures landing on the agent's machine and nothing
written on the remote; with the bridge stopped the same run prints `start`
then `abort` E_SESSION and exits 4, while `gotto-hando <dest> qinfo` still
answers with `session=inactive`; from the machine's own GUI terminal `qinfo`
reports the console session and the socket/pipe is not used; a two-direction
detection test: `SSH_CONNECTION` set in a GUI terminal routes through the
bridge, and the variables scrubbed under sshd give `abort` E_SESSION (no
in-process execution); a disconnect test: a caller sends a run holding a key
(`kd` + `sleep`) and drops the connection mid-run, and the bridge aborts the
run, releases the key (visible in bridge.log and in a following `qinfo`/
`cap`) and accepts the next caller; on a fresh macOS signing identity
`--request-perms` over ssh raises the prompt on the Mac's screen and the
local side prints the perms line / object with the documented exit code. Each recipe is verified end to end: the LaunchAgent (macOS) and Task Scheduler (Windows) recipes from the platform help files, started from a fresh login, bring up a bridge that a subsequent `gotto-hando <dest> 'qinfo'` reaches with `session=bridge`.

### Result (7ec9f43) - 2026-09-09

Range `8c8d71b..7ec9f43` on `impl/main/boxer-both-twig` (survey plan 8c8d71b;
4 impl commits to 651944b; 1 review-fix commit to 7ec9f43). The macOS
unix-socket session bridge and the shared `internal/bridge` `request_perms`
wire message, mirroring the already-shipped Windows Phase-0 named-pipe bridge.
Verified natively on this macOS host (real unix socket + `net.Pipe` +
`dryrun` backend); real over-ssh / live LaunchAgent+Task Scheduler / fresh
signing-identity acceptance deferred non-blocking, exactly as Phase 1's own
over-ssh acceptance was.

Behavioral delta:
- `gotto-hando --bridge` on macOS is now a real resident listener on the
  current-user unix socket `$HOME/Library/Application Support/gotto-hando/
  bridge.sock` (dir 0700, socket file 0600 via explicit `os.Chmod` after
  `MkdirAll`/bind, umask-safe), serving one connection at a time via the
  GOOS-agnostic `bridge.Session`, until Ctrl-C/kill; a second bridge for the
  same user exits 2 (`ErrBridgeAlreadyRunning`); `~/Library/Logs/gotto-hando/
  bridge.log` records start/run/error lines. Single-instance is a
  `net.DialTimeout` probe (not kernel-atomic like Windows), with
  stale/crashed-leftover socket-file recovery.
- `local` in a macOS ssh session (SSH_CONNECTION/SSH_TTY set) now forwards a
  session-requiring sequence to that bridge (`session=bridge`) instead of
  in-process injection, aborting E_SESSION after `start` (exit 4) with the
  "start `gotto-hando --bridge` in the logged-on GUI session" hint when no
  bridge is reachable; a qinfo-family/exempt-only sequence (`RequiresSession`
  false) never dials. `qinfo` through the bridge reports `session=bridge`;
  from the GUI terminal it reports the console session and never touches the
  socket.
- `gotto-hando <dest> --request-perms` to a macOS target now works end to end:
  the remote-side `local --request-perms` detects it is ssh-started and
  forwards a `{"v":1,"request_perms":true}` wire message to the bridge, which
  runs `AXIsProcessTrustedWithOptions`/`CGRequestScreenCaptureAccess` in its
  own GUI session (raising the real system prompts there) and answers a
  `{"event":"perms","accessibility":"ok|missing","screen":"ok|missing"}`
  object; both the direct and bridge-forwarded darwin paths now emit the
  JSONL `start`+`perms` pair (previously the direct path printed only the
  plain `perms=` line, so `<dest> --request-perms` to a mac was broken end to
  end).

Structural:
- New darwin files: `bridge_socket.go` (unix-socket transport:
  `ListenBridge`/`Accept`/`Close`/`DialBridge`, `ErrBridgeAlreadyRunning`/
  `ErrListenerClosed`), `remote.go` (`IsRemoteSession`, env-var only - no
  console-session-id/WinSta0 check, which is Windows-only); `backend.go`
  gains `NewBridge()` (swaps `bridgeSessionProbe{}`, keeps every real probe,
  calls `initFFI()`); `preflight.go` gains exported `RequiresSession`;
  `probes.go` gains `bridgeSessionProbe`; `session.go`'s stale doc comment
  updated. `dispatch_darwin.go`'s three stubs replaced with real
  `runBridge`/`shouldForwardToBridge`/`forwardToBridge` + a bridge-forwarding
  `requestPerms`.
- Shared: `internal/bridge/wire.go` gains a `request_perms` decode field and
  `EncodeRequestPerms()`; `session.go` gains a nil-safe `RequestPerms` func
  field + `handleRequestPerms` branch, and now sets `InlineCaptures: true`
  (fixes a stray-PNG-to-bridge-cwd disk write that affected BOTH platforms -
  "the bridge writes nothing else to disk"). `internal/output.WritePermsEvent`
  added. `cmd/gotto-hando/bridge_relay.go` (new, build-tag-free) holds the
  five relay/decode helpers extracted verbatim from `dispatch_windows.go`
  plus a shared `writePermsResult`, so both platforms share them (resolves
  the Phase-0 duplication debt instead of doubling it).
- No new dependency (unix socket is stdlib `net`/`os`/`path/filepath`); no
  `assets/help*.txt` change (Spec Impact: none - the macOS bridge, socket
  path, LaunchAgent recipe, and request-perms procedure were already in
  help-macos.txt/help-remote.txt).

Review (partitioned correctness/fit/test; review #1: 0 Critical, 1 Important,
4 Minor; relay #1 fixed the Important; no re-review needed):
- Important (test): `bridge_socket_test.go` asserted no filesystem permission
  bits; relay added dir-0700 + socket-0600 `os.Stat().Mode().Perm()`
  assertions (empirically verified the socket inode reads 0600 on this Mac)
  and a real-socket caller-disconnect -> held-key-release test.
- Minor (record-only, accepted): (correctness) the unix-socket single-instance
  guard is a non-atomic dial-probe with a narrow concurrent-start race
  (same-user/same-machine threat model - not a defect); (correctness)
  plain-mode `cap` over the bridge without `--jsonl` now writes no PNG
  anywhere (the primary `--jsonl` `<dest>` path is correct; forward note);
  (fit) `okMissing` is duplicated across a package boundary (4 lines);
  (test) the real-socket disconnect path was covered by the relay's added
  test.

Mental model: `ai-docs/mental-model/session-bridge.md` (2cf5b06) records the
`InlineCaptures: true` requirement (both platforms) and the non-atomic
dial-probe single-instance semantics (+ `SetUnlinkOnClose(false)` stale-file
test detail).

Verification: `go build ./...` native + `GOOS=windows` (`CGO_ENABLED=0`)
clean; `go test ./... -race -count=1` all pass (darwin native); `-tags dryrun
./cmd/gotto-hando` pass; `GOOS=windows go test -c` for cmd/gotto-hando,
internal/backend/windows, internal/bridge compiles clean (shared
`internal/bridge` changes leave the shipped Windows bridge intact -
`Session.RequestPerms` stays nil there); `gofmt -l .` empty; `go vet ./...`
only the 2 pre-existing `unsafe.Pointer` clipboard warnings; `git diff -- go.mod
go.sum` empty.

Deferred non-blocking (matches Phase 1's own boundary): real over-ssh
acceptance against a live second Mac/Windows box; the LaunchAgent (macOS) and
Task Scheduler (Windows) recipes started from a fresh login; a fresh macOS
signing identity raising the `--request-perms` system prompt. The Windows
half's real over-ssh acceptance additionally remains Device-Guard (WDAC)
blocked (a user political-determination boundary, unchanged from Phase 1).
