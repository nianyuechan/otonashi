# 下一轮 Agent 指令：阶段零收口与证据固化

- 文件版本：2026-11-15（替换旧版 `next-agent-prompt.md`；旧版针对 `04aed40`，已过时）
- 项目根：`G:\ALL WORK\音无\音无`
- 开工时核对 HEAD：**必须为 `87ba8cb`**
- 执行者：下一位交接 Agent
- 任务性质：**严格按 `docs/plan.md` §3 阶段零、§4 当前优先级 1–2、§5 总体完成条件执行**
- 本轮不是功能开发轮，也不是阶段九/十/十一；本轮目标是**把阶段零剩余的测量、证据、目标和结论收口**

> 执行前先读完整本文件。任何“看起来可以顺便做”的改动，都不属于本轮；先记录、后报告，不擅自扩项。

---

## 0. 一句话结论与当前事实（不要重复劳动）

当前代码和诊断方向基本成立，但阶段零还没有收口。已知事实如下：

- 新提交链为 `dd92eee` → `e31a9c3` → `1183fe0` → `1525a99` → `56658f3` → `43dc0fb` → `c6ae5f1` → `87ba8cb`；当前 HEAD 为 `87ba8cb`。
- `Test-Go.ps1` 已把包级超时改为 **120s**；旧的 `testgo-120s-run1..3.log` 三次 exit 0，但时间在 23:58–23:59，**早于后续 5 个代码提交**，不能作为当前 HEAD 的最终证据。
- 当前 HEAD 上实际补跑过三次统一验证：`testgo-current-run1.log`、`run2.log` exit 0；`run3.log` 因 Go 链接器 OOM 失败（`VirtualAlloc ... errno=1455` / `fatal error: out of memory`），不是测试断言失败或超时。该失败需要在内存充足时复核，不能用“重跑通过”掩盖。
- Z1 旧 profile 的原始 Build ID 为 **2026-11-15 00:06:53**，采集时间为 00:09–00:15；当前 `yunzai-bench.exe` 时间已是 01:13，报告却写“01:07 重建”。因此旧 profile **不是当前 HEAD 的 profile**，只能作为历史证据保存。
- `go-reply-group-realwrite-200-wal-run2.json` 已完成 30 分钟跑批，结果：
  - sent=377,380；expected_replies=188,690；paired=174,835；
  - missing=**13,855**（占期望回复 7.34%，占 sent 3.67%）；
  - actual_injection=**209.66/s**，reported=**206.91**；
  - p99=1.305ms，max=**29,407.41ms**；
  - action_errors=0，store 错误=0，慢写=0；
  - 重连成功 **7 次 / 上限 8**。
- `wal-check`、`wal-check2` 的 120s WAL+NORMAL 轮 missing=0、p99≈0.713ms；`wal-check4` 出现 missing=**1,124**、p99=**10,380.78ms**。这说明长窗口内机器级停滞/断连会造成丢失和极端尾延迟，不能拿短窗口结果代表长窗口。
- 默认 FULL 的端到端采证已在推送第 **5,816** 条时断连，未产出 JSON；独立 SQLite 基准的源程序在 `artifacts-gocache\sqlite-diag\main.go`，但原始输出没有进入 Git，当前报告里的 8.9–9.7 提交/s等数字缺少可复现原始证据。
- 当前 `Invoke-BenchScenario.ps1` 的 `-Workload` 只允许 `reply` 和 `scan`，**没有 `render`**；虽然 `loadgen` 已实现 `WorkloadRender`，但脚本无法直接跑 Z3。`bench/env/go-yunzai/config-render.yaml` 存在但位于被 `.gitignore` 忽略的目录，不能作为仓库内的可复现配置。
- EdgeCore 157 已确认存在：`C:\Program Files (x86)\Microsoft\EdgeCore\157.0.4341.46\msedge.exe`。
- `docs/phase0-profile-2026-11-14.md` 仍未被 Git 跟踪，内容在 §2.5 结束，引用“见 §3”但没有 §3；旧 profile/结果/日志也大量未跟踪。
- 机器存在已知的秒级到 30 秒级 IO 停滞；本机 16GB 内存，当前空闲物理/虚拟内存约 3.4/3.1GB，Go 链接器可能因提交限额 OOM。不要把内存不足误判为代码失败，也不要为了通过而删除/掩盖日志。

---

## 1. 开工前必须读的文件（按顺序）

1. `plan.md` §2 当前状态、§3 阶段零、§4 当前优先级、§5 总体完成条件。
2. `audit-completed-work-2026-11-14.md` 第八节“本轮”各行。
3. `docs/phase0-baseline.md`（现有首轮基线）。
4. `docs/capacity-100-groups-200rps.md` 第 0 节三口径。
5. `docs/phase0-profile-2026-11-14.md`（历史报告；注意它不是当前 HEAD 的证据）。
6. `bench/internal/loadgen/config.go`、`session.go`、`metrics.go`、`run.go`、`scenario.go`。
7. `bench/scripts/Invoke-BenchScenario.ps1`、`scripts/Test-Go.ps1`。
8. `go-yunzai/internal/observability/pprof.go`、`go-yunzai/internal/storage/sqlite.go`。
9. `go-yunzai/plugins/bench/store.go`、`store_test.go`、`go-yunzai/plugins/builtin/debug_plugin.go`。
10. `go-yunzai/internal/event/event.go`、`internal/plugin/manager.go`（Sender → SDK 映射）。
11. `bench/env/go-yunzai/config.yaml`、`config-render.yaml`、`go-render/cmd/renderer/main.go`。

