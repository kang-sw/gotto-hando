# Plan: 260907-feat-cli-core — Phase 2: Parser, IR, static validation, Backend interface, dry-run

## Relevant Ticket Contract

Single source of truth for every shape below is `assets/help.txt` (embedded via
`assets/assets.go` as `assets.Help`). Line ranges are cited per finding.

- **Deliverables (ticket Phase 2), carried whole:** (1) tokenizer + parser for
  the GRAMMAR — all line shapes, comments, modifier tokens (kv vs flag),
  duplicate/unknown modifier errors, list values with `:`, per-command modifier
  allow-lists, payload trim rules, TEXT ESCAPES, coordinates (`%`/negative/
  decimal), frame exclusivity, `%`-with-`r` rejection, durations, key names +
  chord limits, selectors, per-command payload grammar from COMMANDS; (2) IR
  types matching `== IR JSON ==` (2-D `keys` rule, `exec`/`open` ops); (3)
  `--ir` JSON output and `--check`, wired into the `dispatch.go` paths Phase 1
  exits 2 on; (4) static validation for every LIMITS row that does not need the
  target; (5) the parse/validation **failure contract as a NORMATIVE
  `assets/help.txt` edit in the same commit**; (6) the `Backend` interface (one
  method group per COMMANDS group plus query/info/capture/exec) + a `dryrun`
  backend; (7) the engine executing IR against the backend with the EXECUTION
  pipeline, STATE MACHINE (current window, held keys/buttons, delay precedence),
  and ERROR POLICY (fail-fast, `-k`, skip, held-release-with-warn).
- **Failure contract to add to help.txt (ticket Phase 2 text, normative edit):**
  on exit 2, stdout is empty in BOTH plain and `--jsonl`; stderr carries one
  diagnostic per error as `E_SYNTAX line <n> col <c>: <message>` followed by the
  source line indented two spaces; ALL errors are reported before exit; `--check`
  success prints `ok <n> lines` on stdout and exits 0; `--ir` prints the IR JSON
  document and exits 0.
- **Binding Decisions (ticket `## Decisions`):** repo layout follows CONCEPT.md
  ch.12 (`internal/syntax`, `internal/ir`, `internal/engine`, `internal/backend`);
  names inside `internal/` are the implementer's call, but **the `Backend`
  interface and the IR types are the contract `260907-feat-darwin-backend`,
  `260907-feat-windows-backend` and `260908-feat-remote-ssh` read.** `c`=Control
  always, `m`=Meta, `p`=Primary; no Win-key flag (`w` is the window frame).
  `rect=` is the only list-valued modifier, `:`-separated. **The parser is pure:
  text in, IR out, no OS calls.** Validation that needs the target (bounds,
  permissions, session state) is preflight, in engine/backend, NOT here.
  `Backend` is defined here together with a `dryrun` impl that records calls and
  returns canned results; `dryrun` is test-only wiring (never selectable from
  the CLI, never bound to `local`). Delay precedence is exactly STATE MACHINE.
- **Verification boundary (ticket Phase 2):** table tests for every EXAMPLES /
  SHELL QUOTING invocation and every inline COMMANDS example (extracted args /
  heredoc bodies, not whole shell lines) must parse; negative tests per syntax-
  error class → exit 2 + the E_SYNTAX message format; `--ir` golden test against
  the IR JSON example; engine tests with the dry-run backend covering fail-fast,
  `-k`, held-key release, and `done` counts.

## Out of Scope

