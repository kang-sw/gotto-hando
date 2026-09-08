# Plan: 260907-feat-cli-core — Phase 3: Help drift tests

## Relevant Ticket Contract

Single source of truth is `assets/help.txt` (+ `help-macos.txt`, `help-windows.txt`,
`help-remote.txt`, embedded via `assets/assets.go` as `assets.Help`,
`assets.HelpMacos`, `assets.HelpWindows`, `assets.HelpRemote`). Phase 3 depends on
Phase 2 (landed, d01114f) and Phase 1 (landed, cc2b42d).

- **Deliverables, carried whole** — tests that fail when code and help text
  diverge, one per letter:
  (a) spec-anchor coverage, both directions: every `{#stem}` entry in
  `ai-docs/spec/*.md` whose block contains "See `assets/help.txt` section
  `== NAME ==`" (or the matching `help-<os>.md` / `help-remote.md`) must name a
  real `== SECTION ==`; and every `== SECTION ==` in each help text must have a
  matching `{#stem}` entry in its spec file.
  (b) parser command table == `== COMMANDS ==` command names; each command's
  accepted-modifier set == its COMMANDS `[...]` bracket UNION the common
  modifiers whose "Accepted by" lists (MODIFIERS / MODIFIER KEYS) name that
  command.
  (c) key names in code == `== KEY NAMES ==` after expanding ranges (`a-z`,
  `0-9`, `f1-f24`, `numpad0-9`) and `|` alias groups.
  (d) `ErrorCode`/exit constants == ERROR CODES / EXIT CODES tables.
  (e) numeric LIMITS constants == the LIMITS table.
  (f) format lint: <=80 columns, header regex
  `^== [A-Za-z0-9 ,/:()-]+ ==$`, trailing newline — over all four help texts.
  (g) OPTIONS table == parser flag set — already landed
  (`cmd/gotto-hando/options_drift_test.go`), kept as-is, no changes needed.
- **Regularization authorization (ticket text, verbatim scope):** "Where the
  current prose is not extractable (today: the 'Accepted by' lists, KEY NAMES
  ranges, LIMITS sentences), this phase regularizes `assets/help.txt` into
  extractable form as a normative edit in the same commit as the test, without
  changing meaning; the test is never weakened." This is the ONLY authorized
  regularization scope — three named areas, not a blanket license to reformat
  other sections (survey found COMMANDS command brackets and ERROR
  CODES/EXIT CODES do not need it; see Codebase Findings).
- **Verification boundary (ticket Phase 3):** `go test ./...` green; manually
  breaking one help line in a scratch copy of `assets/help.txt` must make the
  corresponding test fail (a manual technique during implementation, not an
  extra committed test — no prior phase committed such a fixture test either).
