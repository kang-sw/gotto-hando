# Mental Model: release-installation

`scripts/install.*`, their native fixtures, and the release workflows. The
installation contract lives in `ai-docs/spec/installation.md`; these rules
capture platform behavior that is easy to lose during maintenance.

## Domain Rules

- **PowerShell 5.1 needs `NullString.Value` for the absent `File.Replace`
  backup filename.** Passing `$null` through its .NET overload binder becomes
  an empty string and rejects an otherwise valid update. Use
  `[System.Management.Automation.Language.NullString]::Value`; a fresh install
  alone cannot detect this bug because it takes the move path instead.

- **Stage in the destination directory and set executable mode before the
  rename.** A temporary download directory may be on another filesystem, and
  chmod after replacement exposes an unusable executable. The macOS fixture
  inspects mode 0755 and matching parent directories at the actual `mv`
  boundary. Reject directory targets explicitly: ordinary `mv` can silently
  move the staged file inside one instead of replacing the intended path.

- **A running image is not equivalent to a replacement-denying lock.** Windows
  may permit atomic replacement while the original process continues using
  its old image. Preserve that process and print manual restart guidance.
  Test denied replacement separately with an owned subprocess holding
  `FileShare.None`; it must leave the old bytes and holder intact. Do not add
  process discovery, termination, or bridge startup to installation.

- **Keep the vet exception local to the Windows backend.** Its documented
  `GlobalLock`/`HGLOBAL` uintptr-to-pointer conversions trigger `unsafeptr`.
  `scripts/vet.sh` retains full analysis for other packages and disables only
  that analyzer for `internal/backend/windows`. Do not broaden the exception
  to hide unrelated diagnostics.

## Verification Boundaries

Native installer fixtures use an isolated destination, real localhost HTTP,
and captured original URLs to prove that latest resolves once and pins both
downloads. Interrupted responses must fail without changing installed bytes.
Windows update fixtures need two valid, distinct executable builds; appending
bytes to a PE file can trigger host application controls. Fixtures own and
clean up only their HTTP server, ordinary test process, and lock holder; they
must never use `--bridge` or touch an existing installation or PATH.

`release.sh` executes the artifact matching its native macOS architecture;
the other artifacts are cross-builds. Native Windows CI separately executes
its built executable and installer fixture. Packaging checksums alone do not
establish native execution coverage for every architecture.
