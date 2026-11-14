# 新改动代码审查报告

> 状态标注（2026-11-14 交接轮）：本文件是**修复前快照**，正文按当时的审查发现保留，未按后续修复重写；旧结果清理（4 个未跟踪 JSON）和 `Invoke-BenchScenario.ps1` 脚本加固已在后续提交（`f6ffaaa`、`04aed40`）完成，下文相关"待处理"表述以 audit 报告第八节为准。

- 审查日期：2026-11-14
- 审查范围：`a6aa37b`（SQLite 重启测试稳定化）、`dafacbb`（pprof 端点）、`8d82688`（2 倍容量验证与报告）
- 当前 HEAD：`8d82688`
- 审查方式：逐提交 diff + 源码路径核对 + 原始 JSON 复算 + 当前工作区复跑测试
- 结论：**不是“没有问题的代码”。有 1 个确定的压测工具 bug，以及 2 个需要处理的稳定性/验证口径问题；核心容量数据本身可复算，pprof loopback 限制有效。**

## 一、复跑结果

| 检查 | 命令 | 结果 |
|---|---|---|
| SQLite 重启用例 20 次 | `go test ./internal/app -run 'TestNoteSurvivesApplicationRestartThroughOneBot' -count=20` | 通过，14.8s |
| 三模块统一验证 | `scripts/Test-Go.ps1` | go-yunzai、go-render、bench 全部通过 |
| pprof 测试 | `go test ./internal/observability -run 'TestStartPprof'` | 5 个用例全部通过 |
| 700 档复算 | 读取 `go-reply-group-700rps-verified-run1.json` | sent 32594，首尾 span 59.9979s，actual 543.3/s，reported 465.5，missing 0 |

容量复核结果与报告一致：

| requested | actual injection | reported eps | missing |
|---:|---:|---:|---:|
| 400 run1/run2 | 327.3 / 326.4 | 280.5 / 279.8 | 0 |
| 600 run1/run2 | 437.1 / 436.9 | 374.6 / 374.5 | 0 |
| 700 run1/run2 | 543.3 / 543.1 | 465.5 / 465.4 | 0 |
| 900 诊断 | 639.2 | 547.8 | 0 |

## 二、确定 bug

### B1. group 模式允许 `user-count=0`，运行时会除零 panic

**严重度：中（压测工具，不影响机器人主进程）**

`bench/internal/loadgen/config.go`：

```go
if c.GroupCount < 0 || c.UserCount < 0 {
    return errors.New("group-count/user-count 不能为负")
}
if c.MessageType == "group" && (c.GroupCount <= 0 || c.GroupID <= 0) {
    return errors.New("group 模式需要正数 group-count/group-id")
}
```

group 模式没有要求 `UserCount > 0`。`bench/internal/loadgen/session.go` 的消息构造：

```go
userID = s.cfg.UserID + int64(index%s.cfg.UserCount)
```

执行 `-message-type group -user-count 0` 时校验通过，第一条消息即 `integer divide by zero`。

**修复建议**：group 模式改为要求 `GroupCount > 0 && UserCount > 0`，并在 `TestConfigValidate` 增加 `user-count=0` 用例。

## 三、稳定性问题

### S1. 30s 预算小于已观测到的 30.23s 停滞

**严重度：高（决定测试是否真正稳定）**

`go-yunzai/internal/app/app.go` 把存储打开预算改为 30s；但 `a6aa37b` 提交说明记录的实际停滞是 **30.23s**：

- 30.0s 时 context 已到期；
- `modernc SQLite` 的阻塞 IO 在 30.23s 返回后，`Open` 检查 `ctx.Err()` 仍会返回 deadline；
- 同类停滞再次发生时，测试仍可能失败。

也就是说，这个修复**降低了失败概率，但没有覆盖已实测的最大停滞窗口**。建议把存储打开/会话预算提高到 45–60s，或对已知的短暂 IO 停滞做一次受控重试；不要把 30s 写成最终稳定口径。

同时，`app.go` 注释“文件锁仍然立即报错”与 `busy_timeout(2500)` 的实际行为不一致：锁等待会先等 2.5s，而不是立即返回。

### S2. “排除锁竞争根因”的表述过满

**严重度：中（审计结论严谨性）**

`audit-completed-work-2026-11-14.md` 写“`busy_timeout(2500)` 下锁竞争最多 2.5s，不可能造成 8 秒会话超时”。这不严谨：