---

## 2. 任务边界（硬边界）

### 2.1 本轮只做这些

- 当前 HEAD 的统一验证复核；
- Z1 当前 HEAD 的 CPU/heap/goroutine/block/mutex 剖析重采与报告修正；
- Z2 真实插件 + SQLite 写入的 200 msg/s 30 分钟长跑和 400 target 15 分钟压力轮；
- Z3 固定页面真实 Chromium/EdgeCore 渲染基线、并发/队列、失败降级；
- Z4 默认 FULL 低速稳定性窗口与阶段零最终目标数值；
- 上述证据的归档、报告、提交和工作区清理。

### 2.2 本轮明确不做

- 不进入阶段九本机控制面、阶段十 Rust 桌面端、阶段十一 LLM。
- 不先改 `#note` 权限，不先做 M7 渲染出站隔离。
- 不把 `WAL + synchronous(NORMAL)` 设为默认；它仍只是 Z2 的显式实验开关。
- 不修改 `event.Sender` 语义、不修改存储默认、不修改 `maxLoadReconnects=8`，除非后续另有单独批准。
- 不执行 `go test -race`（当前 `CGO_ENABLED=0`，缺 C 编译器）；如果环境变化，另行说明，不在本轮承诺。
- 不把合成 `reply` 当成真实插件验证；Z2 必须走 `bench.store` 的 `Set + Get`。
- 不把渲染耗时混入消息核心 p99；Z3 单独报告。
- 不删除用户未跟踪的工作、旧证据、旧结果；清理必须按 §8 的精确路径流程。

### 2.3 允许的代码改动

本轮原则上只改测量/报告/配置，不改生产语义。**唯一允许的代码改动**是：

- `bench/scripts/Invoke-BenchScenario.ps1`：为 Z3 增加 `render` 到 `ValidateSet`，并支持从仓库内模板复制 `config-render.yaml`；
- `bench/internal/loadgen`：只更新 `-workload` 帮助文本/测试，使 `render` 的 CLI 说明一致；不改消息生成、指标和重连语义；
- 新增 `bench/configs/config-render.yaml`（从现有 `bench/env/go-yunzai/config-render.yaml` 复制为仓库内模板，禁止把被 `.gitignore` 忽略的运行时副本当作唯一证据）；
- 新增 `bench/scripts/Invoke-Z1Profile.ps1`（如果采用脚本化 Z1，必须可重复、可审计，不允许硬编码时间/结果）；
- 报告和文档。

如果发现新的真实代码缺陷（例如重连/存储/渲染生命周期 bug）：**单独记录、单独最小修复、单独提交**，不得混入性能结论；修复前后重跑受影响测试。没有证据不要改。

---

## 3. 判定口径（先定标准，再看数字）

### 3.1 三个吞吐口径不能混用

- `requested`：脚本传入的目标速率，例如 `-Rate 250`、`-Rate 600`。
- `actual_injection_eps`：事件首尾 `sent_ns` 计算的实际注入速率，JSON 字段必须存在。
- `reported_eps`：结果分母口径（负载窗口 + grace，不含预热），不是 requested，也不是 actual。

任何报告都必须把这三个数分开写，禁止只写一个“200/s 达标”。

### 3.2 本轮验收的硬标准

- **Z2-A（200 target，30 分钟，WAL+NORMAL 实验）**：
  - `missing_replies=0`
  - `action_errors=0`
  - `expected_replies > 0` 且 `paired_replies == expected_replies`
  - `actual_injection_eps >= 200`
  - `bench.store` 的 set/get/mismatch 错误为 0，慢写为 0
  - `finished_at` 在本次运行时间窗内，结果文件名是新文件
- **Z2-B（400 target，15 分钟，WAL+NORMAL 实验）**：
  - `actual_injection_eps >= 400`
  - `missing_replies=0`
  - `action_errors=0`
  - store 错误=0、慢写=0
- **Z1**：
  - 必须使用当前 HEAD 构建的二进制；profile manifest 记录 commit、binary SHA256、采集起止时间；
  - CPU/heap/goroutine/block/mutex 五类都有原始 profile 和 `-top` 文本；
  - 报告必须写明采样窗口、负载档位、requested/actual、限制。
- **Z3**：
  - 固定 `#render` 页面成功返回图片段，不能是降级文本；
  - 单次/并发/队列上限/失败降级四类结果都有记录；
  - 渲染失败时核心返回 `图片暂时不可用` 一类的降级文本，且不泄漏请求/工作线程；
  - 不引入任意 HTML 输入；仍只使用固定页面。
