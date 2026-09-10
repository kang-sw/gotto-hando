param([string]$Version = "latest", [string]$Repository = "kang-sw/gotto-hando")
$ErrorActionPreference = "Stop"
$InstallDir = Join-Path ([Environment]::GetFolderPath("UserProfile")) ".local/bin"
if ($Version -eq "latest") { $Version = (Invoke-RestMethod "https://api.github.com/repos/$Repository/releases/latest").tag_name }
$Version = $Version.TrimStart("v")
if ($Version -notmatch '^[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$') { throw "Invalid release version: $Version" }
$arch = if ([Environment]::Is64BitOperatingSystem) { "amd64" } else { throw "64-bit Windows is required" }
$asset = "gotto-hando-windows-$arch.exe"
$base = "https://github.com/$Repository/releases/download/v$Version"
$tmp = Join-Path ([IO.Path]::GetTempPath()) ("gotto-hando-" + [guid]::NewGuid())
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
  Invoke-WebRequest "$base/$asset" -OutFile "$tmp\$asset"
  Invoke-WebRequest "$base/SHA256SUMS" -OutFile "$tmp\SHA256SUMS"
  $expected = ((Get-Content "$tmp\SHA256SUMS" | Where-Object { $_ -match "  $([regex]::Escape($asset))$" }) -split '\s+')[0]
  if (-not $expected) { throw "Checksum entry missing for $asset" }
  $actual = (Get-FileHash "$tmp\$asset" -Algorithm SHA256).Hash.ToLowerInvariant()
  if ($actual -ne $expected.ToLowerInvariant()) { throw "Checksum mismatch for $asset" }
  New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
  $target = Join-Path $InstallDir "gotto-hando.exe"
  try { Move-Item -Force "$tmp\$asset" $target -ErrorAction Stop } catch { throw "Could not replace $target (it may be locked); original was left unchanged" }
  Write-Host "Installed gotto-hando $Version at $target"
  if (-not (($env:Path -split ';') -contains $InstallDir)) { Write-Warning "$InstallDir is not on PATH; add it in your profile (PATH unchanged)" }
} finally { Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue }
