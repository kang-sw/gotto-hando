param(
  [Parameter(Mandatory=$true)][string]$BinaryPath,
  [Parameter(Mandatory=$true)][string]$UpdateBinaryPath,
  [string]$PythonPath = 'python'
)
$ErrorActionPreference = 'Stop'
$pathBefore = $env:Path
$oldInstallDir = $env:GOTTO_HANDO_INSTALL_DIR
$oldBase = $env:GOTTO_HANDO_BASE_URL
$root = Join-Path ([IO.Path]::GetTempPath()) ('gotto-hando-test-' + [guid]::NewGuid())
$installDir = Join-Path $root "install dir's\.local\bin"
$asset = 'gotto-hando-windows-amd64.exe'
$oldVersion = '1.2.2'
$newVersion = '1.2.3'
$binaryFullPath = (Resolve-Path -LiteralPath $BinaryPath).Path
$runtimeVersion = (& $binaryFullPath --version).Trim()
$releases = Join-Path $root 'github\kang-sw\gotto-hando\releases\download'
$api = Join-Path $root 'api\repos\kang-sw\gotto-hando\releases'
$installer = Join-Path $PSScriptRoot 'install.ps1'
$requests = New-Object 'System.Collections.Generic.List[string]'
$server = $null
$lockedProcess = $null
function Assert-True($condition, [string]$message) { if (-not $condition) { throw $message } }
function Hash([string]$path) { (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash }
# Keep real HTTP transport while redirecting the exact production URLs locally.
function Invoke-RestMethod([string]$Uri) {
  $requests.Add($Uri)
  Assert-True ($Uri.StartsWith('https://api.github.com/')) 'unexpected API URL'
  Microsoft.PowerShell.Utility\Invoke-RestMethod ($Uri.Replace('https://api.github.com', "$origin/api"))
}
function Invoke-WebRequest([string]$Uri, [switch]$UseBasicParsing, [string]$OutFile) {
  $requests.Add($Uri)
  Assert-True ($Uri.StartsWith('https://github.com/')) 'unexpected asset URL'
  Microsoft.PowerShell.Utility\Invoke-WebRequest ($Uri.Replace('https://github.com', "$origin/github")) -UseBasicParsing -OutFile $OutFile
}
function Install([string]$version = '') {
  $arguments = @(); if ($version) { $arguments += $version }
  $output = (& $installer @arguments 3>&1 6>&1 | Out-String -Width 4096)
  Assert-True ($env:Path -eq $pathBefore) 'PATH changed'
  return $output
}
function Assert-Preserved {
  Assert-True ((Hash $target) -eq $oldHash) 'failed install changed existing bytes'
  Assert-True ($env:Path -eq $pathBefore) 'PATH changed'
  Assert-True (@(Get-ChildItem -LiteralPath $installDir -Filter '.gotto-hando-*').Count -eq 0) 'staged file leaked'
}
function Expect-Failure {
  $failed = $false
  try { Install $newVersion | Out-Null } catch { $failed = $true }
  Assert-True $failed 'unexpected installation success'
  Assert-Preserved
}
try {
  foreach ($version in @($oldVersion, $newVersion)) {
    $dir = Join-Path $releases "v$version"
    New-Item -ItemType Directory -Force $dir | Out-Null
    Copy-Item -LiteralPath $BinaryPath -Destination (Join-Path $dir $asset)
  }
  $current = Join-Path $releases "v$newVersion"
  Copy-Item -LiteralPath $UpdateBinaryPath -Destination (Join-Path $current $asset) -Force
  Assert-True ((Hash $BinaryPath) -ne (Hash $UpdateBinaryPath)) 'update fixture must have distinct executable bytes'
  foreach ($version in @($oldVersion, $newVersion)) {
    $dir = Join-Path $releases "v$version"
    ((Hash (Join-Path $dir $asset)) + "  $asset") | Set-Content (Join-Path $dir 'SHA256SUMS')
  }
  New-Item -ItemType Directory -Force $api | Out-Null
  ('{"tag_name":"v' + $newVersion + '"}') | Set-Content (Join-Path $api 'latest')
  $portFile = Join-Path $root 'port'
  $serverScript = Join-Path $PSScriptRoot 'testdata\install_server.py'
  $server = Start-Process $PythonPath -ArgumentList "`"$serverScript`" `"$root`" `"$portFile`"" -PassThru -RedirectStandardOutput (Join-Path $root 'server.log') -RedirectStandardError (Join-Path $root 'server-error.log')
  for ($i=0; $i -lt 100; $i++) {
    Start-Sleep -Milliseconds 100
    if (Test-Path $portFile) { break }
    Assert-True (-not $server.HasExited) 'HTTP fixture exited before readiness'
  }
  Assert-True (Test-Path $portFile) 'HTTP fixture did not become ready'
  $origin = 'http://127.0.0.1:' + (Get-Content $portFile)
  $env:GOTTO_HANDO_INSTALL_DIR = $installDir
  Remove-Item Env:GOTTO_HANDO_BASE_URL -ErrorAction SilentlyContinue
  $output = Install "v$oldVersion"
  $target = Join-Path $installDir 'gotto-hando.exe'
  Assert-True ((Hash $target) -eq (Hash (Join-Path $releases "v$oldVersion\$asset"))) 'fresh installation bytes mismatch'
  Assert-True ((& $target --version).Trim() -eq $runtimeVersion) 'fresh executable cannot run'
  Assert-True ($output.Contains('PATH unchanged')) 'PATH warning missing'
  Assert-True ($output.Contains($installDir.Replace("'", "''") + ";'")) 'PATH guidance does not quote actual destination'
  Install $newVersion | Out-Null
  $oldHash = Hash $target
  Assert-True ($oldHash -eq (Hash (Join-Path $current $asset))) 'successful update bytes mismatch'
  foreach ($mode in @('', 'latest')) {
    $requests.Clear()
    Install $mode | Out-Null
    Assert-True (@($requests | Where-Object { $_ -eq 'https://api.github.com/repos/kang-sw/gotto-hando/releases/latest' }).Count -eq 1) 'latest did not resolve exactly once'
    Assert-True (@($requests | Where-Object { $_ -eq "https://github.com/kang-sw/gotto-hando/releases/download/v$newVersion/$asset" }).Count -eq 1) 'binary URL not pinned to resolved version'
    Assert-True (@($requests | Where-Object { $_ -eq "https://github.com/kang-sw/gotto-hando/releases/download/v$newVersion/SHA256SUMS" }).Count -eq 1) 'manifest URL not pinned to resolved version'
    Assert-Preserved
  }
  $oldBytes = [IO.File]::ReadAllBytes((Join-Path $current $asset))
  $manifest = Get-Content (Join-Path $current 'SHA256SUMS') -Raw
  Add-Content (Join-Path $current $asset) 'tampered'; Expect-Failure
  [IO.File]::WriteAllBytes((Join-Path $current $asset), $oldBytes)
  ($manifest + $manifest) | Set-Content (Join-Path $current 'SHA256SUMS'); Expect-Failure
  ('0' * 64 + '  wrong-asset') | Set-Content (Join-Path $current 'SHA256SUMS'); Expect-Failure
  Remove-Item (Join-Path $current 'SHA256SUMS'); Expect-Failure
  $manifest | Set-Content (Join-Path $current 'SHA256SUMS')
  Move-Item (Join-Path $current $asset) (Join-Path $root 'asset'); Expect-Failure
  Move-Item (Join-Path $root 'asset') (Join-Path $current $asset)
  $asset | Set-Content (Join-Path $root 'truncate'); Expect-Failure
  Remove-Item (Join-Path $root 'truncate')
  # A running image may allow atomic replacement. Never terminate it to update.
  $lockedProcess = Start-Process $target -ArgumentList "local --out `"$root\run`" sleep[]60000" -PassThru
  Start-Sleep -Milliseconds 500
  Assert-True (-not $lockedProcess.HasExited) 'running fixture exited too early'
  $replaced = $true
  try { Install $oldVersion | Out-Null } catch { $replaced = $false }
  Assert-True (-not $lockedProcess.HasExited) 'installer killed the running fixture'
  if ($replaced) {
    Assert-True ((Hash $target) -eq (Hash $BinaryPath)) 'atomic replacement bytes mismatch'
    Assert-True ((& $target --version).Trim() -eq $runtimeVersion) 'replacement executable cannot run'
  } else { Assert-Preserved }
  Stop-Process -Id $lockedProcess.Id -Force
  $lockedProcess.WaitForExit()
  $lockedProcess = $null
  Install $newVersion | Out-Null
  Assert-Preserved

  # A separate owned process holds a true exclusive lock that denies replacement.
  $ready = Join-Path $root 'lock-ready'
  $holderCode = @'
$stream = [IO.File]::Open('__TARGET__', [IO.FileMode]::Open, [IO.FileAccess]::ReadWrite, [IO.FileShare]::None)
try { [IO.File]::WriteAllText('__READY__', 'ready'); Start-Sleep -Seconds 60 } finally { $stream.Dispose() }
'@
  $holderCode = $holderCode.Replace('__TARGET__', $target.Replace("'", "''")).Replace('__READY__', $ready.Replace("'", "''"))
  $encoded = [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($holderCode))
  $lockedProcess = Start-Process (Join-Path $PSHOME 'powershell.exe') -ArgumentList "-NoProfile -EncodedCommand $encoded" -PassThru
  for ($i=0; $i -lt 100; $i++) {
    Start-Sleep -Milliseconds 100
    if (Test-Path $ready) { break }
    Assert-True (-not $lockedProcess.HasExited) 'exclusive-lock holder exited early'
  }
  Assert-True (Test-Path $ready) 'exclusive-lock holder not ready'
  $failed = $false
  try { Install $newVersion | Out-Null } catch { $failed = $true }
  Assert-True $failed 'exclusively locked replacement was accepted'
  Assert-True (-not $lockedProcess.HasExited) 'installer killed the lock holder'
  Stop-Process -Id $lockedProcess.Id -Force
  $lockedProcess.WaitForExit()
  $lockedProcess = $null
  Assert-Preserved
  Remove-Item $target
  New-Item -ItemType Directory $target | Out-Null
  'keep' | Set-Content (Join-Path $target 'sentinel')
  $failed = $false; try { Install $newVersion | Out-Null } catch { $failed = $true }
  Assert-True $failed 'target directory was accepted'
  Assert-True ((Get-Content (Join-Path $target 'sentinel')) -eq 'keep') 'target directory contents changed'
  Write-Host 'Windows installer fixtures passed: install/update/latest/checksum/duplicate/missing/interrupted/locked/directory/PATH'
} finally {
  if ($lockedProcess -and -not $lockedProcess.HasExited) { Stop-Process -Id $lockedProcess.Id -Force }
  if ($server -and -not $server.HasExited) { Stop-Process -Id $server.Id -Force }
  $env:GOTTO_HANDO_INSTALL_DIR = $oldInstallDir
  $env:GOTTO_HANDO_BASE_URL = $oldBase
  Remove-Item -Recurse -Force $root -ErrorAction SilentlyContinue
}
