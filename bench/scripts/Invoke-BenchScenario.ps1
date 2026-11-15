<#
.SYNOPSIS
运行一次阶段零基准场景：自动启动被测机器人与负载器，采集端到端延迟与资源占用。

.DESCRIPTION
- Variant go：启动 yunzai-bench（正向 WS 客户端），负载器以 server 模式等待连入。
- Variant old：确保 mini-redis 存活，启动旧版基准副本（反向 WS 服务端），负载器以
  client 模式连入。旧版需先运行 scripts/New-OldYunzaiBench.ps1 与
  scripts/repair-node-modules.mjs 完成环境准备。
每次运行都会重新启动被测机器人，保证冷启动状态一致。
#>
[CmdletBinding()]
param(
    [ValidateSet('go', 'old')][string]$Variant = 'go',
    # render：固定 #render 页面（仅主人），驱动阶段零渲染链路基线；渲染耗时
    # 单独报告，不混入消息核心延迟分位数。配置由仓库模板复制（见启动分支）。
    [ValidateSet('reply', 'scan', 'render')][string]$Workload = 'reply',
    [ValidateSet('steady', 'burst')][string]$Scenario = 'steady',
    [double]$Rate = 20,
    [int]$DurationSec = 60,
    [int]$Count = 500,
    [ValidateSet('private', 'group')][string]$MessageType = 'private',
    [int]$GroupCount = 1,
    [int]$UserCount = 1,
    [string]$OutFile = '',
    # 追加到被测 Go bot 的命令行参数（如 -store、-config config-render.yaml）；
    # 重复的 -config 以最后一个为准（flag 包语义）。
    [string[]]$BotExtraArgs = @(),
    # 覆盖被测 Go bot 可执行文件（默认 bench\env\go-yunzai\yunzai-bench.exe）。
    # Z3 渲染链路必须用 cmd/yunzai 构建的 yunzai.exe：yunzai-bench 入口硬编码
    # DisableBuiltinDebug=true，不注册内置调试插件，#render 规则不存在。
    [string]$BotExecutable = '',
    [switch]$KeepBot
)

$ErrorActionPreference = 'Stop'
$envRoot = Split-Path -Parent $PSScriptRoot                         # bench/（位于 音无/音无 下）
$envDir = Join-Path $envRoot 'env'
$workspaceParent = Split-Path -Parent (Split-Path -Parent $envRoot) # G:\ALL WORK\音无（Go 工具链所在层）
$goExe = Join-Path $workspaceParent 'tools\go\bin\go.exe'
$benchExe = Join-Path $envDir 'onebot-bench.exe'
if (-not $OutFile) {
    $OutFile = Join-Path $envDir ('results\{0}-{1}-{2}.json' -f $Variant, $Workload, $Scenario)
}
# bot 日志跟随结果文件名，避免固定 run.log/run.err 在每轮运行时覆盖历史证据。
$outDir = Split-Path -Parent $OutFile
if (-not $outDir) { $outDir = Join-Path $envDir 'results' }
$outLeaf = Split-Path -Leaf $OutFile
$botLog = Join-Path $outDir ('{0}.bot.log' -f $outLeaf)
$botErr = Join-Path $outDir ('{0}.bot.err' -f $outLeaf)
New-Item -ItemType Directory -Path $outDir -Force | Out-Null

# 构建负载器（幂等，源码变更后自动重建）
$benchSource = Join-Path $envRoot 'cmd\onebot-bench'
$exeTime = if (Test-Path $benchExe) { (Get-Item $benchExe).LastWriteTimeUtc } else { [DateTime]::MinValue }
$srcNewest = Get-ChildItem -Recurse -File (Join-Path $envRoot 'internal\loadgen'), (Join-Path $envRoot 'cmd') -Filter '*.go' |
    Sort-Object LastWriteTimeUtc -Descending | Select-Object -First 1
if (-not (Test-Path $benchExe) -or ($srcNewest -and $srcNewest.LastWriteTimeUtc -gt $exeTime)) {
    Write-Host '构建 onebot-bench...'
    $goEnv = @{ GOPATH = 'G:\ALL WORK\音无\gopath'; GOCACHE = 'G:\ALL WORK\音无\gocache'; GOTMPDIR = 'G:\ALL WORK\音无\gotmp'; TEMP = 'G:\ALL WORK\音无\gotmp'; TMP = 'G:\ALL WORK\音无\gotmp'; GOMODCACHE = 'G:\ALL WORK\音无\gopath\pkg\mod'; GOPROXY = 'https://goproxy.cn,direct'; GOSUMDB = 'off' }
    $saved = @{}
    foreach ($k in $goEnv.Keys) { $saved[$k] = [Environment]::GetEnvironmentVariable($k, 'Process'); [Environment]::SetEnvironmentVariable($k, $goEnv[$k], 'Process') }
    try {
        Push-Location $envRoot
        & $goExe build -o $benchExe ./cmd/onebot-bench
        if ($LASTEXITCODE -ne 0) { throw "onebot-bench 构建失败" }
        Pop-Location
    } finally {
        foreach ($k in $saved.Keys) { [Environment]::SetEnvironmentVariable($k, $saved[$k], 'Process') }
    }
}

