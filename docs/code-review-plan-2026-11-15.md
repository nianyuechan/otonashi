# 当前代码分步审查计划（2026-11-15）

## 1. 目标与边界

### 1.1 目标

从当前 HEAD 重新、独立地审查项目代码，不沿用上一轮审查结论作为事实依据；按模块、按风险、按证据逐步推进，最终回答三个问题：

1. 当前代码是否存在可复现的缺陷、测量口径问题或验收缺口？
2. 上一轮阶段零证据是否与原始结果和 `plan.md` 目标一致？
3. 如果存在问题，最小修复范围是什么，是否应先修再复跑？

### 1.2 当前基线

- 项目根：`G:\ALL WORK\音无\音无`
- 当前 HEAD：`d7afca9`（`fix: harden phase-zero benchmark runners`）
- 上一轮基线提交：`bbb4c32`
- 工作区：`next-agent-prompt.md` 未跟踪；其余文件干净
- 三个 Go 模块：`go-yunzai`、`go-render`、`bench`
- 统一验证入口：`scripts/Test-Go.ps1`
- 阶段零约束：严格按 `plan.md` 的阶段零目标和回退门槛执行；不进入阶段九/十/十一
- 生产代码约束：审查阶段不修改生产语义；修复必须单独列出并经确认

### 1.3 不在本轮范围

- 不把 WAL/NORMAL 设为默认；
- 不先修改 `#note` 权限；
- 不先做 M7 渲染出站隔离；
- 不承诺运行 `-race`（当前 `CGO_ENABLED=0`，缺 C 编译器）；
- 不重跑 30 分钟 Z2 或 15 分钟 Z2-B，除非后续明确决定；
- 不清理或覆盖任何已验证的原始证据文件。

## 2. Markdown 集中归档方案

### 2.1 目标目录

统一放在：

