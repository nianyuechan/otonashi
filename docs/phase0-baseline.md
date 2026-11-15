# 阶段零基线报告：TRSS-Yunzai 旧版（Node）vs go-yunzai（Go）消息链路

- 测量日期：2026-11-14
- 工作负载定义版本：1（`bench.ProtocolVersion` / `plugins/bench` Version，两侧一致）
- 状态：首次可重复基线，单机离线合成负载；非真实流量
- 修订（2026-11-14 审计后重采）：稳态回复延迟先后用 `time.Since` 和 Windows `QueryPerformanceCounter`（QPC）复测。`time.Since` 两轮仍有 362/330 个配对样本记为 `0ms`；改用 QPC 后两轮均无 `0ms` 样本，`latency_count == paired_replies == 593`。QPC 下 p50 0.42/0.41ms、p95 0.83/0.86ms、p99 1.33/1.36ms。原"稳态 p50 约 1/11"撤下，改为 p50 约 1/7、p95 约 1/6、p99 约 1/27–1/47。突发 250/250 全样本数字保留。
- 修订 2（同批次重采，19:34–19:39）：旧版基准副本 node_modules 修复后首次成功重跑，两侧在同一次进程批次内完成稳态与突发对照（见 4.4）。此前的"旧版沿用 15:53 原始 JSON、非同批对照"限制已消除。

## 1. 一句话结论

在同一台机器、同一合成负载、同一插件规则集下，**500 条突发**场景 Go 版端到端回复延迟 p50 约为旧版的 **1/139–1/166**（第一批 4.20ms vs 696.55ms；同批次重采 4.94–5.31ms vs 719.86–736.57ms），CPU 占用约 **1/11–1/12**（突发），常驻内存峰值约 **1/7**。**稳态回复延迟**在改用 QPC 计时、全样本后，p50 约 **1/7–1/8**（0.41–0.42ms vs 2.90–2.94ms；同批次 0.397–0.398ms vs 3.159ms），p95 约 **1/6–1/9**（0.83–0.86ms vs 4.81–5.57ms；同批次 0.637–0.672ms vs 5.728ms），p99 约 **1/27–1/47**（第一批；同批次 1.159–1.361ms vs 43.61ms 约 1/32–1/38）。扫描稳态 CPU 约 1/23（0.15% vs 3.45%）。上述数字仍不是"迁移完成"结论——旧版承载了 Go 版尚未实现的完整 loader 行为（Redis 计数、黑白名单、Runtime.init 等），且渲染、群聊、真实协议端均未纳入。

## 2. 测量口径

- **端到端回复延迟**：负载器通过 WebSocket 推送消息事件，到收到被测机器人回复动作（`send_msg`）的间隔。不含负载器应答 echo 的往返；不含真实协议端/网络传输。
- **配对样本**：约半数消息携带唯一序号（`bench <seq>` 规则，回复 `[bench] <seq>`），可精确配对；`#ping`/`#help` 的固定文本回复计入 unpaired 计数，不进延迟分位数。
- **吞吐 eps**：`sent / 总墙钟`（含推送结束后的 grace 等待），仅作参考，非饱和吞吐。
- **CPU/RSS**：gopsutil 对被测进程 1 秒间隔采样；CPU 百分比按单核 100% 计。
- **扫描（scan）负载**：全部消息不命中任何规则，只测量纯扫描成本，无延迟样本。
- **零延迟与计时口径**：旧统计代码按 `LatencyMS > 0` 过滤，修订前 Go 两轮只有 246/253 个样本进入分位数（配对均为 593）。2026-11-14 修复后，负载器把所有 `ReplyNS > 0` 的配对样本纳入分位数；Windows 上计时改用 `QueryPerformanceCounter`（QPC），其他平台回退到 `time.Since`/`UnixNano`。QPC 重采两轮 `latency_count == paired_replies == 593`、`0ms` 样本 0 个；`time.Since` 中间版本仍有 362/330 个 `0ms` 样本，已作为失败口径保留，不用于结论。

## 3. 固定环境与工作负载

