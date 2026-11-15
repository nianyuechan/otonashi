# 阶段零 CPU/内存剖析报告（Z1/Z2）

> **历史快照（2026-11-15 标注）**：本报告的 profile 采集自 2026-11-15 00:06 构建的二进制（Build ID 2026-11-15 00:06:53，采集 00:09–00:15），**不是当前 HEAD `87ba8cb` 的证据**；文中"01:07 重建"与实际二进制时间 01:13 矛盾，以 Build ID 为准。当前 HEAD 的剖析重采见 `phase0-profile-2026-11-15.md`。本报告 §2.4 的"见 §3"为悬空引用，Z2 稳定性结果现位于 `phase0-baseline.md` 的"2026-11-15 阶段零收口结果"一节；本文件正文数字不再修订，仅作历史对照。

- 测量日期：2026-11-14 夜 – 2026-11-15（会话跨午夜，报告按交接要求沿用 2026-11-14 命名）
- 被测：`cmd/yunzai-bench`（bench/env/go-yunzai，2026-11-15 01:07 重建，含 bench.store 写入插件开关）
- 剖析端点：`internal/observability` pprof（`YZGO_PPROF_ADDR=127.0.0.1:6060`，loopback-only）
- 负载：`reply` 工作负载、group 模式、100 群/2 万发送者轮转；速率档位按容量报告口径
- 原始 profile：`bench/env/profiles/`（cpu/heap/goroutine/block/mutex × 两档，`*-top.txt` 为 `go tool pprof -top` 摘要）

## 0. 速率档位口径

容量判定用 actual injection（事件首尾 sent_ns 差）。requested 250 → actual 216.7/s（200 档），requested 600 → actual 435.6/s（400/2× 档），与 `capacity-100-groups-200rps.md` 的实测一致。

## 1. Z1：合成 group 负载（不含存储写入）剖析结果

### 1.1 采样窗口与负载

| 档位 | requested | actual injection | 采样窗口 | 负载时长 | 结果 JSON |
|---|---:|---:|---|---:|---|
| 200 | 250 | 216.7/s | 00:11:04 起 30s（负载中段） | 180s | `go-reply-group-z1-250-profile.json` |
| 400 | 600 | 435.6/s | 00:13:19 起 30s（负载中段） | 180s | `go-reply-group-z1-600-profile.json` |

两档 sent=38998/78409、missing=0。block/mutex/heap/goroutine 快照取自 CPU 窗口结束后同一负载内的瞬时快照（heap 带 `?gc=1` 强制 GC 后采样）。

### 1.2 CPU profile（`go tool pprof -top`，30s 窗口）

| 指标 | 200 档 | 400 档 |
|---|---:|---:|
| 30s 窗口总采样 | 1.07s（单核 3.57%） | 2.68s（单核 8.93%） |
| 第一名 | `runtime.write1` 67.3% | `runtime.write1` 55.2% |
| 其余前列 | cgocall 7.5%（modernc SQLite 内部调用）、semawakeup、JSON 解码 | semawakeup 4.9%、cgocall 3.0%、JSON 编解码、regexp doOnePass |

结论：CPU 瓶颈是 WebSocket 套接字写入的系统调用（`write1`），两档都远未饱和（单核占用 <9%，机器 20 逻辑核）。`cgocall` 来自 modernc SQLite 的纯 Go libc 模拟调用，400 档也只占 3%。

### 1.3 内存 / GC / goroutine

| 指标 | 200 档 | 400 档 |
|---|---:|---:|
| heap inuse（GC 后） | 4.10MB | 3.59MB |
| 30s 窗口累计分配 | 90.3MB（~3MB/s） | 未单独采样（同量级） |
| alloc 主要来源 | `Manager.withPlugin` 62%（cum）：`io.ReadAll` 12.7%、`toSDKEvent` 10.5%、`DecodeEvent` 8.9%、`cloneData` 5.5% | 同左 |
| goroutine 数 | 9 | 9 |
| GC（gctrace，run.err） | 每 ~0.5s 一次、pause ≤0.6ms、GC CPU 0%、堆目标 4-6MB | 同左 |

结论：常驻内存 ~4-5MB 量级（RSS 峰值含运行时 ~20MB）；分配集中在每事件的 JSON 解码与 SDK 事件复制（预期行为，量级不构成瓶颈）；GC 压力可忽略（0% GC CPU）。

### 1.4 阻塞 / 互斥锁

| 指标 | 200 档 | 400 档 |
|---|---:|---:|
| block 累计（进程启动起） | 122.57s，100% 为 `runtime.selectgo` | 175.12s，100% 为 `runtime.selectgo` |
| block 构成 | websocket timeoutLoop 38.0%、pingLoop 32.8%、Run 3.1%、SendMessage 1.5% | timeoutLoop 43.1%、pingLoop 34.4%、Run/SendMessage ~5% |
| mutex 累计 | 5.56ms（GC 后台 worker 内部锁） | 50.03ms（GC 内部锁） |

结论：block 全部是 goroutine 在 select 上空闲等待（心跳/超时循环），**没有锁竞争、没有队列积压**；mutex 累计只有几十毫秒且都在 GC 内部。200-400 msg/s 下消息链路的瓶颈既不是 CPU、也不是锁、也不是队列。

### 1.5 离群值说明

