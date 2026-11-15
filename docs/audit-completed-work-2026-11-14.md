# 已完成部分审计报告（2026-11-14）

审计对象：`G:\ALL WORK\音无\音无` 中 plan.md 声称"已完成"的阶段零至阶段六。
审计方式：复跑统一验证脚本、从原始 JSON 复算基线、逐文件审读关键路径、文档与实现逐条核对。

> 修订（第二版）：根据复核意见，修正了 H1 的根因表述、H2 的范围、H3 的措辞和 M7/M8 的影响范围；核心结论（稳态样本口径不一致、需改用单调计时重测并撤下倍数）不变。中等问题补充"影响待实测"限定。

## 一、总体结论

已完成部分**不是"只有文档"**：三个 Go 模块的 fmt / test / build / vet 在本次复跑中真实通过，阶段二至阶段六的核心路径有对应实现和测试。但存在四类问题：

1. **阶段零稳态延迟结论不可引用**。记录时间戳在大量配对中完全相同：Go 侧 58% 的配对样本被记为 0 后被过滤，分位数只基于 41%–43% 的样本；旧版 100% 样本。报告的 1/11 是"幸存样本"的比值，不是真实延迟比；零值根因尚未确定，需改用单调计时重测。
2. **存储依赖未受能力声明约束**。能力模型只有 `messages`/`render`，没有 `storage`；`sdk.Context` 无条件注入 Store，声明 `messages` 的插件也能读写存储。`Renderer` 不在 Context 中，内置调试插件通过构造函数单独注入。
3. **OneBot URL query 中的凭据未拦截，连接日志记录完整 URL**，URL 带 query token 时可能造成日志泄漏。
4. **内置调试插件默认启用并回复每条私聊/群消息，只有程序级、没有配置级关闭开关**；`#render` 可被任何用户触发固定页面渲染，`#note` 只能读写用户自己的记事。

功能正确性方面，未发现会让离线闭环失效的缺陷；发现的并发/资源问题主要集中在关闭、重连、浏览器重启和长期运行场景。

## 二、审计方法

1. 复跑 `scripts/Test-Go.ps1`（输出留档 `bench/env/audit-test-go.log`）：go-yunzai、go-render、bench 三模块 fmt / test / build / vet 全部通过。
2. 用 `bench/env/results/*.json` 的 `events` 原始数组复算 p50/p95/p99，与 `summary` 和报告对照（脚本与输出在审计工作区）。（历史证据，已退出索引；按 `docs/evidence-index.md` §4 取回）
3. 用最小 Go 程序观察 Windows 上 `time.Now().UnixNano()` 的更新间隔，并检查正延迟样本的最小值。
4. 审读 `internal/adapter/onebotv11`、`internal/plugin`、`internal/storage`、`internal/render`、`internal/config`、`plugins/builtin`、`plugins/bench`、`go-render/internal/renderer`。
5. 核对 README、plan.md、phase0-baseline.md 与实现/脚本。

## 三、高严重度

### H1 稳态延迟分位数口径不一致，"1/11"不可引用

证据（`bench/env/results` 本机副本；历史证据，已退出索引，见 `docs/evidence-index.md`，本次复算）：

| 文件 | paired_replies | latency_count | latency=0 且 sent_ns==reply_ns |
|---|---:|---:|---:|
| go-reply-steady-run1 | 593 | 246 | 347（58.5%） |
| go-reply-steady-run2 | 593 | 253 | 340（57.3%） |
| old-reply-steady-run1 | 594 | 594 | 0 |
| old-reply-steady-run2 | 594 | 594 | 0 |

`bench/internal/loadgen/metrics.go:94` 用 `if event.LatencyMS > 0` 过滤样本，因此 347/340 个零延迟样本不进入 p50/p95/p99。报告中的 0.23–0.28ms 只代表剩余 41%–43% 的样本；旧版 594/594 全部进入。

时钟实验（本机 Go 1.27.4 / Windows）：