| 项 | 内容 |
|---|---|
| 机器 | 同一台 Windows 主机（win32 10.0.19045 x64），离线回环 |
| Node | v24.13.0（系统 nodejs）；Go go1.27.4 windows/amd64 |
| Redis | miniredis（内存实现，进程 `bench/env/mini-redis.exe`，127.0.0.1:6379） |
| 旧版 | TRSS-Yunzai 3.1.3 基准副本（`bench/env/old-yunzai`），仅加载 adapter + bench 插件，file_watch 关闭 |
| Go 版 | `cmd/yunzai-bench`（`bench/env/go-yunzai`），bench.workload 插件，内置调试 catch-all 停用，SQLite 存储 |
| 插件规则集 | 31 条规则（`^#ping$`、`^#help$`、`^bench (\d+)$` 命中 + 28 条真实风格扫描规则），priority 50，顺序两侧一致 |
| 消息样本 | reply 负载按固定比例轮转 seq/ping/help；scan 负载轮转 12 条不命中闲聊文本；全部私聊 |
| 场景 | steady：20 条/秒 × 60 秒；burst：500 条尽快推送 |

## 4. 结果（每场景两轮，先 run1 后 run2）

### 4.1 稳定负载 · 回复（20 msg/s，60s，约 1186 条）

| 指标 | Go run1（QPC） | Go run2（QPC） | 旧版 run1 | 旧版 run2 |
|---|---:|---:|---:|---:|
| 配对回复 | 593 | 593 | 594 | 594 |
| 进入分位数样本（`latency_count`） | 593 | 593 | 594 | 594 |
| 其中 `0ms` 样本 | 0 | 0 | 0 | 0 |
| 丢失回复 | 0 | 0 | 0 | 0 |
| p50 (ms) | 0.42 | 0.41 | 2.90 | 2.94 |
| p95 (ms) | 0.83 | 0.86 | 4.81 | 5.57 |
| p99 (ms) | 1.33 | 1.36 | 62.68 | 37.05 |
| max (ms) | 1.58 | 1.60 | 274.28 | 80.30 |
| CPU 均值 (%) | 0.3 | 0.0 | 4.9 | 5.0 |
| RSS 峰值 (MB) | 19.4 | 13.9 | 127.2 | 131.2 |

**计时口径已修正**：修订前 Go run1/run2 分别有 347/340 个 `0ms` 样本被 `LatencyMS > 0` 排除，`latency_count` 只有 246/253。改用 QPC 重采后两轮均为 593/593、无 `0ms` 样本；表中 Go 分位数可引用。中间版本 `time.Since` 仍有 362/330 个 `0ms` 样本，不作为结论。CPU/RSS 采样不受该过滤影响。

原始文件：`go-reply-steady-qpc-run1.json`、`go-reply-steady-qpc-run2.json`；旧版沿用 `old-reply-steady-run1.json`、`old-reply-steady-run2.json`。

### 4.2 突发负载 · 回复（500 条尽快推送）

| 指标 | Go run1 | Go run2 | 旧版 run1 | 旧版 run2 |
|---|---:|---:|---:|---:|
| 配对回复 / 进入分位数 | 250 / 250 | 250 / 250 | 250 / 250 | 250 / 250 |
| p50 (ms) | 4.57 | 4.20 | 870.79 | 696.55 |
| p95 (ms) | 5.90 | 5.80 | 1058.46 | 875.65 |
| p99 (ms) | 6.46 | 5.90 | 1066.69 | 883.41 |
| CPU 均值 (%) | 0.6 | 0.6 | 6.4 | 7.2 |
| RSS 峰值 (MB) | 18.6 | 18.3 | 144.1 | 141.2 |

旧版突发下出现秒级排队（事件到达后约 0.7-1.1 秒才发出回复动作），两轮量级一致。Go 版突发 p50 仅比稳态高一个数量级以内。

### 4.3 稳定负载 · 扫描（20 msg/s，60s，约 1185 条，无回复）

| 指标 | Go | 旧版 |
|---|---:|---:|
| CPU 均值 (%) | 0.15 | 3.45 |
| RSS 峰值 (MB) | 17.5 | 124.1 |

纯匹配扫描的成本差距与回复负载一致：旧版每条消息的全链路（日志、Redis 计数、Runtime.init、规则扫描）成本显著更高。

群聊容量另测：100 群、2 万发送者轮转、200 msg/s 目标已实测到 221 eps、0 丢失、p99 0.47ms，见 `capacity-100-groups-200rps.md`。该测试只覆盖基准规则，不含存储写入与渲染。

### 4.4 同批次重采（2026-11-14 19:34–19:39，旧版修复后首轮）

