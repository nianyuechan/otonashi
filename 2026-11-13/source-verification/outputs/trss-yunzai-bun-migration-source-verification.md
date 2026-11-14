# TRSS-Yunzai Bun 迁移评估：外部来源与事实核对

核对日期：2026-11-13  
核对对象：[trss-yunzai-bun-migration-assessment.md](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-12/bun-migration/outputs/trss-yunzai-bun-migration-assessment.md)  
本机运行时：`bun --version` 返回 **1.4.2**；Hummus 对照复现时 Node.js 为 **v24.10.0**。  
核对范围：19 条外部来源、Bun 1.4.x 官方发布说明、指定本地实测记录及 Hummus 安装状态。只核对，不重写原评估。

## 核对结论摘要

- 原报告列出的 **19 条来源均真实存在且可以打开**，没有链接失效。按“来源是否支持报告对它的具体表述”判定：**16 条成立，3 条表述偏差，0 条链接失效，0 条与结论无关**。
- 三处表述偏差分别是：[cite:1] 当前官方兼容页未列 `process.execve`；[cite:8] 的原始环境是 Linux aarch64 / Bun 1.4.4，不是 Windows / Bun 1.4.2；[cite:15] 的 issue 本身没有提到 `bun pm migrate`，且 Bun 1.4.4 已发布相对 `link:` 子项支持，不能继续把整个 issue 概括成该子项尚未支持。
- 官方 1.4.x 发布说明共五版：1.4.0、1.4.1、1.4.2、1.4.3、1.4.4。[cite:20] 本机 1.4.2 不是该系列最新版；**1.4.2 发布说明对本任务五项专题均未提及**。1.4.4 的相对 `link:` 支持不能倒推到 1.4.2；1.4.0–1.4.3 的 N-API/V8 改进是具体范围扩展，不等于完整 V8 C++ 兼容；五版均没有 Puppeteer 在 Bun 上的启动或长期稳定性保证。
- [work](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-12/bun-migration/work) 中找到了覆盖所述项目的探针脚本，Sharp 与 LevelDB 另有产物；但**没有找到当时运行的完整命令、stdout/stderr、退出码或结果日志**。因此，“有可复现脚本”不等于“当时 Bun 运行成功已有可审计记录”。缺少执行结果的项目，其“实测通过”仅为报告陈述。
- Hummus 的包文件存在，但声明的 `binding/hummus.node` 确实不存在。使用现有探针只读复现后，Node.js 24.10.0 与 Bun 1.4.2 均因该文件缺失而加载失败，和原报告一致；这不是 Bun 独有故障。

## 判定口径与方法

| 标记 | 含义 |
|---|---|
| 成立 | 链接可打开，来源内容支持报告对它的表述；可以附带适用范围限定 |
| 表述偏差 | 来源存在，但报告把来源未覆盖的平台、版本、命令或结论归到该来源上，或引用内容不完整 |
| 链接失效 | 原链接已无法打开或目标不存在 |
| 与结论无关 | 页面存在，但内容与所引结论没有实质关系 |

外部页面以 2026-11-13 实际读取到的内容为准；文档后续可能变化。GitHub issue 的“开放”只表示事项尚未关闭，用户报告不等于官方确认或普遍复现；“关闭”也不自动等于“已修复”，须看关闭原因。官方发布说明、issue 与本地探针是三类不同证据，不相互替代。

本地核对只读取文件和文件状态。除 Hummus 加载复现外，没有重新运行其他探针；没有安装依赖、构建原生模块、启动机器人或执行 Git 写操作。

## 19 条外部来源逐条核对

