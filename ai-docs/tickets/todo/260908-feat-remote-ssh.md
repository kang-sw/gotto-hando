---
title: "Remote: ssh-wrapped destinations and the session bridge"
parent: 260907-epic-gotto-hando-v1
related:
  260907-feat-cli-core: prerequisite
  260907-feat-darwin-backend: prerequisite
  260907-feat-windows-backend: prerequisite for the Windows half of Phase 2
sage-review-design: completed
sage-review-design-reviewed: 70232986b745dd0e
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
local side prints the perms line / object with the documented exit code.
