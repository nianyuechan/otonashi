# 工程规范与可扩展性优先计划（2026-11-15）

## 1. 优先级与边界

当前排序：

1. **规范性**：契约、验证、证据、文档先于功能数量。
2. **可扩展性**：接口和生命周期先于具体实现；新增实现不应要求改核心调用方。
3. **功能对齐**：在以上门槛下逐项补 Node 原版功能；不把“先跑通更多功能”作为第一目标。

硬约束：

- 不破坏现有核心消息闭环、插件运行时、渲染客户端和存储行为；每个行为变化必须有回归测试。
- 不把 WAL/NORMAL 设为默认；不先改 `#note` 权限；不先做 M7 出站隔离；不跑 `-race` 作为默认门禁。
- 不重跑 30 分钟/15 分钟长测；长测只在独立受控窗口执行。
- 本计划阶段只做设计、契约和只读复审；代码改造按 Phase 分批提交。

## 2. 当前基线

- HEAD：`c42953b`；工作区干净。
- 统一入口 `scripts/Test-Go.ps1` 已覆盖四个目标：
  `go-yunzai`、`go-render`、`bench`、`bench/tools/sqlite-diag`。
- 当前可自动验证的规范项：gofmt、`go test ./... -count=1 -timeout=120s`、`go build ./...`、`go vet ./...` 全部通过。
- 当前规模：`go-yunzai` 59 个 Go 文件/27 个测试文件；`go-render` 11/5；`bench` 14/1；`sqlite-diag` 1/0。
- 已知缺口：结果 schema 未冻结、无 CI 门禁、`sqlite-diag` 无单元测试、go-render 无 pprof/鉴权/出站隔离、部分 P2/P3 未修。

## 3. 目标定义

### 3.1 规范性完成标准

- 每个模块有明确入口、配置、错误分类和测试入口。
- 结果 JSON 有 schema 版本、字段说明和兼容性测试。
- 统一验证入口是合并前硬门禁；CI 或等价的自动执行存在。
- 每个提交只做一件事，带验证证据；不把测量、文档、脚本和功能混在一个提交。
- 文档只有一个事实源；`docs/INDEX.md` 可检索到全部项目自有 Markdown。
- 关键行为有回归测试；未验证项在文档中显式标注，不写成已达标。

### 3.2 可扩展性完成标准

- 插件、渲染 provider、存储后端、基准 workload 都是可替换接口，不在核心层硬编码单一实现。
- 新增一个插件/provider/backend 时，原则上只新增实现和注册代码，不改调用方协议。
- 接口有版本或兼容策略；新增字段/方法不破坏旧调用方。
- 生命周期（启动、停止、超时、错误、回收）由契约定义，不由调用方各自拼接。
- 配置有 schema/默认值/未知字段策略；新增配置不会静默改变旧行为。

## 4. 工作包

### Q1：基准结果 schema 与兼容层

目标：让 `bench` 结果从“代码结构体直接序列化”变成有版本、可校验、可兼容的契约。

范围：

- 现有 `Result` 只有 `protocol_version`（工作负载协议版本），不能代替结果 schema；
- 在 `bench/internal/loadgen/metrics.go` 的 `Result` 增加 `schema_version`；
- 定义 `summary` 必填/可选字段和类型；
- 增加 JSON Schema 文件（放在 `bench/schema/` 或 `docs/`，二选一，避免双份事实源）；
- `WriteResult` 写入前校验必填字段；
- 增加旧结果反序列化兼容测试；
- 明确 `expected_text_replies` / `missing_text_replies` 是新增可选字段，旧结果缺省为 0。

验收：

- 新旧结果都能读；
- schema 校验失败时有明确错误；
- 字段语义有文档；
- `go test ./...`、`go vet ./...` 通过。

### Q2：统一验证入口与 CI 门禁

目标：把“本地手动跑绿”变成可重复的硬门禁。

范围：

- `scripts/Test-Go.ps1` 保持四个目标覆盖；
- 增加“快速检查”和“长测”分离：默认只跑单元/构建/vet，不跑长测；
- 增加 CI 配置（若仓库托管环境允许）；不支持 CI 时，至少提供 `scripts/Verify-All.ps1` 和固定运行记录；
- `sqlite-diag` 增加最小单元测试（参数解析或临时目录路径），不只依赖 `[no test files]`；
- 更新 README 中“三个模块”的旧表述，改为四个验证目标；
- 记录每次验证的 HEAD、时间、退出码和日志路径。

