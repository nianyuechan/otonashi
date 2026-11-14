# 阶段零 CPU/内存剖析报告（当前 HEAD 重采，2026-11-15）

- 测量日期：2026-11-15 02:28–02:36（UTC+8）
- 被测：`cmd/yunzai-bench`，commit `87ba8cb`（`87ba8cb106a7929188bcc27bc6e974e528cd6718`）
- 二进制 SHA256：`yunzai-bench.exe` = `1d0402cfee212724c968d9e767f5cb5c2fc12480719f9992d7165d7e626083d9`；`onebot-bench.exe` = `90639e059e6b29f4cd1d3954951bfb33562a65be92e64166cb774fb16f4306cc`
- 构建时间：2026-11-15 02:28:15（250 档）/ 02:32:15（600 档，两档 SHA256 相同）
- 剖析端点：`internal/observability` pprof（`YZGO_PPROF_ADDR=127.0.0.1:6060`，loopback-only）
- 负载：`reply` 工作负载、group 模式、100 群/2 万发送者轮转、steady 180s；负载器 `onebot-bench` server 模式
- 采集脚本：`bench/scripts/Invoke-Z1Profile.ps1`（可重复：构建 → SHA256 记录 → 启动 bot/负载器 → 负载 60s 处抓取 cpu(30s)/heap(?gc=1)/goroutine/block/mutex → 负载结束后 `go tool pprof -top` 文本 → 精确 PID 停止 bot → 端口释放确认）
- 原始 profile 与 `-top` 文本：`bench/env/profiles/current-2026-11-15/`（含 `manifest.json`、逐档 `capture-<rate>.json`）
- 结果 JSON 与日志：`bench/env/results/go-reply-group-z1-current-250.json(.log/.log.err/.bot.log/.bot.err)`、`go-reply-group-z1-current-600.json(...)`（同名跟随，未覆盖任何历史文件）

> 历史关系：`phase0-profile-2026-11-14.md` 是 **00:06 构建二进制的历史快照**（其"01:07 重建"表述与实际 01:13 的二进制时间矛盾），不属于当前 HEAD 的证据，仅作对照保留；本报告取代它作为当前基线。旧报告 §2.4 所指的"Z4 稳定性窗口"结果现位于 `phase0-baseline.md` 的"2026-11-15 阶段零收口结果"一节与 `plan.md` §3 目标表（旧报告的"见 §3"为悬空引用，在此修正指向）。

## 0. 速率口径

容量判定用 actual injection（事件首尾 sent_ns 差，结果 JSON 自带 `actual_injection_eps`）。requested ≠ actual injection ≠ reported eps（reported 分母含 10s grace），三口径全程分列。

## 1. 采样窗口与负载

| 档位 | requested | actual injection | reported eps | sent | paired | missing | p99 | CPU 均值 | RSS 峰值 | 负载窗口（UTC+8） |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---|
| 200 target | 250 | **215.5/s** | 204.1 | 38784 | 19392 | **0** | 0.53ms | 1.2% | 20.9MB | 02:28:2x–02:31:3x |
| 400 target | 600 | **436.3/s** | 413.2 | 78525 | 39263 | **0** | 0.43ms | 1.9% | 21.2MB | 02:32:2x–02:35:3x |

CPU/heap/goroutine/block/mutex 抓取时刻：250 档 cpu 窗口 02:29:20–02:29:50（负载开始后约 60–90s，中段）；600 档 cpu 窗口 02:33:20–02:33:50。block/mutex/heap/goroutine 为 cpu 窗口结束后同一负载内的瞬时快照（heap 带 `?gc=1` 强制 GC 后采样）。

## 2. CPU profile（30s 窗口，`go tool pprof -top`）

| 指标 | 200 target（requested 250） | 400 target（requested 600） |
|---|---|---|
| 30s 窗口总采样 | 0.50s（单核 1.67%） | 1.23s（单核 4.10%） |
| 第一名 | `runtime.semawakeup` 12.0% | `runtime.semawakeup` 10.57% |
| 其余前列 | `runtime.cgocall` 10.0%（modernc SQLite libc 模拟调用路径常驻，本负载无存储写入时来自运行时/网络栈）、`websocket reader` cum 20%、JSON 解码 cum ~18% | `runtime.cgocall` 9.76%、`json makeStructArshaler` cum 19.51%、JSON/jsontext 解码若干条目 |

结论：两档单核占用分别 1.67% / 4.10%（机器 20 逻辑核，整机 CPU 均值 1.2% / 1.9%），远未饱和；热点是 JSON 编解码与网络读写路径的运行时调度（semawakeup/cgocall），没有单一业务函数成为瓶颈。

