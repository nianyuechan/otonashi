# 容量验证：100 群 / 2 万注册用户 / 200 msg/s

- 测量日期：2026-11-14
- 目标：100 个群、2 万注册用户，峰值 200 msg/s，按 2 倍余量（400 msg/s）设计
- 负载定义：`bench` 的 `reply` 工作负载，31 条基准规则；消息按 `-message-type group` 构造，群号在 20001–20100 轮转，发送者在 10001–30000 轮转
- 被测：`go-yunzai` 的 `cmd/yunzai-bench`，单个 OneBot 正向 WS 连接，内置调试插件关闭，SQLite 存储
- 对照口径：端到端延迟为事件发出到收到 `send_msg` 动作；`eps` 为 sent / 墙钟，受 Windows 定时器精度影响，低于命令行请求速率

## 1. 结果

| 请求速率 | 消息类型 | sent | paired | 丢失 | 实际 eps | p50 (ms) | p95 (ms) | p99 (ms) | max (ms) | CPU 均值 | RSS 峰值 |
|---:|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| 200 | private | 10666 | 5333 | 0 | 152.3 | 0.30 | 0.47 | 0.82 | 1.35 | 1.6% | 20.4 MB |
| 200 | group（100 群） | 10671 | 5336 | 0 | 152.4 | 0.28 | 0.45 | 0.76 | 1.25 | 1.4% | 20.4 MB |
| 250 | group（100 群） | 12959 | 6480 | 0 | 185.1 | 0.27 | 0.36 | 0.50 | 2.04 | 1.7% | 20.5 MB |
| 300 | group（100 群） | 15478 | 7739 | 0 | 221.1 | 0.27 | 0.36 | 0.47 | 1.77 | 1.3% | 20.8 MB |

原始结果：`bench/env/results/go-reply-steady-200rps.json`、`go-reply-group-200rps.json`、`go-reply-group-250rps.json`、`go-reply-group-300rps.json`。

## 2. 结论

- 在 100 群、2 万发送者轮转的群聊负载下，实际达到 **221 eps**、**0 丢失**，p99 **0.47ms**，CPU **1.3%**，RSS 峰值 **20.8MB**。
- 200 msg/s 目标已通过，按实测结果约有 10% 的速率余量；2 倍目标 400 msg/s 尚未实测，不能据此宣称。
- 当前瓶颈不在消息循环；单连接 221 eps 下 CPU 和内存都有明显余量。
- `dispatch.max_concurrent` 默认 256，已改为配置项（1–4096）。按 221 eps、p99 0.47ms 估算，256 足够覆盖 200 msg/s；若要压 400 msg/s 或接入长耗时插件，建议先调到 512 并复测。

## 3. 本测试未覆盖

- 只使用基准插件的简单规则，不包含真实业务插件、媒体处理、模型调用、存储写入或渲染。
- 100 个群是消息事件字段上的轮转，不是 100 个独立群状态/会话；群冷却、群文本去重尚未实现，重复事件仍可能放大回复。
- SQLite 未承受逐消息写入；当前 `FULL + 单连接` 的写入吞吐未在本场景中验证。
- 未测试真实 OneBot 协议端、多账号、断线重连、长时稳定性、`-race`。
- `#note` 仍对所有群用户开放；2 万注册用户下应限制为仅主人或增加每用户配额/冷却。
- 旧版群路径未参与对照；旧版有 groupCD/文本去重，当前结果不能直接用于“新旧群聊倍数”结论。

## 4. 复现

```powershell
# 100 群、2 万发送者、请求 300 msg/s（实际约 221 eps）
& .\bench\scripts\Invoke-BenchScenario.ps1 `
  -Variant go -Workload reply -Scenario steady -Rate 300 -DurationSec 60 `
  -MessageType group -GroupCount 100 -UserCount 20000 `
  -OutFile "results\go-reply-group-300rps.json"
```

配置项：

```yaml
dispatch:
  max_concurrent: 256
```
