# AGENTS.md - gotto-hando

## Project Memory

Read at every session start, before other action:

1. **Preamble** - repo identity, project map/topology, and canonical flows live in this file's `## Project Orientation` section below; read the `repo` note layer at `ai-docs/ws-notes/` (one file per key) for volatile session context, `ai-docs/manuals/` for procedures, and generated ticket/spec inventories for current status. Keep only context a session must not re-derive.
2. **Project arc** - run `git log --oneline --graph -50`.

## Response Discipline

- **Evidence before claims.** Run verification and read output before stating success.
- **No performative agreement.** Restate the requirement, verify, then act or push back.
- **Actions over words.** Prefer "Fixed. [what changed]" or the diff. Skip filler.

## Code Standards

<!-- Project-wide code quality rules. -->

1. **Simplicity.** Write the simplest complete implementation that satisfies the spec.
2. **Surgical changes.** Change only what the task requires; follow existing style.
3. **Responsibility check.** Keep module roles clean; split when responsibility drifts.
4. **Testability.** Prefer explicit dependencies, minimal hidden state, pure logic over side effects.
5. **[Project-specific rule].** [Description.]

## Workflow

### Approval Protocol

- **Auto-proceed:** bug fixes, pattern-following additions, tests, boilerplate, single-module refactors.
- **Ask first:** new components/protocols, architecture changes, cross-module interfaces, observable behavior changes.
- **Always ask:** deleting functionality, changing protocol/API semantics, modifying persistence schema.

### Commit Rules

Auto-create one commit per logical unit. Include `## AI Context` explaining why the approach was chosen.

```text
<type>(<scope>): <summary>

<what changed - brief>

## AI Context
- <decision rationale, rejected alternatives, user directives, etc.>

## Ticket Updates                          # optional - ticket-driven only
- <ticket-stem>[: <optional-label>]
  > Forward: <future-phase finding>

## Spec                                    # optional - omit when none
- <spec-stem>
```

When a spec heading `{#slug}` changes, include `renamed-spec: <old-stem> -> <new-stem>`.

### Context Window Discipline

- Source code is ground truth; load only docs relevant to the task.
- Update drifted docs on contact.

## Architecture Rules

<!-- Project-wide invariants the AI must never violate. -->

1. **[Rule name].** [Rule description.]
2. **[Rule name].** [Rule description.]

<!-- Optional for GUI/TUI projects:
1. **Headless-testable architecture.** Domain logic and state live in framework-agnostic layers testable without a display. UI layers stay thin: no branching logic, state ownership, or domain knowledge.
-->

## Project Orientation

<!-- Every-session orientation an AI session needs without re-deriving it each
     time: repo identity, project map/topology, and canonical flows. Keep
     compact; route deep detail to specs, mental models, or manuals. -->

