# 容量验证：100 群 / 2 万注册用户 / 200 msg/s（2 倍目标已按实际注入验证）

- 测量日期：2026-11-14（200–300 档首批；400–900 档 2 倍验证批）
- 目标：100 个群、2 万注册用户，峰值 200 msg/s，按 2 倍余量（400 msg/s）设计
- 负载定义：`bench` 的 `reply` 工作负载，31 条基准规则；消息按 `-message-type group` 构造，群号在 20001–20100 轮转，发送者在 10001–30000 轮转
- 被测：`go-yunzai` 的 `cmd/yunzai-bench`（`bench/env/go-yunzai`，验证批使用 2026-11-14 20:28 重建的二进制），单个 OneBot 正向 WS 连接，内置调试插件关闭，SQLite 存储
- 参数证据：400–900 档结果 JSON 已自记录 `message_type=group`、`group_count=100`、`user_count=20000`、`user_id=10001`、`group_id=20001`（本轮起负载器把消息构造参数写进结果 JSON，不再依赖文件名）

## 0. 速率的三种口径（必须区分）

**三者严格不等：requested ≠ actual injection ≠ reported eps。**

| 口径 | 定义 | 说明 |
|---|---|---|
| requested | 命令行 `-rate` | 负载器的目标速率，不等于实际注入 |
| actual injection | `sent / (最后一条 sent_ns − 第一条 sent_ns)` | 负载窗口内的持续注入速率，**容量判定使用这个口径** |
| reported eps | `summary.throughput_eps = sent / (load_wall_ms / 1000)`，分母单位为秒 | `load_wall_ms` 从预热结束、负载开始前起算（`run.go` 的 `monitorStart`），分母只含负载窗口 + 10s grace，**不含 2s 预热**；grace 计入分母使其系统性低于 actual injection |

2026-11-14 起，结果 JSON 的 `summary` 新增 `actual_injection_eps` 字段（由 `events` 首尾 `sent_ns` 计算），`Invoke-BenchScenario.ps1` 同时打印 `actual_injection` 与 `reported_eps` 两个口径。已提交的 `*-verified-run*.json` 不重写，其 JSON 不含该字段，按上表 actual injection 口径用事件首尾 `sent_ns` 复算即可（700 档 verified-run1 复算得 543.3/s）。

单线程负载器（推送 → `sleep` 到 `at+interval`）每条消息有约 0.4–0.6ms 的周期开销（定时器睡眠过冲 + 推送成本），因此 requested 越高，actual injection 越接近 `1/(1/requested + ~0.5ms)`，始终低于 requested。**这是负载器到顶，不是服务端到顶**：900 msg/s 请求下 actual injection 仍有 639 msg/s，而服务端 p99 全程 ≤ 0.68ms、CPU ≤ 2.5%，没有任何饱和迹象。

## 1. 结果

### 1.1 200–300 档（2026-11-14 首批，已提交历史证据）

| requested | 消息类型 | sent | paired | 丢失 | reported eps | p50 (ms) | p95 (ms) | p99 (ms) | max (ms) | CPU 均值 | RSS 峰值 |
|---:|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| 200 | private | 10666 | 5333 | 0 | 152.3 | 0.30 | 0.47 | 0.82 | 1.35 | 1.6% | 20.4 MB |
| 200 | group（100 群） | 10671 | 5336 | 0 | 152.4 | 0.28 | 0.45 | 0.76 | 1.25 | 1.4% | 20.4 MB |
| 250 | group（100 群） | 12959 | 6480 | 0 | 185.1 | 0.27 | 0.36 | 0.50 | 2.04 | 1.7% | 20.5 MB |
| 300 | group（100 群） | 15478 | 7739 | 0 | 221.1 | 0.27 | 0.36 | 0.47 | 1.77 | 1.3% | 20.8 MB |

原始结果：`bench/env/results/go-reply-steady-200rps.json`、`go-reply-group-200rps.json`、`go-reply-group-250rps.json`、`go-reply-group-300rps.json`。

### 1.2 400–900 档（2 倍验证批，每档两轮 + 900 诊断一轮）