- **Z4**：
  - 默认 FULL 的低速 30 分钟窗口有内存/goroutine/错误/重连趋势；
  - 目标数值和回退门槛写入 `docs/plan.md` §3 与 `docs/phase0-baseline.md`；
  - 明确回答 100 群/2 万用户/200 msg/s 下瓶颈是 CPU、锁、存储 IO 还是外部等待；
  - 若数据不足，写“证据不足”，不要拍脑袋定数。
- **P0/当前 HEAD 验证**：
  - 默认 `Test-Go.ps1` 连续三次 exit 0，或按 §6.2 记录“内存受限替代验证”，但后者不能冒充默认三次通过。

### 3.3 失败的定义

以下任一情况，必须写 **FAIL** 或 **BLOCKED**，不能写“基本通过”：

- 30 分钟长跑 `missing_replies > 0`；
- 结果 JSON 未生成、`finished_at` 不在本次窗口、或 `sent <= 0`；
- 8 次重连耗尽导致中途退出；
- `Test-Go.ps1` 非零退出（包括 Go 链接器 OOM）；
- 当前 HEAD 与 `87ba8cb` 不一致；
- profile 二进制不是当前 HEAD；
- Z3 没有真实浏览器路径、或只测了假渲染器；
- Z4 没有目标数值却声称阶段零完成。

---

## 4. 执行顺序与依赖

严格按以下顺序，**不要并行跑多个负载/基准**。同一时刻只允许一个 `yunzai-bench`、一个 `onebot-bench`（及其对应的渲染服务）；端口 `3002`、`18080` 不能有两个实例抢。

1. **P0-0**：锁定 HEAD、工作区、进程、环境和证据清单。
2. **P0-1**：当前 HEAD 统一验证（默认三次；必要时内存受限替代）。
3. **Z1**：当前 HEAD 的 200/600 requested 档剖析重采（不是重跑旧 00:06 结果）。
4. **Z2-A**：WAL+NORMAL、200 target、30 分钟、store 真实写入。
5. **Z2-B**：WAL+NORMAL、400 target、15 分钟、store 真实写入。
6. **Z3**：固定页面渲染；先单次，再并发/队列，再失败降级。
7. **Z4**：默认 FULL 低速 30 分钟；汇总目标数值和瓶颈。
8. **归档**：报告、证据、提交、工作区清单。

如果 Z2-A 失败（missing>0），**不要直接跑 Z2-B 来掩盖**；先记录 Z2-A 的失败事实和时间窗，再决定是否有第二次 30 分钟尝试。Z2-B 可以作为独立压力结果，但不能替代 Z2-A 的通过。

---

## 5. 任务详情

### P0-0：锁定基线（只读检查，不产生代码提交）

执行：

```powershell
$repo='G:\ALL WORK\音无\音无'
$git='C:\Program Files\Git\cmd\git.exe'
& $git -C $repo rev-parse HEAD
& $git -C $repo status --short --untracked-files=all
Get-Process yunzai-bench,onebot-bench,go-render,msedge -ErrorAction SilentlyContinue |
  Select-Object Id,ProcessName,Path,StartTime,CPU
Get-PSDrive G | Select-Object Name,Free,Used
```

要求：

- 记录 HEAD、`git status`、当前进程、G: 剩余空间、当前 UTC+8 时间。
- 如果 HEAD 不是 `87ba8cb`，**停止**并报告；不要自行 rebase/reset/checkout。
- 如果发现非本任务的 `yunzai-bench`/`onebot-bench`/`go-render`，只按精确路径确认；不要按进程名批量杀。
- 把所有未跟踪产物列成三类：代码/文档、可保留证据、旧失败日志；**先不删除**。

### P0-1：当前 HEAD 统一验证

#### 默认三次

在 `G:\ALL WORK\音无\音无` 执行：

```powershell
$repo='G:\ALL WORK\音无\音无'
$logs='G:\ALL WORK\音无\artifacts-gocache'
1..3 | ForEach-Object {
  $p=Join-Path $logs ("testgo-head-run{0}.log" -f $_)
  Set-Content -LiteralPath $p -Value '' -Encoding utf8
  $code=1
  try {
    & (Join-Path $repo 'scripts\Test-Go.ps1') *> $p
    $code=$LASTEXITCODE
  } catch {
    $_ | Out-String | Add-Content -LiteralPath $p -Encoding utf8
    $code=1
  }
  Add-Content -LiteralPath $p -Value ("EXITCODE=$code") -Encoding utf8
  Write-Output "run$_ exit=$code log=$p"
}
```

验收：

- 三次 `EXITCODE=0`：P0-1 默认环境通过。
- 任一次非零：保留完整日志，**不要覆盖**；检查是否 `fatal error: out of memory` / `VirtualAlloc ... errno=1455`。
- 如果失败是 Go 链接器 OOM，先记录当时 `Get-CimInstance Win32_OperatingSystem` 的物理/虚拟空闲值；不要把它写成代码失败。

#### 内存受限替代验证（仅默认失败时）

只有默认三次出现 OOM 才执行；用 `GOFLAGS=-p=1` 降低并行链接/测试内存，不改 `Test-Go.ps1`：