600 档出现 2 个 >300ms 样本（937.0ms/380.6ms，seq 539/352），集中在负载启动后 ~1s 窗口（00:12:35-36），此后 78k 样本 max 1.9ms；剖析采集（00:13:19 起）不在此窗口。250 档的 12-17ms 样本集中在 00:09:34，与第一次 pprof 采集尝试的窗口重叠，属采集活动扰动。历史容量批次（无剖析）同档 max 1.02-3.65ms。两组离群值均不改变结论。

## 2. Z2：真实插件 + SQLite 逐消息写入

### 2.1 写入路径与键设计

`bench.store` 插件（`plugins/bench/store.go`，`-store` 开启）：每条消息一次 `Store.Set` + `Store.Get` 回读校验，键按账号/消息类型/群/用户隔离（`bench:a:<account>:t:<type>:g:<group>:u:<user>`），值由下一次写入覆盖，成功路径不回复。写入延迟与错误由插件日志输出（每 2000 条汇总 + >1s 慢写告警）。

### 2.2 默认持久性（synchronous=FULL + 回滚日志 + 单连接）在本机无法承载目标负载

**独立基准**（`artifacts-gocache/sqlite-diag`，顺序单连接、每提交独立计时、100 提交 × 2 轮）：

| 持久性组合 | G:（ST3500413AS，7200rpm 机械盘） | C:（PC SN520 NVMe） |
|---|---:|---:|
| FULL + 回滚日志（当前默认） | **8.9–9.7 提交/s**（p50 79-84ms，max 0.47-1.0s） | 288–301 提交/s |
| NORMAL + 回滚日志 | 16.6–17.3 提交/s | 350–373 提交/s |
| **WAL + NORMAL** | **~22,000–24,800 提交/s**（max ≤1ms） | ~18,000–20,000 提交/s |
| WAL + FULL | 33–34 提交/s | ~1,650–1,760 提交/s |

200 档需要 ~222 写/s（每消息一次 Set 提交）；FULL 在本机 G: 机械盘上只有 ~9 提交/s，差 24 倍。这是**存储 IO（机械盘 fsync）瓶颈**，与 Z1"消息链路无 CPU/锁瓶颈"的结论拼起来，瓶颈归属明确。

**端到端采证**（默认配置 requested 250，2026-11-15 00:16）：首批写入 elapsed 1.9-6.2s，~34s 后 256 个 dispatcher 槽全部占满（goroutine 266），读循环背压 → 回复 `SendMessage` 10s 超时 → `conn.CloseNow()`（client.go:322 写失败即关连接）→ 负载器推送失败、整轮中止（未产出结果 JSON）。附带机制采证：事件 ctx 无期限（eventCtx 仅 cancel），30s 量级超时全部来自 `SendMessage` 的 `request_timeout` 10s 与心跳 ping 的 10s。

**WAL/NORMAL 对照实验**（`YZGO_STORAGE_EXPERIMENT=wal-normal`，默认仍为 FULL，见提交 `1525a99`）：200 档 120s 校验 actual 217.2/s、missing=0、p99 0.71ms、CPU 7.7%、写入 avg 0.64ms/max 90ms（另一校验轮 avg 4.3ms/max 641ms——磁盘子系统波动，见 2.4）。

### 2.3 持久性取舍（WAL + NORMAL）

- 收益：提交成本从"每提交多次机械寻道"降为顺序追加 WAL（本机 ~2 万提交/s），200/400 档均有 40 倍以上余量。
- 代价：`synchronous(NORMAL)` 下进程崩溃/断电时，最后若干未 checkpoint 的事务可能丢失（WAL 文件内容回放依赖最后一次 fsync 点）；FULL 语义是每次提交都落盘。
- 边界：journal_mode(WAL) 启动切换需要短暂独占锁（交接轮 M3 已记录快速重启竞争），实验开关只用于单实例基准；WAL 自动 checkpoint（默认 1000 页 ≈ 4MB）在 30 分钟跑批中把 -wal 稳定在 ~4MB。
- **默认配置保持 FULL + 回滚日志不变**；是否切换默认值属于后续决策，需要结合"崩溃丢最后若干事务"的业务容忍度。

### 2.4 200 档 30 分钟跑批（WAL+NORMAL）

见 §3（Z4 稳定性窗口）：结果 JSON `go-reply-group-realwrite-200-wal-run*.json`、逐分钟窗口统计、goroutine/heap 采样、写入汇总与错误计数。

### 2.5 已知限制

- 机器级 IO 停滞（~30s，见 windows-env-quirks 与审计报告）在本轮再现实测 3 次（00:55、01:13 等，写入 avg 瞬间 155ms/max 3.1s）。停滞 ≥10s 会触发回复/心跳 10s 超时 → `conn.CloseNow()` → 连接断开；负载器已加固为等待重连续跑（`87ba8cb`），停滞窗口内已推送事件的回复会丢失并计入 missing。
- SQLite 错误在存储层统一为 `sdk.ErrStoreUnavailable`，busy_timeout 次数无法在插件层与其它 IO 错误区分；本轮以"store 错误总数 + 慢写日志"作为代理指标（单进程单连接无外部锁持有者时 busy 理论上应为 0，实测 store 错误均为 0，除 §4 记录的回读不匹配）。
