# Plan: 260907-feat-cli-core — Phase 1: Executable, embedded help, output formatter, exit codes

## Relevant Ticket Contract

- Deliverables (ticket Phase 1 goals): `cmd/gotto-hando` main with option parsing
  for every flag in `assets/help.txt` OPTIONS + SYNOPSIS; embedded
  `--help/--help-macos/--help-windows/--help-remote` output byte-identical to
  the asset, ignoring all other args, exit 0; plain + JSONL result/`done`
  formatters and the `abort` report, implemented against synthetic results and
  unit-tested (exercised end-to-end only from Phase 2); exit code table;
  `--version` (prints the version string help.txt line 1 carries) and
  `--expect-version VER` (mismatch: stderr `version mismatch: remote <x>,
  expected <y>`, exit 3, nothing else printed); `--check`/`--ir`/`-f`/stdin
  collect lines but exit 2 "not implemented" until Phase 2; `local` recognized
  but exits 2 "platform backend not implemented, nothing ran" for anything
  needing a backend (including the bare TTY-shortcut-to-`qinfo` case); any
  other destination exits 2 "remote destinations not implemented"; drift test
  (g) — OPTIONS table == option parser's flag set — lands now.
- Decisions binding this phase (ticket `## Decisions`): no cgo, Go >= 1.23,
  module `github.com/kang-sw/gotto-hando`; repo layout follows CONCEPT.md
  ch. 12, names inside `internal/` are the implementer's call; options whose
  feature lands later (`--bridge`, `--remote-bin`, `--inline-captures`,
  `--request-perms`, non-`local` destinations) are parsed and rejected with
  exit 2 and a "not implemented" message, never exit 1; `--request-perms` on
  Windows stays exit 2 regardless.