```powershell
$env:GOFLAGS='-p=1'
# 再运行三次，日志名 testgo-head-p1-run1..3.log
Remove-Item Env:GOFLAGS
```

要求：

- 报告必须写清“默认脚本失败原因 + 替代验证结果”，不能把 `-p=1` 的结果冒充默认三次通过。
- 如果默认三次 OOM、替代三次通过：结论写“代码验证有条件通过，默认验证仍 BLOCKED”，并在最终报告保留这个限制。

### Z1：当前 HEAD 剖析重采

目标：补一份真正属于 `87ba8cb` 的 Z1 证据，不再拿 00:06 的旧 profile 当当前基线。

#### 构建与元数据

```powershell
$repo='G:\ALL WORK\音无\音无'
$workspace='G:\ALL WORK\音无'
$go='G:\ALL WORK\音无\tools\go\bin\go.exe'
$env:GOCACHE="$workspace\gocache"
$env:GOPATH="$workspace\gopath"
$env:GOMODCACHE="$workspace\gopath\pkg\mod"
$env:GOTMPDIR="$workspace\gotmp"
$env:TEMP="$workspace\gotmp"
$env:TMP="$workspace\gotmp"
$env:CGO_ENABLED='0'

Push-Location (Join-Path $repo 'go-yunzai')
& $go build -o (Join-Path $repo 'bench\env\go-yunzai\yunzai-bench.exe') .\cmd\yunzai-bench
if ($LASTEXITCODE -ne 0) { throw "yunzai-bench 构建失败" }
Pop-Location

Push-Location (Join-Path $repo 'bench')
& $go build -o (Join-Path $repo 'bench\env\onebot-bench.exe') .\cmd\onebot-bench
if ($LASTEXITCODE -ne 0) { throw "onebot-bench 构建失败" }
Pop-Location
```

必须记录：

- `git rev-parse HEAD`；
- `bench\env\go-yunzai\yunzai-bench.exe` 的 SHA256；
- `bench\env\onebot-bench.exe` 的 SHA256；
- 二进制构建时间。

#### 采集

推荐用脚本化流程；如果手写，必须遵守同一时序：

1. 设置 `$env:YZGO_PPROF_ADDR='127.0.0.1:6060'`。
2. 启动 bot，配置文件 `bench/env/go-yunzai/config.yaml`，工作目录 `bench/env/go-yunzai`。
3. 启动 `onebot-bench.exe`：server 模式、`127.0.0.1:3002`、`reply`、`steady`、`-Rate 250`、`-duration 180s`、`group`、`-group-count 100`、`-user-count 20000`、`-pid <bot pid>`、`-out results\go-reply-group-z1-current-250.json`。
4. 负载进入中段后抓取：
   - `http://127.0.0.1:6060/debug/pprof/profile?seconds=30`
   - `http://127.0.0.1:6060/debug/pprof/heap?gc=1`
   - `http://127.0.0.1:6060/debug/pprof/goroutine`
   - `http://127.0.0.1:6060/debug/pprof/block`
   - `http://127.0.0.1:6060/debug/pprof/mutex`
5. 用 `go tool pprof -top -nodecount=25` 保存每份 profile 的 top 文本。
6. 等负载结束，按精确路径停止 bot；不要留下 3002/6060 监听。
7. 再用 `-Rate 600`、`-duration 180s`、输出 `go-reply-group-z1-current-600.json` 重复一次。

输出目录：

```text
bench/env/profiles/current-2026-11-15/
  cpu-250.pb.gz / cpu-250-top.txt
  heap-250.pb.gz / heap-250-inuse-top.txt / heap-250-alloc-top.txt
  goroutine-250.pb.gz / goroutine-250.txt
  block-250.pb.gz / block-250-top.txt
  mutex-250.pb.gz / mutex-250-top.txt
  ...（600 档同理）
  manifest.json
```

`manifest.json` 至少包含：

```json
{
  "commit": "87ba8cb",
  "bot_sha256": "...",
  "bench_sha256": "...",
  "build_time": "2026-11-15T...",
  "captures": [
    {"rate_requested": 250, "actual_injection_eps": 0, "started_at": "...", "finished_at": "...", "cpu_window_seconds": 30}
  ]
}
```

报告：

- 新建 `docs/phase0-profile-2026-11-15.md`；
- 旧 `docs/phase0-profile-2026-11-14.md` 顶部加一行“历史快照，二进制为 00:06 构建，不是当前 HEAD”；不重写旧数字；
- 新报告必须写采样窗口、负载、requested/actual、CPU 占用、heap/goroutine、block/mutex 结论；
- 旧报告里“§3”不存在的问题在新报告中修正，不要继续保留悬空引用。

### Z2-A：200 target、30 分钟、WAL+NORMAL、真实 store

目的：验证在 100 群/2 万发送者、`bench.store` 每条消息 `Set + Get` 的情况下，200 target 的 30 分钟稳定性。

#### 运行命令