与历史 00:06 profile 的差异（如实记录，不做根因推断）：旧 200 档 30s 窗口总采样 1.07s（3.57%）、第一名 `runtime.write1` 67.3%；本轮同档 0.50s（1.67%）、无 `write1` 进前 25。bot 写路径相关代码在两轮之间未变（其间提交为负载器重连、事件解码 json 标签与 bench.store 键隔离），差异只能归因于采样窗口与机器状态的波动（旧窗口内套接字写更密集或单次 write 更慢），现有证据不足以进一步归因。

## 3. 内存 / GC / goroutine

| 指标 | 200 target | 400 target |
|---|---|---|
| heap inuse（GC 后） | 2.05MB（mallocgc 75% + `cancelCtx.Done` 25%，后者为 SQLite `connectionOpener` 常驻） | 2.56MB（mallocgc 60%、`sync.Pool.pinSlow` 20%、`cancelCtx` 20%） |
| 进程启动至采样点累计分配 | 150.51MB（约 1.7MB/s @ 实际注入 215.5/s） | 304.81MB（约 3.4MB/s @ 436.3/s） |
| alloc 主要来源（cum） | `Manager.withPlugin` 57.15%（内含 `toSDKEvent` 19.94%、`DecodeEvent` 14.29%、`io.ReadAll` 12.96%、`cloneData` 7.64%、`SendMessage` 17.28%） | `io.ReadAll` 11.98%、`toSDKEvent` 16.08%、`DecodeEvent` 14.60%、`propagateCancel` 10.50%、`cloneData` 7.38% |
| goroutine 数 | 9 | 9 |
| GC（gctrace/结果日志） | 未单独采集本轮 gctrace；GC CPU 占比由 mutex profile 间接印证（见 §4，仅运行时内部） | 同左 |

结论：常驻 heap 2-2.6MB（RSS 峰值含运行时 ~21MB）；分配集中在每事件的 JSON 解码与 SDK 事件复制（预期行为）；400 target（436/s）分配速率约为 200 target 的 2 倍，量级仍不构成瓶颈。goroutine 数恒定 9，无泄漏迹象。

## 4. 阻塞 / 互斥锁

| 指标 | 200 target | 400 target |
|---|---|---|
| block 累计（进程启动起） | 203.91s，100% 为 `runtime.selectgo` | 205.22s，100% 为 `runtime.selectgo` |
| block 构成 | websocket `timeoutLoop` 42.54%、`pingLoop` 39.40%、`Run` 1.95%、`SendMessage` 1.33% | `timeoutLoop` 41.85%、`pingLoop` 39.15%、`Run` 2.09%、`SendMessage` 2.22% |
| mutex 累计 | 24.99ms，全部为 `runtime.unlock`（GC 后台 worker/scheduler 内部） | 277.59ms，全部为 `runtime.unlock`（GC/scheduler 内部） |

结论：block 全部是 goroutine 在 select 上空闲等待（心跳/超时循环），没有锁竞争、没有队列积压；mutex 累计只有几十至几百毫秒且都在运行时内部。200/400 msg/s 下消息链路的瓶颈既不是 CPU、也不是锁、也不是队列——与 `phase0-profile-2026-11-14.md` 的历史结论方向一致，且本轮数据属于当前 HEAD。

## 5. 限制

- 本剖析负载为 `reply` 工作负载（31 条基准规则），不含存储写入（`-store` 未开启）与渲染；存储写入链路的成本见 `phase0-baseline.md`"2026-11-15 阶段零收口结果"一节（Z2），渲染链路单独报告（Z3）。
- block/mutex 是进程启动起的累计值（含约 60-90s 的连接与预热期），绝对值只用于构成判断，不代表单位时间竞争强度。
- `go test -race` 仍未运行（环境 `CGO_ENABLED=0`，缺 C 编译器）。
- CPU 采样窗口 30s，位于负载中段单点；不同窗口的热点构成会随机器状态波动（见 §2 与历史 profile 的差异记录）。

## 6. 复现

```powershell
& .\bench\scripts\Invoke-Z1Profile.ps1 -Rate 250 -OutDir bench\env\profiles\<新目录名>
& .\bench\scripts\Invoke-Z1Profile.ps1 -Rate 600 -OutDir bench\env\profiles\<新目录名>
# manifest.json 由两档 capture-<rate>.json 汇总（commit/SHA256/构建时间/逐档 actual）
```

脚本内置防覆盖（目标文件已存在即中止）、pprof 就绪探测、结果 `finished_at` 时间窗校验、精确 PID 停止与端口释放确认；构建前检查空闲虚拟内存 ≥1.5GB。
