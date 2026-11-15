[CmdletBinding()]
param(
    [string]$GoExecutable,
    [string]$CacheRoot,
    [switch]$Race,
    [int]$Parallelism = 1,
    [ValidateSet('Fast', 'Full')]
    [string]$Level = 'Full'
)

$ErrorActionPreference = 'Stop'
if ($Parallelism -lt 1) {
    throw 'Parallelism 必须大于 0。'
}
if ($Level -eq 'Fast' -and $Race) {
    throw 'Fast 门禁不运行 go test，因此不支持 -Race；需要竞态检测请用 -Level Full -Race。'
}
$projectRoot = Split-Path -Parent $PSScriptRoot
$workspaceParent = Split-Path -Parent $projectRoot
# Go 工具链解析顺序：显式参数 > 环境变量 > 本机工作区自带 > PATH。
# 前三档保证本机行为不变；最后一档让 CI（如 GitHub Actions 的 windows-latest）
# 在没有 G 盘、没有 tools\go 的环境里也能直接跑。
if (-not $GoExecutable) { $GoExecutable = $env:YZGO_GO_BIN }
if (-not $GoExecutable) {
    $bundledGo = Join-Path $workspaceParent 'tools\go\bin\go.exe'
    if (Test-Path -LiteralPath $bundledGo -PathType Leaf) { $GoExecutable = $bundledGo }
}
if (-not $GoExecutable) {
    $goOnPath = Get-Command go.exe -ErrorAction SilentlyContinue
    if ($goOnPath) { $GoExecutable = $goOnPath.Source }
}
if (-not $GoExecutable) {
    throw '找不到 Go 工具链：请用 -GoExecutable 或环境变量 YZGO_GO_BIN 指定 go.exe，或确保 go 在 PATH 中。'
}
if (-not (Test-Path -LiteralPath $GoExecutable -PathType Leaf)) {
    throw "指定的 Go 工具链不存在：$GoExecutable"
}
# 缓存根解析顺序：显式参数 > 环境变量 > CI 临时目录 > 本机工作区父目录。
# 本机仍落在 G 盘工作区；CI 落到 RUNNER_TEMP，避免写只读或随机路径。
if (-not $CacheRoot -and $env:YZGO_CACHE_ROOT) { $CacheRoot = $env:YZGO_CACHE_ROOT }
if (-not $CacheRoot -and $env:RUNNER_TEMP) { $CacheRoot = $env:RUNNER_TEMP }
if (-not $CacheRoot) { $CacheRoot = $workspaceParent }
$gofmt = Join-Path (Split-Path -Parent $GoExecutable) 'gofmt.exe'
if (-not (Test-Path -LiteralPath $gofmt -PathType Leaf)) {
    throw 'Go 工具链目录中缺少 gofmt.exe。'
}

$cacheEnvironment = @{
    GOPATH = Join-Path $CacheRoot 'gopath'
    GOCACHE = Join-Path $CacheRoot 'gocache'
    GOMODCACHE = Join-Path $CacheRoot 'gopath\pkg\mod'
    GOTMPDIR = Join-Path $CacheRoot 'gotmp'
    TEMP = Join-Path $CacheRoot 'gotmp'
    TMP = Join-Path $CacheRoot 'gotmp'
}
$previousEnvironment = @{}
foreach ($name in $cacheEnvironment.Keys) {
    $previousEnvironment[$name] = [Environment]::GetEnvironmentVariable($name, 'Process')
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
$verificationStartedAt = Get-Date
Write-Host "验证 Level=$Level HEAD=$head 开始时间=$($verificationStartedAt.ToString('s'))"
if ($Level -eq 'Fast') {
    Write-Host 'Fast 门禁：gofmt + build + vet（不运行 go test；收口请用 -Level Full）'
}

function Invoke-GoCheck {
    param([string[]]$Arguments)
    & $GoExecutable @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "Go 验证失败：$($Arguments -join ' ')，退出码 $LASTEXITCODE"
    }
}

try {
    foreach ($name in $cacheEnvironment.Keys) {
        New-Item -ItemType Directory -Path $cacheEnvironment[$name] -Force | Out-Null
        [Environment]::SetEnvironmentVariable($name, $cacheEnvironment[$name], 'Process')
    }
    foreach ($module in @('go-yunzai', 'go-render', 'bench', 'bench/tools/sqlite-diag')) {
        Write-Host "正在验证 $module"
        Push-Location (Join-Path $projectRoot $module)
        try {
            $sourceFiles = @(Get-ChildItem -Recurse -File -Filter '*.go' | ForEach-Object { $_.FullName })
            $unformatted = @(& $gofmt -l @sourceFiles)
            if ($LASTEXITCODE -ne 0) {
                throw '格式检查执行失败。'
            }
            if ($unformatted.Count -gt 0) {
                throw "存在未格式化的 Go 文件：$($unformatted -join ', ')"
            }
            # 包级期限 120s：本机存在 ~30s 机器级 IO 停滞，60s 对 app/storage 等
            # 已知 ~30-40s 的并行测试包余量不足；应用预算与测试自身期限不变。
            if ($Level -eq 'Full') {
                Invoke-GoCheck -Arguments @('test', '-p', "$Parallelism", './...', '-count=1', '-timeout=120s')
            }
            Invoke-GoCheck -Arguments @('build', '-p', "$Parallelism", './...')
            Invoke-GoCheck -Arguments @('vet', '-p', "$Parallelism", './...')
            if ($Race) {
                Invoke-GoCheck -Arguments @('test', '-race', '-p', "$Parallelism", './...', '-count=1', '-timeout=120s')
            }
        }
        finally {
            Pop-Location
        }
    }
    $verificationFinishedAt = Get-Date
    Write-Host "$Level 门禁通过（go-yunzai、go-render、bench、bench/tools/sqlite-diag）。HEAD=$head 用时=$([math]::Round(($verificationFinishedAt - $verificationStartedAt).TotalSeconds, 2))s"
}
finally {
    foreach ($name in $previousEnvironment.Keys) {
        [Environment]::SetEnvironmentVariable($name, $previousEnvironment[$name], 'Process')
    }
}
