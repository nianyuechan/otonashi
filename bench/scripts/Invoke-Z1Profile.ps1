# Invoke-Z1Profile.ps1 - 阶段零 Z1 剖析重采（可重复、可审计）
# 用法（示例）：
#   & bench\scripts\Invoke-Z1Profile.ps1 -Rate 250 -OutDir bench\env\profiles\current-2026-11-15
#   & bench\scripts\Invoke-Z1Profile.ps1 -Rate 600 -OutDir bench\env\profiles\current-2026-11-15
# 流程：构建两个二进制并记录 SHA256 → 启动 bot（YZGO_PPROF_ADDR）→ 启动负载器
# （server 模式，reply/steady/group 100 群 2 万发送者）→ 负载中段抓取
# cpu(30s)/heap(?gc=1)/goroutine/block/mutex → 负载结束后生成 pprof -top 文本与
# capture-<rate>.json → 按精确 PID 停止 bot 并确认端口释放。
# manifest.json 由两档全部完成后另行汇总（见报告脚本），本脚本不写 manifest。
[CmdletBinding()]
param(
    [ValidateSet(250, 600)][int]$Rate = 250,
    [string]$Duration = '180s',
    [int]$DurationSec = 180,
    [string]$OutDir = '',
    [string]$PprofAddr = '127.0.0.1:6060'
)
$ErrorActionPreference = 'Stop'
$benchRoot = Split-Path -Parent $PSScriptRoot                      # bench/
$envDir = Join-Path $benchRoot 'env'
$botDir = Join-Path $envDir 'go-yunzai'
$botExe = Join-Path $botDir 'yunzai-bench.exe'
$benchExe = Join-Path $envDir 'onebot-bench.exe'
$workspaceParent = Split-Path -Parent (Split-Path -Parent $benchRoot) # G:\ALL WORK\音无
$goExe = Join-Path $workspaceParent 'tools\go\bin\go.exe'
$gitExe = 'C:\Program Files\Git\cmd\git.exe'
$repoRoot = Split-Path -Parent $benchRoot                           # 音无\音无
$resultsDir = Join-Path $envDir 'results'
if (-not $OutDir) { $OutDir = Join-Path $envDir 'profiles\current-2026-11-15' }

function Assert-FreeVirtualMemory {
    param([double]$MinimumGB)
    $os = Get-CimInstance Win32_OperatingSystem
    $free = [math]::Round($os.FreeVirtualMemory/1MB, 2)
    if ($free -lt $MinimumGB) {
        throw ("空闲虚拟内存 {0}GB 低于要求的 {1}GB，为避免链接器 OOM 中止；稍后重试。" -f $free, $MinimumGB)
    }
}