- 连续两次 `time.Now().UnixNano()`：500/500 次完全相同，说明同一时刻的重复读取会返回同一值。
- 用忙等观察 wall clock 的更新间隔：300 次采样的 p50 约 0.51ms、p90 约 1.00ms、最大 1.47ms，但也有约 38% 的采样落在 6–100µs。
- 正延迟样本并非全部量化到 0.5ms：Go 两轮的最小正延迟分别为 0.0684ms 和 0.0837ms。
- 结论：wall clock 的更新间隔不稳定，**不能断言固定的 0.5ms 分辨率**；但更新间隔在基准运行期间足以把大量亚毫秒回复记录成同一时间戳（`sent_ns == reply_ns`、`LatencyMS=0`）。零值的根因仍需用单调计时复测确认，不能只归因于时钟。

影响：

- 若把零延迟样本计入，Go 的 p50 是 0ms（58% 样本为 0），无法与旧版 2.9ms 做"倍数"比较；真实值只能表述为"低于当前记录方式能稳定分辨的量级，需用单调计时重测"。
- 旧版延迟 2.9ms 以上，量化误差相对小；两侧样本口径不一致，违反"同一负载、同一口径"的基线原则。
- 报告与 plan 均未披露 `latency_count/paired_replies` 比例。

修复建议：

1. 延迟改用单调时钟测量：`start := time.Now()` 后 `time.Since(start)`，或直接 `runtime.nanotime()`；不要用 `UnixNano()` 差。
2. 分位数统计包含所有配对样本，不按 `LatencyMS > 0` 过滤；零延迟保留为真实值（如果单调时钟仍测出 0，说明低于计时器能分辨的量级，报告注明）。
3. 报告增加 `latency_count / paired_replies` 和测量方法与可分辨量级说明。
4. 重新采集后再决定是否保留"1/11"这类表述；在此之前，plan.md 和 README 中的稳态倍数应撤下或明确标注"基于 41%–43% 样本，不可引用"。

附带口径问题：

- RSS "约 1/7" 是峰值口径（旧版 127–144MB vs Go 17.6–18.6MB）；平均口径为 84–85MB vs 16.8MB，约 1/5。
- burst 场景 CPU 约 1/11–1/12（6.38%–7.16% vs 0.60%）；"1/25"是稳态口径。报告未分场景标注。

### H2 存储依赖未受能力声明约束

`sdk.Context` 暴露 `Store` 和 `Config`（`pkg/plugin/sdk/sdk.go:103-111`），`internal/plugin/dispatch.go:161-165` 在每次调用插件 handler 时无条件注入 `m.storeFor(...)` 和配置视图；`TaskContext` 同样包含 `Store`。能力模型只有 `messages` 和 `render`（`sdk.go:15-18`），**没有 `storage` 维度**。

- 因此，一个只声明 `CapabilityMessages` 的插件仍然可以在 handler/task 中直接读写存储；这不是绕过某个已声明能力，而是能力模型没有覆盖存储。
- `Renderer` 不在 `sdk.Context` 中；内置调试插件是通过构造函数单独拿到 `render.Renderer` 的，普通插件没有从上下文取得渲染器的路径。原报告把 Renderer 算进"无条件注入"是错的。
- 现有测试 `TestManagerPolicyRejectsUndeclaredCapability` 只覆盖"声明了未授权能力被拒绝"，没有覆盖存储依赖与声明的一致性。

影响：能力声明与实际资源暴露不一致。当前插件是进程内受信代码，风险有限；一旦阶段七引入磁盘插件或第三方插件，"声明 messages、实际读写存储"会成为权限边界缺口。

修复建议：要么新增 `storage` 能力并在注入时按声明裁剪，要么在文档中明确 Store 是通用依赖、不属于能力白名单；同时补一条覆盖"未声明 storage 的插件仍能访问 Store"的现状测试或裁剪测试。

### H3 OneBot URL query 中的凭据未拦截，可能造成日志泄漏

- `internal/config/config.go` 的 `Validate` 只拒绝 `parsed.User != nil`（URL 内嵌用户名/密码），不检查 query。
- `internal/adapter/onebotv11/client.go` 的 `Run` 在连接成功时执行 `Logger.Info("OneBot connected", "url", c.opts.URL)`，记录完整 URL。
- 若配置 `ws://host/?access_token=secret`，token 会进入运行日志；`config.example.yaml` 的注释只提示使用 header token，未禁止 query token。

影响：与 README "不记录 token" 的声称存在潜在冲突；只有配置 URL 带 query token 时才发生，日志通常会被收集/转发。

