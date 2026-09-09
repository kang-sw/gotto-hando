# Mental Model: remote-transport

`cmd/gotto-hando/remote.go` (the `<dest>` ssh wrapper: `runRemote`/
`runRemoteRequestPerms`) and `internal/remote` (`Rewrite`, `Relay.Process`,
the JSONL decoders) — plus the `--jsonl` cancel-on-write-failure wiring
`cmd/gotto-hando/dispatch.go`'s `local --jsonl` path shares verbatim with
`internal/bridge/session.go`. The external contract lives at
`{#260908-remote-how-it-works}`; this file captures only what the code
cannot make obvious on its own — rules whose violation is a subtle
output-contract or runtime defect (invisible to plain-mode or
prefix-style tests) rather than a compile error or an obviously-failing
test.

## Domain Rules

- **Every JSONL-to-plain relay must delegate command-specific output to
  `engine.ResultDetailFromJSON`; do not reformat a newly structured result in
  a relay.** Both `internal/remote.Relay.Process` (ssh) and
  `cmd/gotto-hando/bridge_relay.go`'s `relayResult` receive JSONL as the
  complete result wire format, but plain output needs the engine's derived
  `Detail`, `Extra`, and `AlwaysShow` fields. The shared helper is therefore
  the compatibility boundary: adding result metadata means adding its
  JSON-to-plain reconstruction there, then letting both relays call it.
  `rclip` made the failure mode concrete: relaying only the common fields
  silently changed `1 ok rclip type=image bytes=... 640x480` into a bare
  `1 ok rclip` for bridge or ssh runs while direct local execution remained
  correct. JSONL itself can stay byte-for-byte whenever no local rewrite is
  required; only plain mode reconstructs. This is covered by the rclip cases
  in `internal/engine/wire_result_test.go` and
  `internal/remote/relay_test.go`.

- **The wrapper prints its own `start` object exactly once — never make a
  connection-loss closure reusable across sites on both sides of that
  print.** `runRemote` has four terminal "connection lost" sites: the
  pre-first-result post-start EOF (scanner dies before the first
  post-start line ever arrives) and three mid-run sites (relay `Process`
  error, the `stdout.Write` failure, and a mid-run scanner EOF). Only the
  first of these runs *before* `runRemote` has committed to printing its
  own `start`; the other three run *after* it (line ~200,
  `output.WriteStartOS`). A shared closure that prints `start` would
  therefore print it a second time at the mid-run sites, emitting two
  `{"event":"start"}` objects and breaking the "byte-identical to a
  genuine local run" contract (a real local run emits exactly one
  `start`). The fix (commit bd85972, reverting a regression introduced
  by an earlier fix that folded `WriteStartOS` into one shared
  `stateUnknown` closure) is two separate shapes: `stateUnknownDone`
  (used by the three mid-run sites) prints only a synthesized `done`;
  the pre-first-result EOF site inlines its own `start`+`done` pair
  directly, mirroring `runRemoteRequestPerms`'s own pre-result EOF
  branch. This is invisible to a plain-mode test (plain mode never
  prints `start` as a discrete token to grep for) and invisible to any
  JSONL test that only checks for a `start` event's *presence* — only an
  exact-count assertion catches it, which is why
  `TestRemoteKillMidRunJSONLSingleStart` and
  `TestRemoteKillBeforeResultJSONLSingleStart` exist as dedicated exact-
  count regression tests (`cmd/gotto-hando/remote_test.go`) distinguishing
  the two closure shapes explicitly.

- **Held-key/button release on stdout write failure works because the
  engine's end-of-run `releaseAll` ignores context cancellation — the
  cancel only stops further op *execution*, it does not itself release
  anything.** `dispatch.go`'s `local --jsonl` path and
  `internal/bridge/session.go` both run `engine.Run` under a
  `context.WithCancel(...)` and call `cancel()` from inside `OnResult`
  when the per-result write to the caller's stream fails (broken pipe).
  `engine.Run`'s per-op loop (`internal/engine/run.go:135`) checks
  `ctx.Err()` and skips remaining ops once cancelled, but the
  unconditional end-of-run `st.held.releaseAll(ctx, be)`
  (`internal/engine/run.go:192`; `internal/engine/held.go:60`) never
  checks `ctx.Err()` at all — it always calls `be.KeyUp`/`be.ButtonUp` for
  every still-held key/button, best-effort, regardless of cancellation
  state. This two-part split (cancel stops new ops; release is
  unconditional) is what makes "the remote process, on stdout EOF
  mid-run, releases held keys" true for the ssh-wrapper's in-process
  (no-bridge) remote — and it is easy to accidentally break by gating
  `releaseAll` itself on `ctx.Err()` during a future refactor, which
  would silently leave keys held on every cancelled run. Covered only by
  `cmd/gotto-hando/dispatch_dryrun_test.go`'s
  `TestHeldKeyReleasedOnStdoutWriteFailure` (asserts release + prompt
  stop) against `TestHeldKeyReleasedOnCleanRun` (the control, proving
  release happens either way — only the early stop differs); this test
  only runs under `-tags dryrun` since `Backend.Calls` introspection is
  unavailable across the real ssh-spawned subprocess boundary.
  Plain mode is deliberately NOT wired to this cancel pattern — it has no
  per-result write to fail against until the whole run has already
  finished — and must stay that way to preserve plain mode's fully
  buffered, unconditional-byte-output contract.
