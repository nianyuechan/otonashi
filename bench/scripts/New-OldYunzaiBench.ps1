<#
.SYNOPSIS
创建旧版 TRSS-Yunzai 的基准运行副本。不修改原始安装。

.DESCRIPTION
在 bench/env/old-yunzai 下组装最小可启动副本：
- node_modules 与 renderers 用 junction 指向原始安装，避免复制数 GB 数据；
- lib、config、app.js、package.json 复制；
- plugins 只保留 adapter 与 bench 基准插件；
- bot.yaml 关闭 file_watch，保证基准期间无文件监听开销。
#>
[CmdletBinding()]
param(
    # 原始旧版安装目录
    [string]$Source = 'G:\ALL WORK\音无\音无\mysy2\mysy2\home\SERVER2\TRSS_AllBot\TRSS-Yunzai',
    # 基准副本目标目录
    [string]$Destination = ''
)

$ErrorActionPreference = 'Stop'
$benchRoot = Split-Path -Parent $PSScriptRoot
if (-not $Destination) {
    $Destination = Join-Path $benchRoot 'env\old-yunzai'
}
if (-not (Test-Path -LiteralPath (Join-Path $Source 'app.js'))) {
    throw "原始旧版目录无效：$Source"
}

function New-Junction {
    param([string]$Link, [string]$Target)
    if (Test-Path -LiteralPath $Link) {
        $item = Get-Item -LiteralPath $Link -Force
        if ($item.LinkType -eq 'Junction') {
            if ($item.Target -like "*$Target*") { return }
            Remove-Item -LiteralPath $Link -Force -Recurse
        } else {
            throw "$Link 已存在且不是 junction，拒绝覆盖。"
        }
    }
    $null = cmd /c mklink /J "`"$Link`"" "`"$Target`""
    if ($LASTEXITCODE -ne 0) { throw "创建 junction 失败：$Link -> $Target" }
}

New-Item -ItemType Directory -Path $Destination -Force | Out-Null

# 共享依赖：junction 不产生数据拷贝
New-Junction -Link (Join-Path $Destination 'node_modules') -Target (Join-Path $Source 'node_modules')
New-Junction -Link (Join-Path $Destination 'renderers') -Target (Join-Path $Source 'renderers')

# 顶层文件
foreach ($file in @('app.js', 'package.json', 'pnpm-workspace.yaml', '.npmrc')) {
    $from = Join-Path $Source $file
    if (Test-Path -LiteralPath $from) {
        Copy-Item -LiteralPath $from -Destination (Join-Path $Destination $file) -Force
    }
}

# 核心库与配置
Copy-Item -LiteralPath (Join-Path $Source 'lib') -Destination (Join-Path $Destination 'lib') -Recurse -Force
Copy-Item -LiteralPath (Join-Path $Source 'config') -Destination (Join-Path $Destination 'config') -Recurse -Force

# plugins：只保留 adapter 与 bench 插件
$pluginsDir = Join-Path $Destination 'plugins'
New-Item -ItemType Directory -Path $pluginsDir -Force | Out-Null
Copy-Item -LiteralPath (Join-Path $Source 'plugins\adapter') -Destination (Join-Path $pluginsDir 'adapter') -Recurse -Force

$benchSource = Join-Path $benchRoot 'old-plugin\index.js'
$benchDir = Join-Path $pluginsDir 'bench'
New-Item -ItemType Directory -Path $benchDir -Force | Out-Null
Copy-Item -LiteralPath $benchSource -Destination (Join-Path $benchDir 'index.js') -Force

# 运行期目录
foreach ($dir in @('data', 'logs', 'temp')) {
    New-Item -ItemType Directory -Path (Join-Path $Destination $dir) -Force | Out-Null
}

# 关闭文件监听，避免基准期间的 chokidar 开销与不确定性
$botYaml = Join-Path $Destination 'config\config\bot.yaml'
(Get-Content -LiteralPath $botYaml -Raw) -replace 'file_watch: true', 'file_watch: false' |
    Set-Content -LiteralPath $botYaml -NoNewline

Write-Host "旧版基准副本就绪：$Destination"