修复建议：`Validate` 拒绝 query 中的 `access_token`、`token`、`key`、`secret` 等键；日志只记录 scheme+host+path，或统一脱敏。

## 四、中严重度

> 以下问题的源码依据成立，但影响程度未做针对性负载或关闭场景实测；严重度按"可能影响"标注，不作为已验证结论。

### M1 连接断开/停止时在途事件被取消

`readLoop` 把 `sessionCtx` 直接传给 `Handler.Dispatch`。连接断开或应用停止时 `sessionCtx` 取消，正在插件处理器中的事件收到 `context.Canceled`，回复可能丢失。旧版 Yunzai 的事件处理不依赖连接生命周期。

影响：网络抖动重连、滚动重启时丢失在途消息；与"排空事件"的关闭顺序描述不完全一致。建议每事件使用独立 ctx（`context.WithoutCancel` 或在 Dispatch 内派生），关闭时单独排空并设上限。

### M2 事件分发无并发上限

`internal/dispatch/dispatcher.go` 的 `Dispatch` 为每个事件启动一个 goroutine，没有信号量、队列或背压。500 条突发可接受，但刷屏或攻击流量会按消息数创建 goroutine，内存和调度压力无上限。建议增加可配置的最大并发与有界队列，超限计数并拒绝/丢弃。

### M3 SQLite `synchronous(FULL)` + 单连接

`internal/storage/sqlite.go` 使用 `_synchronous(FULL)` 和 `SetMaxOpenConns(1)`。每次提交都 fsync，所有插件存储操作串行。当前 bench 插件不写存储，基线没有覆盖真实写入；旧版业务插件每消息多次 INCR，迁移后 SQLite 写入可能成为瓶颈，与性能目标冲突。

建议：评估 WAL + `synchronous(NORMAL)`（接受崩溃丢失最后若干事务），或明确"持久性优先"并在 README 披露；补一个真实写入负载的基准。

### M4 冷却记录无界增长

`manager.go` 的 `takeCooldown` 在 `m.cooldowns[key]` 写入 `now.Add(duration)`，过期后只覆盖同一个 key，从不删除。key 由 (plugin, rule, subject) 组成；配置了规则级冷却且用户/群很多时，map 会持续增长。现有测试只验证同 key 过期后可再次触发，未验证清理。建议定期清理或惰性删除过期项。

### M5 browserManager 持锁启动浏览器

`go-render/internal/renderer/service.go` 的 `newPage` 持有 `m.mu` 调用 `factory` 和 `browser.NewPage`，后者可能数百毫秒。期间所有 `currentGeneration` / `browserStopped` 调用（每个 job 渲染前都会调用）阻塞，浏览器重启时队列会积压。建议把启动移到锁外并做双检，或拆分 generation 锁与启动锁。

### M6 关闭超时后 worker 仍可能重建浏览器

`Service.Close` 在超时分支调用 `s.force()` 和 `s.manager.close()`，但不等待 worker；`browserManager.close` 不设置 closed 标志，`newPage` 仍可调用 `factory`。虽然 `job.ctx` 已取消通常会让 factory 失败，但仍有低概率在关闭后启动新浏览器进程。建议 manager 增加 closed 状态，`newPage` 直接拒绝。

### M7 渲染服务的通用 API 无出站网络隔离（已知，文档披露）

`go-render` 的 `/v1/render` 接受任意 HTML，HTML 可加载 `http(s)` 资源，渲染器可访问内网，存在 SSRF 面。README 已写"当前没有鉴权或 HTML 网络访问隔离，适用于受信任的本机核心调用"。

范围收窄：当前 `go-yunzai` 的 `#render` 只渲染硬编码的固定页面，不接收用户 HTML，因此 SSRF 不能直接归因于当前命令；风险适用于未来提交任意 HTML 的业务插件或本机其他调用方。迁移真实模板前应加域名白名单、禁网或至少鉴权。

### M8 内置调试插件默认启用、回复所有消息、缺配置级关闭开关