旧版基准副本 node_modules 修复后，两侧在同一时间窗口内重采稳态与突发，样本口径与第一批一致（全部配对样本进入分位数、无 `0ms`、无丢失）：

| 指标 | Go 稳态 run1 | Go 稳态 run2 | 旧版稳态 run1 | Go 突发 run1 | Go 突发 run2 | 旧版突发 run1 | 旧版突发 run2 |
|---|---:|---:|---:|---:|---:|---:|---:|
| 配对 / 进入分位数 | 593/593 | 593/593 | 594/594 | 250/250 | 250/250 | 250/250 | 250/250 |
| 其中 `0ms` 样本 | 0 | 0 | 0 | 0 | 0 | 0 | 0 |
| p50 (ms) | 0.397 | 0.398 | 3.159 | 4.944 | 5.308 | 719.86 | 736.57 |
| p95 (ms) | 0.672 | 0.637 | 5.728 | 6.027 | 6.115 | 1022.68 | 919.73 |
| p99 (ms) | 1.159 | 1.361 | 43.61 | 6.167 | 6.249 | 1035.00 | 927.42 |
| CPU 均值 (%) | 0.32 | 0.24 | 5.23 | 0.96 | 0.36 | 6.12 | 6.90 |
| RSS 峰值 (MB) | 18.2 | 17.7 | 121.1 | 18.9 | 19.1 | 142.0 | 139.7 |

原始文件：`go-reply-steady-qpc2-run1.json`、`go-reply-steady-qpc2-run2.json`、`old-reply-steady-qpc-run1.json`、`go-reply-burst-qpc-run1.json`、`go-reply-burst-qpc-run2.json`、`old-reply-burst-qpc-run1.json`、`old-reply-burst-qpc-run2.json`。

同批次结论与第一批一致：旧版稳态 p50 3.16ms / p99 43.6ms（第一批 2.90–2.94 / 37.05–62.68ms），突发 p50 约 720–737ms（第一批 696–871ms）；Go 稳态 p50 0.397–0.398ms（第一批 0.41–0.42ms），突发 p50 4.9–5.3ms（第一批 4.20–4.57ms）。按同批次数字，稳态 p50 约 1/8（3.159/0.397）、p99 约 1/32–1/38；突发 p50 约 1/139–1/146。第一批的倍数结论方向不变，两批差异在单机波动范围内。

## 5. 初步解读（待更多场景验证）

1. **旧版尾延迟是明确短板**。旧版稳态 p99 37-63ms、突发直接到秒级（250/250 全样本；同批次重采 p99 43.61ms、突发 p50 约 720-737ms），与 2026-11-12 静态分析中"单条事件内部串行 await（Runtime.init → accept → 逐插件逐规则）+ 无全局队列"的结构推断吻合。Go 版 QPC 重采后稳态 p50/P95/P99 为 0.41–0.42/0.83–0.86/1.33–1.36ms，均可引用；突发 p99 5.90–6.46ms（同批次 6.17–6.25ms）。
2. **Go 版稳态成本极低**（20 msg/s 下 CPU 约 0.0–0.3%），当前瓶颈不在这条链路；优化重点应转向功能完备性与渲染链路，而不是继续压消息循环。
3. **内存**：旧版 ~130MB 常驻（Node + 插件运行时），Go 版峰值 ~14–19MB。PM2 的 512M 重启阈值对旧版意味着 ~4 倍余量，对 Go 版几乎不构成约束。
4. 上述倍数**不是迁移收益的最终结论**：旧版本次承载了更完整的行为（Redis 计数、黑白名单、消息统计），Go 版尚未实现；这些差异方向上对 Go 有利，但 mini-redis 的 μs 级 INCR 成本远不足以解释突发下的百倍级延迟差。

## 6. 限制与未覆盖项