| 编号 | 来源与可访问性 | 查证到的事实及适用边界 | 结论 |
|---|---|---|---|
| [cite:1] | [Bun — Node.js compatibility](https://bun.sh/docs/runtime/nodejs-apis)；可打开，跳转至 [nodejs-compat](https://bun.sh/docs/runtime/nodejs-compat) | 当前页支持 Express 可用、`require.cache/resolve`、`exports/module/__dirname/__filename`、child_process 与 worker_threads 限制、`process.binding` 部分实现；`node:vm` 列出核心功能并注明缺失项，不是无条件“全部实现”。`node:v8` 明确写道：“`serialize` and `deserialize` use JavaScriptCore’s wire format instead of V8’s.” **当前页未找到 `process.execve`**，所以报告该行的 API 存在性不能由此页证明，只能另靠本地探针。 | **表述偏差**（仅 `process.execve` 引用） |
| [cite:2] | [PM2 — Using Bun, Deno and other Runtimes](https://pm2.keymetrics.io/docs/usage/bun-deno/)；可打开 | 页面明确写“PM2 supports Bun natively.”；`.js` 默认解释为“node (or bun when PM2 runs under Bun)”，并给出 `--interpreter bun` / `interpreter: "bun"`。支持报告关于官方 Bun 支持和显式解释器配置的表述。 | **成立** |
| [cite:3] | [Puppeteer — Installation](https://pptr.dev/guides/installation)；可打开 | 页面明确给出 `bun add puppeteer` 与 `bun x puppeteer browsers install`；[配置页](https://pptr.dev/guides/configuration)另有下载和缓存配置。它支持“官方提供 Bun 安装方式”，**不构成 Puppeteer 在 Bun 上启动或长期稳定运行的保证**。报告本身也把启动验收列为未做。 | **成立**（限安装方式） |
| [cite:4] | [Bun — TypeScript](https://bun.sh/docs/runtime/typescript)；可打开 | 页面明确写 Bun 支持顶层 await，以及带 `.ts` 扩展的导入，并提供 TypeScript 配置说明。 | **成立** |
| [cite:5] | [Bun — bun install](https://bun.sh/docs/pm/cli/install)；可打开 | 页面有安装速度宣传（“up to 25x faster”），并明确默认不执行已安装依赖的任意生命周期脚本，可由 `trustedDependencies` 放行。workspace 数值基准由 [cite:6] 提供，不是本页必须独立支撑的内容；这些基准不是本项目的实测收益。 | **成立** |
| [cite:6] | [Bun — Workspace management](https://bun.sh/docs/pm/workspaces)；可打开 | 页面说明 workspaces，并给出 Remix monorepo 的官方对比基准。它支持“官方有 workspace 基准”，不支持“TRSS-Yunzai 已获得相同收益”。报告也未作后一种断言。 | **成立** |
| [cite:7] | [Bun — bun patch](https://bun.sh/docs/pm/cli/patch)；可打开 | 页面明确说明 `bun patch <pkg>`、`bun patch --commit`，并由 `package.json` 的 `patchedDependencies` 记录补丁。 | **成立** |
| [cite:8] | [Bun #46900](https://github.com/oven-sh/bun/issues/46900)；可打开，**open** | 2026-10-25 的用户报告称：Bun **1.4.4**、Ubuntu 26.04 / Linux 7.0.0 **aarch64** 下，不同 `file:` URL query 返回同一模块，相对说明符则返回不同模块；0 评论，无官方确认。issue 不涉及 Windows、Bun 1.4.2、Guoba 或热重载。把它用于 Windows Guoba 是结合其他材料作出的外推；该 issue 本身不能证明这条 Windows 业务结论。 | **表述偏差** |
| [cite:9] | [Bun #11992](https://github.com/oven-sh/bun/issues/11992)；可打开，**closed / completed** | 2025-09-20 关闭；[实现提交](https://github.com/oven-sh/bun/commit/7c0574eeb45429f90d666551ccdf1f439eab5a8a)明确含 `fixes #11992`、Windows 实现与测试。[后续评论](https://github.com/oven-sh/bun/issues/11992#issuecomment-3322640959)提到当时先用 canary、等待 1.2.23。支持“Windows 实现 issue 已关闭”。 | **成立** |
| [cite:10] | [Bun #15683](https://github.com/oven-sh/bun/issues/15683)；可打开，**open** | 2024-12-10 用户报告：Bun **1.1.38**、Windows 11 x64，Puppeteer 随机退出；[贡献者评论](https://github.com/oven-sh/bun/issues/15683#issuecomment-2556282060)只称可能是 Windows 特定问题、macOS 未复现。没有官方 Windows 复现、修复或长期稳定性验收。报告把它列为风险而非普遍故障，符合来源边界。 | **成立**（用户报告级证据） |
| [cite:11] | [Bun #20372](https://github.com/oven-sh/bun/issues/20372)；可打开，**open** | 原始报告环境为 Fly.io Linux x86_64_baseline、Bun 1.2.16、sharp `^0.34.2`，出现段错误；不能直接外推为 Windows 上 sharp 0.33.5 / 0.34.5 失败。后续评论也没有补齐 Linux / Windows 新验证。 | **成立** |
| [cite:12] | [Bun #44109](https://github.com/oven-sh/bun/issues/44109)；可打开，**open** | 原始报告为 Windows 11、Bun 1.3.14、sharp 0.35.3 WASM 文件 I/O 崩溃，内存操作正常。[协作者评论](https://github.com/oven-sh/bun/issues/44109#issuecomment-5855279042)补充：1.4.x 可复现但表现为 hang，原 mprotect 崩溃在 1.4.0 已不再出现。报告“不直接套到 0.33.5 / 0.34.5”成立；后续评论所示 1.4.x 行为是报告未展开的补充事实。 | **成立** |
| [cite:13] | [Bun #14686](https://github.com/oven-sh/bun/issues/14686)；可打开，**closed / completed** | 原始环境是 Windows 11 / Bun 1.1.31，历史上有消失后复发记录；贡献者关闭时明确写[“fixed again in bun@1.2.0”](https://github.com/oven-sh/bun/issues/14686#issuecomment-3143338134)。支持报告的关闭说明，不等于所有场景永久无回归。 | **成立** |
| [cite:14] | [Bun #21379](https://github.com/oven-sh/bun/issues/21379)；可打开，**closed / duplicate** | Linux x64 / Bun 1.2.19 的 Worker + N-API finalizer 崩溃报告；2025-09-09 以 duplicate 关闭，[评论](https://github.com/oven-sh/bun/issues/21379#issuecomment-3268680952)指向 #15964 统一跟踪。报告只说“已关闭”，且另写 worker 未测，是准确的；**不得把该关闭解读为已修复**。 | **成立** |
| [cite:15] | [Bun #23026](https://github.com/oven-sh/bun/issues/23026)；可打开，**open** | issue 标题同时包含 non-existent workspaces 与相对 link；正文及两条评论均**没有提到 `bun pm migrate`**。更重要的是，[1.4.4 发布说明](https://bun.sh/blog/bun-v1.4.4#link-supports-paths)已明确支持 `link:./dir`，相关实现说明也覆盖 pnpm lockfile 迁移；#23026 仍开放不能用来否定这个已发布子项。对本机 1.4.2，则不能倒推拥有 1.4.4 能力；本项目迁移失败仍须本地命令与输出单独证明。 | **表述偏差** |
| [cite:16] | [Bun #11874](https://github.com/oven-sh/bun/issues/11874)；可打开，**closed / completed** | [关闭评论](https://github.com/oven-sh/bun/issues/11874#issuecomment-3341392304)明确称 #22262 实现两种 pnpm 文件自动迁移，将随 1.2.23 发布；[1.2.23 发布说明](https://bun.com/blog/bun-v1.2.23)进一步明确是 **`bun install` 自动迁移**。支持报告的实现记录，不证明 `bun pm migrate` 能处理本项目所有相对 link。 | **成立** |
| [cite:17] | [Bun #40106](https://github.com/oven-sh/bun/issues/40106)；可打开，**open** | 用户报告 Bun **1.4.0** 下精确版本键失配后补丁静默消失、`--frozen-lockfile` 退出 0；协作者[同日确认复现](https://github.com/oven-sh/bun/issues/40106#issuecomment-5381131360)。后续 PR 未完整合入该修复，不能称已发布完整修复。 | **成立** |
| [cite:18] | [Bun #19942](https://github.com/oven-sh/bun/issues/19942)；可打开，**open / confirmed bug** | 用户报告 Bun **1.2.14**、Windows / Ubuntu 下 PM2 + 顶层 await 出现 `require() async module ... is unsupported`；贡献者[确认现象](https://github.com/oven-sh/bun/issues/19942#issuecomment-2914522460)，但未确定责任在 Bun 还是 PM2。IIFE、`interpreter:none`、动态 import 等是用户 workaround，不是维护者验证或正式修复。 | **成立** |
| [cite:19] | [Bun #4290](https://github.com/oven-sh/bun/issues/4290)；可打开，**open / tracking** | 该跟踪 issue 支持“V8 C++ API / NAN 兼容仍不完整”；维护者解释过 JSC 与 V8 差异，贡献者评论也说明复杂包仍可能失败。issue **没有直接讨论 Hummus**。本地源码另行证实 Hummus 使用 `node::ObjectWrap`，所以该来源可支持通用高风险边界，但不是“Hummus 在 Bun 重新构建后仍失败”的实测证据。 | **成立**（限通用兼容缺口） |

## Bun 1.4.x 官方发布说明核对

### 版本范围

[GitHub 官方 releases](https://github.com/oven-sh/bun/releases)返回的 1.4.x 共五版，没有 1.4.5 或其他补丁版。[cite:20] 版本日期以官方 release 的 `published_at` 与博客日期相互核对。

| 版本 | 发布日期 | 官方发布说明 |
|---|---|---|
| 1.4.0 | 2026-08-20 | [Bun 1.4](https://bun.sh/blog/bun-v1.4)[cite:21] |
| 1.4.1 | 2026-09-04 | [Bun v1.4.1](https://bun.sh/blog/bun-v1.4.1)[cite:22] |
| 1.4.2 | 2026-09-05 | [Bun v1.4.2](https://bun.sh/blog/bun-v1.4.2)[cite:23] |
| 1.4.3 | 2026-10-10 | [Bun v1.4.3](https://bun.sh/blog/bun-v1.4.3)[cite:24] |
| 1.4.4 | 2026-10-22 | [Bun v1.4.4](https://bun.sh/blog/bun-v1.4.4)[cite:25] |

下表的“未”指该版本发布说明**没有提及本行所问的兼容事项或修复**，不是指对应问题不存在。发布说明不是完整的兼容性验收报告。

| 专题 | 1.4.0 | 1.4.1 | 1.4.2（本机） | 1.4.3 | 1.4.4 |
|---|---|---|---|---|---|
| PM2 与顶层 await | 未 | 未 | 未 | 未 | 未 |
| `file:` URL query | 未 | 未 | 未 | 未 | 未 |
| 相对 `link:` / migrate | 未 | 未 | 未 | 未 | **明确支持相对 `link:`；未点名 `bun pm migrate`** |
| V8 C++ API / N-API | **提及，限范围支持** | **提及，限范围修复** | 未 | **提及，限范围修复** | 未 |
| Puppeteer 启动 / 长期稳定性 | 未 | 未 | 未 | 未 | 未 |

### 五项专题的事实与结论

| 专题 | 官方发布说明中的查证事实 | 核对结论 |
|---|---|---|
| PM2 与顶层 await（#19942） | 五版发布说明均未声明修复 PM2 + 顶层 await 或引用 #19942。1.4.0 有其他 `await` / Worker 改进，不能归成 PM2 修复。#19942 当前仍开放，原始版本为 1.2.14。[cite:18][cite:21][cite:22][cite:23][cite:24][cite:25] | **1.4.x 未提及，1.4.2 也无官方修复依据。** 报告保留 PM2 风险、绕过 PM2 先做试验的谨慎结论与现有证据一致；本核对没有实际启动 PM2。 |
| `file:` URL query（#46900） | 五版均未声明此修复。#46900 创建于 2026-10-25，晚于最后一版 1.4.4 的 2026-10-22；原始环境为 Linux aarch64 / Bun 1.4.4，仍开放。[cite:8][cite:20][cite:25] | **未提及，也没有官方修复声明。** 不能把该 Linux / 1.4.4 issue 直接当作 Windows / 1.4.2 实测；Windows 行为需要本地结果，而现有 [work](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-12/bun-migration/work) 没有保存该次输出。 |
| 相对 `link:` 与 migrate（#23026） | 1.4.4 新增“`link:` supports paths”，原文：“`link:./dir` symlinks a directory into `node_modules`”，路径相对声明它的 `package.json`。[cite:25] #40025 本身为 CLOSED、未直接合并，但被纳入 2026-10-14 已 MERGED 的 #44968；#40025 明确描述 path-style `link:`，并写有 pnpm lockfile 中 `link:` target 的迁移。[cite:26][cite:27] 项目所用的无前缀 `link:lib/modules/*` 对应 #40025 所述 `link:lib/x` 形式；官方博客直接展示的是 `./` / `../`，本核对没有实测无前缀形式。发布说明没有点名 `bun pm migrate` 命令。#23026 还包含 non-existent workspaces，仍开放。 | **1.4.4 已明确支持相对 `link:` 子项；1.4.2 没有这项发布依据。** “整个 #23026 仍开放”不等于“相对 link 在所有 1.4.x 都不支持”。PR 的 pnpm 迁移说明也不等于本项目 `bun pm migrate` 已实测通过。 |
| V8 C++ API / N-API（#4290） | 1.4.0 明确写入 Node-API version 10；Datadog 段落明确实现 `nan ObjectWrap` 模板模式及 pprof 预编译所需的 89 个 V8/Node symbols，同时另一项仍写“V8 C++ APIs Bun doesn't yet support (currently better-sqlite3)”。[cite:21] 1.4.1 修复多项 N-API finalizer 崩溃和 `better-sqlite3@13` 加载；1.4.3 修复 N-API 清理与 `v8::Function::GetScriptOrigin()` 等具体调用。[cite:22][cite:24] 1.4.2、1.4.4 未提及本专题；#4290 仍开放。[cite:19][cite:23][cite:25] | **支持范围在扩大，但不是完整 V8 C++ 兼容。** 不能把 N-API 与 V8 C++ API 混为一谈，也不能仅凭 Hummus 使用 `node::ObjectWrap` 就判定它不受支持；Hummus 的完整符号需求和重建后行为仍未验证。报告的“高风险、不能默认兼容”可以保留为风险判断，不能写成已证明不兼容。 |
| Puppeteer 启动与长期稳定性 | 五版均没有 Puppeteer 在 Bun 上的启动或长期稳定性声明。1.4.0 的 Puppeteer 字样用于“`Bun.WebView` without Puppeteer or Playwright”；同页的 Playwright 支持不是 Puppeteer 证据。[cite:21] 1.4.3 的 `puppeteer/puppeteer` 只出现在 `bun check` 的仓库类型检查统计中，不是运行时验收。[cite:24] Puppeteer 安装页给出 Bun 安装命令；所读取的[系统要求](https://pptr.dev/guides/system-requirements)页面标记为 Version 23.11.1，列出 Node 18+，未给 Bun 运行时保证。[cite:3][cite:28] #15683 仍开放。[cite:10] | **没有官方启动或长期稳定性保证。** 安装命令不等于 Bun 运行时验收；该 23.11.1 要求页也不能用来否定项目的 24.43.0。[runtime-probe](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-12/bun-migration/work/runtime-probe) 中没有 Puppeteer launch/connect/screenshot 或长时间运行探针，和报告“未做启动验收”一致。 |

**总边界：官方发布说明未提及，不等于问题不存在；issue 仍开放，也不等于其每个子项在所有版本都未修复。版本号、平台、具体 API 和实际运行结果必须分别对应。**

## 本地“实测通过”记录核对

本地目录：[work](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-12/bun-migration/work)、[runtime-probe](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-12/bun-migration/work/runtime-probe)。

截至核对时，目录中未找到保存当时完整命令、stdout/stderr、退出码或运行时结果的日志。下表把“脚本/产物存在”与“成功结果已留存”分开判定；本次没有重跑这些项目。

| 报告所述项目 | 留存材料与覆盖范围 | 对“实测通过”的核对结论 |
|---|---|---|
| `level` / `classic-level` | [native-probe.js](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-12/bun-migration/work/runtime-probe/native-probe.js#L14-L22) 包含 open、put/get、iterator、close；另有 [leveldb](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-12/bun-migration/work/runtime-probe/leveldb) 数据文件。脚本按 `level-pnpm` 分支读取实际 `.pnpm` 包路径。 | **有复现脚本和数据库产物；无留存执行结果。** 产物不记录运行时，不能独立证明由 Bun 1.4.2 生成；“全部通过”仅为报告陈述。 |
| `@karinjs/sqlite3` | [native-probe.js](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-12/bun-migration/work/runtime-probe/native-probe.js#L23-L33) 包含 `:memory:` 打开、`SELECT 42 AS value` 和 close。 | **有复现脚本；无留存执行结果。** 内存数据库没有持久产物，“返回 42 并通过”仅为报告陈述。 |
| `sharp@0.33.5` | [native-probe.js](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-12/bun-migration/work/runtime-probe/native-probe.js#L34-L42) 包含 PNG 写入和 metadata 读取；存在 [sharp33-pnpm-output.png](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-12/bun-migration/work/runtime-probe/sharp33-pnpm-output.png)，91 字节，时间为 2026-11-12 22:23:12。 | **有复现脚本和 PNG 产物；无留存执行结果。** PNG 本身不记录生成运行时，Bun 写入成功及 metadata 结果仅为报告陈述。 |
| `sharp@0.34.5` | 同一脚本分支；存在 [sharp34-pnpm-output.png](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-12/bun-migration/work/runtime-probe/sharp34-pnpm-output.png)，91 字节，时间为 2026-11-12 22:23:41。 | **有复现脚本和 PNG 产物；无留存执行结果。** 不能仅凭 PNG 归因于 Bun；成功结论仅为报告陈述。 |
| `bufferutil` | [native-probe.js](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-12/bun-migration/work/runtime-probe/native-probe.js#L46-L53) 包含 mask/unmask 和结果数组输出。 | **有复现脚本；无留存执行结果。** “mask/unmask 通过”仅为报告陈述。 |
| 相对路径 + query 热重载 | [probe.js](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-12/bun-migration/work/runtime-probe/probe.js#L4-L5) 与 [counter.js](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-12/bun-migration/work/runtime-probe/counter.js#L1) 检查两个相对 query 是否形成不同模块实例。 | **有复现脚本；无留存执行结果。** 该脚本只覆盖动态 import 的缓存语义，不是 chokidar“文件变更 → 插件重载”全链验收；“通过”仅为报告陈述。 |
| `package.json#imports` | [package.json](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-12/bun-migration/work/runtime-probe/package.json#L1-L6) 定义 `#probe`，[probe.js](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-12/bun-migration/work/runtime-probe/probe.js#L27) 导入它，[alias.js](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-12/bun-migration/work/runtime-probe/alias.js#L1) 提供返回值。 | **有复现脚本；无留存执行结果。** “解析通过”仅为报告陈述。 |
| `createRequire` | [probe.js](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-12/bun-migration/work/runtime-probe/probe.js#L1-L3) 创建 require，并在[第 26 行](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-12/bun-migration/work/runtime-probe/probe.js#L26)检查 `require.resolve("./alias.js")`。 | **有复现脚本；无留存执行结果。** “解析通过”仅为报告陈述。 |

补充边界：[v8-probe.js](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-12/bun-migration/work/runtime-probe/v8-probe.js) 与 [v8-node.bin](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-12/bun-migration/work/runtime-probe/v8-node.bin) 也存在，但同样没有保存 Bun 反序列化时的错误输出。本次未把该报告陈述当作已独立复现事实。

[pnpm-migration-probe](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-12/bun-migration/work/pnpm-migration-probe) 留有复制的清单与 lock；当前 [package.json](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-12/bun-migration/work/pnpm-migration-probe/package.json#L29-L35) 的四个本地依赖已是 `file:` 形式，与报告所述“后续改 file”阶段相符。但目录中没有 `bun.lock`、迁移命令输出或退出码记录，本次也未复跑；因此 E4 的初次 `link:` 失败及改 `file:` 后失败，均为报告陈述，不是本次独立复现事实。

## Hummus 实际安装状态

项目根目录：[TRSS-Yunzai](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai)。

| 核对项 | 查证到的事实 | 结论 |
|---|---|---|
| 依赖链 | [pnpm-lock.yaml](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/pnpm-lock.yaml#L9939-L9944) 为 `img2pdf@1.0.11 → hummus@1.0.118`；pixiv 插件的 `img2pdf` 链接目录存在。 | 与报告一致。 |
| 包是否落盘 | [package.json](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/node_modules/.pnpm/hummus@1.0.118/node_modules/hummus/package.json#L1-L10) 存在，版本为 1.0.118，入口为 `hummus.js`。 | 包源码存在，不等于原生二进制存在。 |
| 二进制声明 | 同一 [package.json](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/node_modules/.pnpm/hummus@1.0.118/node_modules/hummus/package.json#L36-L41) 声明 `module_name: hummus`、`module_path: ./binding`；[hummus.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/node_modules/.pnpm/hummus@1.0.118/node_modules/hummus/hummus.js#L1-L5) 通过 node-pre-gyp 找到并 `require(binding_path)`。 | 当前加载链需要 `binding/hummus.node`。 |
| 文件存在性 | 对实际 `.pnpm` 包路径执行只读 `Test-Path`：`binding\hummus.node` 返回 **False**。 | 与报告“缺二进制”一致。 |
| Node.js 复现 | 在探针目录执行 `node native-probe.js hummus`：Node **v24.10.0** 退出码 1，报 `Cannot find module ...\hummus\binding\hummus.node`。 | Node 当前不能加载，和报告一致。 |
| Bun 复现 | 在探针目录执行 `bun native-probe.js hummus`：Bun **1.4.2** 退出码 1，报 `ResolveMessage: Cannot find module ...\hummus\binding\hummus.node`。 | Bun 当前不能加载，和报告一致；不是 Bun 独有故障。 |
| V8 / ObjectWrap 源码 | 例如 [AbstractContentContextDriver.h](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/node_modules/.pnpm/hummus@1.0.118/node_modules/hummus/src/AbstractContentContextDriver.h#L50) 继承 `node::ObjectWrap`，其他多个 Driver 也有同类用法。 | “源码直接使用 Node/V8 C++ 包装”成立；“重新构建后在 Bun 必失败”没有被本次核对证明，只能保留为兼容性风险判断。 |

## 证据限制总表

| 事项 | 已查证事实 | 不能据此推出 |
|---|---|---|
| 19 条来源 | 全部存在且可打开；多数支持报告对应表述 | 不能推出报告中的每个本地实验都已实际执行成功 |
| 开放 issue | 可证明有人报告了特定版本/平台的问题，部分有维护者确认 | 不能推出所有版本、所有平台或本项目必然复现 |
| 关闭 issue | 可证明事项已关闭；completed 与 duplicate 须区分 | 不能把 duplicate 关闭当成已修复 |
| 本地脚本和产物 | 可证明准备了相应探针，部分操作留下了文件 | 没有运行日志时，不能独立证明 Bun 1.4.2 的成功输出 |
| Hummus 当前失败 | Node 与 Bun 都缺少同一个原生二进制并复现加载失败 | 不能推出修复二进制后 Bun 一定兼容或一定不兼容 |

## Sources

<a id="cite-1"></a>[cite:1] [Bun — Node.js compatibility（当前跳转页）](https://bun.sh/docs/runtime/nodejs-compat)

<a id="cite-2"></a>[cite:2] [PM2 — Using Bun, Deno and other Runtimes](https://pm2.keymetrics.io/docs/usage/bun-deno/)

<a id="cite-3"></a>[cite:3] [Puppeteer — Installation](https://pptr.dev/guides/installation)

<a id="cite-4"></a>[cite:4] [Bun — TypeScript](https://bun.sh/docs/runtime/typescript)

<a id="cite-5"></a>[cite:5] [Bun — bun install](https://bun.sh/docs/pm/cli/install)

<a id="cite-6"></a>[cite:6] [Bun — Workspace management](https://bun.sh/docs/pm/workspaces)

<a id="cite-7"></a>[cite:7] [Bun — bun patch](https://bun.sh/docs/pm/cli/patch)

<a id="cite-8"></a>[cite:8] [Bun issue #46900 — Dynamic import of a file: URL drops its query](https://github.com/oven-sh/bun/issues/46900)

<a id="cite-9"></a>[cite:9] [Bun issue #11992 — Implement process.report.getReport on Windows](https://github.com/oven-sh/bun/issues/11992)

<a id="cite-10"></a>[cite:10] [Bun issue #15683 — Puppeteer random error](https://github.com/oven-sh/bun/issues/15683)

<a id="cite-11"></a>[cite:11] [Bun issue #20372 — Bun crash using sharp](https://github.com/oven-sh/bun/issues/20372)

<a id="cite-12"></a>[cite:12] [Bun issue #44109 — Sharp file I/O on Windows](https://github.com/oven-sh/bun/issues/44109)

<a id="cite-13"></a>[cite:13] [Bun issue #14686 — classic-level npm module crashes](https://github.com/oven-sh/bun/issues/14686)

<a id="cite-14"></a>[cite:14] [Bun issue #21379 — Crash running classic-level in a worker](https://github.com/oven-sh/bun/issues/21379)

<a id="cite-15"></a>[cite:15] [Bun issue #23026 — Relative link dependencies](https://github.com/oven-sh/bun/issues/23026)

<a id="cite-16"></a>[cite:16] [Bun issue #11874 — Support pnpm-workspace.yaml](https://github.com/oven-sh/bun/issues/11874)

<a id="cite-17"></a>[cite:17] [Bun issue #40106 — patchedDependencies stale version key](https://github.com/oven-sh/bun/issues/40106)

<a id="cite-18"></a>[cite:18] [Bun issue #19942 — Bun + PM2 fails with top-level await](https://github.com/oven-sh/bun/issues/19942)

<a id="cite-19"></a>[cite:19] [Bun issue #4290 — Support V8 C++ APIs for NAN addons and other packages](https://github.com/oven-sh/bun/issues/4290)

<a id="cite-20"></a>[cite:20] [Bun — GitHub Releases](https://github.com/oven-sh/bun/releases)

<a id="cite-21"></a>[cite:21] [Bun 1.4 官方发布说明](https://bun.sh/blog/bun-v1.4)

<a id="cite-22"></a>[cite:22] [Bun v1.4.1 官方发布说明](https://bun.sh/blog/bun-v1.4.1)

<a id="cite-23"></a>[cite:23] [Bun v1.4.2 官方发布说明](https://bun.sh/blog/bun-v1.4.2)

<a id="cite-24"></a>[cite:24] [Bun v1.4.3 官方发布说明](https://bun.sh/blog/bun-v1.4.3)

<a id="cite-25"></a>[cite:25] [Bun v1.4.4 官方发布说明](https://bun.sh/blog/bun-v1.4.4)

<a id="cite-26"></a>[cite:26] [Bun PR #40025 — install: support path-style link:./dir dependencies](https://github.com/oven-sh/bun/pull/40025)

<a id="cite-27"></a>[cite:27] [Bun PR #44968 — install: consolidate the open package manager pull requests](https://github.com/oven-sh/bun/pull/44968)

<a id="cite-28"></a>[cite:28] [Puppeteer — System requirements](https://pptr.dev/guides/system-requirements)