- `internal/app/app.go` 的 `NewWith` 在 `!options.DisableBuiltinDebug` 时注册调试插件；`app.Run` 传空 `Options`，默认启用。程序级有 `DisableBuiltinDebug`，但没有配置级开关。
- `plugins/builtin/debug.go` 的 `HandleEvent` 对所有 private/group 消息发送固定文本；`reply == ""` 时回退到默认文本，改配置无法真正关闭。
- `#note` 的数据按账号、消息类型、群、用户隔离（`plugins/builtin/note.go:28` 的 key），用户只能读写自己的记事，不能改他人数据。
- `#render` 只渲染固定页面，不接收用户 HTML；但任何用户都能触发，反复调用会占用渲染队列。
- `config.example.yaml` 没有 `debug.enabled` 或"生产必须关闭"的警告。

影响：直接使用示例配置部署会变成"每条消息都回复"的机器人；`#render` 可能被普通用户用来消耗渲染并发/队列，但不构成任意存储写入或任意 HTML 渲染。

建议：增加 `debug.enabled: false` 默认值；`#render`/`#note` 至少限制 master/admin；README 增加生产部署警告。

## 五、低严重度与文档一致性

- L1 `scripts/Test-Go.ps1` 末尾仍打印"两个 Go 模块全部验证通过"，实际检查三个模块；README 和 plan.md 的"两个模块"表述同步滞后。
- L2 `docs/phase0-baseline.md` 未披露 `latency_count` 和 `LatencyMS > 0` 过滤；"可精确配对"易被读成 593 个全部进分位数。
- L3 `TestLegacyJSONCompatibilityFixture` 只是 Go 写、Go 读的往返，不是旧版真实写入字节的 fixture；测试名称容易高估兼容性验证范围。真实旧数据迁移仍是未完成项。
- L4 `storage.path` 相对启动目录（`config.example.yaml` 已注释），不同 CWD 会打开不同数据库；建议改为相对配置文件目录，或在启动时打印绝对路径。
- L5 `DecodeEvent` 接受 `"message": null` 为 nil 消息段；非标准 `message_type` 不校验，可能进入权限判断的默认分支。
- L6 `toSDKEvent` 和 `fromSDKEvent` 直接共享 `segment.Data` map 引用；一个插件修改 Data 会影响后续插件对同一事件的读取。
- L7 `message.ImagePNG` 对 32MB PNG 生成约 44MB base64 字符串，发送端没有大小检查；协议端可能拒绝。
- L8 `permissions` 对 `GroupID > 0` 但 `MessageType != "group"` 的事件也设置 owner/admin；非 group 的群级通知可能获得群管理身份。
- L9 `App.Run` 的关闭顺序是先 `plugins.Stop`、再 `dispatcher.WaitContext`，在途事件会被中断；文档未说明"关闭时丢弃在途消息"。

## 六、逐阶段核验

| 阶段 | 声称 | 核验结果 |
|---|---|---|
| 零 | 双轮基线、报告、目标数值 | 脚本和数据存在、数字可复算；稳态延迟口径有 H1 缺陷，目标数值仍空 |
| 一 | 构建验证、示例配置、Git 基线 | 构建/示例/脚本通过；Git 基线已建立（`551a8c5`，218 files） |
| 二 | 4 个生命周期缺陷修复 + 回归 | 测试存在并通过；M5/M6 已补关闭/启动互斥与 closed 状态测试 |
| 三 | 离线闭环、真实握手、一次真实发消息 | 离线闭环有测试；真实入站未验证（README 记录，需第二账号） |
| 四 | PNG 段、降级、超时 | 实现与测试一致；M7 属渲染服务通用 API 风险，M8 为默认启用风险 |
| 五 | 规则、权限、会话、任务、动态加载 | 实现与测试较完整；H2 存储能力与 M2 并发上限已修，M1 事件上下文已修 |
| 六 | SQLite、Redis、legacy、配置快照 | 实现与测试存在；M3 待压测，M4 冷却清理已修，L3 仅改测试命名 |

## 七、建议修复顺序

1. **H1 基线测量**（约半天）：改单调时钟、保留全部样本、重采稳态；这是唯一影响"已完成结论"的问题。
2. **H3 凭据校验/日志脱敏**（约 2 小时）：成本低，直接消除 token 泄漏面。
3. **M8 调试插件默认关闭 + 权限**（约半天）：避免示例配置误部署。
4. **H2 存储能力边界**（约 1 天）：补 `storage` 能力并按声明裁剪，或在文档明确 Store 是通用依赖；在引入磁盘插件前完成。
5. **M3 SQLite 持久性/性能取舍**（半天 + 压测）：决定真实业务迁移的存储基线。
6. **M1/M2 事件 ctx 与并发上限**（1–2 天）：需要设计决策，放在控制面之前。
7. 其余按阶段七至十一推进。