验收：

- 新提交在统一入口下通过；
- `sqlite-diag` 不再是零测试；
- 验证结果可按 HEAD 追溯；
- 失败时能定位到模块和检查项。

### Q3：插件 SDK 契约

目标：让插件扩展不依赖核心实现细节。

范围：

- 审核 `pkg/plugin/sdk` 的公开类型、错误、上下文和生命周期；
- 明确“稳定 API”与“内部 API”边界；
- 增加 SDK 版本或兼容说明；
- 提供一个最小扩展示例（测试插件或文档示例）；
- 契约测试覆盖：注册、规则、权限、会话、任务、停止、错误隔离。

验收：

- 新增插件只依赖 SDK 和文档；
- 公开 API 有版本/兼容说明；
- 核心 Manager 不向插件泄漏具体存储/渲染实现；
- 现有插件测试通过。

### Q4：渲染 provider 与生命周期

目标：在已有 `Page`/`Browser`/`BrowserFactory` 基础上补齐 provider 选择、生命周期和可观测性，支持替换/测试。

范围：

- 保留现有 `Page`、`Browser`、`BrowserFactory` 接口，不重复引入基础抽象；
- 增加 provider 注册/选择机制（chromedp 为默认实现），让部署层不必改 `main` 才能换实现；
- 明确请求队列、并发、超时、Close、worker 回收语义；特别是 `Close` 超时返回后仍在运行的 worker 由谁负责；
- 增加 opt-in pprof/goroutine 快照（默认关闭）；
- 暂不做鉴权和出站隔离，登记为 M7 安全项；
- 保持现有 HTTP 契约和降级行为不变。

验收：

- 单元测试可用 fake provider 验证队列/超时/Close；
- 浏览器 provider 行为不回归；
- pprof 端点默认关闭，开启不影响正常请求；
- 渲染器 goroutine 有界性可被独立观测。

### Q5：存储接口与错误分类

目标：让存储后端可替换，并让上层不依赖 SQLite 细节。

范围：

- 审核 `internal/storage` 的公开接口、事务、错误类型；
- 把“打开/迁移/读写/WAL 实验”分成明确层次；
- 明确错误分类（连接失败、忙、超时、约束、关闭）；
- 不改变默认 FULL；
- 为存储接口增加 fake 或内存实现用于上层测试。

验收：

- 上层测试不依赖真实 SQLite 文件；
- 错误可用类型判断，不靠字符串匹配；
- 默认 FULL 行为不变；
- WAL+NORMAL 仍为显式实验开关。

### Q6：配置 schema 与默认值

目标：新增配置不静默改变旧行为。

范围：

- `go-yunzai` 已有 `KnownFields(true)` 和手写 `Validate`；保留并补充字段类型/默认值/未知字段的文档与测试；
- 为 `go-render` CLI flag 和 `bench` flag 定义等价的字段、默认值和校验规则；
- 增加配置校验测试；
- 明确“空值/缺省/显式 false”的区别；
- 文档与配置示例保持单一事实源。

验收：

- 缺失字段按文档默认值生效；
- 未知字段不会静默忽略到危险状态；
- 配置错误在启动时给出明确错误；
- 现有配置兼容。

### Q7：文档、ADR 与证据

目标：规范不是口头约定，而是可检索的决策记录。

范围：

- 新建 `docs/adr/`，至少记录：
  - 结果 schema 版本策略；
  - 渲染 provider 接口；
  - 存储错误分类；
  - 插件 SDK 稳定性边界；
- `docs/INDEX.md` 纳入 ADR、本计划和复审报告；
- 所有结果/日志命名规则写入文档；
- 证据目录只保留可追溯文件，历史不可复核项显式标注。

验收：

- 关键接口变更都有 ADR；
- `docs/INDEX.md` 无漏项；
- 新成员可按文档完成一次验证；
- 不出现 `bench/reports/` 旧路径。

### Q8：Node→Go 功能兼容矩阵

目标：把“功能对齐”从口头声明变成可检查的矩阵。

范围：

- 建立功能矩阵：命令、事件、权限、会话、插件、渲染、存储、配置、错误语义；
- 标注“已实现/已测试/未实现/不适用”；
- 每个“已实现”指向代码和测试；
- 对已知差异（32 KiB、panic、渲染降级、出站隔离）标注风险等级；
- 不要求本轮补齐全部功能，只要求不再声称“严格等价”。

验收：

