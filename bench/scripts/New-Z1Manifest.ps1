# New-Z1Manifest.ps1 - 从两档 Z1 capture 生成 manifest.json
# 用法：
#   & .\bench\scripts\New-Z1Manifest.ps1 -ProfileDir bench\env\profiles\current-2026-11-15 -Force
# 要求：capture-250.json 与 capture-600.json 存在，且 commit/SHA256/build_time 一致。
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$ProfileDir,
    [string]$OutputPath = '',
    [string]$PprofAddr = '127.0.0.1:6060',
    [string]$Workload = 'reply/steady/group, 100 groups, 20000 users',
    [switch]$Force
)

$ErrorActionPreference = 'Stop'
if (-not (Test-Path -LiteralPath $ProfileDir -PathType Container)) {
    throw "ProfileDir 不存在或不是目录: $ProfileDir"
}
if (-not $OutputPath) {
    $OutputPath = Join-Path $ProfileDir 'manifest.json'
}
if ((Test-Path -LiteralPath $OutputPath) -and -not $Force) {
    throw "manifest 已存在；如需重建请加 -Force: $OutputPath"
}

$rates = @(250, 600)
$capturePaths = @($rates | ForEach-Object {
    Join-Path $ProfileDir ('capture-{0}.json' -f $_)
})
foreach ($path in $capturePaths) {
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
        throw "缺少 capture 文件: $path"
    }
}

$jsonOptions = @{}
if ((Get-Command ConvertFrom-Json).Parameters.ContainsKey('DateKind')) {
    # PowerShell 7.5+ 默认会把 ISO 时间转成 DateTime；manifest 需要原样保留字符串。
    $jsonOptions['DateKind'] = 'String'
}
$captures = @($capturePaths | ForEach-Object {
    [System.IO.File]::ReadAllText($_, [System.Text.Encoding]::UTF8) | ConvertFrom-Json @jsonOptions
})

$identityFields = @('commit', 'bot_sha256', 'bench_sha256')
foreach ($field in $identityFields) {
    $first = [string]$captures[0].$field
    if ([string]::IsNullOrWhiteSpace($first)) {
        throw "capture-250 缺少 $field"
    }
    for ($i = 1; $i -lt $captures.Count; $i++) {
        $value = [string]$captures[$i].$field
        if ($value -ne $first) {
            throw ("capture 构建不一致: {0} 的 {1}={2}，capture-250={3}" -f `
                $rates[$i], $field, $value, $first)
        }
    }
}
foreach ($capture in $captures) {
    if ([string]::IsNullOrWhiteSpace([string]$capture.build_time)) {
        throw "capture 缺少 build_time: $($capture.out_file)"
    }
    if ($null -eq $capture.actual_injection_eps -or $null -eq $capture.missing_replies) {
        throw "capture 缺少 actual_injection_eps 或 missing_replies: $($capture.out_file)"
    }
}

$captureFields = @(
    'rate_requested',
    'actual_injection_eps',
    'reported_eps',
    'sent',
    'paired_replies',
    'missing_replies',
    'latency_p99_ms',
    'cpu_avg_percent',
    'rss_max_mb',
    'started_at',
    'finished_at',
    'duration_seconds',
    'profile_start_seconds',
    'cpu_window_seconds',
    'bot_pid',
    'build_time',
    'out_file'
)
$projected = @($captures | ForEach-Object {
    $source = $_
    $entry = [ordered]@{}
    foreach ($field in $captureFields) {
        $entry[$field] = $source.$field
    }
    [pscustomobject]$entry
})

$manifest = [ordered]@{
    commit      = [string]$captures[0].commit
    bot_sha256  = [string]$captures[0].bot_sha256
    bench_sha256 = [string]$captures[0].bench_sha256
    build_time  = [string]$captures[0].build_time
    pprof_addr  = $PprofAddr
    workload    = $Workload
    captures    = @($projected)
}
$json = $manifest | ConvertTo-Json -Depth 6
$parent = Split-Path -Parent $OutputPath
if ($parent -and -not (Test-Path -LiteralPath $parent -PathType Container)) {
    New-Item -ItemType Directory -Path $parent -Force | Out-Null
}
$tempPath = $OutputPath + '.tmp'
$utf8 = New-Object System.Text.UTF8Encoding($false)
[System.IO.File]::WriteAllText($tempPath, $json + [Environment]::NewLine, $utf8)
Move-Item -LiteralPath $tempPath -Destination $OutputPath -Force
Write-Output "manifest: $OutputPath"