## 八、修复落地状态（2026-11-14）

基线：Git 仓库已初始化，修复前快照为 `551a8c5`（218 files，`baseline: snapshot before audit fixes`）。

| 项 | 状态 | 落地内容 |
|---|---|---|
| H1 | 已修并重采 | `metrics.go` 纳入全部 `ReplyNS > 0` 样本（含 0），不再按 `LatencyMS > 0` 过滤；`session.go` 在 Windows 用 `QueryPerformanceCounter` 计时（其他平台回退 `time.Since`），并在 `push` 记录高精度起点。QPC 重采两轮均为 593/593 样本、0 个 `0ms`：p50 0.41–0.42ms、p95 0.83–0.86ms、p99 1.33–1.36ms。`phase0-baseline.md` 原"1/11"改为 p50 约 1/7、p95 约 1/6、p99 约 1/27–1/47；突发 250/250 数字保留。旧版本轮因 node_modules 缺 `es-toolkit` 未能重跑，沿用 15:53 原始 JSON。 |
| H2 | 已修 | 新增 `sdk.CapabilityStorage`；`Context.Store`/`TaskContext.Store` 仅在插件声明 `storage` 时注入，否则为 `nil`；内置调试插件补声明；新增能力门控测试。 |
| H3 | 已修 | `config.Validate` 拒绝 URL query 中的 `access_token`/`token`/`secret`/`password`/`authorization`；连接日志用 `redactURL` 清除 userinfo/query/fragment；新增配置与脱敏测试。 |
| M1 | 已修 | `Client.Run` 建立独立的 event context，断线只取消当前连接，不取消在途事件；新增断线不取消在途事件测试。 |
| M2 | 已修 | `Dispatcher` 默认最多 256 个并发事件，超过后由读取循环形成背压；新增并发上限测试；`dispatch.max_concurrent` 已做成配置项（1–4096）。 |
| M3 | 未改（取舍已记录，重启竞争已定位） | `synchronous(FULL)` + 单连接仍是写入基线；`Open` 已带 `busy_timeout(2500)` 并保留默认回滚日志（`internal/storage/sqlite.go` 注释记录了不再在每次启动切换 journal_mode 的原因）。交接轮复核结论（修订）：`busy_timeout(2500)` 下的 **SQLite 文件锁**等待最多 2.5s 后报错返回，不足以单独解释 8 秒会话超时；此前怀疑的"快速重启锁竞争"不是该失败的主因。注意 busy_timeout 只约束 SQLite 文件锁，`SetMaxOpenConns(1)` 的连接池等待和阻塞 IO 不受其限制，锁竞争未被完全排除，只是文件锁等待的量级与实测停滞不符。切换 WAL/NORMAL 涉及持久性取舍，仍需真实写入压测后再决定，不盲改。 |
| M4 | 已修 | 冷却表按分钟惰性清理过期项；新增 50 个主体过期清理测试。 |
| M5 | 已修 | `browserManager.newPage` 用独立 `startMu` 串行化获取，启动浏览器期间不再持有 `mu`；`close` 不被启动阻塞；新增阻塞启动/关闭测试。 |
| M6 | 已修 | `browserManager` 增加 `closed` 状态，`close` 后 `newPage` 返回 `errBrowserManagerClosed`，不会重建浏览器；新增测试。 |
| M7 | 未改，范围受限 | 渲染服务默认监听 `127.0.0.1:8080`；`#render` 只渲染固定页面并已限制为主人命令。任意 HTML 调用方的出站隔离仍需网络策略/Chromium 沙箱，单独设计。 |
| M8 | 已修 | `debug.enabled` 配置默认 `false`，`app.NewWith` 据此注册调试插件；`config.example.yaml` 增加开关；`#render`/`#session` 限制 `PermissionMaster`；`#note` 保持按用户/会话隔离（不限制普通用户，避免改变其演示语义）。 |
| L1 | 已修 | 脚本与 README、plan.md 统一为三个模块。 |
| L2 | 已修 | 基线报告披露 `latency_count/paired_replies` 与零值过滤，撤下不可引用分位数。 |
| L3 | 已修（仅命名） | 用例改名为 `TestLegacyJSONGoRoundTrip` 并注明只验证 Go 编解码往返；真实旧字节 fixture 仍未完成。 |
| L4 | 已修（可观测） | `SQLite.Path()` 返回绝对路径，启动日志打印实际数据库路径；相对路径解析规则不变。 |
| L5 | 已修 | `DecodeEvent` 拒绝 `message: null` 与空数组；新增测试。 |
| L6 | 已修 | `toSDKEvent`/`fromSDKEvent` 复制 `segment.Data` 与 `Status` map；避免插件间共享可变引用。 |
| L7 | 已修 | `#render` 超过 8 MiB 的 PNG 不发送，改为文本提示；新增测试。 |
| L8 | 已修 | 权限只对 `MessageType == "group"` 且 `GroupID > 0` 的事件授予 owner/admin；新增非 group 事件测试。 |
| L9 | 已修（文档） | README 说明停止时先停收新事件、5 秒内等待在途事件，超时取消并丢弃。 |
| 交接轮-测试稳定化 | 已修（根因已采证） | **根因证据**：交接轮完整验证第 2 次运行真实复现同一类停滞——`TestSessionConversationAndShutdownThroughOneBot/group` 失败于 `open plugin storage: context deadline exceeded`（30.23s），同一时间窗内 internal/plugin（`TestTasksReceiveScopedStoreAndFreshConfig` 30.61s）、internal/storage（37.5s）、internal/render（31.3s）多个并行测试包同时被拖慢到 30 秒量级：本机存在持续 ~30s 的机器级 IO 停滞。modernc SQLite 的阻塞 IO（建表 fsync）在停滞期间无法被 context 中断，deadline 只能在 IO 返回后生效——这解释了原 note 测试失败的完整链条：存储操作卡 8s+ → 8s 会话期限触发 → 卡住的操作仍持有连接 → `application.Close()` 无法释放句柄 → TempDir 清理报"文件占用"。`busy_timeout(2500)` 下的 SQLite 文件锁竞争最多 2.5s 报错返回，与该签名矛盾（修订：这只说明文件锁等待这一种因素不足以解释停滞；`SetMaxOpenConns(1)` 的连接池等待、阻塞 IO 和其他文件操作不受 busy_timeout 限制，锁竞争未被完全排除）。**修复**：(1) `runNoteConversation`（`internal/app/storage_test.go`）失败路径改为 `t.Cleanup` 有序停止（cancel → 等待 Run（5s + goroutine dump）→ Close → server.Close），LIFO 保证先于 `t.TempDir` 删除执行；增加步骤级耗时记录与超时时全量 goroutine dump；会话期限 8s → 30s 吸收实测停滞（正常会话 0.3–0.6s）。(2) 会话测试 ctx 5s → 30s（同口径）。(3) `openStorage`（`internal/app/app.go`）打开预算 5s → 30s：停滞期间阻塞 IO 本就无法提前取消，放宽预算避免"慢但能完成"的启动被过早判死；损坏路径仍立即报错。(4) `TestTasksReceiveScopedStoreAndFreshConfig` 两阶段改用独立 30s 计时器（原共享 3s 计时器会在停滞期间烧完）。 |
| 交接轮-pprof 端点 | 已修（loopback-only） | 新增 `internal/observability`：`YZGO_PPROF_ADDR` 未设置时不监听；地址解析后必须是 loopback（127.0.0.0/8 或 ::1），`0.0.0.0`、`::`、空主机名、解析到非回环的主机名一律拒绝并有测试覆盖；采样率在监听成功后才开启，stop 归零。README 增加用途、环境变量、默认关闭、仅本地、生产警告与采样开销说明。`cmd/yunzai`、`cmd/yunzai-bench` 接线。 |
| 交接轮-负载器参数自记录 | 已修 | 负载器结果 JSON 新增 `message_type`/`group_count`/`user_count`/`user_id`/`group_id` 字段：容量验证的参数证据取自 JSON 本身，不再依赖文件名（此前未提交的 400/600/700 结果无法自证群数/发送者数）。 |
| 本轮-group 除零修复 | 已修 | `bench/internal/loadgen/config.go`：group 模式校验补上 `UserCount > 0`（此前只要求 GroupCount/GroupID 正数，`-user-count 0` 会在 `session.push` 的 `index%UserCount` 触发除零 panic）；`TestConfigValidate` 新增 group 模式 `user-count=0`/`group-count=0` 拒绝用例与合法 group 配置通过用例。`go test ./internal/loadgen -count=1` 通过。 |
| 本轮-存储打开预算 60s | 已修 | 旧预算 30s 小于实测停滞 30.23s（30.0s 时 ctx 已到期，IO 30.23s 返回后仍报 deadline），同类停滞仍会失败。`openStorage` 打开预算 30s → 60s（约 2 倍余量）；`runNoteConversation`/会话测试 ctx 30s → 90s（测试期限大于应用预算，避免测试先于应用超时）、Run 停止等待 5s/2s → 15s；`TestTasksReceiveScopedStoreAndFreshConfig` 两阶段观察宽限 30s → 60s 同口径。失败路径核验：`openStorage` 的建表 fsync 是唯一受预算约束的路径（阻塞 IO 无法被 ctx 中断，只能在 IO 返回后生效）；`application.Close` 无期限、只会阻塞到 IO 返回，不存在"到期"；`runNoteConversation` 清理顺序中 5s Run 等待在停滞下可能先到期，已放宽至 15s。`app.go` 注释修正：busy_timeout(2500) 下文件锁是最多等待 2.5s 后报错，不是"立即报错"；M3 与交接轮行已加修订注——busy_timeout 只约束 SQLite 文件锁，连接池等待与阻塞 IO 仍保留为可能因素，不声称锁竞争已完全排除。README 运维新增"存储打开预算（60s）"小节。验收：`TestNoteSurvivesApplicationRestartThroughOneBot -count=20 -timeout=240s` 通过（14.5s）。 |
| 本轮-actual injection 固化 | 已修 | `Summary` 新增 `actual_injection_eps` 字段（由 events 首尾 `sent_ns` 计算），单事件/零跨度/空事件返回 0 并有测试；`Invoke-BenchScenario.ps1` 同时打印 `actual_injection` 与 `reported_eps`（旧 JSON 无该字段时按同一口径从事件复算）；`capacity-100-groups-200rps.md` 第 0 节修正：`reported eps = sent / (load_wall_ms / 1000)`，分母为负载窗口 + 10s grace，**不含 2s 预热**（`monitorStart` 在预热后设置），并写明 requested ≠ actual injection ≠ reported eps。已提交的 `*-verified-run*.json` 不重写。验收运行（新文件 `go-reply-group-700rps-actualinj-run1.json`，参数自记录 group/100/20000）：sent=32608、actual=543.6/s（JSON 字段值与首尾 sent_ns 复算逐位一致）、reported=465.8、missing=0、p99=0.56ms、CPU 2.2%、RSS 19.3MB，与既有 543.3/465.5 口径一致。 |
| 本轮-脚本防旧结果误判 | 已修 | `Invoke-BenchScenario.ps1` 删除旧 OutFile 改为 `-ErrorAction Stop` 并断言文件消失（删除失败即终止，不再静默）；运行结束后校验 `summary.sent > 0` 且 `finished_at` 在本次运行时间窗内，防止删除失败或负载器写入失败时把旧文件读成"本次成功"。验收：人为占用旧结果文件后运行脚本，脚本在启动负载器前报错退出（"文件正由另一进程使用"），未返回旧数据。 |
| 本轮-旧结果清理 | 已完成 | 删除 4 个未跟踪旧结果 `go-reply-group-400rps.json`/`600rps.json`/`700rps.json`/`700rps-run2.json`（无自记录参数，已被 `*-verified-run*.json` 取代；删除前用 `git ls-files --error-unmatch` 逐一确认未跟踪）；`next-agent-prompt.md` 同步更新为新状态，未保留互相矛盾的交接副本。**提交口径说明**：`04aed40` 本身只改文档（交接提示词同步），不包含代码；上述 4 个旧结果是未跟踪文件的工作区删除操作，不出现在任何提交里，`git log --stat` 无法也不应看到它们。 |

