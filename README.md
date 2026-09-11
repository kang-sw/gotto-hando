<p align="center">
  <img src="docs/gotto-hando-logo.png" width="720" alt="gotto-hando logo: ゴット ハンド">
</p>

# gotto-hando

**A compact desktop-control language for AI agents.** Drive macOS and Windows
with short, composable instructions that are easy to generate, inspect, and
replay from a shell.

```sh
gotto-hando local \
  'open[wait=5s]Safari' \
  'win[]app:Safari' \
  'k[p]l' \
  'txt[]https://example.com' \
  'k[]enter' \
  'sleep[]1s' \
  'cap[w,label=loaded]'
```

Each quoted argument is one instruction. The sequence launches Safari, focuses
its window, presses the platform-native address-bar shortcut, enters a URL,
waits, and captures the result. The same language works against another Mac or
Windows PC over SSH:

```sh
gotto-hando winbox \
  'win[]Blender' \
  'k[]a' \
  'k[]g' \
  'k[]x' \
  'k[]1 period 5' \
  'k[]enter' \
  'cap[w,label=moved]'
```

## The language

Every instruction has one predictable shape:

```text
command[modifiers]payload
```

The brackets are always present when an instruction has a payload. Short
command names keep generated sequences compact; explicit modifiers keep their
meaning visible.

| Intent | Instruction |
| --- | --- |
| Focus a window | `win[]Blender` |
| Click the center of that window | `c[w]50%,50%` |
| Move smoothly within it | `m[w,ms=250]70%,30%` |
| Press Cmd+S on macOS or Ctrl+S on Windows | `k[p]s` |
| Type Unicode text | `txt[ms=15]Hello, world!` |
| Paste a local file | `paste[f]./prompt.txt` |
| Drag a path | `drag[ms=400]200,300 600,300 600,500` |
| Scroll two pages | `scroll[by=page]down 2` |
| Put a target-side image on the clipboard | `rclip[img]C:\work\reference.png` |
| Capture the focused window | `cap[w,label=after]` |
| Run a target-side process | `exec[timeout=60s]blender -b scene.blend -f 1` |
| Inspect session and permissions | `qinfo` |

Modifiers compose. `w` changes coordinates to the current window, `r` makes a
move relative, `disp=N` selects a display, `p` means the platform's primary
modifier, and `ms=` controls the operation's own timing. For example:

```text
c[ws]120,240                 # Shift-click at window-relative coordinates
m[r,ms=300]40,-20            # Smooth relative move
cap[disp=1,scale=0.5]        # Capture display 1 at half scale
k[n=3,ms=80]tab              # Press Tab three times
```

Queries use a `q` prefix and do not mutate the desktop: `qinfo`, `qdisp`,
`qmouse`, `qwin`, and `qclip`.

## Capture, act, verify

Desktop work is visual, so screenshots are ordinary instructions rather than a
separate workflow. Capture paths are always local to the computer invoking
`gotto-hando`, even when the desktop is remote.

```sh
# 1. Observe the remote desktop.
gotto-hando winbox 'qinfo' 'qwin' 'cap[label=before]'

# 2. Act using coordinates read from the image, then verify immediately.
gotto-hando winbox \
  'win[]Blender' \
  'c[w]50%,50%' \
  'k[]g z 2 enter' \
  'cap[w,label=after]'
```

A default capture uses the same coordinate space as input, so a point read from
the PNG can be used directly in `m`, `c`, or `drag`. Window-relative and
percentage coordinates make sequences resilient to different screen sizes.

For animation or transient UI, capture a short burst:

```sh
gotto-hando local \
  'm[]960,540' \
  'drag[b=middle,ms=300]960,540 1100,540' \
  'cap[n=4,ms=100,label=orbit]'
```

## Practical recipes

### Paste a console command, inspect it, then run it

Splitting inspection and execution into two runs gives an agent a visual
checkpoint before it presses Enter:

```sh
gotto-hando winbox \
  'win[r]Python Console' \
  'paste[f]./console-command.txt' \
  'cap[w,label=script-ready]'

gotto-hando winbox \
  'k[]enter' \
  'sleep[]2s' \
  'cap[w,label=script-finished]'
```

Here `sleep[]2s` guarantees only that the capture will not begin before two
seconds have elapsed. Application work, capture, encoding, and transport add
their own time, so the capture result is not promised to arrive at two seconds.

The file should contain the single line the console will execute. For a
multi-line Python script, that can be an `exec(compile(...))` wrapper with the
script encoded inside it. `[f]` paths are read on the machine invoking
`gotto-hando` and inlined before the sequence crosses SSH. This lets an agent
prepare the command locally without arranging a remote temporary text file.

