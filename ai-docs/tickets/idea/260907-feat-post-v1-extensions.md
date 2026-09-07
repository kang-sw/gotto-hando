---
title: "Post-v1 extensions (M3/M4): qwin filters, jpg, cursor capture, capture APIs"
parent: 260907-epic-gotto-hando-v1
---

# Post-v1 extensions (M3/M4): qwin filters, jpg, cursor capture, capture APIs

## Background

Items deferred past v1 (marked M3/M4 in CONCEPT.md ch. 13). None of them
appears in the help texts today; each one must add itself to the help text
in the same commit as the code, per the single-SoT rule in the epic.

- `qwin` selector filters beyond substring/regex.
- `cap[fmt=jpg,q=]` (v1 captures are PNG only; `fmt=` is E_SYNTAX).
- `cap[cursor]` cursor overlay in captures.
- ScreenCaptureKit (macOS; CGWindowListCreateImage / CGDisplayCreateImage
  are deprecated since 14.4) and DXGI Desktop Duplication (Windows) capture
  paths.
- A CI release matrix producing darwin/windows binaries.
- Clipboard image payload from the agent's machine (base64 wire form for
  `<dest>`): superseded for now by `260908-feat-rclip` (scp + rclip).

## Phases

### Phase 1: To be split when picked up

Each bullet above is small enough to be its own phase or ticket; decide the
split when promoting this ticket.
