---
title: Release Installation
summary: Checksummed per-user installation for macOS and Windows.
---

# Release Installation

GitHub Releases provide macOS arm64/amd64 and Windows amd64 binaries and
installation scripts. Release procedures and version policy are defined in
[the ship configuration](../ship/gotto-hando.md).

## Per-user installation {#260910-release-installation}

- `install.sh [version]` installs `~/.local/bin/gotto-hando` on macOS.
  `install.ps1 [-Version version]` installs
  `$HOME\.local\bin\gotto-hando.exe` on Windows. The Windows script supports
  Windows PowerShell 5.1 and requires an amd64 environment.
- Omitting the version selects `latest`. A latest install resolves one release
  tag and uses that version for both the platform asset and `SHA256SUMS`.
  Explicit versions may include the `v` prefix.
- Installation requires exactly one checksum entry for the selected asset and
  matching SHA-256 bytes. Download failures, missing or duplicate entries, and
  mismatches fail before replacement and preserve an existing installation.
- Updates stage the verified file in the destination directory before replacing
  the target. macOS installs an executable file. A directory at the target path
  is rejected. If the OS denies replacement, installation fails and preserves
  the existing file.
- Neither installer modifies PATH, starts or stops processes, or registers
  startup tasks. When the install directory is absent from PATH, the installer
  warns and prints a platform-appropriate registration command for the user to
  run. The user starts the GUI bridge with `gotto-hando --bridge`.
- Atomic replacement may succeed while an existing process keeps its old image.
  Installers leave it running and advise manual restart; they do not enforce
  restart or detect processes.
- `GOTTO_HANDO_INSTALL_DIR` selects an alternative destination directory.
  `GOTTO_HANDO_BASE_URL` selects an alternative asset-directory URL after version
  selection. The shell script's `GOTTO_HANDO_REPO` and PowerShell's `-Repository`
  select the GitHub repository, defaulting to `kang-sw/gotto-hando`.

Remote execution requires matching client and target versions. An SSH session
may lack the interactive shell's PATH; use an explicit remote binary path with
`--remote-bin` and verify it through `ssh`, as described in the README.