### Move a reference image to a remote clipboard

Transfer the image to the target, load it into the target's native image
clipboard, then paste it into an application that accepts image paste:

```sh
scp ./reference.png winbox:reference.png
gotto-hando winbox \
  'rclip[img]reference.png' \
  'win[]Paint' \
  'k[p]v' \
  'cap[w,label=pasted-reference]'
```

`rclip` understands PNG, JPEG, GIF, BMP, and TIFF and publishes native image
clipboard formats on both platforms. Use `qclip[f]./selection.txt` to bring
target clipboard text back into a local file.

### Use a sequence file

Longer programs stay readable as plain text:

```text
# edit-note.gh
win[]Notes
c[w]50%,30%
k[p]a
txt[f]./replacement.txt
k[p]s
cap[w,label=saved]
```

```sh
gotto-hando local --check -f edit-note.gh
gotto-hando local -f edit-note.gh
```

`--check` parses and validates the complete sequence without touching the
desktop. `--ir` prints the parsed representation when a tool needs to inspect
exactly what will run.

## Designed for unattended sequences

- **Validate before acting.** The entire program is parsed and statically
  checked before the first input event.
- **Fail fast with line numbers.** A failed instruction stops later work by
  default and reports the original source line. Use `-k` when independent
  steps should continue.
- **Release held input.** Keys and mouse buttons held across instructions are
  released when a run finishes, fails, times out, or is interrupted.
- **Verify application behavior.** A successful input event means the OS
  accepted it; place `cap` wherever the sequence needs visual evidence.
- **Stream structured results.** `--jsonl` emits start, per-line result, and
  done events for programmatic consumers.
- **Use one language locally and remotely.** A destination changes transport,
  while the sequence itself stays the same.

Plain output is concise and remains useful in a terminal:

```text
out /tmp/gotto-hando/local/...
1 ok win id=771 app=Safari matched=1 0,25 1440x875 "Example Domain"
2 ok c
3 ok cap /tmp/gotto-hando/local/.../0000-after-20260910T120000.000Z.png 1440x875 origin=0,25 scale=1
done ok=3 err=0 skip=0 elapsed=481ms held_released=0
```

## Install

The installers place a single native binary in the current user's
`.local/bin` directory and verify it against the release SHA-256 manifest.

macOS:

```sh
curl -fsSL https://github.com/kang-sw/gotto-hando/releases/download/v0.1.0/install.sh | bash -s -- 0.1.0
```

Windows PowerShell:

```powershell
& ([scriptblock]::Create((irm https://github.com/kang-sw/gotto-hando/releases/download/v0.1.0/install.ps1))) 0.1.0
```

The macOS installer writes `~/.local/bin/gotto-hando`; Windows writes
`$HOME/.local/bin/gotto-hando.exe`. The installers leave PATH and startup
settings unchanged. If needed, add `.local/bin` to PATH, then start the GUI
bridge manually with `gotto-hando --bridge`.

Release artifacts are available for macOS arm64/amd64 and Windows amd64. See
the [v0.1.0 release](https://github.com/kang-sw/gotto-hando/releases/tag/v0.1.0)
for binaries, installers, and `SHA256SUMS`.

## Remote desktops

Remote control uses SSH for transport and a small bridge running inside the
logged-in GUI session:

```text
agent machine ── SSH ──> remote gotto-hando ── session bridge ──> desktop
```

Install the same version on both machines, then run this from a terminal on
the target desktop:

```sh
gotto-hando --bridge
```

From the agent machine, verify the remote binary and send a sequence:

```sh
ssh winbox gotto-hando --version
gotto-hando winbox 'qinfo' 'cap'
```

If the SSH environment cannot find the per-user install, pass its absolute
path with `--remote-bin`. Captures and `qclip[f]` results still arrive on the
agent machine. No separate network listener is exposed by the bridge.

For platform setup and troubleshooting, use the manuals embedded in the
binary:

```sh
gotto-hando --help-macos
gotto-hando --help-windows
gotto-hando --help-remote
```

The complete language contract is available through `gotto-hando --help` and
in [`assets/help.txt`](assets/help.txt). That file is the normative source for
syntax, commands, limits, output, and exit statuses.

## Build from source

gotto-hando is written in Go and builds with `CGO_ENABLED=0`.

```sh
go test ./...
go build ./cmd/gotto-hando
```

Release packaging is handled by `scripts/release.sh`. Before 1.0, breaking
changes advance the minor version and non-breaking changes advance the patch
version.

## License

[MIT](LICENSE)