- **Repo identity.** gotto-hando: AI 에이전트용 non-MCP 데스크톱 제어 CLI(Go, macOS/Windows). 컨셉은 CONCEPT.md 참조.
- **Project map / topology.** `assets/` holds the normative help texts (`help*.txt`, single source of truth) embedded via `assets/assets.go`. `cmd/gotto-hando/` is the CLI entry point: argv scanning, `--help*`/`--version`/`--expect-version`, destination and line collection, dispatch. `internal/output/` owns the `E_*` error codes, the 0-5 exit-code mapping, and the plain/JSONL result/done/abort writers. `internal/syntax` is the pure text→IR parser (plus a post-parse `[f]` inline pass); `internal/ir` holds the IR types, static validation/LIMITS, and the `--ir`/`--check` serializer; `internal/backend` defines the thin `Backend` interface (with a test-only `dryrun` impl never bound to the CLI); `internal/engine` executes IR against a backend (composition, held-key tracking, delay precedence, fail-fast/`-k`, and — from the darwin backend on — a pre-op `Preflight` whose `backend.PreflightError` maps to the `abort` object). `internal/backend/darwin` and `internal/backend/windows` are the two real backends (purego on macOS, `golang.org/x/sys/windows` LazyDLL on Windows; both `CGO_ENABLED=0`): both run the keyboard/mouse/scroll/clipboard/text/query command set behind the five-check preflight gate and, on top of it, window control (`win`/`qwin`), screen capture (`cap`, whose raw pixels the engine encodes to PNG), `exec`, and `open` (with `wait=` window polling on the shared 100 ms loop) — darwin adds a Screen-Recording-gated capture check plus `--request-perms`, while windows has no Screen-Recording concept so capture is ungated (`EnumWindows`/`DwmGetWindowAttribute` enumeration, `SetForegroundWindow` focus with the `AttachThreadInput` foreground-lock bypass, GDI `BitBlt`/`PrintWindow` capture, a Job-Object-guarded `exec` timeout kill that leaves no orphaned `cmd /C` child, and `ShellExecuteExW` `open` are the windows-native equivalents) — so `local` executes the full command set for real on both macOS and Windows (the windows GUI path is acceptance-verified over the session bridge against a real GUI session, since Go is not installed on the target box). `internal/bridge` is the GOOS-agnostic session-bridge core (runs a forwarded IR through the engine against an injected backend, streaming the same JSONL back over one connection, one run at a time); `gotto-hando --bridge` runs it as a resident listener in the GUI session (Windows named pipe with a current-user ACL, or macOS unix socket at `$HOME/Library/Application Support/gotto-hando/bridge.sock` — dir 0700, file 0600, single-instance via a dial-probe), and `local` in a remote (ssh) Windows or macOS session forwards every run to that bridge (`session=bridge`) instead of observing the ssh-side process, aborting E_SESSION when no bridge is reachable. A non-`local` `<dest>` now spawns `ssh <dest> <remote-bin> local --jsonl --inline-captures ...` (the new GOOS-agnostic `internal/remote` rewrite/decode/relay package plus `cmd/gotto-hando/remote.go` ssh glue), feeding the locally `[f]`-inlined sequence on stdin and relaying the remote JSONL back in the caller's format — writing inline PNG captures and `qclip[f]` files locally and mapping exit 3/4/5 for pre-`start` connect failure / post-`start` abort / connection-lost `state=unknown` (verified on this macOS host by a fake-`ssh` + `dryrun`-tagged-binary harness). `--request-perms` to a macOS `<dest>` over ssh is executed by the bridge in its own GUI session (a `{"v":1,"request_perms":true}` wire message answered with a `perms` object); `local` on any OS other than macOS/Windows still exits 2. `ai-docs/spec/` holds pointer specs — one `{#stem}` anchor per help-text section — and drift tests across the packages fail whenever code and help text diverge.
- **Canonical flows.** `gotto-hando <dest> [options] [line ...]` parses argv, collects lines (argv or `-f`/stdin), parses each line into IR, and statically validates it. `--check` and `--ir` stop there (exit 0); `local` on macOS or Windows then runs each op against the platform backend (Preflight, then execution, streaming results), while a non-`local` `<dest>` spawns `ssh` and relays the remote's JSONL back (writing captures/`qclip[f]` locally), and `local` on any OS other than macOS/Windows still exits 2. Output streams as plain result lines or `--jsonl` objects; `--help*` short-circuits to the embedded asset. See `assets/help.txt` for the full contract.
- **Clipboard files.** `rclip[]<path>` carries a target-machine path through IR/ssh/bridge unchanged. `internal/engine/rclip.go` reads bounded regular files and normalizes text or images; the platform backends publish plain text or PNG plus TIFF (macOS) / CF_DIB (Windows). `golang.org/x/image` supplies BMP/TIFF codecs; native image data ownership and JSON-to-plain result reconstruction matter for resident bridge runs.

- **Release and installation.** `scripts/release.sh <version>` builds checksummed macOS arm64/amd64 and Windows amd64 assets from the help-sourced version. `scripts/install.sh` and `scripts/install.ps1` install to per-user `.local/bin`, give PATH guidance without changing it, and leave bridge startup manual. `.github/workflows/ci.yml` runs native tests and installer fixtures on pushes; `release.yml` validates and publishes `v*.*.*` tags. `ai-docs/ship/gotto-hando.md` owns the pre-1.0 version policy and release procedure. Use `scripts/vet.sh` for the documented Windows FFI analyzer exception; installation maintenance rules live in `ai-docs/mental-model/release-installation.md`.

## Project Knowledge

- Project state and cross-session context live in `ai-docs/`.
- Workflow shape and plugin-less maintenance guidance live in `ai-docs/WORKFLOW.md`; read it only if the `ws` or `wsflow` `workflow-manual` MCP tool is not in your toolbox. It is explanatory and does not override plugin runtime or MCP parser behavior.
- Before creating or editing tickets, follow the ticket conventions and the shape of existing tickets under `ai-docs/tickets/`.
- Reference tickets by stem only, never full path; stems survive status moves.
- To check ticket completion or prior phase results, use `git log --grep=<ticket-stem>` and inspect `## Ticket Updates`.
- Claude Code compatibility is `CLAUDE.md` containing `@AGENTS.md`.
- **Language:** AI-authored docs, plans, commits, tickets, and code comments are English. Human-facing UI strings are exempt.

<!-- Inclusion test: if breaking this rule makes a skill produce wrong results
     AND it applies everywhere, keep it here. Domain-scoped rules belong in
     `ai-docs/mental-model/<domain>.md ## Domain Rules`.
     Context goes in this file's `## Project Orientation` section or the
     `repo` note layer; process goes in skills. -->

<!-- Template Version: v0047 -->
