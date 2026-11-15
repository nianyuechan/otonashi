[CmdletBinding()]
param(
    [switch]$Force
)

$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$sourceDirectory = Join-Path $PSScriptRoot 'hooks'
$targetDirectory = Join-Path $projectRoot '.git\hooks'

if (-not (Test-Path -LiteralPath $sourceDirectory -PathType Container)) {
    throw "找不到钩子源目录：$sourceDirectory"
}
if (-not (Test-Path -LiteralPath $targetDirectory -PathType Container)) {
    throw "找不到 .git\hooks：$targetDirectory（请在 Git 工作副本内运行）"
}

$hooks = @(Get-ChildItem -LiteralPath $sourceDirectory -File)
if ($hooks.Count -eq 0) {
    throw "钩子源目录为空：$sourceDirectory"
}

# 先整体检查冲突，再复制，避免装到一半才失败。
foreach ($hook in $hooks) {
    $target = Join-Path $targetDirectory $hook.Name
    if ((Test-Path -LiteralPath $target) -and -not $Force) {
        throw "钩子已存在：$target。确认要覆盖请加 -Force。"
    }
}
foreach ($hook in $hooks) {
    $target = Join-Path $targetDirectory $hook.Name
    Copy-Item -LiteralPath $hook.FullName -Destination $target -Force
    Write-Host "已安装 $($hook.Name) -> $target"
}

Write-Host ''
Write-Host 'pre-commit：只检查本次暂存的 .go 文件格式（毫秒级），不构建、不跑测试。'
Write-Host 'pre-push  ：运行 scripts/Test-Go.ps1 -Level Fast（gofmt + build + vet，不跑 go test），约 30-60s。'
Write-Host '收口请运行 scripts/Verify-All.ps1 -Level Full（日志写入 bench/env/results）。'
Write-Host '临时跳过推送门禁：YZGO_SKIP_GATE=1 git push（会打印提示）。'
Write-Host '卸载：删除 .git\hooks\pre-commit 与 .git\hooks\pre-push。'