- 未测：Chromium 渲染链路（与消息链路分开是计划要求，渲染基准待做）、多账号、真实协议端、长时稳定性（内存增长斜率）、`-race`（环境缺 C 编译器）。群聊容量已在 100 群/2 万发送者轮转下验证到 221 eps，但 100 个独立群状态、群冷却和文本去重仍未测。
- miniredis 不是生产 Redis/Valkey；旧版生产环境 Redis 行为（持久化、网络往返）未复现。
- 单次会话机器负载（其他进程）未做隔离控制；两轮重复一致性降低了偶然性但未消除。
- 旧版本轮未能重采的限制已消除：同批次重采（4.4）在 19:34–19:39 完成两侧对照，第一批 15:53 的原始 JSON 仍保留为历史证据。重采时旧版基准副本的 node_modules 已经过修复（见 `bench/scripts/repair-node-modules.mjs` 头注），其原始安装自 D:→G: 迁移后 pnpm junction 被解引用、无法启动；修复只新增文件/链接，未改动原安装内容。
- 工作负载不包含真实业务插件的耗时分布；插件级（如 miao-plugin 渲染命令）基线需阶段七迁移时单独建立。

## 7. 复现步骤

```powershell
# 1) 构建两个基准入口（Go 工具链在 G:\ALL WORK\音无\tools\go）
& G:\ALL WORK\音无\音无\scripts\Test-Go.ps1   # 三个模块 fmt/test/build/vet 验证

# 2) 旧版基准副本（首次）：junction node_modules/renderers、复制 lib/config、
#    注入 bench 插件，然后修复 node_modules 解析
powershell -File bench\scripts\New-OldYunzaiBench.ps1
node bench\scripts\repair-node-modules.mjs "G:\ALL WORK\音无\音无\mysy2\mysy2\home\SERVER2\TRSS_AllBot\TRSS-Yunzai" "G:\ALL WORK\音无\音无\bench\env\old-yunzai"

# 3) 单场景（自动冷启动被测机器人 + 资源采样 + 结果 JSON）
powershell -File bench\scripts\Invoke-BenchScenario.ps1 -Variant go   -Workload reply -Scenario steady -Rate 20 -DurationSec 60
powershell -File bench\scripts\Invoke-BenchScenario.ps1 -Variant old  -Workload reply -Scenario steady -Rate 20 -DurationSec 60
powershell -File bench\scripts\Invoke-BenchScenario.ps1 -Variant go   -Workload reply -Scenario burst  -Count 500
powershell -File bench\scripts\Invoke-BenchScenario.ps1 -Variant old  -Workload reply -Scenario burst  -Count 500
# scan 变体：-Workload scan（无延迟样本，看 CPU/RSS）
```

原始结果（含逐事件记录与采样序列）在 `bench/env/results/*.json`；run1/run2 为保留的重复轮。

## 8. 阶段零后续项

- [x] 渲染链路基线（2026-11-15 Z3 完成，见 §9.4）
- [ ] 群聊路径基线（等 Go 版实现 groupCD/文本去重对齐后补）
- [x] CPU/内存剖析（2026-11-15 Z1 当前 HEAD 重采，见 `phase0-profile-2026-11-15.md`）
- [x] 依据本基线设定目标值与不回退门槛（2026-11-15 收口，见 plan.md §3"阶段零目标/回退门槛"表）
- [ ] 长时稳定性窗口（≥6 小时，观察 RSS 斜率与 GC；2026-11-15 收口轮最长窗口 30 分钟，见 §9.3 限制）

## 9. 2026-11-15 阶段零收口结果

本轮在 HEAD `87ba8cb` 上补齐阶段零剩余测量；速率口径全程三分（requested / actual_injection / reported eps）。所有新结果文件名跟随 handoff §6.3 约定，未覆盖任何历史证据。

### 9.1 当前 HEAD 统一验证（P0-1）

| 系列 | 窗口（UTC+8） | 空闲虚拟内存 | 结果 |
|---|---|---|---|
| 默认三次（testgo-head-run1..3） | 02:18–02:19 | 1.58→0.12GB（commit 28.3/29.8GB 贴限） | **3× exit=1，链接器提交限额 OOM**（run2 显式 `link.exe: exit status 0xc0000017`；run1/3 同类，日志留档 artifacts-gocache） |
| `-p=1` 备用四次（testgo-head-p1-run1..4） | 02:20–02:24 | 1.84→4.1GB（期间系统释放约 2.5GB） | pass / **停滞类超时**（TestSQLiteConcurrentWrites 120s 包级超时，goroutine dump 显示单连接池等待+阻塞 fsync，无死锁模式）/ pass / pass |
| 默认三次重试（testgo-head2-run1..3） | 02:37–02:38 | ~4.25GB | **3× exit=0（29s/29s/26s），默认验收满足** |