容量目标（2026-11-14 确认）：100 个群、2 万注册用户、峰值 200 msg/s，按 2 倍余量设计。新增 `-message-type group`、`-group-count`、`-user-count` 负载参数，并以 100 群/2 万发送者轮转实测到 **221 eps、0 丢失、p99 0.47ms、CPU 1.3%、RSS 20.8MB**。交接轮补充：2 倍目标已按实际注入验证——请求 600/700 msg/s 时实际注入 437–543 msg/s（四轮）、`missing_replies=0`、p99 ≤ 0.55ms；请求速率 ≠ 实际注入 ≠ reported eps（分母含 grace），三口径区分见 `docs/capacity-100-groups-200rps.md`。负载器每周期 ~0.4–0.6ms 开销使 900 msg/s 请求只注入 639 msg/s，而服务端 p99 全程 ≤ 0.68ms、CPU ≤ 2.5%，未饱和；继续提高注入需多负载器或负载器改造。该结果只覆盖简单基准规则，不含真实插件、存储写入、渲染和多账号。负载器结果 JSON 自 2026-11-14 起自记录 `message_type`/`group_count`/`user_count` 等参数，参数证据不再依赖文件名；此前未提交的 400/600/700 结果（19:40–19:46 采集）无法自证参数，已由新采集的 `*-verified-run*.json` 取代。

