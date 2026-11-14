# go-yunzai M1

TRSS-Yunzai 原生 Go 重写的 M1 骨架。当前实现 **OneBot v11 正向 WS 离线闭环**、**go-render HTTP 客户端**和**插件运行时基础能力**：连接、鉴权、事件分发、echo/retcode、心跳、重连、PNG 图片发送，以及插件规则、权限、accept、会话、任务和动态启停。

- Module：`github.com/trss-yunzai/go-yunzai`
- Go：实测 `go1.27.4 windows/amd64`
- 协议端目标：SnowLuma v1.14.17，`ws://127.0.0.1:3001/`，`array` / Universal
- 状态：**OneBot 离线 mock、go-render 真实联调、SnowLuma 真实 WebSocket 握手和一次真实 `send_private_msg` 动作均已通过；真实入站消息事件往返仍待外部测试账号。**

蓝图的目录约定实际位于 §3.1，§7 是里程碑判据。本项目按 §3.1 建立 M1 所需目录，没有预建后续里程碑的空目录。

## 结构

```text
cmd/yunzai/                 启动参数、信号与退出
internal/app/               组合根；先注册处理器，再接收流量
internal/config/            YAML 配置、默认值与校验
internal/adapter/onebotv11/ 事件解码、WS 客户端、echo/retcode、心跳与重连
internal/event/             canonical event 与处理器接口
internal/message/           array Segment、文本/PNG 图片段、发送请求/结果与 Sender 接口
internal/dispatch/          并发事件分发
internal/observability/     slog 日志
internal/render/            go-render HTTP 客户端、重试、并发限制、PNG 校验与降级
internal/plugin/            插件注册、规则、策略、会话、任务与生命周期
pkg/plugin/sdk/             公开插件契约、上下文、回复与任务接口
plugins/builtin/            私聊/群固定回复、#render 和 #session 调试命令
config.example.yaml         仅含占位 token 的示例配置
```

## 配置与将来运行

```powershell
Copy-Item config.example.yaml config.yaml
# 编辑本地 config.yaml，替换 onebot.access_token；不要把真实 token 写入命令、日志或提交物。
& "G:\ALL WORK\音无\tools\go\bin\go.exe" run ./cmd/yunzai -config config.yaml
```

只有获得真实联机授权并自行启动 SnowLuma 后才应执行上述运行命令。联调期间使用了临时配置文件，验证结束后已删除；项目不会提交真实 token。

配置项包括 WS 地址、token、日志级别、请求超时、WS ping 间隔、重连初始/最大退避、调试开关（`debug.enabled`，默认 `false`）与回复文本，以及 `dispatch.max_concurrent`（默认 256，允许 1–4096）。默认请求超时为 10 秒，不沿用旧实现的固定 60 秒；默认 ping 间隔 20 秒，重连退避为 500ms 至 30s。多群部署时并发上限可按压测结果调高。

`plugins` 配置包含 `masters`（机器人账号 ID → 主人用户 ID 列表）、`allowed_capabilities`、`allow_users/deny_users`、`allow_groups/deny_groups` 和 `cooldown`。默认为空列表及零冷却；空白名单表示不限制，黑名单优先。配置中账号与用户/群 ID 必须为正数，冷却不能为负数。能力包括 `messages`、`render`、`storage`；`sdk.Context.Store` 只有在插件声明 `storage` 时才注入，未声明时为 `nil`。限制能力时，内置调试插件需要同时允许 `messages`、`render` 和 `storage`；缺少任一个会导致注册失败。配置目前在启动时加载，不提供热更新。

## 离线测试

默认单元测试只监听 `httptest` 分配的本地临时端口，不读取 SnowLuma 配置，也不连接 `127.0.0.1:3001` 或真实 renderer。只有显式设置 `RENDER_INTEGRATION_URL` 时才运行真实渲染联调。

从 `go-yunzai` 目录执行统一验证脚本：

```powershell
& ..\scripts\Test-Go.ps1
```

