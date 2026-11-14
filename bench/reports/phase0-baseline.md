# 阶段零基线报告：TRSS-Yunzai 旧版（Node）vs go-yunzai（Go）消息链路

- 测量日期：2026-11-14
- 工作负载定义版本：1（`bench.ProtocolVersion` / `plugins/bench` Version，两侧一致）
- 状态：首次可重复基线，单机离线合成负载；非真实流量

## 1. 一句话结论

在同一台机器、同一合成负载、同一插件规则集下，Go 版端到端回复延迟 p50 约为旧版的 **1/11**（0.28ms vs 2.94ms），500 条突发下约为旧版的 **1/166**（4.20ms vs 696.55ms），CPU 占用约 **1/25**，常驻内存约 **1/7**。这是首次有数据支撑的性能对比，但当前不能据此下"迁移完成"结论——旧版承载了 Go 版尚未实现的完整 loader 行为（Redis 计数、黑白名单、Runtime.init 等），且渲染、群聊、真实协议端均未纳入。

## 2. 测量口径

- **端到端回复延迟**：负载器通过 WebSocket 推送消息事件，到收到被测机器人回复动作（`send_msg`）的间隔。不含负载器应答 echo 的往返；不含真实协议端/网络传输。
- **配对样本**：约半数消息携带唯一序号（`bench <seq>` 规则，回复 `[bench] <seq>`），可精确配对；`#ping`/`#help` 的固定文本回复计入 unpaired 计数，不进延迟分位数。
- **吞吐 eps**：`sent / 总墙钟`（含推送结束后的 grace 等待），仅作参考，非饱和吞吐。
- **CPU/RSS**：gopsutil 对被测进程 1 秒间隔采样；CPU 百分比按单核 100% 计。
- **扫描（scan）负载**：全部消息不命中任何规则，只测量纯扫描成本，无延迟样本。

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

| 指标 | Go run1 | Go run2 | 旧版 run1 | 旧版 run2 |
|---|---:|---:|---:|---:|
| 配对回复 | 593 | 593 | 594 | 594 |
| 丢失回复 | 0 | 0 | 0 | 0 |
| p50 (ms) | 0.23 | 0.28 | 2.90 | 2.94 |
| p95 (ms) | 0.61 | 0.99 | 4.81 | 5.57 |
| p99 (ms) | 1.24 | 1.38 | 62.68 | 37.05 |
| max (ms) | 1.94 | 1.50 | 274.28 | 80.30 |
| CPU 均值 (%) | 0.2 | 0.2 | 4.9 | 5.0 |
| RSS 峰值 (MB) | 17.6 | 17.8 | 127.2 | 131.2 |

p50/p95 两轮高度一致；旧版 p99/max 波动大（尾部分散），但量级结论稳定。

### 4.2 突发负载 · 回复（500 条尽快推送）

| 指标 | Go run1 | Go run2 | 旧版 run1 | 旧版 run2 |
|---|---:|---:|---:|---:|
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

## 5. 初步解读（待更多场景验证）

1. **尾延迟是旧版的主要短板**。稳态 p99 从 ~5ms 跳到 37-63ms，burst 下直接到秒级，与 2026-11-12 静态分析中"单条事件内部串行 await（Runtime.init → accept → 逐插件逐规则）+ 无全局队列"的结构推断吻合。
2. **Go 版稳态成本极低**（20 msg/s 下 CPU <0.2%），当前瓶颈不在这条链路；优化重点应转向功能完备性与渲染链路，而不是继续压消息循环。
3. **内存**：旧版 ~130MB 常驻（Node + 插件运行时），Go 版 ~18MB。PM2 的 512M 重启阈值对旧版意味着 ~4 倍余量，对 Go 版几乎不构成约束。
4. 上述倍数**不是迁移收益的最终结论**：旧版本次承载了更完整的行为（Redis 计数、黑白名单、消息统计），Go 版尚未实现；这些差异方向上对 Go 有利，但 mini-redis 的 μs 级 INCR 成本远不足以解释百倍延迟差。

## 6. 限制与未覆盖项

- 未测：Chromium 渲染链路（与消息链路分开是计划要求，渲染基准待做）、群聊路径（旧版有 groupCD/singleCD/1 秒文本去重而 Go 版未实现，混入即变成功能差异对比）、多账号、真实协议端、长时稳定性（内存增长斜率）、`-race`（环境缺 C 编译器）。
- miniredis 不是生产 Redis/Valkey；旧版生产环境 Redis 行为（持久化、网络往返）未复现。
- 单次会话机器负载（其他进程）未做隔离控制；两轮重复一致性降低了偶然性但未消除。
- 旧版基准副本的 node_modules 经过了修复（见 `bench/scripts/repair-node-modules.mjs` 头注），其原始安装自 D:→G: 迁移后 pnpm junction 被解引用、无法启动；修复只新增文件/链接，未改动原安装内容。
- 工作负载不包含真实业务插件的耗时分布；插件级（如 miao-plugin 渲染命令）基线需阶段七迁移时单独建立。

## 7. 复现步骤

```powershell
# 1) 构建两个基准入口（Go 工具链在 G:\ALL WORK\音无\tools\go）
& G:\ALL WORK\音无\音无\scripts\Test-Go.ps1   # 两个核心模块 + bench 模块验证

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

- [ ] 渲染链路基线（go-render 压测：队列等待、渲染耗时、浏览器恢复）
- [ ] 群聊路径基线（等 Go 版实现 groupCD/文本去重对齐后补）
- [ ] CPU/内存剖析（Go pprof、Node --inspect heap/eventLoopDelay）定位阶段耗时构成
- [ ] 依据本基线设定目标值与不回退门槛（写入 plan.md）
- [ ] 长时稳定性窗口（≥6 小时，观察 RSS 斜率与 GC）