- 每个功能有状态和证据；
- 未验证项不被写成达标；
- 后续功能开发按矩阵排期。

## 5. 阶段顺序

### Phase 1：契约与门禁（优先）

Q1 结果 schema → Q2 统一验证/CI → Q7 文档/ADR 骨架。

产出：schema、验证入口、ADR、索引更新。

### Phase 2：扩展接口

Q3 插件 SDK → Q4 渲染 provider → Q5 存储接口 → Q6 配置 schema。

产出：接口、fake/测试实现、契约测试；不改变现有行为。

### Phase 3：兼容矩阵与功能补齐

Q8 功能矩阵 → 按矩阵补功能 → 每项回归。

产出：Node→Go 对照表、逐项回归、发布说明。

## 6. 验证与回滚

每个 Phase 的验证：

- `scripts/Test-Go.ps1` 四个目标 exit 0；
- 新增契约测试；
- `git diff --check`；
- 关键行为回归（消息闭环、插件、渲染、存储）；
- 结果 schema 兼容测试；
- 不跑长测时，在计划中记录“未跑”及原因。

回滚：

- 每个 Phase 独立提交；接口变更保留旧实现适配层；
- schema 变更保留旧字段读取；
- provider/存储变更用 feature flag 或构造注入，默认走旧路径；
- 失败时优先回滚该 Phase 的提交，不回滚历史证据。

## 7. 非目标

- 不把 Node 全部功能在本轮一次性补齐；
- 不把 WAL/NORMAL 设为默认；
- 不先做 M7 出站隔离和鉴权；
- 不把 `-race` 设为默认门禁；
- 不重跑长测；
- 不在本计划阶段修改生产代码。

## 8. 复审门禁

计划写完后，允许对当前代码做一次只读复审，重点检查：

- 现有接口是否已具备可扩展性；
- 结果/配置/错误契约是否有版本和测试；
- 是否存在硬编码实现阻碍替换；
- 文档和验证入口是否足以支撑规范。

复审只读：不改代码、不重跑长测、不提交；复审报告单独落盘，发现按 P1/P2/P3 分级，并标注证据路径和行号。

## 9. Phase 1 执行记录（2026-11-15）

### Q1 结果 schema

- 提交：`bdadc08 bench: version and validate result schema`。
- `Result` 增加 `schema_version`，常量 `ResultSchemaVersion` 固定为 `"1"`；`protocol_version` 继续表示负载协议，两个版本不混用。
- `Result.Validate` 校验版本、模式、负载、时间、`sent` 和计数非负；`WriteResult` 写入前调用，失败不落盘。
- 机器 schema：`bench/schema/result-v1.schema.json`；口径文档：`docs/result-schema-v1.md`。
- 测试：`TestWriteResultAtomic`、`TestWriteResultRejectsMissingSchemaVersion`、`TestLegacyResultJSONReads`、`TestResultSchemaFileIsValidJSON`；`go test ./internal/loadgen` 通过。
- 旧结果不重写：没有 `schema_version` 的历史 JSON 仍可 `json.Unmarshal`，但不作为新结果模板。

### Q2 统一验证/门禁

- 提交：`5591f4b test: add four-target gate and verify log`、`daad916 test: serialize go checks for low-memory host`。
- `bench/tools/sqlite-diag` 增加 `TestDBPathUsesTempDirectory`，不再依赖 `[no test files]`。
- 新增 `scripts/Verify-All.ps1`：记录 HEAD、时间、日志路径；调用 `Test-Go.ps1`。
- `Test-Go.ps1` 增加 HEAD 记录；默认 `-Parallelism 1`，可用参数覆盖，避免本机 16GB 内存下 Go 包级并行触发分页文件错误。
- 修正 `go build`/`go vet` 的参数顺序，`-p` 必须在 `./...` 之前。
- 当前仓库没有配置 Git remote，因此没有添加 GitHub Actions；本地统一入口作为当前等价门禁，待托管后接入 CI。
- 验证：`Verify-All.ps1 -Parallelism 1` 在脚本 HEAD `daad916` 四目标 exit 0，日志 `bench/env/results/verify-daad916-20261115-145342.log`，用时 68.49s；此前代码 HEAD `5591f4b` 也通过（日志 `verify-5591f4b-20261115-145016.log`，121.13s）。文档提交不改代码。
- 未跑：30 分钟/15 分钟长测、`-race`（仍受 CGO/C 编译器约束）；历史结果未重写。