```powershell
$repo='G:\ALL WORK\音无\音无'
$env:YZGO_STORAGE_EXPERIMENT='wal-normal'
& (Join-Path $repo 'bench\scripts\Invoke-BenchScenario.ps1') `
  -Variant go `
  -Workload reply `
  -Scenario steady `
  -Rate 250 `
  -DurationSec 1800 `
  -MessageType group `
  -GroupCount 100 `
  -UserCount 20000 `
  -BotExtraArgs '-store' `
  -OutFile (Join-Path $repo 'bench\env\results\go-reply-group-realwrite-200-wal-run3.json')
Remove-Item Env:YZGO_STORAGE_EXPERIMENT
```

要求：

- 必须先确认 `onebot-bench.exe` 和 `yunzai-bench.exe` 都是当前 HEAD 构建；
- `-Rate 250` 是 200 target 的常用请求档，不能写成 `-Rate 200` 后混用 actual；
- `-store` 必须开启；不改默认存储配置；
- 运行中不得并行跑 `Test-Go`、Z3 或另一个基准；
- 记录启动/结束时间、进程 PID、G: 空间、CPU/RSS；
- 运行后保存 `.json`、`.log`、`.log.err`，不要覆盖旧文件。

#### 结果核验

```powershell
$p='G:\ALL WORK\音无\音无\bench\env\results\go-reply-group-realwrite-200-wal-run3.json'
$j=Get-Content -LiteralPath $p -Raw -Encoding utf8 | ConvertFrom-Json
$s=$j.summary
[pscustomobject]@{
  sent=$s.sent
  expected=$s.expected_replies
  paired=$s.paired_replies
  missing=$s.missing_replies
  action_errors=$s.action_errors
  actual=$s.actual_injection_eps
  reported=$s.throughput_eps
  p99=$s.latency_p99_ms
  max=$s.latency_max_ms
  wall=$s.load_wall_ms
}
Select-String -LiteralPath ($p + '.log.err') -Pattern '推送失败|重连成功|bench.store slow write|bench.store set failed|bench.store get failed|mismatch'
```

判定：

- 只有 §3.2 的硬标准全部满足，才写 PASS；
- `missing>0` 必须写 FAIL，并列出断连 attempts、index、时间窗；不要写“基本通过”；
- 若 8 次重连耗尽导致无 JSON：写 BLOCKED/FAIL，不改 `maxLoadReconnects` 来掩盖；
- 最多允许 **2 次** 30 分钟尝试。第二次必须用新文件名 `...-wal-run4.json`；两次失败即停止，报告限制。

### Z2-B：400 target、15 分钟、WAL+NORMAL、真实 store

```powershell
$repo='G:\ALL WORK\音无\音无'
$env:YZGO_STORAGE_EXPERIMENT='wal-normal'
& (Join-Path $repo 'bench\scripts\Invoke-BenchScenario.ps1') `
  -Variant go `
  -Workload reply `
  -Scenario steady `
  -Rate 600 `
  -DurationSec 900 `
  -MessageType group `
  -GroupCount 100 `
  -UserCount 20000 `
  -BotExtraArgs '-store' `
  -OutFile (Join-Path $repo 'bench\env\results\go-reply-group-realwrite-400target-wal-run1.json')
Remove-Item Env:YZGO_STORAGE_EXPERIMENT
```

要求：

- 这里的 requested 是 600，实际注入预期约 435；报告必须写 actual；
- 验收要求 `actual_injection_eps >= 400`；如果 actual <400，先判断是负载器单线程上限还是服务端饱和，不能直接算通过；
- `missing=0`、`action_errors=0`、store 错误=0；否则 FAIL；
- 15 分钟是压力轮，不等于 30 分钟稳定性；不要用 Z2-B 替代 Z2-A 的 30 分钟结论。

### Z3：固定页面真实 Chromium/EdgeCore 渲染

#### 先补脚本缺口（必须）

当前 `Invoke-BenchScenario.ps1` 的 `ValidateSet` 只有 `reply/scan`。必须做最小修改：

- `[ValidateSet('reply','scan','render')]`；
- 帮助文本写明 `render` 为固定 `#render` 页面；
- 不改变 `loadgen` 的消息生成语义；
- 同步更新 `loadgen` 的 `-workload` 帮助文本（现在仍写“reply 或 scan”）；
- 如果有 CLI/测试断言 ValidateSet，补 `render` 的用例。

同时：

- 将现有 `bench/env/go-yunzai/config-render.yaml` 复制为仓库内模板 `bench/configs/config-render.yaml`；
- 在脚本启动 bot 前，从该模板复制到 `bench/env/go-yunzai/config-render.yaml`；运行时副本仍可被 `.gitignore` 忽略；
- 模板中保持 `plugins.masters: {2081344177: [10001]}`，`debug.enabled: true`，`render.max_concurrent: 24`；
- 不要修改生产默认 `config.yaml`。

#### 启动真实渲染服务

EdgeCore 157 路径：

```text
C:\Program Files (x86)\Microsoft\EdgeCore\157.0.4341.46\msedge.exe
```

构建并启动：

