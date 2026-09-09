---
title: "rclip: load a file on the target machine into its clipboard"
parent: 260907-epic-gotto-hando-v1
related:
  260907-feat-cli-core: prerequisite
  260907-feat-darwin-backend: prerequisite
  260907-feat-windows-backend: prerequisite
  260908-feat-remote-ssh: prerequisite
sage-review-design: completed
sage-review-completeness: completed
sage-review-design-reviewed: 946a80f56420f3af
sage-review-completeness-reviewed: 946a80f56420f3af
---

# rclip: load a file on the target machine into its clipboard

## Background

`[f]` always names a file on the agent's machine and carries UTF-8 text of at
most 64 KiB; there is no way to put a binary (an image) on the target's
clipboard except an OS-specific `exec[]` script (`osascript ... «class
PNGf»` on macOS, PowerShell `Clipboard.SetImage` on Windows). Large or binary
files reach a `<dest>` by scp (help-remote.txt HOW IT WORKS), so the missing
half is only "put this file, already on the target, on the target's
clipboard" in an OS-independent way. Typical flow:

    scp ./asset.png winbox:C:/work/
    gotto-hando winbox 'win[]Slack' 'rclip[]C:\work\asset.png' 'k[p]v'

## Decisions

- New command `rclip[]<path>` (CLIPBOARD group). The path is resolved on the
  machine that executes the run: the same machine for `local`, the remote
  machine for a `<dest>`. This keeps the `[f]` rule intact (`[f]` = agent's
  machine, never forwarded) and adds no remote-only concept to the grammar;
  the wire carries a path only, so the 64 KiB line limit, base64 and [f]
  inlining are untouched.
- Content type by extension, overridable by flag: `.png .jpg .jpeg .gif
  .bmp .tif .tiff` -> image; anything else -> UTF-8 text (invalid UTF-8 is
  E_CLIPBOARD). `rclip[img]` / `rclip[txt]` force the type. Images are put
  on the clipboard as PNG (macOS `NSPasteboard` `public.png` + TIFF, Windows
  `PNG` registered format + `CF_DIB`); text as the platform's plain text.
- Separate command rather than a modifier on `clip`: `r` already means
  pointer-relative / regex, and `rclip` pairs with `qclip`. No `rpaste`:
  `rclip[]...` followed by `k[p]v` does the job; a `paste` flag can be added
  later if the two-line form proves noisy.
- Security: the target reads a file of its own user; no new privilege beyond
  what `exec`/`open` already grant. File size limit 64 MiB (E_CLIPBOARD).
- Errors: missing/unreadable file, size limit, decode failure and clipboard
  write failure are all E_CLIPBOARD at run time (the file lives on the
  target, so preflight cannot check it). No default delay exemption: rclip
  is an input-side command and gets the global delay like `clip`.
- Output: `<n> ok rclip type=image|text bytes=N` (image: also `WxH`).
  `bytes` is the source-file byte count, before image conversion.

## Constraints

- help.txt is the single source of truth (epic decision 1): the COMMANDS
  entry, OUTPUT line, JSONL/IR fields, LIMITS row and an EXAMPLES line land in
  `assets/help.txt` in the same commit as the code; help-remote.txt's "Files
  other than text" paragraph is updated to name `rclip` as the consumer
  after scp. Explain `img` and `txt` as named modifier tokens in SYNTAX.
  Drift tests (cli-core Phase 3) must stay green.
- IR: one new op kind `rclip` with `path`, `type` (auto|image|text).
- Sage design and completeness reviews passed on 2026-09-09 after the user
  authorized proceeding. Implementation should clarify named modifier tokens
  in the syntax help and define the reported byte count consistently with
  the input-file size limit.

## Spec Impact

Extend the existing help-text sections for commands, output/JSONL, limits,
examples, and remote file handling with the `rclip` contract described above.
The normative changes land in `assets/help.txt` and `assets/help-remote.txt`;
their existing pointer specs remain applicable. No new section or anchor is
required.

## Phases

### Phase 1: Parser, IR, help text, both backends

Goals: `rclip` in the parser command table with the `img`/`txt` flags; IR op;
engine wiring; darwin writer (purego objc: NSPasteboard clearContents +
setData:forType: for `public.png` and `public.tiff`, NSString for text);
windows writer (OpenClipboard/EmptyClipboard/SetClipboardData with the
registered `PNG` format plus a CF_DIB conversion, CF_UNICODETEXT for text);
help.txt / help-remote.txt edits listed under Constraints.
Verification: unit tests for extension mapping and flag override; a local
run `rclip[]./x.png` followed by `qclip` (text absent -> documented result)
and a paste into Preview/Paint that shows the image; a `<dest>` run whose
path is on the remote machine; E_CLIPBOARD for a missing file with the run
continuing under `-k`.