结论：代码验证通过（默认连续三次 exit 0）；02:18 窗口的三次失败为环境提交限额 OOM，不是测试断言失败；`-p=1` 系列中的停滞超时用 dump 留证并被前后两轮通过括住。

### 9.2 Z1：当前 HEAD CPU/内存剖析（详见 `phase0-profile-2026-11-15.md`）

- 二进制：`yunzai-bench.exe` SHA256 `1d0402cf…d7e626083d9`、`onebot-bench.exe` `90639e05…16f4306cc`，构建自 `87ba8cb`；manifest 与五类 profile（cpu/heap/goroutine/block/mutex × 250/600 两档）在 `bench/env/profiles/current-2026-11-15/`。
- 250 档（actual 215.5/s）：30s CPU 窗口单核 1.67%，p99 0.53ms，missing=0；600 档（actual 436.3/s）：4.10%，p99 0.43ms，missing=0。
- 结论：block 100% 为空闲等待（selectgo：timeoutLoop/pingLoop），mutex 仅运行时内部几十至几百毫秒；heap inuse 2–2.6MB，goroutine 恒 9。**200–400 msg/s 消息链路无 CPU/锁/队列瓶颈**；分配集中在每事件 JSON 解码与 SDK 事件复制（`Manager.withPlugin` cum 57%）。

### 9.3 Z2：真实插件 + SQLite 逐消息写入（WAL+NORMAL 实验开关，默认仍 FULL）

`bench.store` 每消息 Set+Get、键按账号/群/用户隔离。共有四轮运行进入留存证据：200 target 30 分钟 run2/run3/run4 与 400 target 15 分钟 run1；全部 FAIL 于 `missing_replies>0`（硬标准）。run2/run3 的 `action_errors=0`，run4 与 400target 各为 1；run4、400target 同时不满足 `action_errors=0`，且当前没有事件级根因明细。下表的 store 错误、平均写入延迟和慢写计数来自历史报告，但对应 bot stderr 未留存，**无法从现存原始数据复核**；在补采前只作为历史记录，不作为排除存储瓶颈的依据。

| 运行 | 时长 | requested/actual | sent | expected | paired | **missing** | **action_errors** | p99 | max | CPU | RSS 峰值 | 断连 | 慢写(>1s)* | 结果 |
|---|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---|---:|---|
| 200 target run2（30 分钟） | 1800s | 250 / 209.7/s | 377380 | 188690 | 174835 | **13855（7.34%）** | **0** | 1.31ms | 29,407ms | 7.7% | 39.9MB | 7/8（9 窗口） | —（无 stderr） | **FAIL** |
| 200 target run3（30 分钟） | 1800s | 250 / 215.3/s | 387530 | 193765 | 191680 | **2085（1.08%）** | **0** | 0.56ms | 12,302ms | 6.5% | 38.1MB | 1/8（03:00:07） | 865* | **FAIL** |
| 200 target run4（30 分钟） | 1800s | 250 / 215.6/s | 388088 | 194044 | 191640 | **2404（1.24%）** | **1** | 0.51ms | 22,816ms | 6.3% | 36.3MB | 1/8（03:42:59） | 398* | **FAIL** |
| 400 target run1（15 分钟） | 900s | 600 / 431.7/s | 388525 | 194263 | 188738 | **5525（2.84%）** | **1** | 0.50ms | 11,227ms | 7.2% | 38.5MB | 2/8（03:58:40、04:00:57） | 575* | **FAIL** |

\* 慢写计数来自历史报告，对应 bot stderr 未留存，待补采复核。run2 的缺失占 expected 7.343%、占 sent 3.671%。

逐窗口证据（missing 与断连时间逐秒对应，缺失全部集中在停滞窗口内已推送的事件）：

- run3：02:59:52.084–03:00:07.250（15.2s，1640 条）↔ 03:00:07 断连；03:10:15.176–03:10:19.276（445 条）↔ 尾部 12.3s 停滞吃掉 grace。
- run4：03:42:37.583–03:42:59.847（22.3s，2403 条）↔ 03:42:59.856 断连；另 1 条孤立。
- 400target：03:58:26.745–03:58:40.231（13.5s，2922 条）↔ 03:58:40.236 断连；04:00:45.666–04:00:57.516（11.9s，2602 条）↔ 04:00:57.521 断连。
- run2：01:26:44.832–01:53:47.578 共 9 个丢失窗口；`go-reply-group-realwrite-200-wal-run2.json.log.err` 记录 7 次重连尝试、8 条读循环提前结束。