```powershell
$repo='G:\ALL WORK\音无\音无'
$go='G:\ALL WORK\音无\tools\go\bin\go.exe'
Push-Location (Join-Path $repo 'go-render')
& $go build -o (Join-Path $repo 'bench\env\go-render.exe') .\cmd\renderer
if ($LASTEXITCODE -ne 0) { throw "go-render 构建失败" }
Pop-Location

$render = Start-Process -FilePath (Join-Path $repo 'bench\env\go-render.exe') `
  -ArgumentList @(
    '-addr','127.0.0.1:18080',
    '-chromium','C:\Program Files (x86)\Microsoft\EdgeCore\157.0.4341.46\msedge.exe',
    '-concurrency','1',
    '-queue-size','1',
    '-max-timeout','30s'
  ) `
  -WorkingDirectory (Join-Path $repo 'bench\env') `
  -RedirectStandardOutput (Join-Path $repo 'bench\env\go-render.log') `
  -RedirectStandardError (Join-Path $repo 'bench\env\go-render.err') `
  -PassThru -WindowStyle Hidden

Start-Sleep -Seconds 3
Invoke-RestMethod 'http://127.0.0.1:18080/healthz'
```

#### 单次渲染

```powershell
& (Join-Path $repo 'bench\scripts\Invoke-BenchScenario.ps1') `
  -Variant go `
  -Workload render `
  -Scenario steady `
  -Rate 1 `
  -DurationSec 60 `
  -MessageType group `
  -GroupCount 1 `
  -UserCount 1 `
  -BotExtraArgs '-config','config-render.yaml' `
  -OutFile (Join-Path $repo 'bench\env\results\go-render-single-run1.json')
```

要求：

- `#render` 只有主人可用，必须确保 `self_id`/`user_id` 与模板 masters 对齐；
- 成功判定看 bot 出站消息是否为图片段，以及 go-render 日志/metrics；不要只看 loadgen 的 `paired`（render 工作负载没有文本配对）；
- 抓取 `http://127.0.0.1:18080/metrics` 到 `bench/env/results/render-metrics-single.txt`。

#### 并发/队列

重启 go-render，参数改为 `-concurrency 2 -queue-size 2`，同配置发送 `#render` 至少 30–60 秒，速率 8/s：

```powershell
-Rate 8 -DurationSec 60 -Workload render
```

记录：

- `queue_full`/429、降级文本、失败率；
- `/metrics` 的队列等待、active、duration、recovered；
- 不要用任意 HTML；仍是固定 `#render` 页面。

#### 失败降级

- 停止 go-render（精确 PID），再发一次 `#render`；
- 预期 bot 返回 `图片暂时不可用` 一类文本，而不是无响应/崩溃；
- 观察 bot goroutine 数、错误日志、是否留下 pending 请求；
- 重启 go-render，确认可恢复。

Z3 报告必须单独成节，不把渲染延迟和消息 p99 混在同一张表里。

### Z4：默认 FULL 低速 30 分钟 + 目标数值

#### Z4-A：默认 FULL 低速窗口

目的：记录默认持久性在低速下的长时稳定性；**不是** 200 target 通过证据。

```powershell
$repo='G:\ALL WORK\音无\音无'
Remove-Item Env:YZGO_STORAGE_EXPERIMENT -ErrorAction SilentlyContinue
& (Join-Path $repo 'bench\scripts\Invoke-BenchScenario.ps1') `
  -Variant go `
  -Workload reply `
  -Scenario steady `
  -Rate 10 `
  -DurationSec 1800 `
  -MessageType group `
  -GroupCount 100 `
  -UserCount 20000 `
  -BotExtraArgs '-store' `
  -OutFile (Join-Path $repo 'bench\env\results\go-reply-group-default-full-low-30m-run1.json')
```

要求：

- 这是低速基线；默认 FULL 在 G: 机械盘约 9 提交/s，`-Rate 10` 可能已接近上限，必须如实记录 actual/missing/断连；
- 不要把 Z4-A 的低速通过写成“默认 FULL 可承载 200/s”；
- 如果 30 分钟内出现断连/丢失，仍按 FAIL 记录稳定性，不掩盖。

#### Z4-B：目标耐久性

- 如果 Z2-A 已经 PASS，直接把 Z2-A 作为 200 target 30 分钟耐久性证据，不重复跑。
- 如果 Z2-A FAIL，不得用 Z4-A 替代；Z4-B 写 FAIL/BLOCKED。
- 如已选 WAL+NORMAL 作为实验方案，必须在报告中写“持久性取舍”：崩溃/断电可能丢最后若干未 checkpoint 事务；默认仍 FULL。

#### 目标数值与文档

在 Z1/Z2/Z3/Z4 数据完成后：

- 更新 `docs/plan.md` §3 阶段零，新增一张“阶段零目标/回退门槛”表，至少包含：
  - 目标负载：100 群/2 万注册用户/200 msg/s；设计余量 400 msg/s；
  - 200 target 的 requested/actual/reported、missing、p99、CPU、RSS；
  - 400 target 的 actual、missing、p99；
  - 存储持久性方案和取舍；
  - 渲染单次/并发/队列/降级指标；
  - 明确“若 Z2-A 长窗口 missing>0，该目标不通过，不得写成达标”。