- **Spec Impact (ticket-level, explicitly deferred past Phase 3):** the four
  pointer spec files (`ai-docs/spec/help.md`, `help-macos.md`,
  `help-windows.md`, `help-remote.md`) are a **ticket-closeout** deliverable,
  created AFTER Phase 3, "so the reverse-direction drift test (a) is green
  from the day it lands." Phase 1's and Phase 2's `### Result` sections both
  independently confirm this sequencing ("the pointer spec files remain a
  ticket-closeout deliverable after Phase 3's drift tests").

## Out of Scope

- Creating `ai-docs/spec/*.md` (ticket-closeout deliverable, not Phase 3;
  `ai-docs/spec/` currently holds only `.gitkeep`).
- LIMITS rows with no Go constant today (target/runtime-dependent, per
  `internal/ir/limits.go`'s own doc comment): `exec stdout/stderr` cap,
  `--timeout` CLI default (300s) / exec `timeout=` default (10s, an inline
  literal in `internal/syntax/build.go:346`, not a named constant), and
  `argv` OS limit. Test (e) must exclude these three rows explicitly and
  document why (mirrors how test (g) already excludes the four `--help*`
  flags with a documented rationale).
- Any behavior change to the parser, IR, engine, or CLI — Phase 3 adds tests
  and, where authorized, `assets/help.txt` prose edits only.
- Modifying `cmd/gotto-hando/options_drift_test.go` (test g) beyond leaving it
  in place; it already passes and is unrelated to (a)-(f).

## Codebase Findings

- `assets/assets.go#L1-30` — `assets.Help`/`HelpMacos`/`HelpWindows`/`HelpRemote`
  already embedded; reuse directly, no new embed needed for (b)-(g).
- `assets/help.txt` section line numbers (current HEAD, confirmed via
  `grep -n '^== '`): MODIFIERS `:205`, MODIFIER KEYS `:226`, COORDINATES
  `:245`, COMMANDS `:299`, KEY NAMES `:448`, EXECUTION `:460`, ERROR CODES
  `:698`, EXIT CODES `:726`, LIMITS `:742`. These differ from the numbers
  cited in the Phase 2 plan (written pre-Phase-2-edit) — re-derive line
  numbers at implementation time via `grep -n '^== '` rather than trusting
  stale citations.
- `assets/help.txt#L47-53` (`options_drift_test.go` pattern) — the existing
  drift-test idiom to imitate: a section-scoped line extractor with a doc
  comment explaining a load-bearing regex quirk, a table-vs-code comparison
  via `sort.Strings` + `reflect.DeepEqual`, and one `t.Fatal` when the
  extractor itself finds zero rows (guards against a silently-broken
  extractor passing vacuously). Apply this same shape to (b)-(f).
- `assets/help.txt#L205-224` (MODIFIERS) and `#L226-243` (MODIFIER KEYS) —
  **the "Accepted by" wording differs between the two sections**: MODIFIERS
  L209 says "Accepted by k m c md mu drag scroll only." (no colon, trailing
  "only"); MODIFIER KEYS L235 says "Accepted by: k m c md mu drag scroll."
  (colon, no "only"). Same list, inconsistent punctuation — this is the
  concrete regularization target for the "Accepted by lists" category the
  ticket names. Recommended minimal fix (meaning-preserving): make both
  read identically, e.g. both `Accepted by: k m c md mu drag scroll.` This
  is the ONLY "Accepted by"-style line in either section (`d=`, `ms=`, `f`,
  `r`, `w`, `disp=N` describe applicability in free prose, not an
  "Accepted by" list — see next finding).
- `assets/help.txt#L215-219` (the `f` modifier) — applicability ("txt paste
  clip use the file's contents; qclip writes to it") is prose, not an
  "Accepted by" list, and is NOT part of drift test (b)'s union rule (which
  is scoped to "Accepted by" lists only). `f` doesn't need extraction from
  MODIFIERS at all: it already appears explicitly in each accepting
  command's own COMMANDS bracket (`txt [ms=0 per-character gap, f]`,
  `clip [f]`, `paste [f, ms=50 settle]`, `qclip [f]`). Same reasoning applies
  to `r`, `w`, `disp=N` (COORDINATES `:245-268`) — already listed per-command
  in COMMANDS brackets (e.g. `m [ms=0 duration, r|w|disp=]`,
  `cap [w|disp=N, ...]`), so no MODIFIERS-prose extraction needed for them.
- **Non-obvious constraint - `d=` is universal and appears in NEITHER a
  bracket NOR an "Accepted by" list.** `internal/syntax/commands.go#L58-66`
  (`kvkeys`) auto-adds `"d": true` to every command's kv set (comment:
  "the universal delay modifier"). No COMMANDS bracket lists `d=` explicitly
  (checked all 22), and MODIFIERS `:210-212` describes it in prose ("wait DUR
  after this command instead of the global delay... Also applies to
  query/sleep/set/cap/exec/open, which get no delay by default") without an
  "Accepted by" list — that sentence lists commands with *no default delay*,
  not which commands *accept* `d=`. Drift test (b), scoped exactly to
  bracket UNION Accepted-by-list, must therefore **exclude `"d"` from the
  code side before comparing** (document this explicitly in the test), or it
  will false-fail against every command. `ms=` is NOT special-cased this way:
  it appears explicitly per-command in brackets wherever accepted (verified:
  k, m, c, paste, drag, cap have it; kd/ku/scroll/etc. do not), so no
  exclusion is needed for it.
- `assets/help.txt#L299-448` (COMMANDS) — command name lines are `  <name>`
  (lowercase or `#`) optionally followed by `[bracket]`, then a >=2-space gap
  and a payload-grammar token; group headers (`KEYBOARD`, `MOUSE`, ...) are
  ALL-CAPS at the same 2-space indent (QUERY's header carries a trailing
  parenthetical, `:430`); description/example lines are indented 6/8 spaces.
  The leading-char-case + indent-depth pair cleanly separates all three line
  kinds - **no regularization needed** to find command names. 24 group
  entries listed (incl. `#`); `internal/syntax/commands.go#L70-95`
  (`buildCommands`) has 23 (all COMMANDS entries except `#`, which the
  line-shape dispatcher, not `cmdSpec`, handles) - test (b) must special-case
  excluding `#` from the code-table side (document why), not silently drop it
  unexplained.
- `assets/help.txt#L305-446` (per-command brackets) — bracket item shapes mix
  a modifier token with an inline default/description
  (`n=1 repeat`, `ms=30 gap`) and a small closed set of pipe-groups: enum
  values on ONE modifier (`b=left|right|middle`, `by=line|page` - `=` on the
  FIRST token) vs. a mutually-exclusive-modifier group written as literal
  `r|w|disp=` or `w|disp=N` (`=` only on the LAST token, appears verbatim in
  exactly these two forms across all commands that use it). A fully generic
  regex cannot disambiguate these two `|` shapes from position alone; a
  **purpose-built extractor that special-cases the two known literal
  frame-group tokens** (`r|w|disp=`, `w|disp=N`) resolves this without
  touching help.txt - confirmed these two strings appear verbatim (checked
  all occurrences) with no other pipe-group variants outside enum-value
  lists. This is real implementation complexity but not a case the ticket's
  regularization authorization covers (it names "Accepted by lists, KEY
  NAMES ranges, LIMITS sentences" only) - treat as purpose-built-extractor
  work first; only fall back to a help.txt edit if extraction genuinely
  proves infeasible, per the ticket's own "where not extractable" gate.
- `assets/help.txt#L448-458` (KEY NAMES) — a token list wrapped across 6
  lines with ranges (`a-z`, `0-9`, `f1-f24`, `numpad0-numpad9`) and `|`-alias
  groups (`alt|option`, `meta|cmd|win`, `enter|return`, `esc|escape`),
  immediately followed (no blank line) by a prose sentence ("Names are
  case-insensitive PHYSICAL keys..."). The list/prose boundary is currently
  only inferable heuristically (prose has punctuation/sentence casing, the
  list doesn't) - genuinely fragile, matches the ticket's flagged
  regularization target. Recommended minimal fix: insert a blank line
  between the token list (ending `... voldown mute`) and the prose sentence,
  so the extractor can stop at the first blank line. No wording changes.
- `internal/syntax/keys.go#L14-79` (`buildKeyNames`) — the code-side ground
  truth for (c): a `map[string]string` built by an `add(canon, aliases...)`
  helper; ranges are Go `for` loops (`a`-`z`, `0`-`9`, `f1`-`f24`,
  `numpad0`-`numpad9`), aliases are explicit `add("alt","option")` etc. calls.
  Test lives naturally in this package (needs the unexported `keyNames`
  var) - compare its **key set** (both canonical names and aliases, since
  the KEY NAMES prose lists every alias too) against the expanded help.txt
  token list.
- `internal/output/codes.go#L14-38` — `ErrorCode` consts (13: `ESyntax` ...
  `EUnknown`) and exit consts (6: `ExitOK`=0 ... `ExitStateUnknown`=5), both
  exported. `assets/help.txt#L698-724` (ERROR CODES) and `#L726-740`
  (EXIT CODES) are ALREADY cleanly extractable as-is: `E_[A-Z]+` at line
  start for the former, a leading digit `0`-`5` for the latter - **no
  regularization needed for (d)**, confirmed by direct inspection (every row
  matches a simple prefix regex, no multi-value or embedded-number rows).
- `internal/ir/limits.go#L1-26` — 18 exported constants; doc comment already
  states scope ("Only the target-independent rows are enforced... coordinate
  bounds, platform key support and exec output caps are preflight/runtime
  (out of scope here)"), matching the Out-of-Scope exclusions above.
  `assets/help.txt#L742-762` (LIMITS) rows needing regularization (multiple
  numbers or a non-numeric value cell per row, confirmed by direct read):
  - `:747` "k keys    64 sequential; 8 per chord including flags" - two
    constants (`MaxKeysSeq`=64, `MaxChordKeys`=8) in one row; split into two
    rows.
  - `:754-755` "drag    1..200 payload points (path 2..200 including the
    current position); steps 1..200" - three numeric ranges, two of which
    are constants (`MinDragPoints..MaxDragPoints`=1..200,
    `MinDragSteps..MaxDragSteps`=1..200); the third ("path 2..200 including
    current position") is derived prose, not a separate constant - split
    into a points row and a steps row, keep the derived-prose parenthetical
    attached to the points row.
  - `:757` "cap n=    <= 120; at most 121 captures per run" - two constants
    (`MaxCapN`=120, `MaxCapturesTotal`=121) in one row; split into two rows.
  - `:756` "label=    [A-Za-z0-9][A-Za-z0-9_-]{0,47}" - value cell is a
    regex, not a plain number; `MaxLabelLen`=48 is `{0,47}`'s length-plus-one,
    not directly present as a token. Recommended fix: prepend an explicit
    `<= 48 chars:` clause before the regex so the number is a plain token,
    regex kept unchanged as documentation.
  Rows already fine as single-constant, single-number-token rows (no edit
  needed): lines per run (1000), line length (64 KiB, `MaxLineBytes`),
  scroll ticks (1..50), sleep (<=60s), `d=`/`ms=` (<=10s), `wait=` (<=60s),
  exec `timeout=` max (<=60s - the "(default 10s)" parenthetical is a
  default, not a limit bound, and is excluded from comparison, not deleted),
  `scale=` (0.1..4.0 or native).
- `ai-docs/WORKFLOW.md#L64-65` — spec anchors use a stable `{#YYMMDD-slug}`
  heading suffix; exact heading level isn't pinned down repo-wide, but the
  ticket's own Spec Impact text (`ai-docs/tickets/ready/260907-feat-cli-core.md:85-87`)
  fixes the literal per-entry content: "the section name and the line 'See
  `assets/help.txt` section `== NAME ==`'" - test (a)'s extractor should key
  off that literal "See `<help-file>` section `== NAME ==`" line text (robust
  to heading-level formatting choices) rather than parsing arbitrary
  markdown structure.
- `ai-docs/spec/` — currently only `.gitkeep`, confirmed via `find`. Test (a)
  needs filesystem access to `ai-docs/spec/*.md` at test run time, NOT
  `go:embed` (an embed pattern with zero current matches fails to compile,
  and embed cannot reach outside its package directory anyway). Placing the
  test in the `assets` package (sibling to `ai-docs/` one level up:
  `assets/x_test.go` -> `../ai-docs/spec/help.md`) keeps the relative path
  minimal; `cmd/gotto-hando/main_test.go#L20-40` shows the repo's existing
  convention of resolving paths relative to the test's own package
  directory (no repo-root-finding helper exists or is needed).
- Baseline confirmed green before any Phase 3 work: `go build ./...`,
  `go vet ./...`, `go test ./...` all pass on current HEAD (no test files yet
  in `assets` package).

## Implementation Plan

1. **`assets/help.txt` normative edits (same commit as the tests), 3 items,
   meaning-preserving:**
   - MODIFIERS `:209` and MODIFIER KEYS `:235` - make the "Accepted by" line
     wording identical in both sections (e.g. both
     `Accepted by: k m c md mu drag scroll.`).
   - KEY NAMES `:448-458` - insert one blank line between the token list
     (`... voldown mute`) and the following prose sentence.
   - LIMITS `:742-762` - split the `k keys`, `drag`, and `cap n=` rows each
     into two single-constant rows; prepend an explicit `<= 48 chars:` clause
     to the `label=` row before its regex. Re-check the section still fits
     <=80 columns and keeps the `^== ... ==$` header form (both already true
     repo-wide; verify after edits with the same one-liner used during this
     survey: `python3` loop over `open(...).readlines()` computing max line
     width, or an equivalent shell one-liner).
   Do not touch any other section. Re-run `grep -n '^== '
   assets/help.txt` after editing to get correct line numbers for any doc
   comments the new tests add (do not reuse this plan's or the Phase 2
   plan's cited numbers verbatim - both are already stale relative to
   each other).
2. **`assets/spec_drift_test.go`** (new, package `assets`) - test (a):
   - Helper: parse a help text's `== SECTION ==` header lines into a set of
     section names (same extraction idiom as `extractOptionsTableFlags`,
     generalized to full-line header capture).
   - Helper: read `../ai-docs/spec/<file>.md` if present (use
     `os.ReadFile`, tolerate `os.IsNotExist`), extract every section name
     referenced by a "See `assets/help-<x>.txt` section `== NAME ==`" line
     (or the matching per-file variant) via a literal-anchored regex.
   - Forward check: every extracted spec-referenced name exists in the
     help file's section set (vacuously true today - zero spec files).
   - Reverse check: every help-file section name has a matching spec entry -
     **skip with `t.Skip` and an explanatory message when the spec file for
     that help text doesn't exist yet** (documented as the known,
     ticket-acknowledged pre-closeout state - see Codebase Findings /
     ticket Spec Impact). Once `ai-docs/spec/help.md` etc. exist
     (ticket closeout, out of scope here), this same unmodified test starts
     enforcing the reverse direction with no further code change - matching
     the ticket's "green from the day it lands" language without ever
     failing `go test ./...` in the interim.
   - Run for all four (help.txt, help-macos.txt, help-windows.txt,
     help-remote.txt) x (spec/help.md, help-macos.md, help-windows.md,
     help-remote.md) pairs.
3. **`assets/lint_test.go`** (new, package `assets`) - test (f):
   - Iterate all four embedded strings; for each: assert every line is
     <=80 columns (`utf8.RuneCountInString` or byte length - confirm which
     the ticket's 80-column constraint means; byte length is simplest and
     matches how the columns were counted during this survey), assert every
     `== ... ==` line matches `^== [A-Za-z0-9 ,/:()-]+ ==$` via
     `regexp.MustCompile`, assert the raw embedded string ends with exactly
     one trailing `\n`.
4. **`internal/syntax/help_drift_test.go`** (new, package `syntax`) - tests
   (b) and (c), using the unexported `commands`, `keyNames`, `flagSymbol`
   package vars directly:
   - (c): parse KEY NAMES `:448-454` (after the blank-line regularization
     from step 1) into an expanded token set (ranges `X-Y` for letters,
     digits, `f1-f24`, `numpad0-9`; `|`-alias groups become one entry per
     alias); compare against `keyNames`'s key set (canonical names + aliases
     both included, since `keyNames` maps every alias to its canonical form
     - compare `len(keyNames)` keys, not just canonical names).
   - (b): extract command names from COMMANDS per the line-shape rule in
     Codebase Findings (2-space indent, lowercase-or-`#` leading token,
     optional `[bracket]`, then a payload token) minus `#`; compare (sorted)
     against `commands`' key set (document the `#` exclusion inline).
     Extract each command's bracket modifier set via a purpose-built
     per-item parser: split on top-level commas, then per item either (i) a
     bare word (`shell`, `noerr`) - one flag name, or (ii) `name=...` - take
     `name` before `=` (ignore the default/description after it), or (iii)
     the two known literal frame-group tokens `r|w|disp=` / `w|disp=N` -
     expand to `r`,`w`,`disp` (special-cased, per Codebase Findings). Union
     each command's bracket set with `{c,s,a,m,p}` when that command's name
     appears in the (post-regularization, now-identical) MODIFIERS/MODIFIER
     KEYS "Accepted by" list. Compare against each `cmdSpec`'s `single`
     (rune flags) + `word` + `kv` sets **minus `"d"`** (excluded per the
     universal-modifier finding above - assert this exclusion in a comment
     referencing help.txt MODIFIERS `:210-212`).
5. **`internal/output/help_drift_test.go`** (new, package `output`) - test
   (d): extract `E_[A-Z]+` codes from ERROR CODES and compare (sorted) to an
   explicit literal slice of all 13 `ErrorCode` consts; extract leading-digit
   exit numbers from EXIT CODES and compare to an explicit slice of all 6
   exit consts' values.
6. **`internal/ir/limits_drift_test.go`** (new, package `ir`) - test (e):
   extract each (now-regularized, one-number-per-row) LIMITS row into a
   `name -> value(s)` map using a small per-row-name dispatch (plain number,
   `<=N`, `A..B` range, `A..B or native`), explicitly skip the three
   out-of-scope rows (exec stdout/stderr, `--timeout`, `argv`) with a
   documented reason; compare each extracted value against its named
   constant (`MaxLinesPerRun`, `MaxLineBytes`, `MaxKeysSeq`, `MaxChordKeys`,
   `MinScrollTicks`/`MaxScrollTicks`, `MaxSleepMS`, `MaxDelayMS`, `MaxWaitMS`,
   `MaxExecTimeoutMS`, `MinDragPoints`/`MaxDragPoints`,
   `MinDragSteps`/`MaxDragSteps`, `MaxCapN`, `MaxCapturesTotal`,
   `MinScale`/`MaxScale`, `MaxLabelLen`). Convert `s`/`ms`/`KiB` units to the
   constants' units (ms, bytes) inline in the test.
7. **Manual verification pass** (not a committed test, per ticket wording):
   for at least one row in each of (b)-(f), copy `assets/help.txt` to a
   scratch file, edit that copy so the value/name diverges from code, run
   the corresponding test against the scratch copy's content (e.g. via a
   small local `go run`/temp-package swap, or temporarily point the embed at
   the scratch copy) to confirm it fails, then discard the scratch copy.
   Confirm test (g) (`options_drift_test.go`) still passes unmodified.

## Verification Plan

- `go build ./...`, `go vet ./...`, `go test ./...` all green after the
  changes (baseline already confirmed green before this plan).
- `go test ./assets/... ./internal/syntax/... ./internal/output/...
  ./internal/ir/...` scoped run to isolate the new drift tests during
  development.
- Manual scratch-copy negative check per item (b)-(f) as in Implementation
  Plan step 7 - each corresponding test must fail against the deliberately
  broken copy.
- Re-run the header/column/newline checks from this survey
  (`grep -n '^== ' assets/help.txt` plus a max-line-width scan) after the
  LIMITS/KEY NAMES/MODIFIERS edits to confirm the <=80-column and header
  constraints still hold repo-wide (all four help texts), not just in the
  edited section.
- Confirm `assets/help.txt`'s byte content still round-trips through
  `cmd/gotto-hando`'s existing `TestHelpFlagsByteIdentical`
  (`cmd/gotto-hando/main_test.go:65-87`) - it compares against
  `assets.Help` directly, so any edit is automatically covered, but run it
  explicitly since it's the ticket's existing tripwire for accidental
  content drift.

## Escalations

- None. The one apparent contract tension - Phase 3's "go test ./... green"
  verification versus test (a)'s reverse-direction check needing spec files
  that ticket-closeout (not Phase 3) creates - resolves cleanly and with
  high confidence from sources already inside the ticket itself: the Spec
  Impact section and both prior phases' `### Result` sections independently
  and consistently state the spec files land after Phase 3. A `t.Skip` on
  file-absence (Implementation Plan step 2) implements the ticket's own
  stated sequencing rather than inventing a scope cut - both directions of
  test (a) are fully implemented, and the reverse direction begins
  enforcing, unmodified, the moment closeout lands its spec files. The
  COMMANDS-bracket parsing complexity (Codebase Findings, per-command
  modifier extraction) is real implementation work but not a strategy
  question: a purpose-built extractor handling the two known literal
  frame-group tokens is a bounded, closed-set special case, not an
  open-ended design decision, and the ticket's regularization authorization
  already covers the fallback (edit help.txt without changing meaning) if
  that extractor genuinely proves infeasible during implementation.
  Confidence: high.
