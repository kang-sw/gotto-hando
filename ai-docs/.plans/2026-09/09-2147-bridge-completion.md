# Plan: bridge completion

## Relevant Ticket Contract
- Fix matching Windows SSH client/GUI-bridge runs that complete `qwin`/`cap` but lose their final completion, producing duplicate JSONL `start`/results and a spurious `E_CONNECT`/exit 5.
- Preserve the documented JSONL contract: one `start`, each per-line result once, then one `done` with the true totals and successful exit for a completed bridge run. Genuine connection loss and preflight aborts remain error paths.
- Keep the existing bridge wire format and public CLI interface. Do not restart the running bridge or deploy over its executable without coordination; use only read-only live verification.

## Out of Scope
- Bridge protocol redesign, new public options, GUI input, Blender work, and changes to capture/result schemas.
- Replacing the resident bridge or modifying `handoff.md`.

## Codebase Findings
- `internal/backend/windows/bridge_pipe.go#L176-L185` — `Session.Handle` defers `pipeConn.Close`; that close calls `DisconnectNamedPipe` immediately after the bridge has written its terminal `done`. [Microsoft's `DisconnectNamedPipe` contract](https://learn.microsoft.com/en-us/windows/win32/api/namedpipeapi/nf-namedpipeapi-disconnectnamedpipe) says this discards unread pipe data and specifies `FlushFileBuffers` first to wait until the client has consumed buffered data. `golang.org/x/sys/windows` already exposes `FlushFileBuffers`.
- `internal/bridge/session.go#L121-L134` — a successful run writes every result and `done` before the deferred connection close, so the engine has completed once even when its final wire object is later discarded by the pipe transport.
- `cmd/gotto-hando/dispatch_windows.go#L199-L225` — after committing its own start, the Windows forwarder relays result lines but routes an EOF through `abort()`. In JSONL, `abort()` emits another start plus `E_CONNECT`; the outer SSH relay then interprets those unexpected post-start objects as result-shaped input, explaining the inflated `ok=4` observation without repeated backend execution.
- `cmd/gotto-hando/remote.go#L200-L245` — the outer SSH wrapper correctly treats a received terminal `done` as authoritative, and only synthesizes `state=unknown` when its remote stream actually ends without one.
- `internal/output/writer.go#L61-L71` and `#L122-L140` — existing `output.Done`/`WriteDone` support the documented `state=unknown` fallback; no result or output schema change is needed.

## Implementation Plan
1. Update `internal/backend/windows/bridge_pipe.go` so the server-side `pipeConn.Close` flushes the named-pipe output before `DisconnectNamedPipe`. Always perform disconnect/reuse after the flush attempt, including when a client has already gone away and flush returns an error; this keeps cancellation from pinning the single listener instance and lets the next `Accept` proceed. Document the required `FlushFileBuffers`-then-disconnect lifecycle at that wrapper boundary.
2. Update `cmd/gotto-hando/dispatch_windows.go` only for failures after its local start has been committed: count successfully relayed result statuses and emit one synthesized `done` with `state=unknown`/exit 5 on a genuine late pipe EOF or local output-write failure, rather than invoking the full abort writer that would duplicate `start`. Retain the current pre-start dial/write/first-response failures and immediate bridge abort handling, including their existing error mappings.
3. Add Windows-focused regression coverage. In `internal/backend/windows` exercise a real listener/client pipe with the final server frame withheld from reading until close begins, proving the server waits for and preserves the terminal bytes before reuse; separately close the client during/before that flush and assert close returns and the same listener accepts a subsequent client. In `cmd/gotto-hando/dispatch_windows_test.go`, drive an in-process bridge session with two read-only query operations and a forced post-result disconnect; assert each backend operation ran once, the JSONL transcript has exactly one start, two results, and one `state=unknown` done with `ok=2`, and exit 5. Keep the normal bridge-forwarding test asserting one start, two results, one normal done, `ok=2`, and exit 0 once the pipe flush is in place.

## Verification Plan
- Run the focused Windows bridge-pipe and dispatch tests on Windows, then the repository Go test suite and Windows cross-build/check used by the project.
- Build a patched remote client separately and verify its genuine-loss fallback
  against the current bridge without replacing the running executable. Clean
  completion requires the patched server too: stage the reviewed binary at a
  new path, coordinate one GUI bridge restart with the user, then invoke the
  read-only remote `qwin` plus `cap` path and inspect raw JSONL. Expect one
  start, one result per submitted line, one done with `ok=2 err=0 skip=0`, and
  exit 0. Native pipe tests must use unique test endpoints so they never bind
  or disconnect the active desktop bridge.
- Confirm a deliberately disconnected post-start test still yields one start plus one unknown-state done/exit 5, while the existing preflight/version-abort tests retain abort behavior.

## Escalations
- None.