- Abort exit mapping (ticket Phase 1 text + `assets/help.txt:708-723`
  EXIT CODES): `E_VALIDATE -> 2`, `E_CONNECT -> 3`, every other code `-> 4`.
  All Phase 1 "not implemented" paths must exit 2, so they all carry
  `E_VALIDATE` (EXIT CODES line 712-714 explicitly folds "an option the
  platform does not support" into exit 2).
- `assets/help.txt:1` — `gotto-hando 0.1.0 - ...`: the version token `0.1.0`
  is the single source of truth for `--version`/`--expect-version`.
- `assets/help.txt:10-45` (SYNOPSIS) and `:47-116` (OPTIONS): full option set
  and standalone-flag semantics (`--help*` "ignores every other argument").
- `assets/help.txt:543-594` (OUTPUT) and `:595-624` (JSONL): result-line,
  `done`-line and `abort` report formats (plain vs JSONL), including that a
  plain abort has **no** stdout at all (no `out` line), while a JSONL abort
  is `start` then `abort`, no `done`.
- `assets/help.txt:680-707` (ERROR CODES) and `:708-723` (EXIT CODES): the
  canonical `E_*` codes and the 0-5 exit table Phase 1 must encode as Go
  constants (full cross-check against these tables is Phase 3's drift test
  (d); Phase 1 only needs the constants and the abort-exit mapping to exist
  and be correct).
- CONCEPT.md ch. 12 (`:523-559`) puts CLI arg parsing/dest resolution/
  `--bridge`/`--help` branching directly in `cmd/gotto-hando/main.go` (no
  separate `internal/cli` package), and gives `internal/output/` the job of
  "plain/jsonl 라이터... exit code 계산" (formatters + exit-code computation).

## Out of Scope

- The DSL parser, lexer, IR types/JSON, static validation, `Backend`
  interface, dry-run backend, engine/state machine — all Phase 2.
- Full drift tests (a)-(f) (spec-anchor coverage, command/modifier tables,
  key names, error/exit code tables, LIMITS, format lint) — Phase 3. Only
  drift test (g) (OPTIONS table vs. parser flag set) is in scope now, per
  the ticket's explicit early-landing instruction.
- Any real backend behavior (window focus, input, capture, clipboard, exec,
  open) and anything ssh/bridge-related — later tickets
  (`260907-feat-darwin-backend`, `260907-feat-windows-backend`,
  `260908-feat-remote-ssh`).
- Semantic validation of `--delay`/`--timeout` duration strings (DURATIONS
  grammar) — store as opaque strings in Phase 1; nothing in this phase acts
  on their parsed value (every path that would need them exits 2 first), and
  DURATIONS parsing belongs to the Phase 2 parser.
- `ai-docs/spec/help.md` + `help-macos.md`/`help-windows.md`/`help-remote.md`
  creation — ticket says this happens at closeout of the whole ticket (after
  Phase 3 regularizes help.txt), not Phase 1.
- Actually printing/handling `--request-perms`, `--bridge` (foreground),
  `--remote-bin`, `--inline-captures` behavior — all later-ticket options,
  Phase 1 only parses and rejects them (exit 2, `E_VALIDATE`).

## Codebase Findings

- `go.mod` — module `github.com/kang-sw/gotto-hando`, `go 1.23`, no
  dependencies yet; greenfield, only `assets/` exists besides it.
- `assets/assets.go` — exports `assets.Help`, `assets.HelpMacos`,
  `assets.HelpWindows`, `assets.HelpRemote` via `//go:embed`; these are the
  literal bytes Phase 1 must reproduce on stdout for `--help*`.
- `assets/help.txt:47-116` (OPTIONS) — 17 table rows; 3 have a short+long
  pair (`-f, --file`, `-k, --keep-going`, `-q, --quiet`), the rest are
  long-only. `--help*`, `--bridge`, `--version` are **not** in this table
  (they live only in SYNOPSIS, `:10-45`) — the drift test (g) extractor must
  read only the OPTIONS section and the option parser must expose a
  "table-tracked" flag set distinct from the SYNOPSIS-only standalone flags,
  or the bidirectional equality check in the ticket ("every flag in the
  table is accepted... every parser flag appears in the table") will spuriously
  fail on `--help*`/`--bridge`/`--version`.
- `assets/help.txt:10-45` (SYNOPSIS) — confirms options and lines may be
  interleaved in any order and `<dest>` is a leading positional; this rules
  out the stdlib `flag` package (which stops scanning at the first
  non-flag argument), so Phase 1 needs a small manual argv scanner.
- `assets/help.txt:44-45` — "Options start with `-`; command lines start
  with a lowercase letter, `#` or whitespace" — the scanner's rule for
  classifying each remaining argv token as option vs. line.
- `assets/help.txt:24-25` — "[line ...] one command per argument. A newline
  inside one argument splits it into several lines" — line-collection must
  split each positional arg on `\n`.
- `assets/help.txt:543-575` (OUTPUT, Abort paragraph) — plain abort has
  **empty stdout** (no `out` line, no `done` line), one stderr line
  `abort: <message> (<E_CODE>)`. JSONL abort (`:611-614`) is `start` then
  `{"event":"abort","code":...,"msg":...}`, no `done`. This asymmetry (plain
  suppresses even the normally-first `out` line; JSONL still emits `start`)
  is a real spec detail, not an oversight — the formatter must implement it
  as written.
- `assets/help.txt:595-597` — the JSONL `start` object needs
  `out`/`dest`/`target.os`; computing an "out" directory string (from
  `--out`, else `$GOTTO_HANDO_OUT`, else `$TMPDIR/gotto-hando/<dest>/<run-id>/`
  per `:588-590`) is therefore needed even for abort paths in `--jsonl`
  mode. Directory computation is pure string logic here — no filesystem
  writes needed in Phase 1 (nothing runs).
- Ticket Decisions paragraph (`## Decisions`, "Options whose feature lands
  in a later ticket...") is the exhaustive list of options that must be
  accepted-but-rejected in Phase 1: `--bridge`, `--remote-bin`,
  `--inline-captures`, `--request-perms`. Any destination other than
  `local` is rejected the same way but via dest resolution, not option
  parsing.
- No existing `internal/` packages, no test scaffolding, no other Go files
  anywhere in the repo — every file listed below is new.
- Repo has no CI config, no Makefile; `go test ./...` is the only verification
  entry point implied by the ticket's own Verification line.

## Implementation Plan

1. `cmd/gotto-hando/main.go` — entry point only:
   `func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }`.
   Keeps parsing/dispatch in `package main` per CONCEPT.md ch. 12 (no
   separate `internal/cli`), and makes `run` directly unit-testable in the
   same package without spawning a subprocess.

2. `cmd/gotto-hando/options.go` — option model:
   - `optionSpec` table built from `assets/help.txt:47-116` OPTIONS, one
     entry per row (short+long aliases where the row has both, whether the
     option takes a value), used both by the parser and by the drift test.
   - Separately recognized SYNOPSIS-only standalone flags: `--help`,
     `--help-macos`, `--help-windows`, `--help-remote`, `--version`,
     `--bridge` — parsed but excluded from the OPTIONS-table comparison set.
   - Manual argv scanner honoring `:10-45`/`:44-45` (interleaved options and
     lines, dest = first non-option positional token not itself consumed as
     an option value). Unknown flag / missing required value / `-f`
     supplied without dest-or-lines context → usage error, exit 2.

3. `cmd/gotto-hando/lines.go` — line-collection helper: splits each
   positional argv line on `\n` (`:24-25`); reads `-f FILE` or stdin (`-`)
   as raw newline-split lines (no comment/blank-line semantics yet — that is
   Phase 2 SYNTAX territory, out of scope here). Used by the `-f`/argv
   exclusivity check and by the `--check`/`--ir` paths.

4. `cmd/gotto-hando/version.go` — `func version() string` parses the version
   token from `assets.Help`'s first line (`^gotto-hando (\S+)`) once,
   cached. Keeps help.txt as the single source of truth for the version
   string rather than a duplicated Go constant.

5. `internal/output/codes.go` — `ErrorCode` string type with constants for
   every code in `assets/help.txt:680-706` (E_SYNTAX, E_VALIDATE, E_CONNECT,
   E_PERMISSION, E_SESSION, E_BOUNDS, E_NOWINDOW, E_INPUT, E_CAPTURE,
   E_CLIPBOARD, E_EXEC, E_TIMEOUT, E_UNKNOWN); exit-code constants 0-5 per
   `:708-723`; `func AbortExit(code ErrorCode) int` implementing
   `E_VALIDATE -> 2`, `E_CONNECT -> 3`, else `-> 4` (ticket Phase 1 text).

6. `internal/output/writer.go` — formatter functions operating on synthetic
   structs (no real execution):
   - `WriteResult`/`WriteDone` (plain): `<n> ok|err|skip|warn <cmd> ...` and
     `done ok=N err=N skip=N elapsed=<ms>ms held_released=N[ state=unknown]`
     per `:562-569`.
   - JSONL equivalents per `:595-610` (`start`, per-line objects with
     `line`/`status`/`cmd`/`t_ms`, `done` object).
   - `WriteAbort(w io.Writer, jsonl bool, dest string, outDir string, code
     ErrorCode, msg string)`: plain → exactly one stderr line
     `abort: <msg> (<code>)`, nothing on stdout; JSONL → `start` object then
     `{"event":"abort","code":...,"msg":...}` on stdout, no `done` — per
     `:543-575` and `:611-614`.
   - `func DefaultOutDir(explicit, dest, runID string) string` — pure string
     construction per `:588-590` (`--out`, else `$GOTTO_HANDO_OUT`, else
     `$TMPDIR/gotto-hando/<dest>/<run-id>/`), used only to populate the
     JSONL `start`/`abort` object's `out` field in Phase 1 (no directory is
     created).

7. `cmd/gotto-hando/dispatch.go` — `func run(args []string, stdin io.Reader,
   stdout, stderr io.Writer) int` implementing the priority-ordered decision
   tree (highest first), each terminal branch returning the exit code:
   1. Any `--help*` flag present → write the matching `assets.Help*`
      constant to stdout verbatim, ignore everything else, return 0.
   2. `--version` present (and no help flag) → write `version() + "\n"` to
      stdout, return 0.
   3. `--expect-version VER` present and `VER != version()` → stderr
      `version mismatch: remote <version()>, expected <VER>`, return 3,
      nothing else printed.
   4. `--bridge` present → not implemented in Phase 1 → `WriteAbort(...,
      E_VALIDATE, "session bridge not implemented")`, return 2.
   5. Any of `--remote-bin`, `--inline-captures`, `--request-perms` present
      → `WriteAbort(..., E_VALIDATE, "<flag> not implemented")`, return 2.
   6. `-f`/stdin-collected lines together with positional `[line ...]` both
      present → `WriteAbort(..., E_VALIDATE, "-f and [line ...] cannot be
      mixed")`, return 2 (`:26-28`).
   7. `--check` or `--ir` present → collect lines (step 3's helper), then
      `WriteAbort(..., E_VALIDATE, "parser not implemented")`, return 2
      (Phase 1 goal text; bypasses dest resolution per `:471`/`:626-627`
      "no ssh").
   8. Dest resolution: `dest == "local"` → `WriteAbort(..., E_VALIDATE,
      "platform backend not implemented, nothing ran")`, return 2 (covers
      the bare-TTY-to-`qinfo` shortcut and `--ping`, since neither has a
      backend yet). `dest != "local"` (including no dest / bad usage where a
      dest was syntactically expected) → `WriteAbort(..., E_VALIDATE,
      "remote destinations not implemented")`, return 2.
   Every branch reads `--jsonl`/`-q` off the parsed options to choose the
   writer mode; `-q`, `--delay`, `--timeout`, `-k`, `--cap-on-error`,
   `--out` are accepted and stored but otherwise inert in Phase 1 (every
   path that would use them exits 2 first).

8. `cmd/gotto-hando/options_drift_test.go` — drift test (g): a small
   extractor reads `assets.Help`, isolates the text between `== OPTIONS ==`
   and the next `== ... ==` header, regex-matches each 2-space-indented row
   (`^  (-\w, )?--[\w-]+`) to collect every `-x`/`--long` token; flattens
   and sorts that set; flattens and sorts the option parser's table-tracked
   flag tokens from step 2; asserts equality. `--help*`, `--bridge`,
   `--version` are excluded from both sides (SYNOPSIS-only, not OPTIONS).

9. `cmd/gotto-hando/main_test.go` — subprocess-level tests (ticket
   explicitly asks for a test that "runs the binary"): `TestMain` builds the
   binary once via `go build`; per-test `exec.Command` runs it with
   `--help`, `--help-macos`, `--help-windows`, `--help-remote` and diffs
   stdout byte-for-byte (trailing newline included) against
   `assets.Help`/`HelpMacos`/`HelpWindows`/`HelpRemote`; a mixed `-f`+argv
   invocation asserts exit 2; one case per later-ticket option, `local`
   plain dest, `--check`, `--ir`, and an arbitrary remote dest assert exit 2
   with the exact "not implemented"/"not implemented, nothing ran" message
   on stderr; `--version` and `--expect-version` (match and mismatch) assert
   stdout/stderr/exit exactly per `:77-82`.

10. `internal/output/writer_test.go` + `internal/output/testdata/*.golden`
    — golden tests for `WriteResult`/`WriteDone`/`WriteAbort` (plain and
    JSONL) against synthetic `Result`/`Done` values, using the QUICK START
    (`:117-133`) and OUTPUT (`:548-560`) transcripts as fixtures where
    convenient, plus dedicated cases for the abort asymmetry noted in
    Codebase Findings.

## Verification Plan

- `go test ./...` — must be green; covers unit tests for `internal/output`
  and `cmd/gotto-hando` (dispatch/options/drift), plus the subprocess
  `--help*` byte-identity tests.
- Drift test (g) is exercised by `go test ./cmd/gotto-hando/...`; a manual
  sanity check (not required, for reviewer confidence only) is temporarily
  removing one OPTIONS row from a scratch copy of `assets/help.txt` and
  confirming the test fails.
- Manual smoke check (optional, redundant with the subprocess test):
  `go run ./cmd/gotto-hando --help | diff - assets/help.txt` and
  `go run ./cmd/gotto-hando local` (expect stderr `abort: platform backend
  not implemented, nothing ran (E_VALIDATE)` and exit 2).

## Escalations

- None.