脚本依次检查 `go-yunzai`、`go-render` 和 `bench` 的格式、单元测试、构建与 `vet`，遇到失败立即退出。默认使用 `G:\ALL WORK\音无\tools\go\bin\go.exe`，本次运行的 `GOPATH`、`GOMODCACHE`、`GOCACHE`、`GOTMPDIR`、`TEMP/TMP` 均放在 `G:\ALL WORK\音无` 下，结束后恢复原有进程环境。可用 `-GoExecutable`、`-CacheRoot` 覆盖路径。系统和用户级 Go 配置未全局迁移，直接调用 Go 时需要自行设置路径。

2026-11-14，该脚本对三个模块的检查全部通过。可选 `-Race` 需要预先配置 CGO 和 C 编译器；当前环境尚未完成该项验证。

假 OneBot WS 服务端覆盖：

- 正确/错误 Bearer 握手；
- 私聊和群 array 消息进入后各产生一次 `send_msg`，目标与固定回复正确；
- 通知、请求、OneBot 心跳事件的最小解码，非消息事件不回复；
- 未知顶层字段容忍，未知消息段类型及 data 保留；string 消息格式被拒绝；
- 合成 echo、message_id、非零 retcode；
- 请求超时后的 pending echo 清理；
- 模拟突然断线后的 pending 清理、指数退避重连和重连后发送。

### 2026-11-13 实测

工作目录：`d:\Toshikoshi_chan\Agent\音无\go-yunzai`。

```powershell
& "G:\ALL WORK\音无\tools\go\bin\go.exe" version
# go version go1.27.4 windows/amd64
```

```powershell
& "G:\ALL WORK\音无\tools\go\bin\go.exe" fmt ./...
& "G:\ALL WORK\音无\tools\go\bin\go.exe" build ./...
& "G:\ALL WORK\音无\tools\go\bin\go.exe" test ./... -count=1
& "G:\ALL WORK\音无\tools\go\bin\go.exe" vet ./...
```

四条命令均退出码 0；`fmt`、`build`、`vet` 无标准输出。测试输出：

```text
?    github.com/trss-yunzai/go-yunzai/cmd/yunzai [no test files]
ok   github.com/trss-yunzai/go-yunzai/internal/adapter/onebotv11 0.867s
?    github.com/trss-yunzai/go-yunzai/internal/app [no test files]
ok   github.com/trss-yunzai/go-yunzai/internal/config 1.378s
?    github.com/trss-yunzai/go-yunzai/internal/dispatch [no test files]
?    github.com/trss-yunzai/go-yunzai/internal/event [no test files]
?    github.com/trss-yunzai/go-yunzai/internal/message [no test files]
?    github.com/trss-yunzai/go-yunzai/internal/observability [no test files]
?    github.com/trss-yunzai/go-yunzai/plugins/builtin [no test files]
```

额外尝试的 `go test -race ./... -count=1` 未运行：当前环境返回 `go: -race requires cgo; enable cgo by setting CGO_ENABLED=1`。没有为此安装额外软件，也不将其记为通过。

默认 `proxy.golang.org` 首次拉取依赖超时；随后仅对命令设置 `$env:GOPROXY="https://goproxy.cn,direct"` 后 `go mod tidy` 成功，未修改全局 Go 环境。

## M1 最小设计约定

- 只实现 Universal WS 上的 `send_msg`，群和私聊共用该动作；尚未实现其他 OneBot API。
- 同时支持 WS ping 与 OneBot heartbeat 事件解码；蓝图未规定“连续丢失几次 heartbeat 后主动断开”，M1 未自行增加该策略。
- 重连使用无抖动指数退避；连接稳定达到最大退避时长后，下次退避重置为初始值。
- 单事件内按插件优先级和注册顺序调用规则，不同事件可并发；默认最多 256 个事件同时处理，超过后由适配器读取循环形成背压。
- 插件上下文提供账号、用户、群、消息 ID 和消息类型等统一运行时字段。
- 内置调试插件默认关闭，需在配置中设置 `debug.enabled: true` 才注册；`#render` 和 `#session` 仅主人可用，`#note` 按账号/会话/用户隔离且受 `note_max_bytes` 限制。
- 调试处理器回复固定文本，不回显用户正文；默认文本为 `[go-yunzai] M1 调试回复`。
- 调试处理器启用渲染客户端后，主人发送独立的 `#render` 文本消息会渲染固定测试页面并发送 OneBot `image` 段；超过 8 MiB 的 PNG 不发送并回复提示。普通消息仍回复固定文本。
- 停止时先停止接收新事件，再在 `DefaultShutdownTimeout`（5 秒）内等待在途事件；超时后取消剩余处理器并丢弃，未完成的事件可能没有回复。
- `reportSelfMessage=false` 是 SnowLuma 侧配置。离线 fake 不上报自消息；真实配置与行为未在 M1 改动或验证。