- `busy_timeout` 只限制 SQLite 文件锁等待；
- `SetMaxOpenConns(1)` 下，其他操作还会排队等待 `database/sql` 连接；
- 阻塞文件 IO / 连接池等待不受 2.5s 的 `busy_timeout` 限制。

“机器级 IO 停滞”可以保留为高概率解释，但不应写成已经完全排除锁竞争。建议改为“SQLite 文件锁等待被 2.5s 限制；连接池等待、阻塞 IO 和其他文件操作仍需单独观察”。

## 四、验证口径问题

### V1. `reported eps` 的定义与实际代码不一致

`capacity-100-groups-200rps.md` 写：

```text
reported eps = summary.throughput_eps = sent / load_wall_ms
分母含 2s 预热 + 10s grace
```

但 `bench/internal/loadgen/run.go` 的 `monitorStart` 是在 2s 预热之后才设置的：

```go
// 预热结束后才设置
monitorStart := time.Now()
loadErr := runLoad(...)
...
wallMS := float64(time.Since(monitorStart)) / 1e6
```

因此 `load_wall_ms` 只包含 **60s 负载 + 10s grace**（实测 70016ms），不含 2s 预热。报告中的 465.5 等数字本身是对的，但定义文字错误；另外公式 `sent / load_wall_ms` 少了 `/1000`（`load_wall_ms` 是毫秒）。

这会造成后续复算时口径不一致。建议改为：

```text
reported eps = sent / (load_wall_ms / 1000)
分母为负载窗口 + grace，不含预热
```

### V2. actual injection 没有进入结果 JSON，脚本仍只打印 reported eps

报告把 actual injection 作为容量判定口径，但 `loadgen.Result` / `Summary` 没有 `actual_injection_eps` 字段；`Invoke-BenchScenario.ps1` 只打印：

```powershell
eps={throughput_eps}
```

例如 700 档控制台会显示 465.5，而不是 543.3。报告解释了区别，但工具本身仍容易让人误判。

建议在结果 JSON 增加 `actual_injection_eps`，或至少在脚本中同时打印 actual/reported 两个值。

## 五、其他风险与清理项

- **脚本可能把旧结果误判为本次成功**：`Invoke-BenchScenario.ps1` 在启动前用 `Remove-Item -ErrorAction SilentlyContinue` 删除旧结果；如果文件被占用删除失败，负载器又写入失败，脚本后面的 `Test-Path $OutFile` 会把旧文件当成新结果。建议改为 `-ErrorAction Stop`，删除后断言文件不存在，或校验本次 `started_at`/`sent`。
- **未跟踪的旧结果文件仍在工作区**：`go-reply-group-400rps.json`、`600rps.json`、`700rps.json`、`700rps-run2.json` 没有自记录参数，报告已说明它们被 `*-verified-run*.json` 取代，但文件仍留在 `bench/env/results`，后续汇总可能误用。建议移到 `discarded/` 或删除（删除前确认）。
- **`next-agent-prompt.md` 已过时**：里面仍写 HEAD `a526168`、工作区有未提交 pprof；当前 HEAD 是 `8d82688`，这些已提交。交给下一位 Agent 前需要更新。
- **pprof loopback 不是身份认证**：只允许 `127.0.0.0/8` 或 `::1` 是正确的，但本机其他进程/用户仍可读取 heap/goroutine。单用户场景可接受，README 已记录；如需更强隔离，需要本机 ACL/令牌。
- **测试清理边缘**：`runNoteConversation` 在 `finished` 已被消费后又 `t.Fatal` 时，cleanup 会白等 5s；`New` 失败时 `httptest` server 未关闭。属于测试卫生问题，不影响生产。
- **pprof 测试覆盖**：`TestStartPprofServesOnIPv6Loopback` 对任何 `StartPprof` 错误直接 `t.Skip`，真实的监听 bug 可能被跳过；固定端口 60613–60615 也可能被其他进程占用。建议至少区分“端口占用”和“实现错误”。

## 六、结论

- **核心容量数据可用**：600/700 档 actual injection ≥400、0 丢失，2 倍目标成立；700 档复算 543.3 与报告一致。
- **pprof 实现可用**：默认关闭、loopback 限制、测试和 README 都到位；残余风险是本机其他进程可访问。
- **不应直接判定“代码没有问题”**：`UserCount=0` 是确定的 panic；30s 预算小于 30.23s 观测值，稳定性仍有缺口；`reported eps` 定义和工具输出仍会造成口径误读。
- 建议优先级：先修 `UserCount=0` 和 30s 余量，再把 `actual_injection_eps` 固化到结果 JSON/脚本，最后清理旧结果文件和过时的交接提示词。
