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

- **Repo identity.** gotto-hando: AI 에이전트용 non-MCP 데스크톱 제어 CLI(Go 예정, macOS/Windows). 컨셉은 CONCEPT.md 참조.
- **Project map / topology.** `assets/` holds the normative help texts (`help*.txt`, single source of truth) embedded via `assets/assets.go`. `cmd/gotto-hando/` is the CLI entry point: argv scanning, `--help*`/`--version`/`--expect-version`, destination and line collection, dispatch. `internal/output/` owns the `E_*` error codes, the 0-5 exit-code mapping, and the plain/JSONL result/done/abort writers. Parser (`internal/syntax`), IR, the `Backend` interface, and the platform/remote backends are not built yet (later tickets).
- **Canonical flows.** `gotto-hando <dest> [options] [line ...]` parses argv, collects lines (argv or `-f`/stdin), builds the run, and (from Phase 2 on) parses each line into IR and executes it against a backend, streaming plain or `--jsonl` output. `--help*` short-circuits to the embedded asset. See `assets/help.txt` for the full contract.

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
