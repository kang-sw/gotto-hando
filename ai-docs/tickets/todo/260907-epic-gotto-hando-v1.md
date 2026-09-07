---
title: "gotto-hando v1: agent-facing desktop control CLI (M0-M2)"
sage-review-design: completed
sage-review-design-reviewed: 974fd0bee26f3338
---

# gotto-hando v1: agent-facing desktop control CLI (M0-M2)

## Scope

Deliver the first usable release of `gotto-hando`: a single static Go binary
that lets an AI agent drive a macOS or Windows desktop from the shell with the
one-line-per-command syntax defined in `assets/help.txt`, locally
(`gotto-hando local ...`) and on a remote PC reached over ssh
(`gotto-hando <ssh-destination> ...`).

Milestones covered (see CONCEPT.md ch. 13):

- M0: CLI core (parser, IR, output, embedded `--help*`) + macOS backend.
- M1: Windows backend.
- M2: ssh-wrapped remote destinations and the session bridge (both OSes).

## Non-Scope

- M3/M4 extensions (`qwin` filters, `cap[fmt=jpg,q=]`, `cap[cursor]`,
  ScreenCaptureKit/DXGI capture): tracked in `260907-feat-post-v1-extensions`.
- Apple notarization, installers, Homebrew tap, CI release matrix.
- Any MCP server, GUI, network listener, or resident daemon beyond the
  session bridge.
- Linux backend.

## Child Tickets

- `260907-feat-cli-core` - M0 part 1: module layout, embedded help, output
  formatter, parser -> IR, `--check`/`--ir`, backend interface, help drift
  tests. No prerequisites. Starts in `ready/`.
- `260907-feat-darwin-backend` - M0 part 2: macOS input, window/query/capture,
  permissions, `exec`/`open`. Prerequisite: `260907-feat-cli-core`.
- `260907-feat-windows-backend` - M1: Windows counterpart. Prerequisite:
  `260907-feat-cli-core`; reads the darwin ticket's landed interface as
  reference.
- `260908-feat-remote-ssh` - M2: ssh transport (Phase 1) and the session
  bridge on both platforms (Phase 2). Prerequisites:
  `260907-feat-darwin-backend` for Phase 1, `260907-feat-windows-backend`
  for the Windows half of Phase 2.

## Cross-Child Decisions

1. **Single source of truth.** `assets/help.txt`, `help-macos.txt`,
   `help-windows.txt`, `help-remote.txt` are the only normative documents.
   Any caller-visible behavior change edits the help text in the same commit
   as the code. CONCEPT.md carries rationale only; when it disagrees with the
   help text, the help text wins. Spec files under `ai-docs/spec/` are thin
   anchor-per-section pointers into the help files, never a second
   description of behavior.
2. **Help drift is a test failure.** The drift tests landed by
   `feat-cli-core` (spec anchor <-> `== SECTION ==` existence, parser command
   table <-> `== COMMANDS ==`, enum/limit constants <-> help tables,
   `--help*` output byte-identical to the embedded asset) must stay green in
   every child. `feat-cli-core` therefore creates all four pointer spec
   files (`help.md`, `help-macos.md`, `help-windows.md`, `help-remote.md`)
   at its closeout; later children add an anchor only when they add a
   `== SECTION ==`.
3. **One IR, one backend interface.** The parser produces the IR shown by
   `--ir` (help.txt IR JSON section); the engine drives a Go `Backend`
   interface defined in `feat-cli-core`; darwin and windows are two
   implementations of that interface and add no platform-specific commands.
   Local and remote execution differ only in transport.
4. **No cgo.** Windows via `golang.org/x/sys/windows`, macOS via
   `purego` dlopen of Quartz/ApplicationServices/AppKit. Cross-compiling both
   targets from one host must keep working.
5. **Security posture.** gotto-hando opens no network listener. Remote use
   is an ssh invocation of the same binary; authentication and encryption
   are ssh's. Remote runs must go through the session bridge
   (`gotto-hando --bridge`), a same-machine, same-user socket/pipe the user
   starts in the GUI session: a running bridge is the consent to be
   controlled and stopping it is the kill switch. Local GUI-terminal runs
   never use it. One exception, needed by the documented recovery loop
   (`qinfo`, `--ping`): a run consisting only of `qinfo`/`qdisp`/`qmouse`/
   `sleep`/`set`/comments is answered without a bridge and reports
   `session=inactive` (help.txt EXECUTION step 4). `exec`/`open` are allowed by design (the user already has a
   shell on the target).
6. **Policy inherited from cua-batch.** Validate everything before executing
   anything; fail-fast; release held keys/buttons in reverse order on failure;
   never paste when setting the clipboard failed; refuse to run in a locked
   or inactive session. Deliberate deviations (100 ms default delay, no
   implicit capture on error, 1000-line limit) are fixed by the help text.
7. **Help text is English**, ticket and spec text is English, conversation
   language does not change that.

## Completion Criteria

- Done: all four child tickets are in `.done/`; `go build` produces working
  darwin/arm64, darwin/amd64, windows/amd64 binaries from one host; the
  `== EXAMPLES ==` section of `assets/help.txt` runs end to end on a macOS
  machine locally and on a Windows machine over ssh via the bridge, with the
  named apps, paths and window titles substituted by ones present on the
  test machines (TextEdit/Notepad, a temp directory) - every line must
  parse unchanged (drift test (g)) and the substituted sequence must exit 0;
  drift tests pass; `ai-docs/spec/` carries an anchor for every help
  section.
- Dropped: the project direction changes to MCP or to a non-Go runtime.
- Deferred: everything listed under Non-Scope.
