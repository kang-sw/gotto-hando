param([string]$Version = "latest", [string]$Repository = "kang-sw/gotto-hando")
$ErrorActionPreference = "Stop"
$InstallDir = if ($env:GOTTO_HANDO_INSTALL_DIR) { $env:GOTTO_HANDO_INSTALL_DIR } else { Join-Path ([Environment]::GetFolderPath("UserProfile")) ".local/bin" }
if ($Version -eq "latest") { $Version = (Invoke-RestMethod "https://api.github.com/repos/$Repository/releases/latest").tag_name }
$Version = $Version.TrimStart("v")
if ($Version -notmatch '^[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$') { throw "Invalid release version: $Version" }
$arch = if ($env:PROCESSOR_ARCHITECTURE -eq "AMD64") { "amd64" } else { throw "Windows amd64 is required" }
$asset = "gotto-hando-windows-$arch.exe"
$base = if ($env:GOTTO_HANDO_BASE_URL) { $env:GOTTO_HANDO_BASE_URL.TrimEnd('/') } else { "https://github.com/$Repository/releases/download/v$Version" }
$tmp = Join-Path ([IO.Path]::GetTempPath()) ("gotto-hando-" + [guid]::NewGuid())
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
  Invoke-WebRequest "$base/$asset" -UseBasicParsing -OutFile "$tmp\$asset"
  Invoke-WebRequest "$base/SHA256SUMS" -UseBasicParsing -OutFile "$tmp\SHA256SUMS"
  $rows = @(Get-Content "$tmp\SHA256SUMS" | Where-Object { ($_ -split '\s+', 3)[1] -eq $asset })
  if ($rows.Count -ne 1) { throw "Expected exactly one checksum entry for $asset" }
  $expected = ($rows[0] -split '\s+', 3)[0]
  $actual = (Get-FileHash "$tmp\$asset" -Algorithm SHA256).Hash.ToLowerInvariant()
  if ($actual -ne $expected.ToLowerInvariant()) { throw "Checksum mismatch for $asset" }
  New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
  $target = Join-Path $InstallDir "gotto-hando.exe"
  $stage = Join-Path $InstallDir (".gotto-hando-" + [guid]::NewGuid() + ".tmp")
  Copy-Item "$tmp\$asset" $stage
  try { if (Test-Path $target) { [IO.File]::Replace($stage, $target, $null) } else { Move-Item $stage $target -ErrorAction Stop } } catch { Remove-Item $stage -Force -ErrorAction SilentlyContinue; throw "Could not replace $target (it may be locked); original was left unchanged" }
  Write-Host "Installed gotto-hando $Version at $target"
  if (-not (($env:Path -split ';') -contains $InstallDir)) { Write-Warning ($InstallDir + " is not on PATH (PATH unchanged); run [Environment]::SetEnvironmentVariable('Path', '" + $InstallDir + ";' + [Environment]::GetEnvironmentVariable('Path','User'), 'User') if desired") }
} finally { Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue }
