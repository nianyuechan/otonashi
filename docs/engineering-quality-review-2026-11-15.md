# 工程规范与可扩展性复审（2026-11-15）

## 1. 范围与结论

- 复审对象：当前 HEAD `c42953b`；工作区干净。
- 复审性质：只读；未修改代码、未提交、未重跑 30 分钟/15 分钟长测。
- 复审重点：结果/配置契约、插件 SDK、渲染 provider 与生命周期、存储接口、验证门禁、文档一致性。
- 结论：**项目已经具备一部分可扩展基础设施，但规范性和可扩展性仍不能称为“保证”。** 最需要先补的是结果 schema 和统一门禁；渲染器与 go-yunzai 的边界问题属于第二批。

## 2. 已具备的基础（避免重复造接口）

- `go-render` 已有 `Page`、`Browser`、`BrowserFactory` 接口（`go-render/internal/renderer/service.go:21-32`），并有基于 fake provider 的并发、队列、页面/浏览器重建测试（`service_test.go`）。
- `go-yunzai` 已有应用级 `storage.Backend` 接口和插件侧 `sdk.KVStore` 接口（`internal/storage/backend.go:5-10`、`pkg/plugin/sdk/storage.go:25-29`），错误也有类型化定义。
- `go-yunzai` 配置使用 `yaml.Decoder.KnownFields(true)`，未知字段会报错（`internal/config/config.go:127-143`）。
- `go-yunzai` 插件 SDK 已有 `Plugin`、`Router`、`Descriptor`、`Session` 等公开契约；插件回调已有 panic 隔离（`internal/plugin/dispatch.go:184-203`）。
- `go-render` HTTP 层使用 `DisallowUnknownFields` 和 `api.Request.Validate`（`internal/httpapi/handler.go:58-80`）。

## 3. 发现

| ID | 级别 | 发现 | 证据 | 影响 | 对应计划 |
|---|---|---|---|---|---|
| P1-1 | P1 | `bench` 结果没有 `schema_version`；`WriteResult` 只序列化，不校验必填字段 | `bench/internal/loadgen/metrics.go:69-90`、`:222-240` | 新旧结果无法机器判定兼容性；新增字段只能靠约定 | Q1 |
| P1-2 | P1 | 统一验证只存在于本地脚本；没有 CI 或等价强制门禁；`sqlite-diag` 仍无测试 | `scripts/Test-Go.ps1:51`、`:76`；`bench/tools/sqlite-diag` 无 `_test.go` | “本地跑绿”不能约束后续提交；独立模块可能被绕过 | Q2 |
| P2-1 | P2 | `go-render` 没有 pprof/goroutine 快照通道 | `go-render` 全模块无 `pprof` 引用；`cmd/renderer/main.go:45-53` 只注册 Prometheus | 渲染器 goroutine 有界性无法独立验证；“恒 9”只能证明 bot 主进程 | Q4 |
| P2-2 | P2 | `go-render` HTTP 端点无鉴权；注入 HTML 可触发任意 `http(s)` 资源抓取，无代理/白名单 | `internal/httpapi/handler.go:39-56`；`internal/renderer/browser.go:131-134`、`:230-237` | 渲染服务不是安全边界；属于已知 M7 缺口 | Q4/Q7，M7 排期 |
| P2-3 | P2 | `go-yunzai` 入站 `conn.Read` 没有设置读上限 | `internal/adapter/onebotv11/client.go:222-240` | 沿用库默认约 32 KiB；大图/转发/大数组事件可能 `MessageTooBig` 断连 | Batch D |
| P2-4 | P2 | `go-yunzai` 顶层没有 panic recover；recover 只在插件回调/生命周期 | `cmd/yunzai/main.go:17-40`；`internal/plugin/dispatch.go:184-203`、`lifecycle.go:361/403`、`manager.go:131` | 非插件路径的 panic 会终止进程，不满足“可恢复”规范 | Batch D |
| P2-5 | P2 | 插件 SDK 没有 SDK 级版本/兼容策略；`Clock` 仍是保留接口 | `pkg/plugin/sdk/sdk.go:1-2`、`Descriptor:36-43`、`Clock:220-223` | 扩展者无法判断 API 稳定性；后续变更易破坏插件 | Q3 |
| P2-6 | P2 | `go-render` 的浏览器实现已接口化，但 provider 选择仍硬编码在 `main`，没有多 provider 注册/替代实现 | `cmd/renderer/main.go:47-53` | 核心接口可扩展，部署层仍只能改代码换实现 | Q4 |
| P2-7 | P3 | `Service.Close` 超时返回后 worker 可能继续运行；测试已固定该语义，但契约未写明调用方如何处理 | `internal/renderer/service.go:227-251`；`service_test.go:379-414` | 关闭后可能出现“已返回但仍有 worker”的窗口，资源回收责任不清 | Q4 |
| P3-1 | P3 | README 仍写统一脚本检查三个模块；实际已四个目标 | `docs/README-go-yunzai.md:56-58` | 文档与门禁不一致，新成员会漏掉 `sqlite-diag` | Q2/Q7 |
| P3-2 | P3 | `bench` 结果字段继续直接扩展，历史 JSON 没有 schema 文档 | `metrics.go:28-67`；`docs/phase0-baseline.md` 只描述字段含义 | 字段语义可读但不可机器校验；跨版本比较靠人 | Q1 |