## 真实 SnowLuma 验证（2026-11-14）

已确认 `G:\ALL WORK\SnowLuma` 正在监听 `ws://127.0.0.1:3001/`，配置为 Bearer、`array`、Universal。运行 `TestSnowLumaHandshake` 成功，记录为：

```text
真实 SnowLuma WS 握手成功：url=ws://127.0.0.1:3001/ authorization=Bearer *** message_format=array
```

随后通过 SnowLuma 控制台以账号 `2081344177` 调用 `send_private_msg`，向 `1486632609` 发送 `[go-yunzai] 真实联调测试`，返回 `status=ok`、`retcode=0`、`message_id=1077594689`。随后尝试通过控制台账号 `2406354517` 向 `2081344177` 发起入站验证，但该账号当前不在线，SnowLuma 返回 `404 account offline`，因此没有伪称入站通过。没有修改 SnowLuma 配置；临时 Go 联调配置已删除。联调用例仍默认跳过，只有显式提供 `YUNZAI_TEST_ONEBOT_URL` 和 `YUNZAI_TEST_ONEBOT_TOKEN` 才运行，错误输出会脱敏。

## 插件 SDK 基础阶段（2026-11-14）

新增 `pkg/plugin/sdk` 稳定契约和 `internal/plugin.Manager`。插件现在可以声明描述信息、优先级、事件规则、能力和生命周期；核心按优先级稳定执行规则，插件错误会被记录并隔离，启动失败会回滚已启动插件，停止按逆序执行。策略层支持能力白名单、用户/群黑白名单、全局冷却和规则级冷却覆盖，运行时上下文提供统一账号、用户、群和消息字段。内置调试处理器已通过 SDK 插件路径注册，原有 OneBot 离线闭环和 `#render` 图片闭环保持通过。

### 会话与回复

调用 `Manager.Start(ctx)` 成功后才接收事件；未启动、启动未完成或停止期间的事件会被忽略。`sdk.Context.Reply` 根据当前事件向私聊用户或群发送消息，校验目标，并返回发送结果或错误。

处理器可以调用 `c.Session.Set(timeout, handler)` 等待同一范围内的下一条消息，调用 `Clear()` 取消等待。范围包含插件、账号、用户、群和消息类型；每个范围最多保留一个等待回调，重复设置会替换原回调。超时必须大于零，到期后静默清除；收到下一条通过主体策略检查的消息时先取出回调，最多执行一次。所有会话先于 accept 和普通规则执行；会话返回 `sdk.Stop` 时跳过该插件本次事件的 accept 和普通规则，返回 `sdk.Next` 则继续，其他插件照常执行。会话回调出错时记录错误并继续后续阶段。

```go
err := c.Session.Set(30*time.Second, func(ctx context.Context, next *sdk.Context) (sdk.Result, error) {
    _, err := next.Reply(ctx, sdk.Segment{
        Type: "text", Data: map[string]any{"text": "已收到下一条消息"},
    })
    return sdk.Stop, err
})
```

会话仅存于内存，停止或重启会丢弃；停止后保存的旧会话对象调用 `Set` 返回 `sdk.ErrNotRunning`。内置 `#session` 命令会提示在 30 秒内发送下一条消息，成功后回复 `[go-yunzai] 会话验证完成`；第三条消息恢复普通调试回复。私聊和群聊的完整对话已通过离线 WebSocket 测试。

### 后台任务与停止

插件在 `Register` 中通过 `Router.Task` 注册任务，任务 ID 在同一插件中必须唯一，间隔不能为负数，处理函数不能为空。例如：

