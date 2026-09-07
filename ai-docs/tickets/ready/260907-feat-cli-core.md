---
title: "CLI core: module layout, embedded help, output, parser -> IR, drift tests"
parent: 260907-epic-gotto-hando-v1
sage-review-design: completed
sage-review-completeness: completed
sage-review-design-reviewed: ef5ce9b02e6b847b
sage-review-completeness-reviewed: ef5ce9b02e6b847b
---

# CLI core: module layout, embedded help, output, parser -> IR, drift tests

## Background

Nothing but `go.mod`, `assets/` (four help texts + `//go:embed` stubs),
CONCEPT.md and LICENSE exists. This ticket lands everything that is
platform-independent so that the two backend tickets can start from a
compiling binary with a stable interface: the executable, argument handling,
`--help*` output, the sequence parser, the IR, static validation, the result
formatter, exit codes, and the tests that pin the help text to the code.

The normative contract for all of this is `assets/help.txt`. Sections this
ticket implements: SYNOPSIS, OPTIONS, SYNTAX, GRAMMAR, MODIFIERS, MODIFIER
KEYS, COORDINATES, DURATIONS, TEXT ESCAPES, WINDOW SELECTORS, COMMANDS (parse
level and validation only), KEY NAMES, EXECUTION (stages 1-2: parse and
validate; `--check`/`--ir` never connect), OUTPUT, JSONL, ERROR CODES, EXIT
CODES, LIMITS, SHELL QUOTING, DESTINATIONS (only `local`; ssh destinations
land in `260908-feat-remote-ssh`), IR JSON, SEE ALSO. Platform runtime
semantics belong to the backend tickets.

## Decisions

- Language Go (>= 1.23), module `github.com/kang-sw/gotto-hando`, no cgo.
  Rejected: Python/PyInstaller (startup latency, runtime on the remote host,
  AV false positives); Rust (equivalent deployment, higher cost, awkward
  macOS cross-compile).
- Repo layout follows CONCEPT.md ch. 12 (`cmd/gotto-hando`, `internal/syntax`,
  `internal/ir`, `internal/engine`, `internal/backend`, `internal/output`,
  `assets`). Names inside `internal/` are the implementer's call; the
  `Backend` interface and the IR types are the contract other tickets read.
- Bracket form is the only statement shape (`cmd[mods]payload` or bare
  `cmd`); space-separated `cmd payload` is a syntax error. Rationale in
  CONCEPT.md ch. 4; rules in help.txt SYNTAX/GRAMMAR.
- Modifier flag `c` is always Control, `m` is Meta (Cmd/Win), `p` is the
  platform primary; there is no Win-key flag because `w` is the window frame.
- `rect=` is the only list-valued modifier and uses `:` as separator.
- The parser is pure: text in, IR out, no OS calls. Validation that needs the
  target (bounds, permissions, session state) is preflight and lives in the
  engine/backend, not here.
