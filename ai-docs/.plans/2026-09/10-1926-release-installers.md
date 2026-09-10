# Plan: release CI and installers

## Relevant Ticket Contract
- Implement GitHub Releases CI and per-user installers for the first explicit release `0.1.0`; use the existing `assets.Version()`/`assets/help.txt` source for version consistency ([assets/version.go](/Users/kang-sw/Repos/gotto-hando/assets/version.go:8), [assets/help.txt](/Users/kang-sw/Repos/gotto-hando/assets/help.txt:1)).
- Publish checksummed macOS and Windows binaries, with optional explicit version selection; install to macOS `~/.local/bin/gotto-hando` and Windows `$HOME/.local/bin/gotto-hando.exe`.
- Never modify PATH or bridge startup. Warn and print platform commands when the install directory is absent from PATH; bridge remains manual `gotto-hando --bridge`.
- Updates must fail safely when the executable is locked and must never kill running processes. CI must run tests on native macOS and Windows and verify cross-build/package checksums.
- Source commits and pushes are authorized; release publishing remains a separate explicit final gate. Do not change protocols, package-manager registries, signing policy, or a live bridge.

## Out of Scope
- Automatic startup registration, PATH mutation, package-manager publication, protocol changes, code-signing/notarization policy changes, or release publication during implementation.
- No installer behavior that replaces a running executable in place without a safe lock/failure check.

## Codebase Findings
- `Makefile#L1-L23` — only local `CGO_ENABLED=0` build/sign/test helpers exist; no release, archive, checksum, or cross-build target.
- `assets/version.go#L8-L31` and `assets/help.txt#L1` — version is parsed from the embedded help first line; no ldflags injection or SemVer validation exists. Release tooling must compare generated artifact version output with the requested tag/version.
- `cmd/gotto-hando/dispatch.go#L84-L105` and `assets/help.txt#L67-L82` — `--version` is the compatibility check, and remote runs forward `--expect-version`; both machines must carry the same release version.
- `assets/help-remote.txt#L141-L159` — remote ssh sees a reduced/non-interactive PATH and supports `--remote-bin`; an installer should make the requested per-user binary path explicit and only warn if PATH does not contain it.
- `assets/help-macos.txt#L155-L176` and `assets/help-windows.txt#L80-L109` — bridge startup is an absolute-path LaunchAgent or interactive-only Task Scheduler task. Install/upgrade must leave registration untouched; users restart the bridge themselves after replacing a binary.
- `scripts/codesign-dev.sh#L1-L15` and `assets/help-macos.txt#L72-L93` — available signing is a local self-signed development identity for TCC persistence; distribution work must not silently replace or bypass this policy.
- `CONCEPT.md#L557-L561` — future GoReleaser/release scripts and cross-build assumptions are documented but not implemented; use the actual Go build path and verify all produced artifacts.
- `git remote -v`, `gh repo view`, `gh release list`, and `git tag --list` — repository is confirmed public at `https://github.com/kang-sw/gotto-hando`; GitHub CLI is authenticated as `kang-sw`; no existing tags or releases were found.

## Implementation Plan
1. Add a release policy at `ai-docs/ship/gotto-hando.md` defining the `0.1.0` baseline, pre-1.0 breaking-minor/non-breaking-patch policy, explicit version input, artifact matrix, checksum requirements, release gate, and no-PATH/no-startup/no-signing-bypass rules.
2. Add a concise `README.md` covering download/install to the two per-user `.local/bin` locations, optional version selection, checksum verification, PATH warning/commands, manual bridge startup, and safe upgrade behavior.
3. Add reusable build/package tooling (Makefile or scripts) that builds darwin and windows targets with `CGO_ENABLED=0`, emits raw per-OS/architecture binaries plus SHA-256 checksums, and derives/validates the binary version against `assets.Version()` and the release version.
4. Add GitHub Actions workflow(s) under `.github/workflows/` with native macOS/Windows test jobs and a release package/checksum job. Keep publishing behind the separately approved release gate and grant only the permissions required by the selected job.
5. Implement installer/update commands or scripts that select an explicit/latest release, download and verify checksums before replacement, install to the specified per-user location, detect locked executables, leave the existing executable intact on any failure, and report PATH setup commands without changing PATH or bridge startup.
6. Add focused tests for version/artifact consistency, checksum verification, path selection, locked-file safe failure, and platform command generation; retain existing bridge protocol and startup behavior unchanged.

## Verification Plan
- `go test ./... -race` plus native CI runs on macOS and Windows.
- Cross-build `CGO_ENABLED=0 GOOS=darwin GOARCH=arm64|amd64` and `GOOS=windows GOARCH=amd64` (and supported additional Windows architecture), then verify each artifact’s `--version`, archive/file names, and SHA-256 manifest.
- Exercise installer success, checksum mismatch, interrupted download, explicit version, missing PATH, and locked executable cases; verify the original binary remains intact after failures.
- Inspect workflow permissions and run a dry-run/package-only workflow before the explicit release-publish gate. Confirm no bridge process is killed and no startup/PATH registration is modified.

## Escalations
- None.
