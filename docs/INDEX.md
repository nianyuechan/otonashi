# Markdown 归档索引

本目录集中存放项目自有 Markdown 文档。此前的 15 个受跟踪文档已用 `git mv` 迁移到这里，未跟踪的 `next-agent-prompt.md` 也已移入；第三方依赖、`mysy2`、`node_modules` 和运行时证据目录中的文档不在归档范围。

2026-11-15 起新增 `docs/adr/`（架构决策记录）、`docs/evidence/`（可复核快照）与两份证据规范文档，结构见 §1。

## 1. 目录结构

| 位置 | 内容 | 入口 |
|---|---|---|
| `docs/*.md` | 计划、基线、审查、两个 README 的归档 | 本文件 §3 |
| `docs/adr/` | 架构决策记录 0001–0005（固定结构：状态/背景/决定/后果/备选方案） | `docs/adr/` |
| `docs/evidence/` | 文档结论的可复核快照（20 个文件，单文件 ≤ 50MB） | `docs/evidence-index.md` §2 |
| `docs/evidence-index.md` | 归档清单、未归档清单、按 HEAD 取回与复算方法、归档选择规则 | — |
| `docs/evidence-conventions.md` | 结果/日志命名、进库口径、Fast/Full 门禁、评审清单 | — |

## 2. 2026-11-15 维护性收口新增

| 文件 | 说明 |
|---|---|
| `docs/adr/0001-result-schema-versioning.md` | 结果 JSON 为什么用独立的 `schema_version`，v1 字段怎么增删 |
| `docs/adr/0002-render-provider-interface.md` | `Page`/`Browser`/`BrowserFactory` 为什么这样切、provider 怎么替换 |
| `docs/adr/0003-storage-error-taxonomy.md` | 存储为什么用六个哨兵错误、默认为什么保持 FULL |
| `docs/adr/0004-plugin-sdk-stability.md` | 插件 SDK 哪些是稳定契约、哪些是实现、变更怎么处理；稳定性分级已落地 |
| `docs/adr/0005-render-provider-registry.md` | 渲染后端如何按名字注册与选择（Q4），以及它的已知边界 |
| `docs/evidence-index.md` | 证据索引：哪些进 `docs/evidence/`、哪些只留本机、怎么按 HEAD 取回 |
| `docs/evidence-conventions.md` | 证据与验证约定（命名/进库/门禁/追溯） |

## 3. 路径约定

- `docs/plan.md` 等阶段零/审查文档中的 `bench/...`、`go-yunzai/...` 源码路径仍按项目根解析。
- `docs/README-go-yunzai.md` 中的源码路径仍相对 `go-yunzai/`；`docs/README-go-render.md` 同理相对 `go-render/`。
- 两个 README 因原文件名相同，归档时加了来源前缀，避免同名覆盖。

> **历史路径说明（2026-11-15）**：下面迁移表的"原路径"列，以及历史文档（`audit-completed-work-2026-11-14.md`、`code-review-plan-2026-11-15.md`、`fix-plan-2026-11-15.md`、`engineering-quality-*.md`、`review-new-changes-2026-11-14.md`）正文里出现的 `bench/reports/...` 与 `bench/env/results/...` 都是**当时的位置**，不是现行路径：
>
> - `bench/reports/` 已随文档迁移清空，不再有文件；
> - `bench/env/results/` 自 2026-11-15 起整体退出 Git 索引（磁盘 145 个文件保留、不进版本库），归档快照在 `docs/evidence/`；
> - 现行位置以本文件 §1 与 `docs/evidence-index.md` 为准。

## 4. 文件索引

| 原路径 | 当前路径 |
|---|---|
| `2026-11-12/bun-migration/outputs/trss-yunzai-bun-migration-assessment.md` | `docs/trss-yunzai-bun-migration-assessment.md` |
| `2026-11-12/perf-baseline/outputs/TRSS-Yunzai-perf-stability-baseline.md` | `docs/TRSS-Yunzai-perf-stability-baseline.md` |
| `2026-11-12/trss-refactor-assessment/outputs/trss-yunzai-refactor-assessment.md` | `docs/trss-yunzai-refactor-assessment.md` |
| `2026-11-13/framework-survey/outputs/robot-framework-survey.md` | `docs/robot-framework-survey.md` |
| `2026-11-13/go-rewrite-blueprint/outputs/TRSS-Yunzai-Go-native-rewrite-blueprint.md` | `docs/TRSS-Yunzai-Go-native-rewrite-blueprint.md` |
| `2026-11-13/source-verification/outputs/trss-yunzai-bun-migration-source-verification.md` | `docs/trss-yunzai-bun-migration-source-verification.md` |
| `audit-completed-work-2026-11-14.md` | `docs/audit-completed-work-2026-11-14.md` |
| `bench/reports/capacity-100-groups-200rps.md` | `docs/capacity-100-groups-200rps.md` |
| `bench/reports/phase0-baseline.md` | `docs/phase0-baseline.md` |
| `bench/reports/phase0-profile-2026-11-14.md` | `docs/phase0-profile-2026-11-14.md` |
| `bench/reports/phase0-profile-2026-11-15.md` | `docs/phase0-profile-2026-11-15.md` |
| `go-yunzai/README.md` | `docs/README-go-yunzai.md` |
| `go-render/README.md` | `docs/README-go-render.md` |
| `plan.md` | `docs/plan.md` |
| `review-new-changes-2026-11-14.md` | `docs/review-new-changes-2026-11-14.md` |
| `next-agent-prompt.md`（未跟踪） | `docs/next-agent-prompt.md` |
| 新建 | `docs/code-review-plan-2026-11-15.md` |
| 新建 | `docs/fix-plan-2026-11-15.md` |
| 新建 | `docs/engineering-quality-plan-2026-11-15.md` |
| 新建 | `docs/engineering-quality-review-2026-11-15.md` |
| 新建 | `docs/result-schema-v1.md` |
| 新建 | `docs/adr/`（5 份，见 §2） |
| 新建 | `docs/evidence-index.md` |
| 新建 | `docs/evidence-conventions.md` |
| 新建 | `docs/evidence/`（20 个快照，清单见 `docs/evidence-index.md` §2） |

## 5. 维护规则

- 新增文档必须在本文件登记（§2 或 §4），否则视为漏项。
- 归档文档后，旧路径不再作为现行引用；历史记录里的路径按 §3 的说明理解为"当时位置"。
- ADR 只追加不改写；需要推翻既有决定时新写一份并标注被取代关系。