- The `Backend` interface is defined in this ticket together with a
  `dryrun` implementation that records calls and returns canned results. The
  dry-run backend is test-only wiring (Go test code / build tag); it is never
  selectable from the CLI and never bound to `local`. In this ticket's
  interim binary `local` therefore exits 2 ("platform backend not
  implemented, nothing ran") for anything that needs a backend, including the
  bare `gotto-hando local` TTY shortcut to `qinfo`, until
  `260907-feat-darwin-backend` lands.
- Options whose feature lands in a later ticket (`--bridge`, `--remote-bin`,
  `--inline-captures`, `--request-perms`, non-`local` destinations) are
  parsed and rejected with exit 2 and a "not implemented" message, never
  exit 1, because exit 1 means "some input was executed" (help.txt EXIT
  CODES). `--request-perms` on Windows stays exit 2 as help.txt fixes it.
- There is no configuration file and no "profile" concept; the positional is
  `<dest>` (help.txt SYNOPSIS/DESTINATIONS). Delay precedence is exactly
  STATE MACHINE: `--delay` else 100 ms, then `set[delay=]` overrides.

## Constraints

- `gotto-hando --help`, `--help-macos`, `--help-windows`, `--help-remote`
  print the embedded asset byte-for-byte (trailing newline included) and
  exit 0, ignoring every other argument.
- Every enum the help text tabulates (error codes, exit codes, key names,
  command names, modifier names, limits) is a Go constant/table that the
  drift tests compare against the help text; the help text is never
  generated from code, code is checked against the help text.
- Output goes to stdout as the `<n> ok|err|skip|warn <cmd> ...` lines and
  `done ...` line described in OUTPUT; diagnostics go to stderr only;
  `--jsonl` switches stdout to the JSONL objects described in JSONL.
- Exit codes 0-5 exactly as EXIT CODES; 2/3/4 guarantee nothing was executed.
- All help text stays <= 80 columns and section headers match
  `^== [A-Za-z0-9 ,/:()-]+ ==$` (enforced by a test).

## Spec Impact

Target spec area: `ai-docs/spec/help.md` (new, flat). The spec is a pointer
document: one `{#stem}` anchor per `== SECTION ==` of `assets/help.txt`, each
entry consisting of the section name and the line "See `assets/help.txt`
section `== NAME ==`". No behavior prose is duplicated. At closeout this
ticket creates the file with an anchor for every section of
`assets/help.txt` (runtime sections included, since the anchor only points
at the section). The same closeout also creates `help-macos.md`,
`help-windows.md` and `help-remote.md` in the same pointer-only style, one
anchor per `== SECTION ==` of the matching `assets/help-*.txt`, so the
reverse-direction drift test is green from the day it lands. The backend
and remote tickets do not create spec files; they add an anchor only when
they add a new `== SECTION ==` to a help text, and never touch other
anchors.

## Phases

### Phase 1: Executable, embedded help, output formatter, exit codes

Goals: `cmd/gotto-hando` main with argument handling for every option in
help.txt OPTIONS and SYNOPSIS (`<dest>` positional, `-f`/stdin line
collection, `--jsonl`, `-q`, `--check`, `--ir`, `--delay`, `--timeout`,
`-k`, `--cap-on-error`, `--out`, `--ping`, `--version`, `--expect-version
VER`, `--help*`; the later-ticket options listed in Decisions are parsed and
exit 2 "not implemented"). `--version` prints the version string that
help.txt line 1 carries; `--expect-version VER` compares that string with
VER and on mismatch prints `version mismatch: remote <x>, expected <y>` on
stderr and exits 3 with nothing else printed (help.txt OPTIONS). Result-line
and JSONL formatters (`<n> ok|err|skip|warn`, `done`, JSONL start/line/done
objects, and the `abort` report - plain: empty stdout, one stderr line
`abort: <message> (<E_CODE>)`; JSONL: `{"event":"abort","code":..,"msg":..}`
after `start`, no `done` - with its exit mapping E_VALIDATE -> 2 (an option
the target OS lacks, e.g. `--request-perms` on a Windows target),
E_CONNECT -> 3, every other code -> 4, per help.txt OUTPUT/JSONL) implemented against synthetic results
and unit-tested here; they are exercised end to end from Phase 2. `--check`,
`--ir` and `-f`/stdin collect lines in Phase 1 and exit 2 "not implemented"
until Phase 2 wires the parser. Exit code table. `local` destination
recognized (exit 2 per Decisions until a backend exists); any other
destination exits 2 "remote destinations not implemented" until
`260908-feat-remote-ssh` lands.
Verification: `go test ./...`; a test that runs the binary with each
`--help*` flag and compares stdout to the embedded asset; a test that a
mixed `-f` + argv invocation exits 2; a test that every flag named in the
OPTIONS table is accepted by the option parser and every parser flag appears
in the table (this is drift test (g), landed early); formatter golden tests.

### Phase 2: Parser, IR, static validation, Backend interface, dry-run

Depends on Phase 1. Goals: tokenizer and parser for the GRAMMAR section
(line shapes, comments, modifier tokens, kv vs flag, duplicate/unknown
modifier errors, list values with `:`, per-command modifier allow-lists,
payload trim rules, TEXT ESCAPES, coordinates with `%`/negative/decimal,
frame exclusivity, `%` with `r` rejected, durations, key names and chord
limits, selectors, per-command payload grammar from COMMANDS). IR types
matching the help.txt IR JSON section (including the 2-D `keys` rule and
`exec`/`open` ops), `--ir` JSON output, `--check`. Static validation for
every LIMITS row that does not need the target. Parse/validation failure
contract, to be added to help.txt EXECUTION/OUTPUT in this phase as a
normative edit (same commit as the code): stdout is empty in both plain and
`--jsonl` mode when exit 2 is taken; stderr carries one diagnostic per error
as `E_SYNTAX line <n> col <c>: <message>` followed by the source line
indented two spaces, all errors reported before exiting; `--check` success
prints `ok <n> lines` on stdout and exits 0; `--ir` prints the IR JSON
document and exits 0. `Backend` interface with one
method group per COMMANDS group plus query/info/capture/exec, and a `dryrun`
backend. The engine executes IR against the backend with the EXECUTION
pipeline, STATE MACHINE (current window, held keys/buttons, delay
precedence: d= on the line > `set[delay=]` > `--delay` > 100 ms), and ERROR POLICY
(fail-fast, `-k`, skip lines, held-release with warn) using the dry-run
backend.
Verification: table tests for every example in help.txt (the single-quoted
arguments and heredoc bodies of the shell invocations in `== EXAMPLES ==`
and SHELL QUOTING, extracted rather than fed as whole lines, plus every
inline example in COMMANDS, must parse); negative
tests for each syntax error class mapped to exit 2 and the E_SYNTAX message
format; `--ir` golden test against the IR JSON example; engine tests with the
dry-run backend covering fail-fast, `-k`, held-key release and `done` counts.

### Phase 3: Help drift tests

Depends on Phase 2. Goals: tests that fail when code and help text diverge:
(a) every `== SECTION ==` referenced by a `{#stem}` entry in
`ai-docs/spec/*.md` exists in the named help file, and, in the reverse
direction, every `== SECTION ==` in each help text has a `{#stem}` anchor in
its matching spec file (`help.md` / `help-<os>.md` / `help-remote.md`);
(b) the parser's command
table equals the command names listed in `== COMMANDS ==`, and each
command's accepted modifier set equals the bracketed command-specific list
shown there united with the common modifiers whose "Accepted by" lists in
MODIFIERS / MODIFIER KEYS name that command; (c) key names in code equal
`== KEY NAMES ==` after expanding its ranges and `|` alias groups; (d) error
code and exit code constants equal the ERROR CODES / EXIT CODES tables;
(e) numeric limits equal the LIMITS table; (f) format lint (80 columns,
header regex, trailing newline); (g) the OPTIONS table equals the option
parser's flag set (landed in Phase 1, kept here). The tests parse the help
text with small purpose-built extractors. Where the current prose is not
extractable (today: the "Accepted by" lists, KEY NAMES ranges, LIMITS
sentences), this phase regularizes `assets/help.txt` into extractable form
as a normative edit in the same commit as the test, without changing
meaning; the test is never weakened.
Verification: `go test ./...` green; deliberately breaking one help line in a
scratch copy makes the corresponding test fail.