- 更新 `docs/phase0-baseline.md`，加入“2026-11-15 阶段零收口结果”一节；
- 新建/完成 `docs/phase0-profile-2026-11-15.md`；
- 不要把推测写成根因；对机器级停滞只能写“与断连窗口时间相关，需逐窗口证据”，除非已逐段核对。
- 所有目标数值必须有测量依据；没有数据的指标写“未测量”，不要编造。

---

## 6. 具体命令与日志规范

### 6.1 PowerShell 环境

- 统一使用 PowerShell；不要用 Bash。
- Git：`C:\Program Files\Git\cmd\git.exe`。
- Go：`G:\ALL WORK\音无\tools\go\bin\go.exe`。
- 手动 Go 命令环境：

```powershell
$env:GOCACHE='G:\ALL WORK\音无\gocache'
$env:GOPATH='G:\ALL WORK\音无\gopath'
$env:GOMODCACHE='G:\ALL WORK\音无\gopath\pkg\mod'
$env:GOTMPDIR='G:\ALL WORK\音无\gotmp'
$env:TEMP='G:\ALL WORK\音无\gotmp'
$env:TMP='G:\ALL WORK\音无\gotmp'
$env:CGO_ENABLED='0'
```

- `Test-Go.ps1` 会自行设置缓存环境；不要额外覆盖它的 `GOCACHE`。
- 写 `.ps1` 文件必须 UTF-8 BOM；写 JSON/Markdown 用 UTF-8，避免 PowerShell 默认编码损坏中文。
- 每次 PowerShell 调用结束前记录 `$LASTEXITCODE`；不要用 `-ErrorAction SilentlyContinue` 隐藏失败。

### 6.2 进程与残留

- 启动前按精确路径清理残留 `yunzai-bench.exe`、`onebot-bench.exe`、`go-render.exe`；不要按进程名批量杀。
- 启动后记录 PID；结束用 `Stop-Process -Id` 或现有脚本的 `Stop-Tree`，并确认端口释放。
- 不要在 `Test-Go` 运行期间跑负载；不要在负载期间跑另一个负载。
- 如果发现 3002/6060/18080 被不明进程占用，先报告，不要杀未知进程。

### 6.3 文件命名

- 不覆盖已提交的 `*-verified-run*.json`、`*-qpc*.json`、旧 profile。
- 新结果使用：

```text
go-reply-group-z1-current-250.json
go-reply-group-z1-current-600.json
go-reply-group-realwrite-200-wal-run3.json
go-reply-group-realwrite-200-wal-run4.json   # 仅第二次尝试
go-reply-group-realwrite-400target-wal-run1.json
go-render-single-run1.json
go-render-concurrent-run1.json
go-render-fallback-run1.json
go-reply-group-default-full-low-30m-run1.json
```

- 每个结果旁边保留 `.log`、`.log.err`；文件名跟随结果名，禁止固定日志名覆盖旧证据。

---

## 7. 验收矩阵（最终必须逐项填）

| 项 | 通过条件 | 结果 |
|---|---|---|
| HEAD | 开工/收工均为 `87ba8cb`（或报告变化） | 待填 |
| P0-1 默认三次 | `EXITCODE=0` 三次 | 待填 |
| P0-1 内存受限替代（如适用） | `-p=1` 三次通过，且明确标注非默认 | 待填 |
| Z1 当前 profile | 五类 profile + manifest + 报告；二进制 SHA 与 HEAD 对应 | 待填 |
| Z2-A | missing=0、action_errors=0、store 错误=0、actual≥200、expected/paired 完整 | 待填 |
| Z2-B | missing=0、action_errors=0、actual≥400、store 错误=0 | 待填 |
| Z3 单次 | 图片段成功，非降级文本；metrics 有记录 | 待填 |
| Z3 并发/队列 | 队列上限/失败率/降级均有记录 | 待填 |
| Z3 失败 | 停渲染后有降级文本、无泄漏 | 待填 |
| Z4-A | 默认 FULL 30 分钟趋势完整 | 待填 |
| Z4-B | 200 target 30 分钟耐久性（优先复用 Z2-A） | 待填 |
| 文档 | `plan.md` + `phase0-baseline.md` + 新 profile 报告已写目标/瓶颈/限制 | 待填 |
| 工作区 | 证据已提交或明确列出；无意外删除/覆盖 | 待填 |

**最终结论只能是 PASS / FAIL / BLOCKED 三种之一；不要使用“基本通过”。**

---

## 8. 提交、证据归档与清理

### 8.1 提交拆分建议

按内容拆成独立提交，不做 `--amend`，不 force push：

1. `test: verify current head unified suite` —— 只放 P0-1 日志/说明/必要的测试脚本修复（如果有）。
2. `bench: record current-head cpu and memory profiles` —— Z1 原始 profile、manifest、报告。
3. `bench: record real-write storage stability runs` —— Z2 JSON、日志摘要、报告。
4. `bench: add fixed-page render workload runner` —— Z3 脚本/模板/测试/结果。
5. `docs: set phase-zero targets and final stability findings` —— plan/baseline/audit 文档。

如果某个提交没有实际文件，不要为了凑数提交。

### 8.2 证据保留