```go
func (p *ExamplePlugin) Register(r sdk.Router) error {
    return r.Task(sdk.Task{ID: "poll", Every: time.Minute},
        func(ctx context.Context, task *sdk.TaskContext) error {
            return p.poll(ctx)
        })
}
```

`Every == 0` 表示启动后在后台执行一次；大于零时首次执行等待一个间隔，之后按固定间隔串行执行，同一任务不会重叠。不同任务可以并发。任务只在所有插件启动成功后启动，重复 `Start` 不会重复启动任务。周期任务出错或 panic 会记录错误并等待后续执行；规则、会话和启动/停止回调中的 panic 也会被隔离，不输出 panic 内容。

`Stop` 先取消运行中的事件处理与任务、清除等待会话，等待工作结束后逆序调用插件停止回调。重复或并发停止共用同一次清理；没有传入期限时默认等待 5 秒。应用在 OneBot 停止后取消运行上下文，并为插件停止与事件分发排空共用 5 秒期限。

整体停止超时会返回错误，清理仍在后台继续；结束前禁止重新启动或注册插件，后续可再次 `Stop` 等待完成。处理器、任务和生命周期回调必须主动响应 context 取消，框架无法强制终止忽略取消的 Go 代码；生命周期变更串行执行，尚未返回的启动回调也会延迟并发停止。任务目前不支持 cron、持久化或停机期间的补偿执行。

新增测试覆盖会话替换/清除/过期/隔离/并发单次消费、后台任务取消与重启、周期任务失败和 panic 恢复、任务不重叠、启动失败回滚、停止超时与并发停止，以及私聊/群聊对话和应用退出。

### 角色权限与 accept

规则可设置 `Permission: sdk.PermissionMaster`、`PermissionOwner`、`PermissionAdmin` 或 `PermissionAll`，留空也表示允许所有主体。主人只由对应账号的 `plugins.masters` 配置授予，并绕过规则角色限制；群主和管理员来自协议事件的 `sender.role`，未知角色或身份不一致不会被提升权限。`c.Permissions` 提供 `IsMaster/IsOwner/IsAdmin`，群主也具有管理员标记。

拒绝时沿用旧核心的“暂无权限，只有主人/群主/管理员才能操作”文案，并结束当前事件。旧核心在私聊中不应用 `owner/admin` 限制，这里保留该行为；仅群可用的功能应匹配 `message.group`。`master` 在私聊和群中都检查。规则权限只作用于普通规则；accept 和会话回调如需限制，应自行检查 `c.Permissions`。

插件可在 `Register` 中调用一次 `r.Accept(func(ctx context.Context, c *sdk.Context) (sdk.AcceptDecision, error) { ... })`。事件先通过主体策略，再按优先级处理待定会话，之后进入 accept 阶段，最后匹配普通规则。`Descriptor.Events` 会过滤该插件收到的事件；未声明则接收全部类型。

| accept 返回值 | 后续行为 |
| --- | --- |
| `sdk.AcceptNextPlugin` | 执行下一个插件的 accept |
| `sdk.AcceptBreak` | 跳过剩余 accept，进入所有未跳过插件的普通规则 |
| `sdk.AcceptReturn` | 立即结束整个事件 |

accept 出错、panic 或返回非法值会记录错误并继续其他插件，不会终止进程。普通规则的 `sdk.Stop` 仍仅跳过当前插件剩余规则；与旧 JS 全事件短路的差异保留在后续兼容验证范围内。

### 运行时加载和卸载

启动前使用 `Manager.Register`；应用运行后使用 `application.LoadPlugin(ctx, pluginInstance)`、`application.UnloadPlugin(ctx, pluginID)`，内部对应 `Manager.Load/Unload`。接口接收已构造的 Go 插件实例，提供进程内动态启停；尚不包含磁盘插件发现、原生动态库载入或 JS 宿主。

加载时执行描述和规则校验、能力策略及启动回调，成功后才参与事件处理并启动任务。加载请求结束后的 context 取消不会停止已成功加载的插件；其存活期归应用管理。加载失败不会影响已有插件，启动回调成功后发生取消会调用停止回调回滚；启动回调本身失败时应自行释放其部分初始化资源。

