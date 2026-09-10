param([Parameter(Mandatory=$true)][string]$BinaryPath)
$ErrorActionPreference = 'Stop'
$root = Join-Path ([IO.Path]::GetTempPath()) ("gotto-hando-test-" + [guid]::NewGuid())
$assetDir = Join-Path $root 'v0.1.0'
$installDir = Join-Path $root 'install\.local\bin'
New-Item -ItemType Directory -Force $assetDir | Out-Null
Copy-Item $BinaryPath (Join-Path $assetDir 'gotto-hando-windows-amd64.exe')
$hash = (Get-FileHash (Join-Path $assetDir 'gotto-hando-windows-amd64.exe') -Algorithm SHA256).Hash.ToLowerInvariant()
"$hash  gotto-hando-windows-amd64.exe" | Set-Content (Join-Path $assetDir 'SHA256SUMS') -NoNewline
$portFile = Join-Path $root 'port'
$server = Start-Process python -ArgumentList "-m http.server 0 --bind 127.0.0.1 --directory `"$root`"" -PassThru -RedirectStandardError (Join-Path $root 'err') -RedirectStandardOutput (Join-Path $root 'out')
try {
  for ($i=0; $i -lt 50; $i++) {
    Start-Sleep -Milliseconds 100
    $line = Get-Content (Join-Path $root 'err') -ErrorAction SilentlyContinue | Select-String 'port ([0-9]+)'
    if ($line) { $port = [int]$line.Matches[0].Groups[1].Value; break }
  }
  if (-not $port) { throw 'local HTTP server did not start' }
  $env:GOTTO_HANDO_BASE_URL = "http://127.0.0.1:$port/v0.1.0"
  $env:GOTTO_HANDO_INSTALL_DIR = $installDir
  & $PSScriptRoot\install.ps1 0.1.0
  $target = Join-Path $installDir 'gotto-hando.exe'
  if ((& $target --version) -ne '0.1.0') { throw 'installed binary version mismatch' }
  $old = [IO.File]::ReadAllBytes($target)
  Add-Content (Join-Path $assetDir 'gotto-hando-windows-amd64.exe') 'tampered'
  $failed = $false; try { & $PSScriptRoot\install.ps1 0.1.0 } catch { $failed = $true }
  if (-not $failed) { throw 'checksum mismatch was accepted' }
  if ([Convert]::ToBase64String($old) -ne [Convert]::ToBase64String([IO.File]::ReadAllBytes($target))) { throw 'checksum failure replaced existing binary' }
  Add-Content (Join-Path $assetDir 'SHA256SUMS') "`n$hash  gotto-hando-windows-amd64.exe"
  $failed = $false; try { & $PSScriptRoot\install.ps1 0.1.0 } catch { $failed = $true }
  if (-not $failed) { throw 'duplicate checksum was accepted' }
  Write-Host 'PowerShell installer fixture tests passed'
} finally {
  if ($server) { Stop-Process -Id $server.Id -Force -ErrorAction SilentlyContinue }
  Remove-Item Env:GOTTO_HANDO_BASE_URL -ErrorAction SilentlyContinue
  Remove-Item Env:GOTTO_HANDO_INSTALL_DIR -ErrorAction SilentlyContinue
}