| requested | actual injection | reported eps | sent | paired | 丢失 | p50 (ms) | p95 (ms) | p99 (ms) | max (ms) | CPU 均值 | RSS 峰值 | 结果文件 |
|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---|
| 400 | 327.3/s | 280.5 | 19636 | 9818 | 0 | 0.274 | 0.470 | 0.630 | 1.82 | 1.4% | 19.3 MB | `go-reply-group-400rps-verified-run1.json` |
| 400 | 326.4/s | 279.8 | 19586 | 9793 | 0 | 0.275 | 0.452 | 0.599 | 1.24 | 2.0% | 19.1 MB | `go-reply-group-400rps-verified-run2.json` |
| 600 | **437.1/s** | 374.6 | 26225 | 13113 | 0 | 0.255 | 0.416 | 0.543 | 1.50 | 1.8% | 19.2 MB | `go-reply-group-600rps-verified-run1.json` |
| 600 | **436.9/s** | 374.5 | 26215 | 13108 | 0 | 0.258 | 0.409 | 0.530 | 1.02 | 2.2% | 19.6 MB | `go-reply-group-600rps-verified-run2.json` |
| 700 | **543.3/s** | 465.5 | 32594 | 16297 | 0 | 0.238 | 0.377 | 0.490 | 1.08 | 2.4% | 19.4 MB | `go-reply-group-700rps-verified-run1.json` |
| 700 | **543.1/s** | 465.4 | 32583 | 16292 | 0 | 0.240 | 0.376 | 0.494 | 1.60 | 2.4% | 19.3 MB | `go-reply-group-700rps-verified-run2.json` |
| 900（诊断） | 639.2/s | 547.8 | 38351 | 19176 | 0 | 0.229 | 0.356 | 0.474 | 3.65 | 2.5% | 19.4 MB | `go-reply-group-900rps-loader-cap-run1.json` |

## 2. 结论

- **2 倍目标（400 msg/s）已按实际注入验证**：请求 600 msg/s 两轮 actual injection 437.1/436.9 msg/s，请求 700 msg/s 两轮 543.3/543.1 msg/s，均 ≥ 400，且全部 `missing_replies=0`、p99 ≤ 0.55ms、CPU ≤ 2.5%、RSS 峰值约 19.6MB。请求 400 msg/s 时 actual injection 只有约 327 msg/s（负载器周期开销），**不能以"requested=400"作为通过依据**；判据是 actual injection ≥ 400 的四轮可复现结果。
- reported eps（如 700 档 465.4）系统性低于 actual injection（543.1），差异来自 10s grace 计入分母（预热不计入，`load_wall_ms` 从预热结束后起算）；引用历史 JSON 中的 `throughput_eps` 时必须注明该口径。不要拿 reported eps 判定容量达标：700 档 reported 465.5 曾被误读为"未达 400"，实际 actual injection 是 543.3。
- 服务端远未饱和：actual injection 从 327 提到 639 msg/s（1.95 倍），p99 从 0.63 降到 0.47ms，CPU 从 1.4% 升到 2.5%。瓶颈是单线程负载器每周期 ~0.4–0.6ms 的开销（定时器睡眠过冲 + 推送成本），继续提高注入速率需要多负载器并行或负载器改造，而不是服务端优化。
- `dispatch.max_concurrent` 默认 256（配置项，1–4096）：在 ≥2 倍设计目标、p99 0.5ms 的实测下未触顶；接入耗时更长的插件前建议按 `go-yunzai/README.md` 的运维说明评估。

## 3. 采样可信性与脚本加固

验证首轮曾发现 `bench/env` 下有 8 个残留 `yunzai-bench` 进程（来自更早会话，`Stop-Tree` 静默失败所致）：残留 bot 的重连退避可能先于新被测进程抢到负载器的 3002 连接，使监控采样盯住空闲进程、冷启动一致性被破坏——首轮 700rps-run2 即表现为逐秒 CPU 全 0、RSS 恒 12MB，该结果作废重采。加固后（启动前按精确可执行路径 `bench\env\go-yunzai\yunzai-bench.exe` 清理残留、`Stop-Tree` 校验并兜底强杀）重采的每一轮采样均与负载相关（逐秒 CPU 非零样本 31–48/73、RSS 爬升并稳定在 19.1–19.6MB）。清理后确认无残留进程。

## 4. 本测试未覆盖

- 只使用基准插件的简单规则，不包含真实业务插件、媒体处理、模型调用、存储写入或渲染。
- 100 个群是消息事件字段上的轮转，不是 100 个独立群状态/会话；群冷却、群文本去重尚未实现，重复事件仍可能放大回复。
- SQLite 未承受逐消息写入；当前 `FULL + 单连接` 的写入吞吐未在本场景中验证。
- 未测试真实 OneBot 协议端、多账号、断线重连、长时稳定性、`-race`。
- actual injection 上限受单线程负载器约束（本机约 543 msg/s @requested 700）；更高注入需要多负载器并行或负载器改造，届时再测服务端真正饱和点。
- `#note` 仍对所有群用户开放；2 万注册用户下应限制为仅主人或增加每用户配额/冷却。
- 旧版群路径未参与对照；旧版有 groupCD/文本去重，当前结果不能直接用于"新旧群聊倍数"结论。

## 5. 复现

```powershell
# 100 群、2 万发送者；容量判定看"actual injection"（sent / 事件首尾 sent_ns 差），
# 结果 JSON 自记录 message_type/group_count/user_count，文件名不作参数证据
& .\bench\scripts\Invoke-BenchScenario.ps1 `
  -Variant go -Workload reply -Scenario steady -Rate 700 -DurationSec 60 `
  -MessageType group -GroupCount 100 -UserCount 20000 `
  -OutFile "G:\ALL WORK\音无\音无\bench\env\results\go-reply-group-700rps-verified-run1.json"
```

配置项：

```yaml
dispatch:
  max_concurrent: 256
```
