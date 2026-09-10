# Ship: gotto-hando

Public target: GitHub Releases in `kang-sw/gotto-hando`.

## Version Strategy

- The first release is `0.1.0`.
- Before 1.0, a breaking change increments the minor version and resets the patch
  to zero; any non-breaking release increments the patch version. For example,
  `0.1.0` becomes `0.2.0` for a breaking release or `0.1.1` otherwise.
- Classify the complete change range since the previous release by compatibility.
  A non-breaking feature is a patch release; commit type alone does not determine
  the version. If compatibility is uncertain, resolve it before release.
- Keep the version on the first line of `assets/help.txt` as the single source of
  truth used by `assets.Version()` and `gotto-hando --version`. Commit any version
  change before tagging. Release tooling must reject a requested version that
  differs from this source.
- Transitioning to 1.0 requires a separate policy decision.

## Pre-flight

- Require a clean working tree and a reviewed commit on `main`.
- Verify that local `main` and `origin/main` identify the intended release commit.
- Require successful CI for that commit, including native macOS and Windows Go
  tests and installer tests. GUI acceptance is separate from hosted CI.
- Verify that the version's tag and published release do not already exist.
- Use the repository's existing GitHub authentication; never put credentials in
  release assets, manifests, scripts, or this file.

## Build

- Run `bash scripts/release.sh <version>` to prepare release assets in `dist/`.
- Produce macOS arm64/amd64 and Windows amd64 binaries, plus `install.sh`,
  `install.ps1`, `SHA256SUMS`, and `RELEASE-MANIFEST`.
- Keep checksums and binaries bound to the same exact release version. Installers
  resolve `latest` once and then download version-specific assets.
- The tag workflow repeats validation and packaging before publication.

## Tag

Format: `v<version>` (first release: `v0.1.0`).

- Create an annotated tag on the verified release commit:
  `git tag -a v<version> -m "Release <version>" <commit>`.
- Confirm the concrete version, commit, tag, and public target before pushing,
  unless the user has explicitly authorized proceeding through publication.
- Push only that tag: `git push origin v<version>`.
- A tag push is the publication trigger. Never force-move a released tag.

## Publish

- `.github/workflows/release.yml` publishes the version's assets to GitHub Releases
  after its required checks succeed.
- Only the publishing job receives `contents: write`; ordinary CI has read-only
  repository permissions.
- Source branch pushes perform CI without publishing a release.

## Installation Contract

- macOS: `~/.local/bin/gotto-hando`.
- Windows: `$HOME\.local\bin\gotto-hando.exe`.
- Download and verify the exact asset's SHA-256 before installing or updating.
  A failed download or checksum mismatch must preserve an existing installation.
- Do not modify PATH. If the install directory is absent, warn and show commands
  the user can choose to run.
- Do not start, stop, or register the bridge, create startup entries, or alter
  execution/security policies. The user runs `gotto-hando --bridge` manually.
- If Windows cannot replace an executable because it is in use, preserve it and
  explain that the user must stop it before retrying. On macOS an atomic file
  replacement can leave an old process running; advise a manual bridge restart.
- Remote callers and targets need matching versions. Document `--remote-bin` for
  SSH environments where the per-user install directory is not on PATH.

## Post-ship

- Wait for the tag workflow and read its result; report a failed publication
  without treating the tag alone as a successful release.
- Verify the public release's tag, asset set, and checksums.
- Exercise a released installer in an isolated install location and verify
  `--version` reports the released version. Do not disturb an active GUI bridge.
- Report the release URL, version, CI results, and native verification limits.