`G:\ALL WORK\音无\音无\docs\`

采用扁平结构。只迁移项目自有 Markdown，不迁移 `.git`、`mysy2`、`node_modules`、`bench/env` 内第三方/运行时文档。

### 2.2 迁移清单

当前项目自有 Markdown 共 15 个受跟踪文件，另有 1 个未跟踪交接文档；加上本计划，最终全部进入 `docs/`：

| 原路径 | 目标路径 | 说明 |
|---|---|---|
| `2026-11-12/bun-migration/outputs/trss-yunzai-bun-migration-assessment.md` | `docs/trss-yunzai-bun-migration-assessment.md` | 保持文件名 |
| `2026-11-12/perf-baseline/outputs/TRSS-Yunzai-perf-stability-baseline.md` | `docs/TRSS-Yunzai-perf-stability-baseline.md` | 保持文件名 |
| `2026-11-12/trss-refactor-assessment/outputs/trss-yunzai-refactor-assessment.md` | `docs/trss-yunzai-refactor-assessment.md` | 保持文件名 |
| `2026-11-13/framework-survey/outputs/robot-framework-survey.md` | `docs/robot-framework-survey.md` | 保持文件名 |
| `2026-11-13/go-rewrite-blueprint/outputs/TRSS-Yunzai-Go-native-rewrite-blueprint.md` | `docs/TRSS-Yunzai-Go-native-rewrite-blueprint.md` | 保持文件名 |
| `2026-11-13/source-verification/outputs/trss-yunzai-bun-migration-source-verification.md` | `docs/trss-yunzai-bun-migration-source-verification.md` | 保持文件名 |
| `audit-completed-work-2026-11-14.md` | `docs/audit-completed-work-2026-11-14.md` | 保持文件名 |
| `bench/reports/capacity-100-groups-200rps.md` | `docs/capacity-100-groups-200rps.md` | 保持文件名 |
| `bench/reports/phase0-baseline.md` | `docs/phase0-baseline.md` | 保持文件名 |
| `bench/reports/phase0-profile-2026-11-14.md` | `docs/phase0-profile-2026-11-14.md` | 历史快照 |
| `bench/reports/phase0-profile-2026-11-15.md` | `docs/phase0-profile-2026-11-15.md` | 当前 HEAD profile |
| `go-yunzai/README.md` | `docs/README-go-yunzai.md` | 避免与 go-render README 重名 |
| `go-render/README.md` | `docs/README-go-render.md` | 避免重名 |
| `plan.md` | `docs/plan.md` | 阶段零主计划 |
| `review-new-changes-2026-11-14.md` | `docs/review-new-changes-2026-11-14.md` | 上一轮审查记录 |
| `next-agent-prompt.md` | `docs/next-agent-prompt.md` | 未跟踪交接文档，迁移前保留 SHA256 |
| 新建 `docs/code-review-plan-2026-11-15.md` | 本文件 | 当前审查计划 |

### 2.3 迁移规则

1. 受 Git 跟踪的文件使用 `git mv`，保留历史；
2. `next-agent-prompt.md` 是未跟踪文件：先计算 SHA256，再用 `Move-Item -LiteralPath` 精确移动，移动后复算 SHA256；
3. 不移动依赖目录中的 README/LICENSE/CHANGELOG；
4. 不修改正文，只做必要的路径引用更新；
5. 两个 README 在 `docs/` 中使用带来源前缀的文件名，并在开头加一行来源说明，避免读者误以为源码相对路径仍在当前目录；
6. 更新非 Markdown 代码中的文档路径引用，例如 `bench/internal/loadgen/config.go` 中的 `plan.md` 注释；
7. 执行 Markdown 相对链接检查，确认所有 `](...)` 本地链接仍可解析；
8. 使用 `git status`、`git diff --summary` 和文件 SHA256 三路核对，确保只发生移动和必要的路径文字更新。

### 2.4 归档验收

- `docs/` 中包含全部项目自有 Markdown；
- 旧路径不再残留同名 Markdown；
- `git diff --summary` 以 rename 为主，正文改动只限路径/来源说明；
- `next-agent-prompt.md` 内容 SHA256 移动前后一致；
- 链接检查无失效本地链接；
- 提交必须与代码审查提交分开（先归档，再审查）。

## 3. 分步审查流程

### Step 0：建立独立基线

- 确认 HEAD、工作区、模块、测试入口、构建工具链；
- 记录 `go version`、PowerShell 版本、Go 工具链路径；
- 建立文件清单：模块、入口、配置、脚本、测试；
- 本轮审查不直接相信上一轮报告中的结论，只把其作为待验证假设。

### Step 1：模块与架构地图

- 阅读 `go-yunzai` 的入口、插件注册、事件链路、配置加载、存储接口；
- 阅读 `go-render` 的 HTTP/渲染服务、浏览器生命周期、队列与降级路径；
- 阅读 `bench` 的 loadgen、onebot-bench、配置、结果 schema、脚本；
- 阅读 `plan.md` 阶段零目标表和回退门槛；
- 输出一份“模块—入口—关键不变量—对应测试”的映射，作为后续审查索引。

### Step 2：构建与测试基线

- 运行 `scripts/Test-Go.ps1`，记录三个模块的 `gofmt -l`、`go test ./...`、`go build ./...`、`go vet ./...`；
- 检查测试日志中的 exit code、超时、OOM、skip 和已知环境停滞；
- PowerShell 脚本做语法解析；
- 检查 `bench/tools/sqlite-diag` 是否被统一测试覆盖，若没有，单独记录；
- 不在这一阶段修改代码，只记录真实基线。

### Step 3：静态代码审查

按以下顺序逐模块检查：

1. `go-yunzai`：入口、配置、插件/事件分发、连接生命周期、存储与错误处理；
2. `go-render`：服务端点、并发/队列、浏览器进程管理、降级与恢复；
3. `bench`：loadgen、结果统计、重连、action 计数、store 测量、脚本参数；
4. `scripts`：`Test-Go.ps1`、基准启动脚本、结果校验、失败清理；
5. `bench/tools/sqlite-diag`：独立模块、超时、路径、编码、是否被测试覆盖。

对每个发现记录：

- 文件/行号；
- 触发条件；
- 可复现证据或代码路径；
- 是否影响阶段零结论；
- 严重度（P1/P2/P3）；
- 根因或明确标注“假设，未证实”。

### Step 4：动态与边界验证

只做低成本、可回滚的验证：

- PowerShell 脚本解析和非法参数拒绝；
- render 的零动作假成功路径、错误 bot、错误配置；
- Z1 失败清理路径的静态/短路径验证；
- manifest 从 capture 重建；
- 结果 summary 的字段、阈值和口径；
- 不在未确认前重跑 30 分钟/15 分钟长测。

### Step 5：阶段零证据复核

逐项对照原始结果 JSON 与 `plan.md`：

- Z1 250/600：actual、missing、CPU、RSS、profile 是否同构建；
- Z2-A 200 target 30 分钟：`missing=0` 是否达成，`action_errors` 是否纳入硬标准；
- Z2-B 400 target 15 分钟：`missing=0` 与 `action_errors` 是否达成；
- Z3：单次、并发+队列、降级、恢复的 `action_count`、`text_replies`、metrics；
- Z4-A：低速 FULL 30 分钟是否只作为低速基线；
- P0：`Test-Go` 的失败窗口、重试窗口和最终树是否被正确解释；
- 所有“未达标/未运行”的项必须显式保留，不得被摘要掩盖。

### Step 6：形成审查报告

输出 `docs/code-review-2026-11-15.md`，包含：

1. 审查范围与基线；
2. 构建/测试结果；
3. 按严重度排序的发现；
4. 已确认的测量/验收缺口；
5. 与上一轮结论的差异；
6. 建议修复顺序；
7. 明确“未做/未验证”的项；
8. 不把推测写成根因。

### Step 7：修复决策

- 先提交审查报告，再决定是否修代码；
- P1 修复必须单独提交，附最小验证；
- 不在审查阶段顺手改生产逻辑；
- 任何复跑把目标、时长、环境、原始结果路径写清楚。

## 4. 交付物

- `docs/code-review-plan-2026-11-15.md`（本文件）
- `docs/code-review-2026-11-15.md`（最终审查报告）
- `docs/INDEX.md`（Markdown 归档索引，若迁移后需要）
- 构建/测试日志路径清单
- 如实施修复：单独提交与验证记录

## 5. 完成标准

- 每个结论都有文件/行号或原始数据支撑；
- 构建、测试、脚本解析均有当前 HEAD 的实际结果；
- 阶段零每个目标都有 PASS/FAIL/未验证标注；
- Markdown 已全部归档且本地链接可用；
- 没有未经确认的生产代码改动；
- 没有进入阶段九/十/十一。

## 6. 执行方式

默认由主 Agent 单写入、按步骤执行，不并行派发写操作的子代理；如需独立复核，只派发只读审查任务，避免同一工作树的多写入冲突。