验证（交接轮最终口径）：`TestNoteSurvivesApplicationRestartThroughOneBot -count=20` 通过（13.5s）；修复后 `scripts/Test-Go.ps1` 连续 3 次通过 go-yunzai、go-render、bench 三模块的 fmt/test/build/vet（日志 `bench/env` 工作区外留档 artifacts-gocache/testgo-final2-run1..3）。本轮修复后同口径复验：`TestNote... -count=20` 通过（14.5s），`scripts/Test-Go.ps1` 连续 3 次通过（日志 artifacts-gocache/testgo-actualinj-run1..3）。此前同日历史：原 pprof 未提交改动下的复跑曾出现 note 测试 10.06s 超时（单独复跑 0.85s 通过，当时误判为 M3 锁竞争）；交接轮第 1 次 3 连跑的 run2 真实复现同类停滞并定位根因为机器级 IO 停滞（见"交接轮-测试稳定化"行），据此扩大修复后获得 3 连通过。本报告旧版"M3 锁竞争导致测试失败"的表述作废。H1 已用 QPC 重采两轮并纳入基线报告，同批次重采见 4.4。`-race` 仍未运行（缺 CGO/C 编译器）。

## 九、审计限制

- 未运行 `go test -race`：环境 `CGO_ENABLED=0`，缺 C 编译器。
- 未启动 SnowLuma 或 Chromium 做真实联调，真实链路结论沿用 README 记录。
- 未逐行审读全部约 1.1 万行 Go 代码，重点覆盖"已完成"声称对应的路径。
- H1 已用 QPC 重采两轮稳态并纳入基线报告；同批次重采（19:34–19:39）已补齐旧版对照并纳入 `phase0-baseline.md` 4.4，第一批原始 JSON 保留为历史证据。容量结果来自 100 群/2 万发送者轮转的合成负载。
- SQLite 重启用例的失败触发在本轮已采证：交接轮完整验证第 2 次运行复现同类停滞（~30s 机器级 IO 停滞，多并行测试包同时被拖慢，`TestSessionConversationAndShutdownThroughOneBot/group` 失败于存储打开超时）。此前 `-count=20` 单测、全量套件 4 次、`go test ./...` 并行 10 次均未复现，说明停滞为低频事件；测试已改为停滞容忍口径 + 有序清理 + 证据采集，再复现时可直接定位停滞点。
- 工作区状态：上一轮曾把 10 个已跟踪基准 JSON 覆盖为新批次数据（其中 4 个是其他结果文件的逐字节复制，`go-reply-steady.json` 被换成 700rps 群聊结果）；本轮已把非重复的新数据保存为 `go-reply-steady-qpc2-run1/2.json` 后将已跟踪文件恢复到 HEAD 提交版本，新证据文件（burst/steady QPC 重采、400–900 档群聊验证）以新文件名入库，无任何历史证据丢失。