卸载先阻止新的回调，再取消该插件的处理器和任务、清除会话，等待正在执行的工作退出后调用一次停止回调，并清除冷却记录和注册项。卸载不存在的 ID 可重复执行。卸载超时后仍继续清理，同名 ID 在清理结束前不可重新加载，其他插件仍可处理事件或加载；旧事件快照和旧会话对象无法唤起已卸载的实例。整体停止会接管未完成的清理，停止回调不会重复执行。

生命周期接口应由应用控制路径调用；生命周期回调不能同步重入这些接口，处理器也不应同步等待自身卸载，避免等待自身退出。上述角色、accept、动态启停、失败回滚与并发停止路径均已纳入普通测试；`-race` 仍待 C 编译器环境具备后验证。

## go-render 接入

核心组合根创建、持有并关闭 `internal/render.Client`。默认连接 `http://127.0.0.1:8080`，调用 `POST /v1/render`，成功响应必须是可解码的 `image/png`。

- `render.timeout` 是单次 HTTP 尝试的超时；重试和退避时间另计，调用方可通过 context 限制总期限。请求未指定 `timeout_ms` 时由客户端超时推导。
- `render.max_retries` 是首次请求之外的额外重试次数。仅超时、连接失败、429 和 5xx 可重试，退避按 `retry_backoff` 指数增长；400/413 参数错误和坏 PNG 不重试。
- 默认最多 4 个并发调用，等待并发名额可被取消；PNG 响应上限为 32MiB。
- 错误分为 `timeout`、`unavailable`、`invalid_argument`、`invalid_image`，保留服务端 code、HTTP 状态、尝试次数和底层错误。
- `RenderWithFallback` 在失败时返回不含内部细节的可读中文文本，同时保留 `Err` 供调用方记录。
- 业务插件依赖 `render.Renderer` 最小接口；渲染成功的 PNG 通过 `base64://` 内联编码为 OneBot 图片段，失败时发送可读的降级文本。

假 HTTP 服务测试覆盖成功、客户端超时、服务端 504、重试后成功、持续 5xx、400 不重试、非法 PNG、服务未启动、并发上限、本地参数校验和文本降级。

### 真实 go-render 联调

使用本机已有 Chrome 启动隔离服务：

```powershell
$env:CHROMIUM_PATH="C:\Program Files\Google\Chrome\Application\chrome.exe"
& "G:\ALL WORK\音无\tools\go\bin\go.exe" run ./cmd/renderer -addr 127.0.0.1:18080 -concurrency 1 -queue-size 2 -max-timeout 30s
```

随后在 `go-yunzai` 执行：

```powershell
$env:RENDER_INTEGRATION_URL="http://127.0.0.1:18080"
& "G:\ALL WORK\音无\tools\go\bin\go.exe" test ./internal/render -run TestRenderIntegration -v -count=1 -timeout=60s
```

实际输出：

```text
=== RUN   TestRenderIntegration
    integration_test.go:48: real render succeeded: elapsed=564.5207ms elapsed_ms=564 bytes=5577 dimensions=640x400 sha256=1681A662C0C9C67B2840FE20B9D9F21DB5EDB4B78ADB400496059A87D73E0B34
--- PASS: TestRenderIntegration (0.56s)
PASS
ok      github.com/trss-yunzai/go-yunzai/internal/render 1.218s
```

服务端同次请求记录为 `status=200 duration_ms=557`。这是单次联调，不是 P50/P95 性能结论。联调后已停止本次后台服务，确认 `18080` 无剩余监听，也未发现本次 headless Chrome 进程残留。

### 渲染接入阶段验证

```powershell
& "G:\ALL WORK\音无\tools\go\bin\go.exe" fmt ./...
& "G:\ALL WORK\音无\tools\go\bin\go.exe" build ./...
& "G:\ALL WORK\音无\tools\go\bin\go.exe" test ./... -count=1
& "G:\ALL WORK\音无\tools\go\bin\go.exe" vet ./...
```

均退出码 0。完整测试输出（真实联调用例在未设置环境变量时跳过）：