机制与 `phase0-profile-2026-11-14.md` §2.5 记录一致：**机器级 IO 停滞 ≥10s → 回复/心跳 10s 超时 → `conn.CloseNow()` 连接断开 → 停滞窗口内已推送事件的回复丢失并计入 missing**；负载器重连续跑（`87ba8cb`）使负载在断连后恢复，但窗口内回复不可恢复。当晚停滞频发的环境背景：系统提交限额 28.3/29.8GB 贴限（16GB 物理内存 + 14GB 页面文件），本轮 Z2 四轮运行均出现断连/丢失窗口；200 target 的三轮 30 分钟（run2/run3/run4）均失败后按 handoff 停止重试。

写入路径本身（WAL+NORMAL）：历史报告记录 store 错误 0、逐 2000 条 avg 4.5–21ms（含 WAL 自动 checkpoint 停顿，单次 max 0.28–0.62s）、`-wal` 文件稳定在 ~4MB；但对应 bot stderr 未留存，上述数字无法复核，暂不作为阶段零结论的直接证据。

### 9.4 Z3：固定页面真实 Chromium/EdgeCore 渲染（PASS）

EdgeCore 157：`C:\Program Files (x86)\Microsoft\EdgeCore\157.0.4341.46\msedge.exe`；go-render 监听 127.0.0.1:18080；bot 用 `cmd/yunzai` 构建（SHA256 `dd7978d7…ed597`，`yunzai-bench` 入口硬编码 `DisableBuiltinDebug=true` 不注册 `#render`，详见 §9.7）；配置由仓库模板 `bench/configs/config-render.yaml` 复制。

| 阶段 | go-render 参数 | 注入 | 结果 | 证据 |
|---|---|---|---|---|
| 单次 | concurrency 1 / queue 1 | 1/s × 60s（60 条） | **60/60 图片段成功**（text_replies=0、action_errors=0）；单次渲染 avg 0.52s、全部 ≤1s；浏览器启动 1 次 | `go-render-single-run1.json` + `render-metrics-single.txt`（success=60） |
| 并发/队列 | concurrency 2 / queue 2 | 8/s × 60s（478 条） | **success=234 + queue_full=244**（429 瞬时拒绝，累计 3.6ms）；bot 对应 234 图片 + **244 条降级文本**（1:1）；action_errors=0 | `go-render-concurrent-run1.json` + `render-metrics-concurrent.txt` |
| 失败降级 | go-render 已停止 | 1/s × 10s（10 条） | **10/10 降级文本**"图片暂时不可用：渲染服务暂不可用，请稍后重试。"；无崩溃；**bot 主进程 goroutine 总数 9**（该转储无任何 go-render 栈帧，渲染器有界性未验证） | `go-render-fallback-run1.json` + `goroutine-fallback-midrun.txt` |
| 恢复 | 重启 go-render（1/1） | 1/s × 30s（30 条） | **30/30 图片段成功**，渲染链路自动恢复 | `go-render-recovery-run1.json` + `render-metrics-recovery.txt` |

说明：`goroutine-fallback-midrun.txt` 的 9 个栈全部属于 go-yunzai 主进程；go-render 未暴露 pprof/goroutine 端点，渲染器有界性未验证。不能把该证据写成“go-render 无泄漏”。

渲染延迟单独成表，不与消息核心 p99 混算：固定页面单次 0.52s（并发 2 时 avg 0.97s）；降级文本即时返回。

### 9.5 Z4-A：默认 FULL 低速 30 分钟窗口（PASS，仅低速基线）

`go-reply-group-default-full-low-30m-run1.json`（04:20:47–04:51:02，1800s，无实验开关，`-store` 开启）：

| 指标 | 值 |
|---|---|
| requested / actual / reported | 10 / **9.93/s** / 9.88（requested≈actual：FULL 容量上限 ~9–11 提交/s 恰好承接 10/s） |
| sent / expected / paired / **missing** | 17882 / 8941 / 8941 / **0** |
| action_errors / store 错误 / 慢写(>1s) | 0 / 0 / 11 |
| p50 / p95 / p99 / max | 0.39 / 0.53 / 1.16 / 3.97ms |
| CPU 均值 / RSS 峰值 | 0.84% / 26.5MB |
| 断连 | 0 |

