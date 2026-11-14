[CmdletBinding()]
param(
    [string]$GoExecutable,
    [string]$CacheRoot,
    [switch]$Race
)

$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$workspaceParent = Split-Path -Parent $projectRoot
if (-not $GoExecutable) {
    $GoExecutable = Join-Path $workspaceParent 'tools\go\bin\go.exe'
}
if (-not (Test-Path -LiteralPath $GoExecutable -PathType Leaf)) {
    throw '找不到 Go 工具链，请使用 -GoExecutable 指定 go.exe。'
}
if (-not $CacheRoot) {
    $CacheRoot = $workspaceParent
}
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
    foreach ($module in @('go-yunzai', 'go-render', 'bench')) {
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
            Invoke-GoCheck -Arguments @('test', './...', '-count=1', '-timeout=60s')
            Invoke-GoCheck -Arguments @('build', './...')
            Invoke-GoCheck -Arguments @('vet', './...')
            if ($Race) {
                Invoke-GoCheck -Arguments @('test', '-race', './...', '-count=1', '-timeout=120s')
            }
        }
        finally {
            Pop-Location
        }
    }
    Write-Host '三个 Go 模块全部验证通过。'
}
finally {
    foreach ($name in $previousEnvironment.Keys) {
        [Environment]::SetEnvironmentVariable($name, $previousEnvironment[$name], 'Process')
    }
}