$botProc = $null
$redisProc = $null
$benchProc = $null
function Stop-Tree([System.Diagnostics.Process]$Proc) {
    if ($Proc -and -not $Proc.HasExited) {
        try { & taskkill /PID $Proc.Id /T /F 2>$null | Out-Null } catch {}
        # taskkill 可能静默失败；残留进程会在下一轮抢走 3002 连接，
        # 导致监控采样盯住空闲进程、冷启动一致性被破坏。这里校验并兜底强杀。
        try {
            $Proc.Refresh()
            if (-not $Proc.HasExited) {
                Start-Sleep -Milliseconds 500
                $Proc.Refresh()
                if (-not $Proc.HasExited) {
                    Stop-Process -Id $Proc.Id -Force -ErrorAction SilentlyContinue
                }
            }
        } catch {}
    }
}
# 残留的同路径 bot 会与新一轮被测进程竞争 3002 连接（重连退避可能先连上），
# 使负载打到不受监控的旧进程上。只按 bench 环境内的精确可执行路径清理，
# 不做按进程名的宽匹配。
$candidateBotPaths = @((Join-Path $envDir 'go-yunzai\yunzai-bench.exe'))
$botPath = Join-Path $envDir 'go-yunzai\yunzai-bench.exe'
if ($BotExecutable) {
    $botPath = (Resolve-Path -LiteralPath $BotExecutable -ErrorAction Stop).Path
    if (-not (Test-Path -LiteralPath $botPath -PathType Leaf)) {
        throw "BotExecutable 不是文件: $botPath"
    }
    $candidateBotPaths += $botPath
}
# render 不能用默认 yunzai-bench，也不能依赖调用者记得传 render 配置：
# 默认入口硬编码 DisableBuiltinDebug=true，#render 规则不存在；而脚本只
# 校验 sent>0，会把 action_count=0 的零渲染轮误报为成功。
if ($Workload -eq 'render') {
    if ($Variant -ne 'go') {
        throw "Workload=render 只支持 Variant=go（旧版 Node bot 不提供 #render 链路）"
    }
    if (-not $BotExecutable) {
        throw "Workload=render 必须显式指定 -BotExecutable（应使用 cmd/yunzai 构建的 yunzai.exe；默认 yunzai-bench 禁用内置调试插件）"
    }
    $defaultBotPath = Join-Path $envDir 'go-yunzai\yunzai-bench.exe'
    if ([IO.Path]::GetFullPath($botPath) -eq [IO.Path]::GetFullPath($defaultBotPath)) {
        throw "Workload=render 不能使用默认 yunzai-bench.exe（#render 规则不存在）"
    }
    # 强制把模板配置放到最后一个 -config：Go flag 包以最后一个为准，
    # 即使调用者的 BotExtraArgs 里带了别的 -config，也不会退回默认配置。
    $BotExtraArgs = @($BotExtraArgs + @('-config', 'config-render.yaml'))
    Write-Host 'render: 已强制追加 -config config-render.yaml'
}
$strayBots = @(Get-Process yunzai-bench,yunzai -ErrorAction SilentlyContinue | Where-Object { $_.Path -in $candidateBotPaths })
if ($strayBots.Count -gt 0) {
    Write-Host ("发现 {0} 个残留 yunzai-bench 进程，先按精确路径清理: {1}" -f $strayBots.Count, (($strayBots | ForEach-Object Id) -join ','))
    foreach ($stray in $strayBots) {
        try { & taskkill /PID $stray.Id /T /F 2>$null | Out-Null } catch {}
        Stop-Process -Id $stray.Id -Force -ErrorAction SilentlyContinue
    }
    Start-Sleep -Milliseconds 800
}
try {
    if ($Variant -eq 'old') {
        # mini-redis 必须先于旧版启动（连接失败会触发旧版自启动逻辑并退出）
        $existing = Get-NetTCPConnection -LocalPort 6379 -State Listen -ErrorAction SilentlyContinue
        if (-not $existing) {
            $redisProc = Start-Process -FilePath (Join-Path $envDir 'mini-redis.exe') `
                -ArgumentList '-addr', '127.0.0.1:6379' `
                -WorkingDirectory $envDir -RedirectStandardOutput (Join-Path $envDir 'mini-redis.log') `
                -RedirectStandardError (Join-Path $envDir 'mini-redis.err') -PassThru -WindowStyle Hidden
            Start-Sleep -Milliseconds 800
        }
        $botDir = Join-Path $envDir 'old-yunzai'
        Remove-Item -LiteralPath (Join-Path $botDir 'run.log') -Force -ErrorAction SilentlyContinue
        $botProc = Start-Process -FilePath 'node' -ArgumentList 'app.js' `
            -WorkingDirectory $botDir -RedirectStandardOutput (Join-Path $botDir 'run.log') `
            -RedirectStandardError (Join-Path $botDir 'run.err') -PassThru -WindowStyle Hidden
        # 等待启动完成标记（Bot.run 结束时输出 ----^_^----）
        $deadline = (Get-Date).AddSeconds(60)
        do {
            Start-Sleep -Milliseconds 500
            $ready = Select-String -LiteralPath (Join-Path $botDir 'run.log') -Pattern '\-\-\-\-\^_\^\-\-\-\-' -Quiet -ErrorAction SilentlyContinue
        } while (-not $ready -and (Get-Date) -lt $deadline -and -not $botProc.HasExited)
        if (-not $ready) { throw "旧版启动超时或退出，见 $($botDir)\run.log" }
        Start-Sleep -Seconds 1
        Write-Host "旧版就绪 pid=$($botProc.Id)"
    } else {
        $botDir = Join-Path $envDir 'go-yunzai'
        if ($Workload -eq 'render') {
            # 渲染配置的唯一可复现来源是仓库内模板；运行时副本（被 .gitignore
            # 忽略的 bench/env/go-yunzai/config-render.yaml）每次运行都从这里
            # 重新复制，不以运行时副本为证据。
            $renderTemplate = Join-Path $envRoot 'configs\config-render.yaml'
            if (-not (Test-Path -LiteralPath $renderTemplate)) { throw "渲染配置模板不存在: $renderTemplate" }
            Copy-Item -LiteralPath $renderTemplate -Destination (Join-Path $botDir 'config-render.yaml') -Force
            Write-Host '已从仓库模板复制 config-render.yaml（Z3 渲染基线配置）'
        }
        Remove-Item -LiteralPath $botLog -Force -ErrorAction SilentlyContinue
        Remove-Item -LiteralPath $botErr -Force -ErrorAction SilentlyContinue
        $botProc = Start-Process -FilePath $botPath `
            -ArgumentList (@('-config', 'config.yaml') + $BotExtraArgs) `
            -WorkingDirectory $botDir -RedirectStandardOutput $botLog `
            -RedirectStandardError $botErr -PassThru -WindowStyle Hidden
        Start-Sleep -Seconds 2
        Write-Host "go-yunzai-bench 已启动 pid=$($botProc.Id)"
        Write-Host "bot 日志: $botLog / $botErr"
    }

    # 负载器参数。-out 使用相对路径：绝对路径含空格（G:\ALL WORK\...），
    # Start-Process 拼接后会截断后续 flag 解析。
    $benchOutRelative = 'results\' + (Split-Path -Leaf $OutFile)
    $benchArgs = @('-workload', $Workload, '-scenario', $Scenario, '-pid', $botProc.Id, '-out', $benchOutRelative, '-message-type', $MessageType, '-group-count', $GroupCount, '-user-count', $UserCount)
    if ($Scenario -eq 'steady') {
        $benchArgs += @('-rate', $Rate, '-duration', ('{0}s' -f $DurationSec))
    } else {
        $benchArgs += @('-count', $Count)
    }
    if ($Variant -eq 'go') {
        $benchArgs += @('-mode', 'server', '-listen', '127.0.0.1:3002')
    } else {
        $benchArgs += @('-mode', 'client', '-url', 'ws://127.0.0.1:2536/OneBotv11')
    }
    New-Item -ItemType Directory -Path (Split-Path -Parent $OutFile) -Force | Out-Null
    # 旧结果文件必须在启动负载器之前删除成功：如果删除静默失败而负载器写入也
    # 失败，后面的 Test-Path 会把旧文件当成新结果，产出伪造的"成功"。这里显式
    # 失败并断言文件确实消失。
    if (Test-Path -LiteralPath $OutFile) {
        Remove-Item -LiteralPath $OutFile -Force -ErrorAction Stop
        if (Test-Path -LiteralPath $OutFile) { throw "旧结果文件删除失败（可能被其他进程占用）: $OutFile" }
    }
    # 日志名跟随结果文件名：固定名（go-reply-steady.log）会把每次运行的日志
    # 覆盖到同一批文件上，曾经覆盖掉已跟踪的历史证据。
    $benchLog = Join-Path $envDir ('results\{0}.log' -f (Split-Path -Leaf $OutFile))
    New-Item -ItemType Directory -Path (Split-Path -Parent $benchLog) -Force | Out-Null
    Write-Host "负载器参数: $($benchArgs -join ' ')"
    # 本次运行时间窗起点：结果 JSON 的 finished_at 必须不早于它。
    $runStart = Get-Date
    $benchProc = Start-Process -FilePath $benchExe -ArgumentList ($benchArgs -join ' ') `
        -WorkingDirectory $envDir -RedirectStandardOutput $benchLog `
        -RedirectStandardError ($benchLog + '.err') -PassThru -WindowStyle Hidden
    # 轮询等待，避免 ExitCode 尚未就绪被读取为空。
    $deadline = (Get-Date).AddSeconds($DurationSec + 180)
    while (-not $benchProc.HasExited -and (Get-Date) -lt $deadline) { Start-Sleep -Milliseconds 500 }
    if (-not $benchProc.HasExited) { throw "负载器超时未退出" }
    Start-Sleep -Milliseconds 300
    $null = $benchProc.WaitForExit(2000)
    # PS 5.1 重定向场景下 ExitCode 可能为空；以结果 JSON 是否生成作为成功依据。
    if (-not (Test-Path -LiteralPath $OutFile)) {
        throw "负载器未产出结果文件，退出码 $($benchProc.ExitCode)，见 $benchLog.err"
    }

    $resultText = [System.IO.File]::ReadAllText($OutFile, [System.Text.Encoding]::UTF8)
    $result = $resultText | ConvertFrom-Json
    $s = $result.summary
    # 结果必须是本次运行产出：sent > 0 且 finished_at 在本次运行时间窗内。
    # 即使删除断言已通过，也要防止任何残留旧结果被误读为本次成功。
    if (-not $s -or -not $s.sent -or [int]$s.sent -le 0) {
        throw "结果文件缺少 summary.sent 或 sent<=0，不是有效结果: $OutFile"
    }
    if ($Workload -eq 'render') {
        # render 的结果 expected_replies=0，missing=0 是恒定值；必须单独
        # 确认机器人真的处理了动作，否则零动作轮会被 sent>0 误判为成功。
        if ($null -eq $s.action_count -or [int]$s.action_count -le 0) {
            throw "render 结果 action_count<=0，未触发任何渲染动作: $OutFile"
        }
        if ($null -eq $s.action_errors -or [int]$s.action_errors -ne 0) {
            throw ("render 结果 action_errors={0}，不是零错误动作轮: {1}" -f $s.action_errors, $OutFile)
        }
    }
    $finishedAt = [DateTimeOffset]::Parse($result.finished_at)
    if ($finishedAt.LocalDateTime -lt $runStart.AddSeconds(-2)) {
        throw ("结果 finished_at ({0}) 早于本次运行开始 ({1})，疑似旧结果文件: {2}" -f `
            $result.finished_at, $runStart.ToString('yyyy-MM-dd HH:mm:ss'), $OutFile)
    }
    # 双口径：actual injection（sent / 事件首尾 sent_ns 差）是容量判定口径；
    # reported eps 分母含 10s grace，系统性低于 actual，不能拿它判达标。
    # 新结果 JSON 自带 actual_injection_eps；旧结果没有该字段时按同一口径复算。
    $actual = $s.actual_injection_eps
    if ($null -eq $actual) {
        $events = @($result.events | Where-Object { $_.sent_ns -gt 0 })
        if ($events.Count -ge 2) {
            $firstNs = ($events | Measure-Object -Property sent_ns -Minimum).Minimum
            $lastNs = ($events | Measure-Object -Property sent_ns -Maximum).Maximum
            if ($lastNs -gt $firstNs) {
                $actual = $s.sent / (($lastNs - $firstNs) / 1e9)
            }
        }
    }
    Write-Host ('结果: sent={0} paired={1} missing={2} actual_injection={3:N1}/s reported_eps={4:N1} p50={5:N2}ms p95={6:N2}ms p99={7:N2}ms max={8:N2}ms cpu_avg={9:N1}% rss_max={10:N1}MB' -f `
        $s.sent, $s.paired_replies, $s.missing_replies, $actual, $s.throughput_eps, `
        $s.latency_p50_ms, $s.latency_p95_ms, $s.latency_p99_ms, $s.latency_max_ms, `
        $s.cpu_avg_percent, ($s.rss_max_bytes / 1MB))
    if ($Workload -eq 'render') {
        Write-Host ("render 动作确认: action_count={0} action_errors={1} text_replies={2}" -f `
            $s.action_count, $s.action_errors, $s.text_replies)
    }
    Write-Host "输出: $OutFile"
} finally {
    if (-not $KeepBot) { Stop-Tree $botProc }
    Stop-Tree $benchProc
    if ($redisProc -and -not $KeepBot) { Stop-Tree $redisProc }
}