function Get-FileSHA256 {
    param([string]$Path)
    (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLower()
}

# ---------- 构建 ----------
Assert-FreeVirtualMemory 1.5
$goEnv = @{ GOPATH = 'G:\ALL WORK\音无\gopath'; GOCACHE = 'G:\ALL WORK\音无\gocache'; GOTMPDIR = 'G:\ALL WORK\音无\gotmp'; TEMP = 'G:\ALL WORK\音无\gotmp'; TMP = 'G:\ALL WORK\音无\gotmp'; GOMODCACHE = 'G:\ALL WORK\音无\gopath\pkg\mod'; CGO_ENABLED = '0'; GOPROXY = 'https://goproxy.cn,direct'; GOSUMDB = 'off' }
$saved = @{}
foreach ($k in $goEnv.Keys) { $saved[$k] = [Environment]::GetEnvironmentVariable($k, 'Process'); [Environment]::SetEnvironmentVariable($k, $goEnv[$k], 'Process') }
$pushed = 0
try {
    Push-Location (Join-Path $repoRoot 'go-yunzai'); $pushed++
    & $goExe build -o $botExe .\cmd\yunzai-bench
    if ($LASTEXITCODE -ne 0) { throw "yunzai-bench 构建失败" }
    Pop-Location; $pushed--
    Push-Location $benchRoot; $pushed++
    & $goExe build -o $benchExe .\cmd\onebot-bench
    if ($LASTEXITCODE -ne 0) { throw "onebot-bench 构建失败" }
    Pop-Location; $pushed--
} finally {
    foreach ($k in $saved.Keys) { [Environment]::SetEnvironmentVariable($k, $saved[$k], 'Process') }
    while ($pushed -gt 0) { Pop-Location; $pushed-- }
}
$botSha = Get-FileSHA256 $botExe
$benchSha = Get-FileSHA256 $benchExe
$buildTime = (Get-Item $botExe).LastWriteTime.ToString('o')
$commit = (& $gitExe -C $repoRoot rev-parse HEAD).Trim()
Write-Output ("commit={0} bot_sha={1} bench_sha={2} build_time={3}" -f $commit, $botSha, $benchSha, $buildTime)

# ---------- 输出目录（只拒绝会覆盖本档文件的场景） ----------
$outBase = 'go-reply-group-z1-current-{0}' -f $Rate
$targets = @(
    (Join-Path $OutDir ('cpu-{0}.pb.gz' -f $Rate)),
    (Join-Path $OutDir ('heap-{0}.pb.gz' -f $Rate)),
    (Join-Path $OutDir ('goroutine-{0}.pb.gz' -f $Rate)),
    (Join-Path $OutDir ('block-{0}.pb.gz' -f $Rate)),
    (Join-Path $OutDir ('mutex-{0}.pb.gz' -f $Rate)),
    (Join-Path $OutDir ('capture-{0}.json' -f $Rate)),
    (Join-Path $resultsDir ($outBase + '.json')),
    (Join-Path $resultsDir ($outBase + '.json.log')),
    (Join-Path $resultsDir ($outBase + '.json.log.err')),
    (Join-Path $resultsDir ($outBase + '.json.bot.log')),
    (Join-Path $resultsDir ($outBase + '.json.bot.err'))
)
foreach ($t in $targets) {
    if (Test-Path -LiteralPath $t) { throw "输出文件已存在，避免覆盖证据: $t" }
}
New-Item -ItemType Directory -Path $OutDir -Force | Out-Null
New-Item -ItemType Directory -Path $resultsDir -Force | Out-Null

# ---------- 残留清理（按精确路径） ----------
$strays = @(Get-Process yunzai-bench,onebot-bench -ErrorAction SilentlyContinue | Where-Object { $_.Path -in @($botExe, $benchExe) })
foreach ($stray in $strays) {
    Write-Output ("清理残留进程 pid={0} path={1}" -f $stray.Id, $stray.Path)
    & taskkill /PID $stray.Id /T /F 2>$null | Out-Null
    Stop-Process -Id $stray.Id -Force -ErrorAction SilentlyContinue
}
foreach ($port in 3002, 6060) {
    $c = Get-NetTCPConnection -LocalPort $port -State Listen -ErrorAction SilentlyContinue
    if ($c) { throw ("端口 {0} 已被占用 pid={1}，先报告不要杀未知进程" -f $port, ($c | Select-Object -First 1 -ExpandProperty OwningProcess)) }
}

# ---------- 启动 bot（pprof 环境变量仅本进程） ----------
Assert-FreeVirtualMemory 1.5
$env:YZGO_PPROF_ADDR = $PprofAddr
$botLog = Join-Path $resultsDir ($outBase + '.json.bot.log')
$botErr = Join-Path $resultsDir ($outBase + '.json.bot.err')
$botProc = Start-Process -FilePath $botExe `
    -ArgumentList @('-config', 'config.yaml') `
    -WorkingDirectory $botDir -RedirectStandardOutput $botLog `
    -RedirectStandardError $botErr -PassThru -WindowStyle Hidden
Start-Sleep -Seconds 2
if ($botProc.HasExited) { throw "bot 启动即退出，见 $botErr" }
Write-Output ("bot pid={0} pprof={1}" -f $botProc.Id, $PprofAddr)

# pprof 就绪探测
$pprofReady = $false
$deadline = (Get-Date).AddSeconds(20)
while ((Get-Date) -lt $deadline) {
    try {
        $resp = Invoke-WebRequest -Uri ("http://{0}/debug/pprof/" -f $PprofAddr) -UseBasicParsing -TimeoutSec 3
        if ($resp.StatusCode -eq 200) { $pprofReady = $true; break }
    } catch { Start-Sleep -Milliseconds 500 }
}
if (-not $pprofReady) {
    & taskkill /PID $botProc.Id /T /F 2>$null | Out-Null
    Remove-Item Env:YZGO_PPROF_ADDR -ErrorAction SilentlyContinue
    throw "pprof 端点未就绪: $PprofAddr"
}

# ---------- 启动负载器 ----------
$loadLog = Join-Path $resultsDir ($outBase + '.json.log')
$loadErr = Join-Path $resultsDir ($outBase + '.json.log.err')
$outFile = Join-Path $resultsDir ($outBase + '.json')
$runStart = Get-Date
$benchArgs = @(
    '-mode', 'server', '-listen', '127.0.0.1:3002',
    '-workload', 'reply', '-scenario', 'steady',
    '-rate', "$Rate", '-duration', $Duration,
    '-message-type', 'group', '-group-count', '100', '-user-count', '20000',
    '-pid', "$($botProc.Id)",
    '-out', ('results\' + (Split-Path -Leaf $outFile))
)
$benchProc = Start-Process -FilePath $benchExe -ArgumentList ($benchArgs -join ' ') `
    -WorkingDirectory $envDir -RedirectStandardOutput $loadLog `
    -RedirectStandardError $loadErr -PassThru -WindowStyle Hidden
Write-Output ("负载器 pid={0} args={1}" -f $benchProc.Id, ($benchArgs -join ' '))

# ---------- 负载中段抓取五类 profile ----------
Start-Sleep -Seconds 60   # 预热(2s)+连接建立后约 56s，处于 180s 负载窗口中段
$cpuWindowSeconds = 30
Invoke-WebRequest -Uri ("http://{0}/debug/pprof/profile?seconds={1}" -f $PprofAddr, $cpuWindowSeconds) `
    -OutFile (Join-Path $OutDir ('cpu-{0}.pb.gz' -f $Rate)) -UseBasicParsing -TimeoutSec 90
Invoke-WebRequest -Uri ("http://{0}/debug/pprof/heap?gc=1" -f $PprofAddr) `
    -OutFile (Join-Path $OutDir ('heap-{0}.pb.gz' -f $Rate)) -UseBasicParsing -TimeoutSec 60
Invoke-WebRequest -Uri ("http://{0}/debug/pprof/goroutine" -f $PprofAddr) `
    -OutFile (Join-Path $OutDir ('goroutine-{0}.pb.gz' -f $Rate)) -UseBasicParsing -TimeoutSec 60
Invoke-WebRequest -Uri ("http://{0}/debug/pprof/block" -f $PprofAddr) `
    -OutFile (Join-Path $OutDir ('block-{0}.pb.gz' -f $Rate)) -UseBasicParsing -TimeoutSec 60
Invoke-WebRequest -Uri ("http://{0}/debug/pprof/mutex" -f $PprofAddr) `
    -OutFile (Join-Path $OutDir ('mutex-{0}.pb.gz' -f $Rate)) -UseBasicParsing -TimeoutSec 60
Write-Output ("五类 profile 采集完成（cpu 窗口 {0}s）" -f $cpuWindowSeconds)

# ---------- 等待负载结束 ----------
$deadline = (Get-Date).AddSeconds($DurationSec + 180)
while (-not $benchProc.HasExited -and (Get-Date) -lt $deadline) { Start-Sleep -Seconds 2 }
if (-not $benchProc.HasExited) { & taskkill /PID $benchProc.Id /T /F 2>$null | Out-Null; throw "负载器超时未退出" }
Start-Sleep -Seconds 1
if (-not (Test-Path -LiteralPath $outFile)) { throw "负载器未产出结果文件: $outFile；见 $loadErr" }
$result = [System.IO.File]::ReadAllText($outFile, [System.Text.Encoding]::UTF8) | ConvertFrom-Json
$finishedAt = [DateTimeOffset]::Parse($result.finished_at)
if ($finishedAt.LocalDateTime -lt $runStart) { throw "结果 finished_at 早于本次运行，疑似旧结果: $outFile" }

# ---------- pprof -top 文本（UTF-8，带退出码检查） ----------
function Save-PprofTop {
    param([string[]]$PprofArgs, [string]$OutPath)
    $lines = & $goExe tool pprof @PprofArgs 2>&1
    $code = $LASTEXITCODE
    $lines | Set-Content -LiteralPath $OutPath -Encoding utf8
    if ($code -ne 0) { throw ("pprof top 失败 (exit {0}): {1}" -f $code, $OutPath) }
}
$pprofEnvSaved = @{}
foreach ($k in @('GOCACHE','GOPATH','GOMODCACHE','GOTMPDIR','TEMP','TMP')) { $pprofEnvSaved[$k] = [Environment]::GetEnvironmentVariable($k, 'Process') }
[Environment]::SetEnvironmentVariable('GOCACHE', $goEnv['GOCACHE'], 'Process')
[Environment]::SetEnvironmentVariable('GOPATH', $goEnv['GOPATH'], 'Process')
[Environment]::SetEnvironmentVariable('GOMODCACHE', $goEnv['GOMODCACHE'], 'Process')
try {
    Save-PprofTop @('-top','-nodecount=25',$botExe,(Join-Path $OutDir ('cpu-{0}.pb.gz' -f $Rate))) (Join-Path $OutDir ('cpu-{0}-top.txt' -f $Rate))
    Save-PprofTop @('-top','-nodecount=25',$botExe,(Join-Path $OutDir ('heap-{0}.pb.gz' -f $Rate))) (Join-Path $OutDir ('heap-{0}-inuse-top.txt' -f $Rate))
    Save-PprofTop @('-top','-nodecount=25','-sample_index=alloc_space',$botExe,(Join-Path $OutDir ('heap-{0}.pb.gz' -f $Rate))) (Join-Path $OutDir ('heap-{0}-alloc-top.txt' -f $Rate))
    Save-PprofTop @('-top','-nodecount=25',$botExe,(Join-Path $OutDir ('goroutine-{0}.pb.gz' -f $Rate))) (Join-Path $OutDir ('goroutine-{0}.txt' -f $Rate))
    Save-PprofTop @('-top','-nodecount=25',$botExe,(Join-Path $OutDir ('block-{0}.pb.gz' -f $Rate))) (Join-Path $OutDir ('block-{0}-top.txt' -f $Rate))
    Save-PprofTop @('-top','-nodecount=25',$botExe,(Join-Path $OutDir ('mutex-{0}.pb.gz' -f $Rate))) (Join-Path $OutDir ('mutex-{0}-top.txt' -f $Rate))
} finally {
    foreach ($k in $pprofEnvSaved.Keys) { [Environment]::SetEnvironmentVariable($k, $pprofEnvSaved[$k], 'Process') }
}

# ---------- 停止 bot（精确 PID）并确认端口释放 ----------
& taskkill /PID $botProc.Id /T /F 2>$null | Out-Null
try {
    $botProc.Refresh()
    if (-not $botProc.HasExited) { Stop-Process -Id $botProc.Id -Force -ErrorAction SilentlyContinue }
} catch {}
Remove-Item Env:YZGO_PPROF_ADDR -ErrorAction SilentlyContinue
Start-Sleep -Seconds 1
foreach ($port in 3002, 6060) {
    $c = Get-NetTCPConnection -LocalPort $port -State Listen -ErrorAction SilentlyContinue
    if ($c) { Write-Warning ("端口 {0} 仍被监听 pid={1}" -f $port, ($c | Select-Object -First 1 -ExpandProperty OwningProcess)) } else { Write-Output ("端口 {0} 已释放" -f $port) }
}

# ---------- 结果摘要 + capture 元数据 ----------
$s = $result.summary
Write-Output ('Z1 结果: sent={0} paired={1} missing={2} actual={3:N1}/s reported={4:N1} p99={5:N2}ms cpu_avg={6:N1}% rss_max={7:N1}MB' -f `
    $s.sent, $s.paired_replies, $s.missing_replies, $s.actual_injection_eps, $s.throughput_eps, `
    $s.latency_p99_ms, $s.cpu_avg_percent, ($s.rss_max_bytes/1MB))

[pscustomobject]@{
    rate_requested       = $Rate
    actual_injection_eps = [math]::Round($s.actual_injection_eps, 2)
    reported_eps         = [math]::Round($s.throughput_eps, 2)
    sent                 = $s.sent
    paired_replies       = $s.paired_replies
    missing_replies      = $s.missing_replies
    latency_p99_ms       = $s.latency_p99_ms
    cpu_avg_percent      = $s.cpu_avg_percent
    rss_max_mb           = [math]::Round($s.rss_max_bytes/1MB, 2)
    started_at           = $runStart.ToString('o')
    finished_at          = $finishedAt.ToString('o')
    cpu_window_seconds   = $cpuWindowSeconds
    bot_pid              = $botProc.Id
    commit               = $commit
    bot_sha256           = $botSha
    bench_sha256         = $benchSha
    build_time           = $buildTime
    out_file             = $outFile
} | ConvertTo-Json -Depth 3 | Set-Content -LiteralPath (Join-Path $OutDir ('capture-{0}.json' -f $Rate)) -Encoding utf8
Write-Output 'Z1-PROFILE-DONE'