## 4. 功能兼容性说明

- `docs/README-go-yunzai.md:5-10` 明确当前是 **M1 骨架**：OneBot v11 正向 WS 离线闭环、go-render 客户端、插件运行时基础；真实入站消息往返仍待外部测试账号。
- 因此本次复审**不是** Node→Go 全功能等价审查；没有功能矩阵和并排回归，不能写“严格遵守原版功能”。
- 已实现功能按现有测试通过，不等于所有原版命令、插件、错误语义都已覆盖。

## 5. 与计划的对应

- **Phase 1**：先做 Q1（结果 schema）和 Q2（门禁），再做 Q7（ADR/文档）；这两项改动小、约束面大。
- **Phase 2**：Q3 插件 SDK、Q4 渲染 provider/生命周期、Q5 存储接口、Q6 配置 schema。
- **Batch D**：32 KiB、顶层 panic、渲染出站隔离等仍在后续；本轮不修。

## 6. 复审验证

- 只读检查；未跑长测；未跑 `-race`。
- 现有统一入口的四个目标此前已通过（见 `docs/fix-plan-2026-11-15.md` §5）；本次复审未改动代码，因此不重复长跑。
- 复审报告与计划同时登记到 `docs/INDEX.md`。

## 7. Phase 1 后续状态（2026-11-15）

本报告是 HEAD `542d207` 时的只读快照；后续 Phase 1 提交已处理两项 P1：

- Q1：`bdadc08` 增加 `schema_version`、`Result.Validate`、写入前校验、JSON Schema 和旧 JSON 读取测试；P1-1 的“无版本/不校验”已不再成立。
- Q2：`5591f4b` + `daad916` 增加 `sqlite-diag` 测试、`Verify-All.ps1`、HEAD/日志记录和低并行默认；P1-2 的“无测试/无强制入口”已部分关闭，但仓库无 Git remote，CI 仍未接入，当前以本地统一入口为等价门禁。
- README 的“三个模块”旧表述已在 `5591f4b` 修正为四个目标；P3-1 关闭。
- 仍未关闭：P2-1 pprof、P2-2 鉴权/出站隔离、P2-3 32 KiB 读上限、P2-4 顶层 panic、P2-5 SDK 版本策略、P2-6 provider 选择、P2-7 Close 语义；这些留给 Phase 2/Batch D。
- 验证：`Verify-All.ps1 -Parallelism 1` 在脚本 HEAD `daad916` 四目标 exit 0；日志 `bench/env/results/verify-daad916-20261115-145342.log`。未跑长测和 `-race`。
