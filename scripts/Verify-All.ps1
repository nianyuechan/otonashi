[CmdletBinding()]
param(
    [string]$GoExecutable,
    [string]$CacheRoot,
    [switch]$Race,
    [int]$Parallelism = 1,
    [string]$LogDirectory,
    [ValidateSet('Fast', 'Full')]
    [string]$Level = 'Full'
)

$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
if (-not $LogDirectory) {
    $LogDirectory = Join-Path $projectRoot 'bench\env\results'
}

$head = 'unknown'
$gitCommand = $null
$git = Get-Command git.exe -ErrorAction SilentlyContinue
if ($git) {
    $gitCommand = $git.Source
}
else {
    $gitCandidate = 'C:\Program Files\Git\cmd\git.exe'
    if (Test-Path -LiteralPath $gitCandidate -PathType Leaf) { $gitCommand = $gitCandidate }
}
if ($gitCommand) {
    $head = & $gitCommand -C $projectRoot rev-parse --short HEAD 2>$null
    if ($LASTEXITCODE -ne 0 -or -not $head) { $head = 'unknown' }
}

$stamp = Get-Date -Format 'yyyyMMdd-HHmmss'
New-Item -ItemType Directory -Path $LogDirectory -Force | Out-Null
$logPath = Join-Path $LogDirectory "verify-$head-$stamp-$($Level.ToLower()).log"
$testArguments = @{}
if ($GoExecutable) { $testArguments['GoExecutable'] = $GoExecutable }
if ($CacheRoot) { $testArguments['CacheRoot'] = $CacheRoot }
if ($Race) { $testArguments['Race'] = $true }
$testArguments['Parallelism'] = $Parallelism
$testArguments['Level'] = $Level

Write-Host "统一验证 Level=$Level HEAD=$head 时间=$stamp 日志=$logPath"
$failed = $false
try {
    & (Join-Path $PSScriptRoot 'Test-Go.ps1') @testArguments 2>&1 | Tee-Object -FilePath $logPath
    if ($LASTEXITCODE -ne 0) { $failed = $true }
}
catch {
    Write-Host $_
    $failed = $true
}
if ($failed) {
    Write-Host "统一验证失败（Level=$Level）；日志已写入 $logPath"
    exit 1
}
Write-Host "统一验证通过（Level=$Level）；日志已写入 $logPath"
exit 0
