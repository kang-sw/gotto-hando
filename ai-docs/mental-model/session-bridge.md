---
domain: session-bridge
description: "GOOS-agnostic bridge request/response core (internal/bridge) plus the per-OS listener endpoints (darwin unix socket, windows named pipe)"
sources:
  - internal/bridge/
  - internal/backend/darwin/
  - internal/backend/windows/
related:
  remote-transport: "cmd/gotto-hando's forwardToBridge dials the per-OS listener and relays internal/bridge/session.go's JSONL stream back to the caller"
  windows-backend: "internal/backend/windows/bridge_pipe.go is the windows listener endpoint this domain contrasts against darwin's"
---

# Mental Model: session-bridge

The external contract lives at `{#260908-remote-session-bridge}`; this file
captures only what the code cannot make obvious on its own.

## Domain Rules

- **`internal/bridge/session.go`'s `Handle` must set `InlineCaptures: true`
  in the `engine.RunOptions` it builds for a normal run.** This field is
  GOOS-agnostic, so it governs both the macOS and Windows bridges. Without
  it, a `cap` op run through the bridge falls into
  `internal/engine/capture.go`'s `!inlineCaptures` branch and calls
  `WriteCaptureFile(CapturePath("", ...))` — a relative path resolved
  against the bridge process's own working directory — silently violating
  help-remote.txt SESSION BRIDGE's "the bridge writes nothing else to
  disk" on the wrong machine. It is easy to reintroduce when `Handle`'s
  `RunOptions` is next refactored, and is caught only by
  `internal/bridge/session_test.go`'s
  `TestHandleCaptureIsInlinedAndWritesNoFile` (asserts an inline `data`
  field AND `os.ReadDir` returns zero entries in the run's working
  directory). This is why the `<dest>` ssh wrapper's inline-capture relay
  path (`remote-transport.md`) is the primary Phase-2 capture path rather
  than a file left on the bridge host.

- **The macOS `--bridge` single-instance guarantee
  (`internal/backend/darwin/bridge_socket.go`'s `ListenBridge`) is a
  dial-probe, not atomic — unlike windows's `FILE_FLAG_FIRST_PIPE_INSTANCE`
  (`internal/backend/windows/bridge_pipe.go`), which the kernel makes
  atomic.** `ListenBridge` detects an already-running bridge by
  `net.DialTimeout`-probing the socket path: a successful dial means a live
  bridge is listening (`ErrBridgeAlreadyRunning`); `ECONNREFUSED`/`ENOENT`
  means a stale/crashed-bridge leftover, so it `os.Remove`s the path and
  proceeds to `net.ListenUnix`. Two consequences for a future editor:
  - There is a narrow concurrent-start race: two `--bridge` processes
    starting within the probe window can both pass the probe and both
    proceed to listen. This is accepted as low-impact under the
    same-user/same-machine threat model — not a defect to "fix" by
    removing the probe. The dial-probe is deliberately preferred over a
    bind-time `EADDRINUSE` check because a leftover socket file from a
    crashed bridge would make a naive bind fail even with no live bridge
    listening.
  - `net.ListenUnix` listeners unlink their socket file on `Close` by
    default, so `SocketListener.Close`'s explicit `os.Remove(l.path)` is
    reinforcing default kernel/stdlib behavior, not the only thing doing
    it. A test that wants to reproduce a crashed-bridge leftover file
    (rather than a clean shutdown) must bind a listener and call
    `SetUnlinkOnClose(false)` before closing it, or the file disappears
    like a normal exit — see
    `bridge_socket_test.go`'s `TestBridgeSocketStaleFileRecovers`.