- 新 JSON、profile、manifest、报告、模板配置必须进入 Git；大日志可以只保留最新一轮并记录 SHA256。
- 旧 `phase0-profile-2026-11-14.md`、旧 profile、旧失败日志：保留为历史，不覆盖；在报告中标注“历史/不是当前 HEAD”。
- 独立 SQLite 诊断必须保存原始输出：
  - 源程序可复制到 `bench/tools/sqlite-diag/`（不要只留在 `artifacts-gocache`）；
  - 运行输出保存为 `bench/env/results/sqlite-diag-2026-11-15.txt`；
  - 如果无法重跑，报告写“原始输出未保存，表格仅为历史结论”，不能继续把它当新证据。
- 如果清理旧结果，必须先列清单、确认 `git ls-files --error-unmatch` 未跟踪、保留至少一份失败证据；未明确批准不得删除。

### 8.3 最终工作区

- `git status --short --untracked-files=all` 必须列出剩余项；预期只允许本文件 `next-agent-prompt.md` 未跟踪（如果决定提交它，单独 docs 提交）。
- 不得出现“旧结果仍在工作区”“报告没提交”“profile 没提交”这类状态却声称阶段零完成。

---

## 9. 失败处理与停止条件

遇到以下情况，立即停止当前分支，保留现场，写报告，不要用重置/删除/清空掩盖：

1. HEAD 不是 `87ba8cb`，或工作区有他人未提交改动；
2. G: 空间不足 10GB；
3. 系统空闲虚拟内存低于 4GB（尤其准备跑 Test-Go/30 分钟时）；
4. `Test-Go.ps1` 三次中出现非零退出且不是可复现的环境 OOM；
5. 30 分钟跑批 `missing>0`、无 JSON、或 8 次重连耗尽；
6. Z3 找不到真实 EdgeCore/Chromium，或渲染服务启动失败；
7. 端口 3002/6060/18080 被未知进程占用；
8. 任何结果文件的 `finished_at` 不在本次运行窗口、`sent<=0`、或参数与命令不一致。

对 OOM 类失败：

- 记录完整日志、`Get-CimInstance Win32_OperatingSystem` 的空闲内存、当前进程；
- 可以用 `GOFLAGS=-p=1` 做诊断性替代，但在报告中必须写明“默认验证失败，替代验证有条件通过”；
- 不允许通过修改 `Test-Go.ps1` 超时/降低测试标准来“解决”OOM。

对 `missing>0`：

- 记录 `missing/expected` 百分比、断连 attempt、index、日志时间；
- 不在没有逐窗口证据时断言“全部由机器停滞造成”；
- 最多重试一次；两次失败即停，写 FAIL。

---

## 10. 最终报告模板（回复给用户）

最终回复必须包含以下内容，不能只写“已完成”：

1. **一句话结论**：阶段零是否收口；哪项 PASS/FAIL/BLOCKED。
2. **HEAD 与提交**：开工/收工 HEAD、每个新提交 hash、文件清单。
3. **当前 HEAD 验证**：
   - 默认三次 exit code、日志路径；
   - 如适用，内存受限替代验证；
   - 明确是否仍有 OOM/环境限制。
4. **Z1**：binary SHA256、profile manifest、200/600 requested/actual、CPU/heap/goroutine/block/mutex top 结论；旧 profile 的历史标注。
5. **Z2-A**：requested/actual/reported、sent/expected/paired/missing、p99/max、action_errors、store 错误/慢写、重连次数/上限、是否 PASS。
6. **Z2-B**：同上；actual 是否≥400；是否 PASS。
7. **Z3**：EdgeCore 路径、单次/并发/队列/失败降级、队列等待、渲染耗时、失败率、恢复次数、是否有泄漏。
8. **Z4**：默认 FULL 低速 30 分钟趋势；目标数值表和瓶颈结论；是否已写入 `plan.md` / `phase0-baseline.md`。
9. **仍未验证/风险**：真实外部业务插件、多账号、`-race`、M7 出站隔离、`#note` 权限等。
10. **工作区**：`git status --short` 摘要；证据是否已提交；未跟踪项列表。
11. **失败原因**：如果有，写清日志/参数/环境，不要用“环境问题”一句带过。

---

## 11. 最后再强调的禁止项

- 不把 `missing>0` 写成通过。
- 不把 `Test-Go` 的 OOM 写成代码失败；也不要把 OOM 说成通过。
- 不把 00:06 的旧 profile 当当前 HEAD。
- 不把 `actual_injection_eps`、`reported_eps`、`requested` 混为一个数。
- 不把 Z4-A 默认 FULL 低速通过外推为 200/s 目标通过。
- 不把 WAL+NORMAL 设为默认。
- 不用任意 HTML 测渲染；只测固定 `#render`。
- 不扩大 `maxLoadReconnects`、不改测试预算、不改生产默认来“修”失败。
- 不删除/覆盖旧证据；不用 `git reset --hard`、`git clean -fd`、`git checkout --`。
- 不进入阶段九/十/十一；不先做 `#note` 权限、M7 出站隔离。
- 不以“重跑一次通过”替代根因分析和失败记录。
- 不在多个基准之间并行；端口、磁盘和机器级停滞会污染结果。