- **Real platform backends** (darwin/windows input, focus, capture, clipboard,
  exec, open) — `260907-feat-darwin-backend`, `260907-feat-windows-backend`.
  Consequence: **the CLI `local` execution path stays exit 2** ("platform
  backend not implemented, nothing ran") in this phase's binary. The engine is
  built and exercised only through the `dryrun` backend in tests; it is NOT
  reachable from the CLI (no real backend exists and `dryrun` is not
  CLI-selectable). Phase 2's only CLI-observable change is `--check` and `--ir`.
- **ssh / bridge / transport** and `[f]`→remote line rewriting — `260908-feat-remote-ssh`.
- **Drift tests (a)-(f)** (spec-anchor coverage, command/modifier tables, key
  names, error/exit tables, LIMITS, format lint) — Phase 3. This phase must
  still keep the help.txt edit ≤ 80 columns and header regex intact (ticket
  `## Constraints`), because Phase 3(f) will enforce it later.
- **The pointer spec files** (`help.md` + three `help-<os>.md`) — ticket closeout.
- **Target-dependent / runtime validation:** absolute & `disp=` coordinate
  BOUNDS (preflight, exit 4; `--check` checks number format only, help.txt
  :262-268); platform key-name SUPPORT (preflight E_INPUT; parse-time only
  checks the name is in the KEY NAMES table); `exec` stdout/stderr 64 KiB cap
  and `argv` OS limit (runtime/target-dependent LIMITS rows, :735,:743-744).
- Semantic parsing of `--delay`/`--timeout` values beyond DURATIONS syntax is
  needed here (Phase 1 stored them opaque); their runtime enforcement is engine.

## Codebase Findings

### Reuse (Phase 1, already on this branch)
- `internal/output/codes.go#L14-L52` — `ErrorCode` constants (incl. `ESyntax`,
  `EValidate`) and exit constants (`ExitValidation`=2, `ExitOK`=0) + `AbortExit`.
  Reuse directly; the new diagnostics use `ESyntax`/`EValidate` and `ExitValidation`.
- `internal/output/writer.go#L18-L59` — `Result{Line,Status,Cmd,Src,ErrMsg,ErrCode,
  Detail,Extra,AlwaysShow,JSON []KV,TMS}` and `KV`; `WriteResult`/`WriteDone`
  (`#L74-141`), `WriteAbort` (`#L169-180`), `DefaultOutDir` (`#L187-203`). The
  engine builds `Result`/`Done` values and streams them through these writers.
  Note the JSONL per-line contract is ordered `KV` in help.txt example order.
- `cmd/gotto-hando/dispatch.go#L97-110` — the two branches to rewire: L97-103
  (`--check`/`--ir`, currently `abort(EValidate,"parser not implemented")`) and
  L105-110 (`local`/other dest). Phase 2 rewires ONLY the `--check`/`--ir` branch;
  the `local` branch stays exit 2 (no backend). `collectLines` is already called
  there and returns `[]string`.
- `cmd/gotto-hando/lines.go#L17-53` — `collectLines(file,hasFile,positional,stdin)
  ([]string,error)` already resolves `-f`/stdin/argv and splits argv on `\n`. It
  does NO comment/blank/syntax handling by design — the parser owns SYNTAX
  (:134-176). Line numbering (1-based, counts comments+blanks, :564) must be
  assigned over this `[]string` (index+1).
- `cmd/gotto-hando/version.go#L23-33` — IR `"v"` schema version is separate from
  the tool version; use a Go constant `1` (help.txt IR JSON `"v": 1`, :631).

### Normative shapes to implement (help.txt)
- SYNTAX `:134-176`, GRAMMAR `:177-204` — two line shapes only; char right after
  `<command>` decides shape (`[`=bracket, EOL=no-payload, else E_SYNTAX); no
  space form; modifier token with `=` is kv, else a group of single flags;
  values cannot contain `,` `]`; list value is `:`-separated, `rect=` only;
  unknown/repeated flag or key = E_SYNTAX. Payload = everything after the FIRST
  `]` to EOL; `[` `]` allowed freely in payload (`txt[]a[b]c` → `a[b]c`, :173).
- Payload trim (`:167-173`): txt/paste/clip keep whitespace verbatim + apply TEXT
  ESCAPES; every other command trims; `[f]` payload is a file path, always trimmed.
- TEXT ESCAPES `:275-284` — `\n \t \\ \uXXXX` only, in txt/paste/clip; `\n`/`\t`
  differ txt (key press) vs paste/clip (literal char) — but that split is an
  ENGINE/backend concern; the parser records the escape resolution the IR needs
  (IR `"text"` carries the resolved string, :639). Any other backslash = E_SYNTAX.
  With `[f]` no escapes; file contents used as-is (≤ 64 KiB).
- MODIFIERS `:205-224`, MODIFIER KEYS `:226-243` — common mods `c s a m p`
  (accepted by `k m c md mu drag scroll` only, :208/:235), `d=`, `ms=`, `f`, `r`,
  `w`, `disp=`. Flag order for held keys is fixed `c,s,a,m/p` (:228). Per-command
  allow-lists come from COMMANDS `[...]` united with the common-mod "Accepted by"
  lists — the parser needs an explicit per-command allowed-modifier set (this is
  also what Phase 3 drift test (b) will compare against; build the table now).
- COORDINATES `:245-268` — text always `x,y`; frame chosen by ≤ ONE of `r`/`w`/
  `disp=` (:260); `N%` cannot combine with `r` (:261); integers or decimals,
  negative allowed, `%` optional per-coordinate. `--check` validates NUMBER
  FORMAT ONLY (:266-268) — no bounds.
- DURATIONS `:270-273` — number + optional `ms`/`s` (no suffix = ms); decimals ok;
  negative/NaN/Inf = error. Used by sleep, `d=`, `ms=`, `wait=`, `timeout=`.
- WINDOW SELECTORS `:286-297` — `id:<n>`, `pid:<n>`, `app:<name>`, else `<text>`
  title substring; with `r` the `<text>` is an RE2 regex; `win[wait=DUR]` polls.
- COMMANDS `:299-448` — per-command payload grammars + defaults. Groups:
  KEYBOARD `k kd ku txt` (:305), MOUSE `m c md mu drag scroll` (:322), CLIPBOARD
  `clip paste qclip` (:344), WINDOWS/APPS `win qwin open exec` (:355), CAPTURE
  `cap` (:400), FLOW `sleep set #` (:419), QUERY `qinfo qdisp qmouse` (:430).
  Chords: `+`-joined, ≤ 8 keys incl. flags, ≤ 64 sequential per line (:308-310);
  `exec` argv split on spaces with `"..."`-only quoting, no escapes (:373-376);
  `exec[shell]` keeps whole payload as `cmd` (:377/:653-655); `scroll` ticks are
  payload-only 1..50 default 3 (:339-341); `drag` polyline 1..200 points (:334).
- KEY NAMES `:448-458` — the valid physical-key name set (with ranges `a-z`,
  `0-9`, `f1-f24`, `numpad0-9` and `|` alias groups `alt|option`, `meta|cmd|win`,
  `enter|return`, `esc|escape`). Parser validates a `k`/`kd`/`ku` name against
  this expanded set; platform support is preflight (out of scope).
- EXECUTION `:460-501` — pipeline `parse(2)→validate(2)→connect(3)→preflight(4)→
  run(1)`. Step 1 = parse (E_SYNTAX, reports line/col/source). Step 2 = static
  validation (E_VALIDATE): ranges + LIMITS; held balance (every `ku`/`mu` matches
  an earlier `kd`/`md`; `c`/`md`/`drag` on a button already held by `md` rejected);
  one frame modifier; no `%` with `r`; chords ≤ 8; `[f]` files exist/UTF-8/≤64KiB.
  `--check` and `--ir` stop after step 2, never connect. **This defines exactly
  what Phase 2 static validation is.**
- STATE MACHINE `:502-517` — current window (none at start; `w` frames use OS-
  focused window; set by `win`); held keys/buttons (empty at start; kd/ku/md/mu;
  modifier flags held only within their own command); delay/txtms/keyms start
  `--delay|100ms / 0 / 30ms`, changed by `set`; capture counter/out dir local.
  **Delay precedence:** `d=` on the line > `set[delay=]` (the running global) >
  `--delay` > 100ms; and query/sleep/set/cap/exec/open take NO default delay
  (:212-214,:426,:497) — only an explicit `d=` applies to them.
- ERROR POLICY `:518-541` — fail-fast: failed line stops run, later lines `skip`,
  held keys released, exit 1. `-k`: continue; keys taken by the failed line
  released immediately; final exit still 1 if any line failed. `qwin` zero match
  is NOT failure; `win` no-match IS (:524). `exec` non-zero = E_EXEC fail-fast
  unless `noerr`; E_TIMEOUT never softened. Held keys released in reverse order
  when run ends/fails/interrupts, reported as `warn` (auto-released, :566).
- IR JSON `:625-679` — the exact op object shapes. Op discriminator `"op"`:
  `focus`(win), `key`(k), `text`(txt), `move`(m), `click`(c), `capture`(cap),
  `exec`, `open`. Every op keeps `"line"` and `"src"` (:661). `"keys"` is a list
  of chords (2-D): `k[]ctrl+shift+a b` → `[["ctrl","shift","a"],["b"]]` (:662-663,
  :636). `mods` + key names are symbols `ctrl shift alt meta primary` (:664-666).
  `[f]` contents inlined into `"text"` (:667-668). Capture/`qclip[f]` carry NO
  path (:669-671). `exec` non-shell carries `"argv"`; shell carries `"cmd"`
  (:650-655). Top level: `{"v":1,"ops":[...],"defaults":{"delay_ms","text_interval_ms","key_gap_ms"}}`.
- ERROR CODES `:680-707`, EXIT CODES `:708-723`, LIMITS `:724-745` — the numeric/
  code tables the validator enforces (see decision points below on which LIMITS
  rows are in scope).

### Design guidance (CONCEPT.md, not normative but authoritative for layout)
- CONCEPT.md `:386-410` (ch.8.2) — the concrete `Backend` interface draft:
  `Info`, `Preflight(ctx, seq *ir.Sequence)`, `KeyDown/KeyUp`, `TypeText`,
  `MouseMove`, `ButtonDown/ButtonUp`, `Scroll`, `Windows/Focus`, `Capture`,
  `ClipboardGet/ClipboardSet`, `MousePos`, `Open`, `Exec`. Explicitly:
  "click, drag, key codes, repeat, interpolation, held tracking, delay are
  implemented by the ENGINE composing backend primitives; backend stays thin."
  **This settles the engine/backend boundary** — no strategic uncertainty.
- CONCEPT.md `:523-559` (ch.12) — file layout: `internal/syntax/`
  (lexer/parser/escape/coord/duration/selector/inline), `internal/ir/`
  (ops/json/validate/limits/version), `internal/engine/` (run/held/drag/timing +
  fake_backend_test), `internal/backend/backend.go` (interface + `Info`/`Window`/
  `Image`/`ExecResult` types), plus `internal/backend/fake/` for the test backend.
- CONCEPT.md `:378-384` (ch.8.1) — `[f]` files are read at the parser stage on the
  agent machine and inlined into the IR `text`. This RESOLVES the tension with
  the "parser is pure, no OS calls" Decision: `[f]` reading is a **distinct
  post-parse `inline` pass** (`internal/syntax/inline.go`), not the pure tokenizer/
  parser. Both `--check` and `--ir` run it (they perform step-2 `[f]` validation).

### Contract-detail decisions the executor must settle (grounded, low-stakes)
1. **Diagnostic code prefix for validation errors.** The ticket template is
   `E_SYNTAX line <n> col <c>: <message>`, but step-2 failures are E_VALIDATE
   (:686). Recommend: use the ACTUAL code as the prefix (`E_SYNTAX ...` for parse
   errors which have a real column; `E_VALIDATE line <n> col <c>: ...` for
   validation errors, using the offending token's column, or col 1 for
   whole-line/cross-line errors such as held-balance). Keep the trailing indented
   source line for all. State this explicitly in the help.txt normative edit.
2. **`ok <n> lines` count.** Recommend `n` = number of input source lines
   (including blank/comment lines), consistent with the 1-based line numbering
   that "counts comments and blank lines" (:564). Confirm wording in the edit.
3. **`[f]` inlining placement** — resolved above (post-parse `inline` pass); note
   it in code comments so the purity Decision is visibly honored.

## Implementation Plan

Package order follows dependencies: `ir` types → `syntax` (parser+validate) →
engine+backend → CLI wiring + help.txt edit. Names inside `internal/` are the
implementer's call (Decisions); the paths below follow CONCEPT.md ch.12.

1. **`internal/ir/ops.go`** — Go types for the IR (contract for downstream
   tickets). Define `Sequence{V int; Ops []Op; Defaults Defaults}` and the op
   representation. Model each op with an `Op` carrying common `Line int`, `Src
   string`, `Kind string` (the `"op"` discriminator) plus per-op fields, matching
   `== IR JSON ==` (:630-679) field names exactly: `focus`(selector, wait_ms),
   `key`(mods, keys `[][]string`, repeat, gap_ms), `text`(text, interval_ms),
   `move`(point, duration_ms), `click`(button, count, gap_ms, mods, point),
   `md`/`mu`/`kd`/`ku`/`drag`/`scroll`/`clip`/`paste`/`qclip`/`win`/`qwin`/`open`/
   `exec`/`capture`/`sleep`/`set`/`qinfo`/`qdisp`/`qmouse`. `Point{Frame string;
   X,Y float64; XPct,YPct bool}` (:641-646). `Selector{Kind,Value string;Regex
   bool}` (:634). Keep field JSON tags in the spec's order/casing. `keys` is the
   2-D chord list (:662-663). Const `SchemaVersion = 1`.
2. **`internal/ir/json.go`** — `--ir` serialization producing the exact document
   in `:630-660` (ordered keys as the example shows; omit path fields on
   capture/qclip per :669-671; `exec` emits `argv` xor `cmd` per shell). A golden
   test pins this against the help.txt example.
3. **`internal/syntax/` tokenizer + parser** (pure, no OS calls, Decisions):
   - `lexer.go`/`parser.go` — line-shape dispatch (:147-154), command name
     (:156/:183), modifier tokenization (kv vs flag group, `:`-list for `rect=`,
     unknown/duplicate → E_SYNTAX, :158-166/:184-190), payload split at first `]`
     (:142-144/:167-173), per-command dispatch to payload sub-grammar.
   - `escape.go` — TEXT ESCAPES (:275-284) for txt/paste/clip payloads.
   - `coord.go` — `point`/`points`/`rect`/`coord` (:199-202/:245-268): decimals,
     negative, `%`; enforce ≤1 frame modifier and no-`%`-with-`r` at parse.
   - `duration.go` — DURATIONS (:270-273); reject negative/NaN/Inf.
   - `selector.go` — WINDOW SELECTORS (:286-297) incl. `r`→regex.
   - Key-name table (`a-z 0-9 f1-f24 ...`, :448-454) with range + `|`-alias
     expansion; validate `k`/`kd`/`ku` names and map to IR symbols (:664).
   - Per-command allowed-modifier table (COMMANDS `[...]` ∪ common-mod "Accepted
     by" lists) driving the allow-list check; also feeds Phase 3 drift (b).
   - Parser emits `[]Diagnostic{Line,Col,Code,Msg,SrcText}` accumulating ALL
     errors (report-all-before-exit, :464). Each parsed line → an `ir.Op`.
4. **`internal/syntax/inline.go`** — post-parse `[f]` pass: read LOCAL file, check
   exists/UTF-8/≤64 KiB (E_VALIDATE, :470/:726), inline contents into the op's
   `text` (:667). Keeps `parser.go` OS-free. Run by `--check` and `--ir`.
5. **`internal/ir/validate.go` + `limits.go`** — static validation (EXECUTION
   step 2, :468-471) over the whole `Sequence`, appending `Diagnostic`s:
   - **In-scope LIMITS rows** (no target): lines/run ≤ 1000; line length 64 KiB
     and `[f]` file 64 KiB; k ≤ 64 sequential & ≤ 8/chord incl. flags; scroll
     ticks 1..50; sleep ≤ 60s; `d=`/`ms=` ≤ 10s; `wait=` ≤ 60s; exec `timeout=`
     ≤ 60s; drag 1..200 points & steps 1..200; `label=` regex; cap `n=` ≤ 120 and
     ≤ 121 captures per run (whole-sequence count); `scale=` 0.1..4.0 or `native`
     (:724-744).
   - **Held-balance** across lines: `ku`/`mu` match an earlier `kd`/`md`;
     `c`/`md`/`drag` on a button already held by `md` = E_VALIDATE (:468-469).
   - Frame-modifier and `%`-with-`r` re-checks (belt-and-suspenders with parse).
   - **Explicitly NOT here:** coordinate bounds (number-format only at `--check`,
     :266-268), platform key support, exec output cap, argv OS limit — target/
     runtime, out of scope.
6. **`internal/backend/backend.go`** — the `Backend` interface (contract) + shared
   value types (`Info`, `Window`, `Image`, `ExecResult`, `Point`, `Button`,
   `Dir`, `ScrollUnit`, `CaptureReq`, `ExecReq`), following CONCEPT.md ch.8.2
   (`:386-410`): one method group per COMMANDS group plus query/info/capture/exec
   — `Info`; `Preflight(ctx, *ir.Sequence)`; keyboard `KeyDown/KeyUp/TypeText`;
   mouse `MouseMove/ButtonDown/ButtonUp/Scroll`; windows `Windows/Focus`; capture
   `Capture`; clipboard `ClipboardGet/ClipboardSet`; query `MousePos`; `Open`;
   `Exec`. Backend stays thin (no click/drag/repeat/interpolation/held/delay —
   those are engine).
7. **`internal/backend/dryrun`** (or `fake`) — records every call in order and
   returns canned results (`Info`, `Window` list, `Image`, `ExecResult`). Ticket:
   test-only, never CLI-selectable, never bound to `local`. Implement as a plain
   package that `cmd/` never imports (satisfies "not selectable"); a build tag is
   an acceptable alternative. `internal/engine` and its tests import it.
8. **`internal/engine/` (run.go, held.go, drag.go, timing.go)** — execute a
   `Sequence` against a `Backend`, per EXECUTION step 5 (:495-500), STATE MACHINE
   (:502-517), ERROR POLICY (:518-541):
   - Compose primitives: `click`=move?+button down/up ×count; `drag`=move+down+
     polyline steps+up (released even on mid-drag failure, :337); `key`=flag
     down (order c,s,a,m/p)+per-chord press/release+repeat+gap; interpolation for
     `m ms>0`. Held tracking (kd/ku/md/mu) in `held.go`.
   - Current window state; `w`/`cap[w]` frame uses it (STATE MACHINE).
   - Delay precedence in `timing.go`: `d=` > running global (`set[delay=]`) >
     `--delay` > 100ms; no default delay for query/sleep/set/cap/exec/open.
   - Error policy: fail-fast (later lines `skip`, exit 1), `-k` (continue, release
     failed line's keys immediately), held-release-at-end reported `warn`
     (auto-released), `done` counters. `exec` `noerr`/E_TIMEOUT semantics.
   - Emit `output.Result` per line and a final `output.Done` through the Phase 1
     writers. `fake_backend_test.go`/engine tests cover fail-fast, `-k`, held
     release, done counts (ticket Verification).
9. **`cmd/gotto-hando/dispatch.go` rewiring** — replace ONLY the `--check`/`--ir`
   branch (`#L97-103`). After `collectLines`, run parse → inline(`[f]`) →
   validate:
   - On any diagnostics: write NOTHING to stdout (both plain and `--jsonl`);
     write one stderr diagnostic per error (`<CODE> line <n> col <c>: <message>`
     + the two-space-indented source line); exit 2.
   - `--check` success: stdout `ok <n> lines`, exit 0.
   - `--ir` success: stdout the IR JSON document (`ir/json.go`), exit 0.
   Leave the `local`/other-dest branch (`#L105-110`) exiting 2 unchanged (no
   backend). The engine + dryrun are built and unit-tested but not CLI-wired.
10. **`assets/help.txt` NORMATIVE edit (same commit)** — add the parse/validation
    failure contract to EXECUTION (near step 1-2, ~:464-471) and/or OUTPUT
    (~:567-575, distinct from the runtime `<n> err ...` line which stays): empty
    stdout on exit 2 in both modes; one stderr diagnostic per error as `<CODE>
    line <n> col <c>: <message>` + indented source line; all errors before exit;
    `--check` → `ok <n> lines` exit 0; `--ir` → IR JSON exit 0. Keep ≤ 80 columns
    and the `^== [A-Za-z0-9 ,/:()-]+ ==$` header form (ticket Constraints); the
    Phase 1 `--help` byte-identity test reads the same embedded asset, so it stays
    green. Do not alter unrelated sections.

## Verification Plan

- `go test ./...` — green.
- **Positive parse tests:** extract the single-quoted args / heredoc bodies of
  every `== EXAMPLES ==` (:853-888) and `== SHELL QUOTING ==` (:785-804)
  invocation, plus every inline COMMANDS example (:299-448), and assert each
  parses without error (ticket Verification). Extract, do not feed whole shell
  lines.
- **Negative tests:** one per syntax-error class (space form; unknown/duplicate
  modifier; bad escape; `%` with `r`; two frame mods; out-of-range duration/limit;
  unbalanced ku/mu; over-long chord) → exit 2 and the exact `<CODE> line <n> col
  <c>: <message>` + indented-source-line format; assert stdout empty in plain and
  `--jsonl`.
- **`--ir` golden test** against the `== IR JSON ==` example document (:630-660).
- **`--check`** success prints `ok <n> lines` exit 0.
- **Engine tests (dryrun backend):** fail-fast (later lines skip, exit 1), `-k`
  (continue, immediate release), held-key auto-release with `warn`, `done`
  counters and `held_released` (ticket Verification).
- **Manual sanity:** `go run ./cmd/gotto-hando local --ir 'k[c]v'` prints IR JSON
  exit 0; `go run ./cmd/gotto-hando local --check 'm 1,2'` prints an `E_SYNTAX`
  diagnostic to stderr, empty stdout, exit 2.

## Escalations

- None. The three contracts the task flagged as escalation triggers are settled
  by the sources: the IR type layout is dictated field-for-field by help.txt
  `== IR JSON ==` (:625-679); the `Backend` interface has a concrete draft in
  CONCEPT.md ch.8.2 (:386-410) that the ticket's "one method group per COMMANDS
  group plus query/info/capture/exec" maps onto cleanly; and CONCEPT.md ch.8.2
  fixes the engine/backend boundary ("engine composes primitives, backend stays
  thin"). No materially different viable designs remain. The three contract-detail
  decisions listed under Codebase Findings (diagnostic code prefix, `ok <n>
  lines` count, `[f]` inlining placement) are low-stakes and resolved inline with
  a recommended answer and its grounding; none is a strategy or scope question.
  Confidence: high.