```text
?    github.com/trss-yunzai/go-yunzai/cmd/yunzai [no test files]
ok   github.com/trss-yunzai/go-yunzai/internal/adapter/onebotv11 0.853s
ok   github.com/trss-yunzai/go-yunzai/internal/app 1.846s
ok   github.com/trss-yunzai/go-yunzai/internal/config 1.356s
?    github.com/trss-yunzai/go-yunzai/internal/dispatch [no test files]
?    github.com/trss-yunzai/go-yunzai/internal/event [no test files]
?    github.com/trss-yunzai/go-yunzai/internal/message [no test files]
?    github.com/trss-yunzai/go-yunzai/internal/observability [no test files]
ok   github.com/trss-yunzai/go-yunzai/internal/render 1.827s
?    github.com/trss-yunzai/go-yunzai/plugins/builtin [no test files]
```

## 尚未覆盖

- 真实 SnowLuma 事件与消息往返、角色数据，以及旧 2536 链路停用方式；当前可见外部账号 `2406354517` 不在线。
- M0 历史日志脱敏 manifest / `contract-replay` 和 JS/Go 黄金差分。
- 磁盘插件发现、JS 宿主、更完整的账号模型、通配事件兼容及业务插件迁移；现有普通规则短路语义尚未完成 JS/Go 差分验证。
- 除 `send_msg` 外的动作、撤回/回复/转发等完整消息能力。
- Redis、持久化会话、LevelDB 兼容迁移、cron 调度、媒体 worker、灰度 owner 和回滚。
- `#render` 目前只用于固定测试页面，尚未迁移真实业务插件的页面模板、参数和权限规则。
- M2 及后续里程碑。

## 阶段六第一、二批：插件存储、配置快照与 Redis

`go-yunzai` 现在提供插件专属的 `sdk.KVStore` 和 `sdk.ConfigView`。应用默认在 `storage.path` 指定的位置创建独立 SQLite 文件；示例配置使用 `data/plugin-state.sqlite`，不会打开或改写旧 Node 数据库。存储命名空间固定为 `yzgo:<plugin>:v1`，插件只能拿到自己的范围。

- `Get` 区分不存在/过期、存储不可用、存储已关闭和调用方取消；`Set` 支持零 TTL 永久保存、正 TTL 过期，负 TTL 拒绝；返回值会复制，删除幂等。
- 插件停止后旧 `KVStore` 句柄立即失效，重新加载同 ID 后可以读取已保存的数据；停止不会清空持久化数据。
- 配置快照先完整校验再发布，失败保留旧版本。配置字段采用旧核心的浅覆盖，群配置顺序为默认、账号默认、群、账号与群；同一事件的会话、accept 和规则共用一个版本，下一事件或下一次任务看到新版本。
- `App.ReloadPluginSettings` 只替换插件设置，核心连接、权限和数据库路径仍需重启变更；不会把 OneBot token 放进插件配置视图。
- 内置 `#note` 命令验证了私聊/群聊、账号和用户隔离、SQLite 重启保留、删除、配置限制以及存储故障提示。
- 新增可选 Redis 后端：将 `storage.backend` 设为 `redis` 并填写 `storage.redis.addr` 后，应用组合根会使用 Redis；默认仍为 SQLite。Redis 命令遵循调用方 context，连接/关闭错误转换为稳定存储错误，不会把 Redis 原始错误或密钥返回给插件。
- 新增显式 Redis legacy 适配器，保留旧核心的 `Yz:...` 原始键、JSON 字节、秒级 TTL 和 `INCR` 计数行为。它只对兼容/迁移代码开放；`Batch` 使用普通 pipeline，逐条返回结果，错误不会被包装成事务成功。`Inspect` 只读取指定键并返回大小、SHA-256、JSON 有效性和 TTL，不返回值也不写回。

本批新增 SQLite 持久化、Redis 适配、TTL、命名空间、并发写入、关闭/取消/损坏数据库错误、配置原子替换、插件卸载重载和 OneBot 重启对话测试。旧 LevelDB/SQLite 数据迁移、持久化会话和媒体 worker 仍在后续批次。
