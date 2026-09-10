# gotto-hando

`gotto-hando` drives a macOS or Windows desktop from the shell. Releases are
single native binaries with a SHA-256 manifest. The first release is `0.1.0`.

## Install

Download a release asset and verify it against `SHA256SUMS`, or use the
included installers (they verify before replacing anything):

```sh
curl -fsSL https://github.com/kang-sw/gotto-hando/releases/download/v0.1.0/install.sh | bash -s -- 0.1.0
```

In PowerShell:

```powershell
& ([scriptblock]::Create((irm https://github.com/kang-sw/gotto-hando/releases/download/v0.1.0/install.ps1))) 0.1.0
```

The macOS installer writes `~/.local/bin/gotto-hando`; Windows writes
`$HOME/.local/bin/gotto-hando.exe`. Omit the version to install the latest
release. The installers never change PATH, register startup tasks, or stop a
running bridge. If the directory is absent from PATH, add it in your shell
profile and restart that shell. Start the GUI bridge manually with
`gotto-hando --bridge`; platform permissions and session requirements are in
`gotto-hando --help-macos` and `gotto-hando --help-windows`.

## Releases and upgrades

Release artifacts include macOS arm64/amd64 and Windows amd64 binaries plus
`SHA256SUMS`. Updates download into a temporary directory, verify the checksum,
and replace the destination atomically. A locked executable causes the update
to fail while preserving the existing file; no process is killed. Remote runs
require the same version on both machines (`ssh <dest> gotto-hando --version`,
or pass `--remote-bin /Users/REMOTE_USER/.local/bin/gotto-hando` when sshd cannot see
the per-user install directory).

The embedded first line of `assets/help.txt` is the version source used by
`--version`; release tooling checks the source version before producing every
cross-compiled artifact. Before 1.0.0, breaking changes advance the minor version and reset
patch to zero; non-breaking changes advance patch. Version changes are explicit
release decisions and are never inferred from commit prefixes.
