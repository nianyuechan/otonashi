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
    [ValidateSet('reply', 'scan')][string]$Workload = 'reply',
    [ValidateSet('steady', 'burst')][string]$Scenario = 'steady',
    [double]$Rate = 20,
    [int]$DurationSec = 60,
    [int]$Count = 500,
    [string]$OutFile = '',
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
    }
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
        Remove-Item -LiteralPath (Join-Path $botDir 'run.log') -Force -ErrorAction SilentlyContinue
        $botProc = Start-Process -FilePath (Join-Path $botDir 'yunzai-bench.exe') `
            -ArgumentList '-config', 'config.yaml' `
            -WorkingDirectory $botDir -RedirectStandardOutput (Join-Path $botDir 'run.log') `
            -RedirectStandardError (Join-Path $botDir 'run.err') -PassThru -WindowStyle Hidden
        Start-Sleep -Seconds 2
        Write-Host "go-yunzai-bench 已启动 pid=$($botProc.Id)"
    }

    # 负载器参数。-out 使用相对路径：绝对路径含空格（G:\ALL WORK\...），
    # Start-Process 拼接后会截断后续 flag 解析。
    $benchOutRelative = 'results\' + (Split-Path -Leaf $OutFile)
    $benchArgs = @('-workload', $Workload, '-scenario', $Scenario, '-pid', $botProc.Id, '-out', $benchOutRelative)
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
    Remove-Item -LiteralPath $OutFile -Force -ErrorAction SilentlyContinue
    $benchLog = Join-Path $envDir ('results\{0}-{1}-{2}.log' -f $Variant, $Workload, $Scenario)
    New-Item -ItemType Directory -Path (Split-Path -Parent $benchLog) -Force | Out-Null
    Write-Host "负载器参数: $($benchArgs -join ' ')"
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
    Write-Host ('结果: sent={0} paired={1} missing={2} eps={3:N1} p50={4:N2}ms p95={5:N2}ms p99={6:N2}ms max={7:N2}ms cpu_avg={8:N1}% rss_max={9:N1}MB' -f `
        $s.sent, $s.paired_replies, $s.missing_replies, $s.throughput_eps, `
        $s.latency_p50_ms, $s.latency_p95_ms, $s.latency_p99_ms, $s.latency_max_ms, `
        $s.cpu_avg_percent, ($s.rss_max_bytes / 1MB))
    Write-Host "输出: $OutFile"
} finally {
    if (-not $KeepBot) { Stop-Tree $botProc }
    Stop-Tree $benchProc
    if ($redisProc -and -not $KeepBot) { Stop-Tree $redisProc }
}
