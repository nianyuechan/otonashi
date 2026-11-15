# ADR 0004：插件 SDK 是稳定契约，内部包不是

- 状态：已采纳（边界已落地）；稳定性分级标注已落地；版本号机制仍未引入
- 相关代码：`go-yunzai/pkg/plugin/sdk`（公开契约）、`go-yunzai/internal/plugin`（Manager/分发/生命周期，内部实现）、`go-yunzai/internal/{config,storage,adapter,render}`（内部实现）

## 背景

插件是这套 Go 核心的扩展面：外部作者要能只读 SDK 和文档就把插件写出来，核心内部怎么重构都不该让他们改代码。现状是：

- SDK 已经有一组公开类型（`Plugin`、`Descriptor`、`Router`、`Context`、`Session`、`Task`…），但没有明确的"哪些稳定、哪些会变"的声明；
- 内部包（`internal/plugin.Manager`、`internal/config`）与 SDK 都在同一个仓库，很容易被插件作者直接引用；
- 没有 SDK 版本号或兼容策略，后续改字段时无法判断会不会破坏已有插件。

## 决定

### 1. 稳定契约清单（`pkg/plugin/sdk`，插件只能依赖这些）

| 类别 | 类型/符号 | 稳定性 |
|---|---|---|
| 插件身份 | `Descriptor`（ID/Version/Priority/Events/Capabilities） | 稳定 |
| 生命周期 | `Plugin`（Descriptor/Register/Start/Stop） | 稳定 |
| 注册面 | `Router`（Rule/Accept/Task） | 稳定 |
| 规则 | `Rule`、`Result`（Stop/Next）、`HandlerFunc`、`RegexEngine`（当前只有 RE2） | 稳定 |
| 上下文 | `Context`（Event/Sender/Runtime/Session/Permissions/Store/Config）、`TaskContext` | 稳定 |
| 会话 | `Session`（Set/Clear，一次性、按账号/用户/消息类型/群隔离） | 稳定 |
| 任务 | `Task`、`TaskFunc`（串行、必须响应 ctx 取消） | 稳定 |
| 事件模型 | `Event`、`Segment`、`SenderInfo`、`RuntimeContext` | 稳定 |
| 发送 | `Sender`、`Outgoing`、`SendResult`、`(*Context).Reply` | 稳定 |
| 策略 | `Policy`、`Capability`（messages/render/storage） | 稳定 |
| 权限 | `Permission` 及角色常量 | 稳定 |
| 存储 | `KVStore`、`ConfigView`、六个哨兵错误 | 稳定 |
| 时间 | `Clock` | **保留**（接口占位，未启用；不得依赖） |

### 2. 内部实现（插件不得直接引用）

- `internal/plugin`：Manager、规则分发、生命周期与 panic 隔离的实现细节；
- `internal/config`、`internal/storage`、`internal/adapter`、`internal/render`；
- `internal/observability`（pprof 端点）。

插件通过 SDK 拿到的是"已经脱敏、已经按账号/群隔离"的对象（`RuntimeContext`、`KVStore`、`Session`），而不是核心的内部结构；`Manager.withPlugin` 等实现细节不出现在 SDK 里。

### 3. 变更规则

| 变更类型 | 处理 |
|---|---|
| 新增 SDK 类型/字段/方法（不改已有语义） | 兼容，直接加；在本文表格登记 |
| 删除、改名、类型变化 | 不兼容：必须写 ADR + 迁移说明，并给出至少一个版本的过渡期 |
| 语义变化（如 `Session` 消费顺序、`Stop` 的作用范围） | 视为不兼容，必须有契约测试 + ADR |
| `Clock` 这类"保留接口" | 未启用前不视为契约；启用时单独 ADR |

### 4. 本轮不做版本号机制

理由：当前是单仓库单发行，SDK 与核心同版本发布，没有独立分发渠道，也没有第三方插件仓库需要解析版本。Q3 再决定是否引入语义化版本号与稳定性标注（`stable`/`experimental`/`internal`）。

### 5. 稳定性分级（Q3 已落地，2026-11-15）

`go-yunzai/pkg/plugin/sdk/stability.go` 用一张表声明每个导出符号的等级，`StabilityOf(name)` 供文档、工具与测试查询：

| 等级 | 含义 | 当前适用 |
|---|---|---|
| `stable` | 契约：删除、改名、改类型、改语义都算不兼容，必须 ADR + 迁移说明 | SDK 里绝大多数符号 |
| `experimental` | 可以变、可以消失，插件不得当成定稿 | 目前为空（没有真正处于试验期的导出符号） |
| `reserved` | 占位，不得依赖，没有行为保证 | `Clock` |

**没有 `internal` 等级**：`sdk` 包里导出的东西按定义就是插件能碰到的，标成 `internal` 是一句编译器无法兑现的承诺；实现细节放在 `internal/*` 包，由 Go 的编译期规则挡住。

分级不是"写在注释里就算"：`stability_test.go` 用 `go/ast` 扫描本包所有非测试文件，要求**每个导出顶层符号都被登记**，同时**登记表里不能有已经不存在的符号**。新增一个导出类型却忘了分级，测试直接失败——这样表不会和代码漂移。

`experimental` 现在没有条目，是因为现有导出符号都已稳定；等级本身保留，等真正需要试错的接口再用。

## 后果

- 插件作者可以只读 `pkg/plugin/sdk` 与 `docs/README-go-yunzai.md`；核心重构只要不改 SDK 就不影响插件。
- 插件回调的 panic 被隔离（`internal/plugin/dispatch.go`）、生命周期失败会回滚，这些行为对插件是稳定的可观测语义。
- 没有版本号带来的代价：SDK 不兼容变更只能靠 ADR 与提交信息追溯，外部作者无法用版本号判断"我的插件还能不能跑"。这是 Q3 要补的。
- 插件不能拿到 `Manager`、不能读写别的插件的命名空间、不能拿到 core 凭据（`ConfigView` 明确排除核心凭据）。

## 备选方案与被否原因

| 方案 | 为什么不用 |
|---|---|
| 直接导出 `internal/plugin` 让插件用 Manager | 插件会耦合分发/生命周期实现，核心每重构一次就要改插件；也破坏隔离（能直接操作别的插件）。 |
| 现在就给 SDK 加语义化版本号 | 没有消费者：单发行、无第三方插件源。加了只会变成"文档里的新字段"，维护成本先到、收益后到。 |
| 用 `any`/反射做通用扩展点 | 失去类型检查，插件错误在运行时才暴露；也让契约无法用测试固定。 |
| 把 `Clock` 直接标成稳定并启用 | 现在没有确定性时间注入的实现与测试；标成稳定等于承诺一个不存在的契约。保留为占位、明确"不得依赖"更诚实。 |
| 用 build tag 隔离内部包（`internal` 之外再分层） | Go 的 `internal` 已经提供了编译器级隔离；再加一层只是命名游戏。 |

## 如果重来一次

仍然会把"稳定契约"压在一个包里、把实现留在 `internal`。唯一会提前的是：在第一批 SDK 落地时就写下这张稳定性表——因为"没有声明"本身会被读成"都可以依赖"，而修正这种预期比一开始声明要贵得多。