结论：**默认 FULL 在 10 msg/s 低速下可以无损承接 30 分钟**（本机 G: 机械盘 FULL 容量 ~9–11 提交/s，actual 9.93/s 已贴上限）。这**不是**"默认 FULL 可承载 200/s"的证据——200 target 下 FULL 会积压到 dispatcher 占满、连接断开（`phase0-profile-2026-11-14.md` §2.2 已采证，本轮 Z2 用 WAL+NORMAL 实验）。

### 9.6 SQLite 独立基准原始输出（2026-11-15 重跑）

源码入库 `bench/tools/sqlite-diag/`（原 `artifacts-gocache/sqlite-diag`，gofmt 修正）；原始输出 `bench/env/results/sqlite-diag-2026-11-15.txt`（100 提交 × 4 持久性组合 × G:/C: × 2 轮，顺序单连接）：

| 组合 | G:（机械盘） | C:（NVMe） |
|---|---:|---:|
| FULL + 回滚日志（当前默认） | **9.3–11.8 提交/s**（p50 72–84ms） | 298.8–314.4 提交/s |
| NORMAL + 回滚日志 | 15.1；0.8（round2 命中 118.8s 停滞，max=118,790ms——本轮现场再次捕获机器级停滞） | 378.9–390.4 提交/s |
| **WAL + NORMAL**（Z2 实验开关） | **22,411–28,708 提交/s** | 20,445–25,003 提交/s |
| WAL + FULL | 24.9–32.8 提交/s | 1,577.7–1,681.3 提交/s |

与历史结论（FULL ~8.9–9.7、WAL+NORMAL ~2.2–2.5 万）一致；本轮再次确认"默认 FULL 无法承载 200 msg/s（需 ~222 写/s），差距 20 倍以上"。

### 9.7 本轮记录的实现事实与脚本改动

- **`cmd/yunzai-bench` 入口硬编码 `DisableBuiltinDebug: true`**：基准入口永远不注册内置调试插件，`#render` 规则不存在——Z3 必须用 `cmd/yunzai`（尊重 `debug.enabled`）承载渲染负载。为此 `Invoke-BenchScenario.ps1` 新增 `-BotExecutable` 参数（默认不变，仍 yunzai-bench.exe），渲染轮传 `yunzai.exe`。
- `Invoke-BenchScenario.ps1`：`-Workload` 增加 `render`；`render` 轮必须显式提供非默认的 `-BotExecutable`，并强制追加 `-config config-render.yaml`；结果校验增加 `action_count>0` 与 `action_errors=0`，防止零动作轮被 `sent>0` 误报成功。
- `bench/internal/loadgen/config.go`：`-workload` 帮助文本同步（reply、scan 或 render）；消息生成/指标/重连语义未动。
- 新增 `bench/scripts/Invoke-Z1Profile.ps1`（Z1 单档剖析采集：构建+SHA256 记录+五类 profile+pprof top+capture；防覆盖/端口检查/失败也回收进程）；新增 `bench/scripts/New-Z1Manifest.ps1` 从两档 capture 生成 manifest。
- 全部为测量/脚本/文档改动，无生产语义修改；`go test` 全量通过（§9.1 默认三次）。

### 9.8 阶段零收口限制

- Z2 200 target 30 分钟 `missing=0` 硬标准未达成：200 target 三轮（run2/run3/run4）均失败，其中 run2 是最差一轮（missing 13855，7.34%；9 个丢失窗口、7 次重连）。现有证据可把缺失与断连窗口逐秒对齐，但**不足以排除或确认存储写入**：存储侧原始 stderr 未留存，store 错误/avg/慢写无法复核。**机器级 IO 停滞**是当前最有力的方向性解释，但未完全闭合；在受控内存环境（commit 余量 ≥8GB）下的复测尚未进行，列为后续项。
- Z2 run4 与 400target 各有 `action_errors=1`：这是独立于 missing 的未达标项；当前没有事件级明细，不能把它解释为“错误全 0”。
- `-race` 仍未运行（CGO_ENABLED=0，缺 C 编译器）。
- 渲染仅覆盖固定页面，不含任意 HTML/真实模板；单浏览器实例、未测崩溃恢复（仅测进程停止后的降级与重启恢复）。
- 长时稳定性仅 30 分钟窗口，≥6 小时 RSS 斜率窗口未做。
