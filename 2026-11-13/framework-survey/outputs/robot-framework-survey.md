# 机器人框架选型调研：成熟框架迁移与自研 Go 核心对比

- 调研日期：2026-11-13
- 数据截止：2026-11-13；提交和发布时间统一按页面返回的 UTC 或原始时区记录
- 本地基线：`d:\Toshikoshi_chan\Agent\音无\mysy2\mysy2\home\SERVER2\TRSS_AllBot`
- 调研方式：实际联网打开官方仓库、提交/发布页、GitHub 搜索/API、官方文档、npm/PyPI 元数据与插件注册表；本地仅作对照
- 执行边界：未修改 `mysy2`，未安装依赖，未构建或启动机器人，未执行 Git 写操作

## 0. 结论摘要

### 0.1 直接回答

**「成熟框架 + 迁插件」在核心层优于从零自研，但「直接换一个框架」不等于能低成本接管现有系统。**

1. **若 Go 是硬约束**：优先验证 **ZeroBot 核心作为依赖 + 自有插件逐步迁移**，而不是从零写通信、匹配器和插件生命周期，也不是直接 fork 整个 ZeroBot-Plugin。ZeroBot 已提供 OneBot v11、HTTP、正/反向 WebSocket 和可替换 Driver；ZeroBot-Plugin 有 107 个默认启用的本仓库插件，可补充通用能力。[cite:1](#cite-1) [cite:4](#cite-4) [cite:7](#cite-7)
2. **若 Go 只是偏好而非硬约束**：**NoneBot2 是本次证据最完整的“成熟框架 + 迁插件”基线**。核心 MIT，官方 OneBot 适配器同时覆盖 v11/v12，另有官方 Milky 适配器；默认分支最近提交为 2026-11-12，注册表实取 955 条、其中 `valid=true` 627 条。其协议、许可证和近期维护证据均强于 ZeroBot，但不满足 Go 技术栈要求。[cite:20](#cite-20) [cite:22](#cite-22) [cite:23](#cite-23) [cite:24](#cite-24)
3. **现有插件不能直接搬走**：本地 38 个 `plugins` 目录中有 36 个加载候选、32 个 `index.js` 入口；非 Yunzai 框架能直接加载这些 JS 插件的数量是 **0/32**。游戏数据、登录态、AI Agent 调用 Yunzai 插件、HTML 模板和数据结构仍需适配或重写。[L1](#local-1) [L3](#local-3) [L4](#local-4)
4. **速度没有可验证的赢家**：本次未查证到覆盖这些候选、采用相同硬件/协议端/插件/渲染负载并公开原始结果的端到端 benchmark。Go goroutine、Python TaskGroup、Node 异步事件只能证明并发模型，不能证明实际吞吐、尾延迟或内存排名。[cite:4](#cite-4) [cite:19](#cite-19) [cite:26](#cite-26)
5. **当前 Lagrange 需要单独处理**：Lagrange 官方主分支已是 V2，V1 标为 sunset，V2 通过 Lagrange.Milky 提供 Milky 服务。ZeroBot/Koishi 的旧 OneBot v11 对接依据不能外推为支持当前 V2；NoneBot2 有官方 Milky 适配器，但其 README 也把 Lagrange.Milky 标为 WIP，仍须实测。[cite:23](#cite-23) [cite:42](#cite-42)

### 0.2 建议排序

| 条件 | 建议 | 结论性质 |
|---|---|---|
| Go 为硬约束，接受 GPL-3.0，OneBot v11/NapCat 足够 | **基于 ZeroBot 二次开发**，按需选用 ZBP 插件，迁移自有功能 | 推断，中高置信；由 Go、协议、插件和并发源码支持 |
| Go 为硬约束，必须使用当前 Lagrange V2/Milky | ZeroBot 需先补 Milky Driver 并完成互通测试；否则才考虑自研核心 | 推断；未找到 ZeroBot 官方 Milky 驱动，不等于证明不存在 |
| 可接受 Python，重视许可证、协议覆盖和维护证据 | **优先 NoneBot2 + 插件迁移** | 推断，中高置信；不代表已证明性能更快 |
| 要求现有 Yunzai 功能与命令完全不变 | 任何跨框架路线都要做兼容层或逐插件移植；直接替换不可行 | 事实与直接推论 |
| 仅为“提速”而从 TRSS-Yunzai 换 Koishi | 不建议据此立项；仍是 Node，且插件需迁移，缺同负载性能证据 | 推断，高置信 |
| 直接采用 LittlePaimon 或 Liteyuki v7 作为新基线 | 不建议：前者默认分支停在 2025-03；后者公开索引暂无 Alpha15 兼容插件 | 事实支持的选型判断 |

---

## 1. 口径、角色与证据等级

### 1.1 框架与发行版不能混为一谈

- **ZeroBot** 是 Go 框架；**ZeroBot-Plugin（ZBP）** 是基于它的插件合集和可执行程序。
- **NoneBot2、Koishi** 是框架；插件商店包含社区包，登记数量不等于官方维护、兼容或生产可用数量。
- **LittlePaimon、Zhenxun、Liteyuki、TRSS-Yunzai、Miao-Yunzai** 更接近完整机器人应用/发行版，不应只按核心框架大小比较。

### 1.2 统一活跃度口径

- 最近提交：官方默认分支最新提交，不用 fork 或本地脚本时间替代。
- Release：GitHub Releases；ZeroBot 另列 tag，TRSS/Miao 的滚动提交不伪装成正式发版。
- issue：查询 `2026-05-13..2026-11-13` 内**新建且排除 PR**的 issue；关闭数是**同批新建 issue 当前已关闭**的数量。`0/0` 只表示没有样本，不能证明维护好或坏。
- 长期维护：使用持续提交、发布作者、贡献者记录和 npm maintainer 等证据；不把贡献者名单当作仓库权限审计。
- 插件数量：分别注明“目录数”“默认导入数”“注册条目数”“有效标记数”，互不换算。
- 性能：只接受带硬件、版本、负载、脚本和原始结果的数据；没有则写“未查证”。

### 1.3 标记

- **事实**：实际打开的页面或本地代码直接支持。
- **推断**：由事实作出的工程判断，附置信度。
- **未查证**：本次没有取得足够证据，不表示功能或项目必然不存在。

---

## 2. 本地系统基线

### 2.1 本地目录的真实状态

| 目录 | 实际内容 | 能否作为现成代码基线 |
|---|---|---|
| `TRSS-Yunzai` | 完整源码、`package.json`、`.git`；本地 HEAD 为 `a3d75d5`，提交时间 2026-07-02 | 可以作为行为与兼容性基线 |
| `ZeroBot` | 只有 `zbp.sh`，调用外部 `zbp -c config.json`；无 `go.mod`、框架源码或插件源码 | **不能**；必须以线上官方仓库为候选 |
| `Miao-Yunzai` | 只有启动脚本 | 不能据此确认版本或源码状态 |
| `LittlePaimon` | 只有启动脚本 | 同上 |
| `Zhenxun` | 只有启动脚本 | 同上 |
| `Liteyuki` | 只有启动脚本 | 同上 |
| `Amiya`、`Sagiri`、`Yunzai`、`go-cqhttp`、`Lagrange`、`Mirai` | 均只见到启动脚本；无 Git 元数据 | 版本与源码状态未查证 |

本地证据：[ZeroBot/zbp.sh](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/ZeroBot/zbp.sh)、[Miao-Yunzai.sh](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/Miao-Yunzai/Miao-Yunzai.sh)、[LittlePaimon.sh](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/LittlePaimon/LittlePaimon.sh)。<a id="local-1"></a>

**重要边界**：本地脚本存在不代表对应框架源码已经下载，也不代表版本与线上相同。后续框架判断均使用线上官方仓库，而不是这些脚本。

### 2.2 Yunzai 插件加载基线

本地 [loader.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/plugins/loader.js#L45-L73) 先寻找 `plugins/<name>/index.js`，否则加载目录顶层 `.js`。按这一规则只读盘点：

- 目录总数：**38**。
- 有 `index.js`：**32**。
- 加载候选目录：**36**，包括 `adapter`、`example`、`other`、`system` 四个内置/示例目录。
- `bin`、`plugins` 两个目录没有对应入口，不计入加载候选。
- `earth-k-plugin` 与 `earth-k-plugin-tmp` 的清单均为 `earth-plugin@2.0.0`，至少有一个临时/重复项需要在迁移清单阶段确认，不能把目录数直接当成独立业务功能数。<a id="local-2"></a>

32 个 index 入口为：

`agent-plugin`、`rconsole-plugin`、`Guoba-Plugin`、`earth-k-plugin-tmp`、`earth-k-plugin`、`miao-plugin`、`StarRail-plugin`、`San-plugin`、`mora-plugin`、`phi-plugin`、`liangshi-calc`、`flower-plugin`、`ark-plugin`、`yenai-plugin`、`ZZZ-Plugin`、`yunzai-plugin-deer-pipe`、`y-tian-plugin`、`xitian-plugin`、`xianxin-plugin`、`Speech-statistics-plugin`、`logier-plugin`、`genshin`、`FanSky_Qs`、`Atlas`、`arknights-plugin`、`Yunzai.TRSS.me`、`TRSS-Plugin`、`pixiv-plugin`、`nai-plugin`、`memz-plugin`、`mc-plugin`、`waves-plugin`。

### 2.3 已核实的自有能力，而非按名称猜功能

| 能力 | 本地证据 | 迁移含义 |
|---|---|---|
| 多协议 | [OneBotv11.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/plugins/adapter/OneBotv11.js) 与 [Milky.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/plugins/adapter/Milky.js) 均实际存在；后者含 WS、Webhook、重连逻辑 | 新核心若只支持旧 OneBot v11，会出现协议回退 |
| 游戏查询 | [StarRail-plugin README](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/plugins/StarRail-plugin/README.md#L16-L27) 明确角色面板、体力与米游社功能；[waves-plugin README](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/plugins/waves-plugin/README.md#L5-L10) 明确鸣潮玩家、日常、数据坞、抽卡查询 | 通用“游戏查询”插件不能替代这些数据契约和登录态 |
| Minecraft 互通 | [mc-plugin README](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/plugins/mc-plugin/README.md#L44-L57) 明确服务器消息、指令、图片和多服务器 WS | 与普通 MC 状态查询不是同一功能 |
| AI Agent | `agent-plugin/package.json` 写明支持对话、Agent 模式和调用 Yunzai 插件 | 普通 AI 聊天插件不能替代“调用现有插件”的耦合能力 |
| AI 绘图与多模型 | [nai-plugin README](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/plugins/nai-plugin/README.md#L5-L10) 明确 NovelAI 绘图；[y-tian-plugin README](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/plugins/y-tian-plugin/README.md#L49-L66) 声明多模型与娱乐集成 | 需分别核后端 API、账号、配额和内容审核，不是单一“AI”复选框 |
| 发言统计与卡片 | [Speech-statistics-plugin README](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/plugins/Speech-statistics-plugin/README.md#L13-L28) 明确多维排行、图片和数据管理 | ZBP `chatcount` 只能算部分功能重合，不能直接视为等价替换 |
| 签到 | `yunzai-plugin-deer-pipe/package.json` 明示签到用途 | 其他签到插件是否等价仍需检查命令和数据 |

<a id="local-3"></a>

### 2.4 当前渲染与版本

- 本地 `package.json` 为 `trss-yunzai@3.1.3`，依赖 `puppeteer`；线上默认分支还列出 `shotium`。[L4](#local-4) [cite:38](#cite-38)
- [runtime.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/plugins/runtime.js#L189-L248) 从插件 `resources` 读取 HTML 模板并调用 `puppeteer.screenshot`，说明模板渲染与框架运行时直接耦合。<a id="local-4"></a>
- 线上 TRSS 最近提交为 2026-11-01，本地 HEAD 为 2026-07-02；因此本地行为基线与线上最新版不能混称。[cite:36](#cite-36)

---

## 3. 候选框架、协议、许可证与渲染总表

| 候选 | 语言/角色 | 许可证 | OneBot 与协议端证据 | 渲染方式 |
|---|---|---|---|---|
| **ZeroBot + ZBP** | Go；框架 + 插件合集 | ZeroBot GPL-3.0；ZBP AGPL-3.0 [cite:1](#cite-1) [cite:8](#cite-8) | ZeroBot 明确 OneBot v11、NapCat/NapNeko 扩展、HTTP/正反向 WS；v12 未查证。Lagrange 仅可按旧 v1/OB11 配置推断，当前 V2/Milky 官方支持未查证 [cite:1](#cite-1) [cite:42](#cite-42) | 核心不自带浏览器渲染；已核 `chatcount` 使用 Go `image`、`gg`、`rendercard`，为纯代码渲染实例 [cite:10](#cite-10) |
| **Koishi** | TypeScript/Node；框架 | 核心及现行 OneBot 适配器 MIT；社区插件另核 | 适配器明确 v11 与 HTTP/WS/反向 WS；NapCat 有官方接入步骤；v12、当前 Milky 未查证 [cite:14](#cite-14) [cite:15](#cite-15) | 核心不是 HTML 渲染器；官方 Puppeteer 插件调用本机 Chrome/Edge/Firefox 截图 [cite:18](#cite-18) |
| **NoneBot2** | Python；框架 | 核心 MIT；适配器/插件另核 | 官方 OneBot 适配器明确 v11/v12；NapCat 有官方接入步骤；官方 `adapter-milky` 支持 Milky，README 标 Lagrange.Milky 为 WIP [cite:22](#cite-22) [cite:23](#cite-23) [cite:42](#cite-42) | 核心通过生态渲染；`htmlrender`/Playwright 属浏览器路线 [cite:25](#cite-25) |
| **LittlePaimon** | Python；NoneBot2 应用 | AGPL-3.0 | `bot.py` 只注册 OneBot v11；项目级 v12、NapCat、当前 Lagrange 对接未查证 [cite:29](#cite-29) | `pyproject.toml` 声明 Playwright、Pillow、matplotlib [cite:29](#cite-29) |
| **Zhenxun** | Python；NoneBot2 应用 | AGPL-3.0 | 配置启用 OneBot v11；README 列 NapCat/Lagrange.Core，但不能外推到当前 V2；v12 未查证 [cite:32](#cite-32) | `htmlrender` + Playwright，另有 Pillow [cite:32](#cite-32) |
| **Liteyuki v7** | Python；独立应用/内核 | LSO-Common v1.4，自定义许可证；不能按 MIT 理解 | Alpha15 为 OneBot v11 + SnowLuma WS；v12、NapCat、当前 Milky 未查证 [cite:35](#cite-35) | 当前 v7 渲染能力未查证；不能把 v6 `htmlrender` 直接记到 v7 |
| **TRSS-Yunzai** | Node；现有应用 | GPL-3.0 | README 列 OneBot v11、旧 Lagrange.OneBot 等；本地另有 Milky 适配器。v12 未查证 [cite:38](#cite-38) [L3](#local-3) | 内置 Puppeteer 截图运行时；线上清单另有 Shotium [cite:38](#cite-38) [L4](#local-4) |
| **Miao-Yunzai** | Node；Yunzai 应用 | GPL-3.0 | 官方 README 仍以 ICQQ 为主；本次未查证其官方 OneBot v11/v12、NapCat 或当前 Lagrange 配置依据 [cite:41](#cite-41) | Puppeteer 路线；与 TRSS 有继承关系，但不据此推定协议完全一致 [cite:41](#cite-41) |

**许可证提示**：这是工程选型核对，不是法律意见。现有 TRSS-Yunzai 已是 GPL-3.0，因此 ZeroBot 的 GPL-3.0 不一定是新增阻碍；但引入 AGPL-3.0 的 ZBP、Zhenxun 或 LittlePaimon 代码，以及各社区插件时，必须逐项审查，不能由核心许可证推断整个部署栈许可证。

---

## 4. 活跃度与稳定性证据

### 4.1 统一数据表

下表的 issue 为“近六个月新建 / 同批当前关闭”。所有数据均截至 2026-11-13。

| 项目 | 默认分支最近提交 | 最新发布记录与节奏 | issue | 维护证据与判断 |
|---|---|---|---|---|
| ZeroBot | `d207de3`，2026-04-07 | 无 GitHub Release；最新 tag v1.8.2，2025-12-10；v1.8.1 为 2025-01-28，v1.8.0 为 2024-10-14，节奏稀疏且不固定 | 0/0 | `fumiama` 完成最新提交，贡献记录中还有 `wdvxdr1123`；有持续维护信号，但不能用 0 issue 证明稳定 [cite:2](#cite-2) [cite:3](#cite-3) |
| ZBP | `65619f0`，2026-09-10 | v1.10.23，2026-04-07；最近十次 Release 集中在 2026-01-04—04-07，密集修复与数周间隔并存，4 月后有提交无 Release | 0/0 | 最新 Release 作者 `fumiama`，2026 年提交记录持续出现；仍属滚动维护 [cite:5](#cite-5) [cite:6](#cite-6) |
| Koishi | 核心 `97d5c39`，2026-10-08；OneBot 适配器同日更新 | 核心 npm 4.18.11，2026-02-27；适配器 6.9.5，2026-10-08。核心近几个正式版呈不固定的数月间隔，适配器较新 | **11/5** | Shigma 的提交链从 2019 年延续到 2026 年，两个 npm 包 maintainer 均为 `shigma` [cite:11](#cite-11) [cite:12](#cite-12) [cite:13](#cite-13) |
| NoneBot2 | `2450b67`，2026-11-12 | v2.5.0，2026-04-01；近六个月无新 Release，但默认分支持续更新 | **116/103** | 贡献记录显示 `yanyongyu`、`stdrc` 等长期贡献；近期还有自动 changelog 与功能提交 [cite:20](#cite-20) [cite:21](#cite-21) |
| LittlePaimon | `3b2e89f`，**2025-03-06** | v3.0.8，**2023-06-14**；近六个月 0 Release | 0/0 | 历史主要贡献者为 `CMHopeSunshine`，但未见 2026 默认分支提交；维护风险高是推断 [cite:27](#cite-27) [cite:28](#cite-28) |
| Zhenxun | `e0fd4a0`，2026-10-07 | v0.2.4-fix3，2024-11-16；近六个月 0 Release，提交领先 Release | 4/1 | `HibiKier` 有大量历史贡献，近期有 `molanp` 等提交；有维护但发布滞后 [cite:30](#cite-30) [cite:31](#cite-31) |
| Liteyuki | `2ccd25a`，2026-09-02 | 最新**稳定** Release v6.3.9，2024-08-22；最新**预发布** v7.0.0b1，2026-08-12。main 文档称 Alpha15 为签名 Release bundle，但本次 Release 列表未核到对应条目，文档/发布状态存在待核差异 | 0/0 | `snowykami` 为历史主要贡献者，`Nanaloveyuki` 负责近期提交和预发布；处于 v7 迁移期 [cite:33](#cite-33) [cite:34](#cite-34) [cite:35](#cite-35) |
| TRSS-Yunzai | `d94cb7f`，2026-11-01 | 无 GitHub Release，滚动提交 | **12/9** | `TimeRainStarSky` 长期维护，近期另有 `starim00` 等提交；近期提交直接修复 Puppeteer 图片与浏览器复用问题 [cite:36](#cite-36) [cite:37](#cite-37) |
| Miao-Yunzai | `354b806`，2026-10-19 | 无 GitHub Release，滚动提交 | 1/0 | 贡献记录含 `yoimiya-kokomi`、`kaguramaisakuya`、`TimeRainStarSky`；近期仍有提交 [cite:39](#cite-39) [cite:40](#cite-40) |

### 4.2 数据冲突与修正

- Koishi 子任务曾按“期间关闭日期”得到 8 个关闭 issue；为与其他项目统一，本报告改用“同批新建议题当前关闭”口径，实际查询结果为 **11/5**。两者不是同一指标，不混算关闭率。[cite:13](#cite-13)
- Liteyuki 的 `/releases/latest` 只返回最新稳定版 v6.3.9；Release 列表另有预发布 v7.0.0b1。报告分别列出，不把预发布当稳定版。[cite:33](#cite-33)
- 0/0 项目可能同时存在低 issue 流量、用户转到其他渠道或 issue 使用较少等情况；本次没有进一步证据，不推断原因。

---

## 5. 插件生态与五类常用能力

### 5.1 数量的真实含义

| 生态 | 实际查证数量 | 不能怎样解读 |
|---|---|---|
| ZBP | `plugin/` 下 109 个目录；`main.go` 默认启用 107 个本仓库插件，另有 1 个被注释的 `wtf`，`nsfwauto` 未默认导入 | 109 不等于 109 个适合本项目的替代品；107 也不含所有外部插件 [cite:7](#cite-7) |
| Koishi 官方市场索引 | 实取 `total=objects=4879` | 包含社区包，不代表全部官方、审核通过、维护中或兼容当前版本 [cite:16](#cite-16) |
| NoneBot 注册表 | 实取 955 条，其中 `valid=true` 627 条 | `valid` 不等于完成本项目功能、安全或许可证审计 [cite:24](#cite-24) |
| Liteyuki v7 | 官方 README 明说公共索引当前**没有 Alpha15 兼容条目**，旧 `liteyukibot.plugins` 不可互换 | v6 插件数量与能力不能迁移记到 v7 [cite:35](#cite-35) |
| 本地 Yunzai | 38 目录、36 加载候选、32 index 入口 | 目录数不等于独立功能数，更不等于其他框架可直接复用数 [L2](#local-2) |

### 5.2 能力对照

下表只证明“找到对应实现或实例”，不证明生产质量、接口等价或迁移完成。

| 候选 | 游戏查询 | 图片渲染 | AI 对话/生成 | 点歌 | 签到 |
|---|---|---|---|---|---|
| ZeroBot + ZBP | 有 `ygocdb`、`ygotrade`，偏游戏王；**不是**现有原神/星铁/鸣潮等面板插件的等价替代 | `chatcount` 等为纯 Go 绘图 | `aichat`、`llm` | `music` | `manager` 含群签到 [cite:9](#cite-9) |
| Koishi | 市场实例 `wcl-lili` 查询 Warcraft Logs | 官方 Puppeteer | 社区 ChatLuna 等 | 社区 `music-voice` | 社区 `codegang-qd`；实例均须逐包审查 [cite:16](#cite-16) [cite:17](#cite-17) |
| NoneBot2 | GenshinUID 等 | `htmlrender` | ChatGPT API 等 | MultiNCM | 云签到等注册实例 [cite:25](#cite-25) |
| LittlePaimon | 原神 UID、抽卡、图鉴等，与本项目部分米游社需求接近 | Playwright | `Learning_Chat` 存在，但不能据此称生成式 AI | 未查证 | 米游社自动签到源码存在 [cite:29](#cite-29) |
| Zhenxun | 本体目标游戏查询未完整核实，可尝试 NoneBot 生态 | `htmlrender` + Playwright | 插件仓库列 AI/伪人 AI | 插件仓库有 `music` | 内置签到 [cite:32](#cite-32) |
| Liteyuki v7 | 当前兼容插件未查证 | 当前兼容渲染插件未查证 | 未查证 | 未查证 | 未查证；公共索引为零是主要限制 [cite:35](#cite-35) |
| TRSS/Miao-Yunzai | 与现有 Yunzai 游戏插件最匹配 | Puppeteer，现有模板可直接沿用程度最高 | 有 Yunzai AI 插件生态 | 核心能力未完整核实 | 依具体插件；不能把核心与插件混算 [cite:38](#cite-38) [cite:41](#cite-41) |

### 5.3 插件质量结论

- **事实**：ZBP 的五类通用能力覆盖最直接，而且与 Go 技术栈一致。
- **事实**：NoneBot2、Koishi 的可检索数量更大，但大量条目是社区包，质量、维护、许可证和当前兼容性必须逐包检查。
- **推断，中高置信**：对本项目，插件生态的价值排序应是“目标业务等价程度 > 当前维护 > 许可证 > 数量”。仅按 4,879 或 955 计算可迁移比例会严重高估收益。
- **未查证**：本次没有逐一安装、运行或审计上述商店插件，不能给出“可用插件占比”。

---

## 6. 并发、渲染与内存：能证明什么

| 候选 | 并发/事件模型证据 | 渲染与内存特点 | 可量化性能结论 |
|---|---|---|---|
| ZeroBot | 默认事件接收后启动 goroutine；Driver 并发监听；单事件仍按 Matcher 规则处理，并有最大处理时间配置 [cite:4](#cite-4) | 已核实部分 ZBP 卡片不启动浏览器；可避免相应浏览器子进程开销是**推断**。字体、图片解码仍占内存 | 有解析微基准源码，但未附公开端到端吞吐、RSS 或长期运行结果；不能承诺“更快/更省” [cite:4](#cite-4) |
| Koishi | `emit` 同步、`parallel` 异步并发，`bail/serial` 串行；仍是 Node 事件体系 [cite:19](#cite-19) | Puppeteer 需要额外浏览器；内存统计必须包含浏览器子进程 | 未查证到公开可复现核心 benchmark；`status` 的实时 CPU/内存显示不是 benchmark [cite:19](#cite-19) |
| NoneBot2 | `handle_event` 通过 `driver.task_group.start_soon` 调度，属于异步 TaskGroup 模型 [cite:26](#cite-26) | 使用 Playwright/htmlrender 时同样要统计浏览器进程 | 未查证到统一负载的公开端到端数据 |
| LittlePaimon / Zhenxun | 继承 NoneBot2 异步模型 | 两者均声明 Playwright；业务插件还含图像/科学计算依赖 | 未查证到可复现对比数据 |
| Liteyuki v7 | 当前架构要求命令与资源回调 async-only；Cordis 是受信任的同进程代码，不是安全沙箱 [cite:35](#cite-35) | 当前渲染实现未查证 | 旧站启动/内存宣传缺测试方法，不采用 |
| TRSS/Miao-Yunzai | Node 异步事件与 WebSocket；PM2 是进程托管，不等于自动多线程业务扩展 | HTML 模板经 Puppeteer 截图；浏览器是额外内存来源 | TRSS 的 PM2 512M 配置是重启阈值，不是实测占用；未做横向排名 [cite:38](#cite-38) |

### 6.1 不能从语言直接推出的结论

- 不能从“Go 使用 goroutine”推出目标业务吞吐一定高于 NoneBot2 或 Koishi。
- 不能从“纯 Go 画图”推出整个 ZBP 的 RSS 一定更低；107 个默认插件、字体缓存、图片解码和外部 API 都影响总量。
- 不能只采集父进程内存；Puppeteer/Playwright 的浏览器子进程必须纳入。
- 不能把框架微基准、PM2 重启阈值或状态页当前值当作跨框架 benchmark。

### 6.2 后续可复现 benchmark 方案

本任务未执行以下测试，仅给出验收设计：

1. **固定环境**：同一机器、相同协议端或本地录制回放、相同 Node/Python/Go 版本、相同插件命令与输出图片。
2. **三组最小核心**：当前 TRSS、ZeroBot 最小核心、NoneBot2 最小核心；Koishi 可作为 Node 对照，不必默认全量测试。
3. **负载分层**：纯文本事件、外部 API 模拟响应、相同 HTML 卡片浏览器渲染、相同纯代码图片渲染。浏览器与纯代码渲染分开比较，不混为单一分数。
4. **指标**：吞吐、端到端 p50/p95/p99、错误率、冷启动、稳态 CPU、进程树总 RSS、长时间运行后的内存趋势。
5. **稳定性**：断线重连、重复事件、慢 API、渲染失败、插件异常、协议端重启。
6. **通过条件**：先测出现有 TRSS 基线，再设相对阈值；在基线缺失时不编造“提升百分之多少”。

---

## 7. 迁移到底能省多少工作量

### 7.1 可验证的节省与不可节省部分

| 工作项 | 采用成熟框架后的结果 | 证据/性质 |
|---|---|---|
| OneBot v11 HTTP/WS、消息编码、基础发送 API | ZeroBot、NoneBot2、Koishi 均可避免从零实现 | 官方文档事实 |
| 事件分发、匹配器、中间件、插件生命周期 | 可复用框架实现 | 源码/文档事实 |
| 当前 Lagrange V2/Milky | NoneBot2 可复用官方适配器；ZeroBot 需新增或另找 Driver | 官方仓库事实；生产可用性仍需测试 |
| 32 个本地 index JS 插件直接加载 | **0 个可由非 Yunzai 框架直接加载** | 语言、加载器与运行时接口直接决定 |
| 原神/星铁/鸣潮/明日方舟等数据契约、Token、命令兼容 | 需逐插件迁移或确认等价替代 | 本地功能证据；不能用“有游戏插件”替代 |
| AI Agent 调用 Yunzai 插件 | 需重新设计工具接口与权限边界 | 本地清单明示耦合 |
| HTML 模板和静态资源 | **可能部分复用**，但截图服务、路径、数据字段、模板 helper 要适配 | 推断；runtime 代码证明存在耦合 |
| 数据库、Redis/Level/SQLite 数据、用户配置 | 未逐插件盘点，不能给自动迁移比例 | 未查证 |
| ZBP 通用功能 | 接受行为变化时可少写聊天统计、AI、点歌、签到等通用功能 | 有对应插件；与本地功能并非完全等价 |

### 7.2 不虚构人日或百分比

本次能可靠量化的只有：

- **0/32**：现有 index 插件可直接被其他语言/ API 框架加载的数量。
- **107**：ZBP 默认启用的本仓库插件数，可带来通用功能，但不是 107 个本地插件替代品。
- **36**：本地需要逐项决定“保留、替换、重写、淘汰”的加载候选目录数。

**除此之外，公开证据不足以给出迁移人日或“节省百分之多少”。** 这是证据边界，不是遗漏。合理估算应在试点后按下列成本项实测：

`单插件成本 = 命令/事件适配 + 外部 API 与登录态 + 数据迁移 + 渲染适配 + 权限/配置 + 回归测试`。

### 7.3 用于校准工作量的三个试点

1. `Speech-statistics-plugin`：数据库、统计和纯卡片候选，可比较 ZBP `chatcount` 的功能差距。
2. `agent-plugin`：验证 AI、工具调用和现有插件接口解耦成本。
3. `waves-plugin` 或 `miao-plugin`：验证复杂游戏数据、登录态、HTML 模板与浏览器渲染成本。

试点完成后才能形成有证据的插件分级和整体迁移估算。

---

## 8. 三种实施路线对比

| 路线 | 优点 | 缺点/风险 | 前置条件 | 本项目判断 |
|---|---|---|---|---|
| **直接采用某框架/发行版** | 核心立即可用；ZBP/NoneBot/Koishi 可快速补通用能力；部署文档现成 | 现有 JS 插件零直接加载；命令、数据和渲染不等价；社区插件质量不一；ZBP/部分发行版为 AGPL | 接受功能替换和行为变化；完成协议、许可证、目标插件兼容审查 | **不适合作为“完整替换现有系统”的方案**；可作为通用能力验证环境 |
| **基于成熟框架二次开发 + 迁插件** | 复用通信、事件和插件生命周期；可保留自有业务；分阶段替换渲染与插件；核心维护可跟上游 | 仍需插件迁移和测试；可能需 Milky Driver；要控制 fork 与上游分叉 | 先锁定框架版本；完成协议 spike、许可证审查、三个试点与 benchmark | **默认推荐**。Go 硬约束选 ZeroBot；语言可放宽则 NoneBot2 证据更强 |
| **自研 Go 核心从零重写** | 可自定义 Milky/OneBot、多协议、插件 API、兼容层与渲染；许可证可自主选择 | 通信、回声/重连、事件顺序、背压、插件隔离、热更新、故障恢复和测试都需自行承担；核心收益没有现成维护者 | 必须证明现有框架的许可证、协议、扩展点或实测性能不能满足；有长期核心维护预算 | **条件性后备，而非默认首选**。仅“想用 Go”或“可能更快”不足以证明其优于复用 |

### 8.1 最终推荐

**推荐决策：先走“成熟核心 + 迁插件”，不要直接换成完整发行版，也暂不从零自研核心。**

- **Go 路线**：以 ZeroBot 为依赖，尽量不为业务需求长期 fork 核心；先验证 NapCat/OneBot v11。若当前 Lagrange V2 是必需项，再实现并测试 Milky Driver。ZBP 只选择经业务、维护和 AGPL 审查的插件，不默认全盘接收 107 个插件。
- **跨语言路线**：若项目愿意把“Go 优先”降为偏好，NoneBot2 更值得先做功能试点：MIT、v11/v12/Milky、近期提交与 issue 处理记录更完整。选择它是因为证据链，而不是因为插件总数大。
- **暂缓选项**：LittlePaimon 因维护信号弱；Liteyuki v7 因兼容插件索引为空和版本迁移；Koishi 因与现有 Node 技术栈重合且没有速度证据；Miao-Yunzai 则没有形成比当前 TRSS 更明确的迁移收益。

### 8.2 事实与推断分栏

| 判断 | 类型 | 依据 |
|---|---|---|
| 非 Yunzai 框架不能直接加载现有 32 个 JS index 插件 | 事实 | 本地加载器、语言和插件 API |
| ZeroBot 能减少自研 OneBot v11 核心的工作 | 事实支持的直接结论 | 官方 Driver、API、Matcher 源码 |
| ZeroBot 是当前 Go 优先方向的最接近候选 | 推断，中高置信 | Go、ZBP 生态、纯代码渲染、持续提交；受协议与许可证约束 |
| NoneBot2 是语言可放宽时证据最完整的候选 | 推断，中高置信 | MIT、v11/v12/Milky、提交、issue、注册表 |
| ZeroBot 或 Go 一定更快、更省内存 | **未证实** | 没有同负载公开数据 |
| 直接采用 ZBP 可替代现有全部游戏插件 | **不成立** | 已核插件目标与本地数据契约不同 |
| 自研核心一定更灵活但成本更高 | 工程推断，高置信 | 自研范围增加；具体成本需试点估算 |
| NapCat/Lagrange v1 可经 OneBot v11 与相应框架连接 | 文档事实 + 配置推断 | 双方协议和 WS 配置匹配；未在本任务启动验证 |
| 当前 Lagrange V2 可直接接 ZeroBot | **未查证，不得默认支持** | ZeroBot 未见 Milky 文档；Lagrange V2 使用 Milky |

---

## 9. 落地前的验证门

1. **协议门**：明确生产环境到底使用 NapCat、旧 Lagrange.OneBot 还是当前 Lagrange.Milky；对选定框架做收发、重连、文件、合并转发与异常测试。
2. **许可证门**：确认 ZeroBot GPL-3.0、拟引入 ZBP/插件的 AGPL 或其他条款可接受；社区插件逐个记录许可证。
3. **功能门**：完成 `Speech-statistics`、`agent-plugin`、`waves/miao` 三个试点，列出命令兼容、数据和渲染差异。
4. **性能门**：按第 6.2 节测 TRSS 基线与候选最小核心；没有原始结果前不写“提速多少”。
5. **维护门**：优先以依赖方式跟随上游，记录上游 commit/tag 和本地补丁；避免业务代码直接堆入框架 fork。
6. **退出门**：若 Go + 当前 Milky + 许可证/插件兼容要求使 ZeroBot 改造成本超过收益，再比较 NoneBot2 与自研 Go，而不是预设自研必然胜出。

---

## 10. 检索记录与来源链接

### 10.1 记录说明

- “主会话复核 02:24—02:35 +08”表示本报告主流程在该时段实际打开或重新请求页面。
- “并行检索，分钟未记录”表示同日由独立在线检索子任务实际打开并返回摘录与 URL；不编造具体分钟。
- GitHub 页面/API 是动态数据，后续复核会得到不同结果，应保留本报告日期和查询口径。
- 一次 `contents/plugin` 批量复核遇到未认证 GitHub API rate limit；没有反复重试，目录数采用已成功打开的官方目录页，默认导入数则由 `main.go` 独立复核。
- Koishi raw LICENSE 的一次抓取返回空内容，但 npm 元数据标明 MIT，且官方 LICENSE 页已在并行检索中打开；未把空抓取当证据。
- PyPI 的 Milky 项目页/JSON 抓取未成功，随后实际打开 `nonebot/adapter-milky` 官方 GitHub 仓库取得协议与配置证据。

### 10.2 Go / ZeroBot

<a id="cite-1"></a>
**[cite:1] ZeroBot 协议、通信与 GPL-3.0**  
访问：2026-11-13，主会话复核 02:24—02:35 +08；并行检索同日。  
https://raw.githubusercontent.com/wdvxdr1123/ZeroBot/main/README.md  
https://github.com/wdvxdr1123/ZeroBot/blob/main/LICENSE

<a id="cite-2"></a>
**[cite:2] ZeroBot 最近提交、tag 与发布时间**  
访问：2026-11-13，主会话复核 02:24—02:35 +08。  
https://api.github.com/repos/wdvxdr1123/ZeroBot/commits/main  
https://api.github.com/repos/wdvxdr1123/ZeroBot/tags?per_page=5  
https://github.com/wdvxdr1123/ZeroBot/commit/f73cb05fc457d5f80a271c8a02ad4ac0ab6e03f5  
https://github.com/wdvxdr1123/ZeroBot/releases

<a id="cite-3"></a>
**[cite:3] ZeroBot issue 与贡献者记录**  
访问：2026-11-13，并行检索，分钟未记录。  
https://api.github.com/search/issues?q=repo%3Awdvxdr1123%2FZeroBot%20is%3Aissue%20created%3A2026-05-13..2026-11-13&per_page=1  
https://api.github.com/search/issues?q=repo%3Awdvxdr1123%2FZeroBot%20is%3Aissue%20is%3Aclosed%20created%3A2026-05-13..2026-11-13&per_page=1  
https://api.github.com/repos/wdvxdr1123/ZeroBot/contributors?per_page=100

<a id="cite-4"></a>
**[cite:4] ZeroBot 并发源码与微基准范围**  
访问：2026-11-13，主会话打开 `bot.go`；基准文件由并行检索打开。  
https://github.com/wdvxdr1123/ZeroBot/blob/main/bot.go  
https://github.com/wdvxdr1123/ZeroBot/blob/main/ring.go  
https://github.com/wdvxdr1123/ZeroBot/blob/main/all_test.go  
https://github.com/wdvxdr1123/ZeroBot/blob/main/message/cqstring_test.go

<a id="cite-5"></a>
**[cite:5] ZBP 最近提交与 Release**  
访问：2026-11-13，主会话复核 02:24—02:35 +08。  
https://api.github.com/repos/FloatTech/ZeroBot-Plugin/commits/master  
https://api.github.com/repos/FloatTech/ZeroBot-Plugin/releases/latest  
https://github.com/FloatTech/ZeroBot-Plugin/releases.atom

<a id="cite-6"></a>
**[cite:6] ZBP issue 与维护记录**  
访问：2026-11-13，并行检索，分钟未记录。  
https://api.github.com/search/issues?q=repo%3AFloatTech%2FZeroBot-Plugin%20is%3Aissue%20created%3A2026-05-13..2026-11-13&per_page=1  
https://api.github.com/search/issues?q=repo%3AFloatTech%2FZeroBot-Plugin%20is%3Aissue%20is%3Aclosed%20created%3A2026-05-13..2026-11-13&per_page=1  
https://github.com/FloatTech/ZeroBot-Plugin/commits/master.atom

<a id="cite-7"></a>
**[cite:7] ZBP 插件目录与默认导入数**  
访问：2026-11-13，`main.go` 由主会话实际请求并计算 107 个有效导入；目录页由并行检索打开。  
https://github.com/FloatTech/ZeroBot-Plugin/tree/master/plugin  
https://raw.githubusercontent.com/FloatTech/ZeroBot-Plugin/master/main.go

<a id="cite-8"></a>
**[cite:8] ZBP AGPL-3.0 与项目定位**  
访问：2026-11-13，主会话复核 02:24—02:35 +08。  
https://raw.githubusercontent.com/FloatTech/ZeroBot-Plugin/master/LICENSE  
https://github.com/FloatTech/ZeroBot-Plugin/blob/master/README.md

<a id="cite-9"></a>
**[cite:9] ZBP 五类功能插件实例**  
访问：2026-11-13，并行检索，分钟未记录。  
https://github.com/FloatTech/ZeroBot-Plugin/tree/master/plugin/ygocdb  
https://github.com/FloatTech/ZeroBot-Plugin/tree/master/plugin/ygotrade  
https://github.com/FloatTech/ZeroBot-Plugin/tree/master/plugin/aichat  
https://github.com/FloatTech/ZeroBot-Plugin/tree/master/plugin/llm  
https://github.com/FloatTech/ZeroBot-Plugin/tree/master/plugin/music  
https://github.com/FloatTech/ZeroBot-Plugin/tree/master/plugin/manager

<a id="cite-10"></a>
**[cite:10] ZBP 纯代码渲染实例**  
访问：2026-11-13，主会话复核 02:24—02:35 +08。  
https://raw.githubusercontent.com/FloatTech/ZeroBot-Plugin/master/plugin/chatcount/chatcount.go  
https://raw.githubusercontent.com/FloatTech/rendercard/main/go.mod  
https://github.com/wdvxdr1123/ZeroBot/blob/main/go.mod

### 10.3 Koishi

<a id="cite-11"></a>
**[cite:11] Koishi 核心提交与 npm 发版**  
访问：2026-11-13，主会话复核 02:24—02:35 +08；历史版本时间由并行检索打开完整元数据。  
https://api.github.com/repos/koishijs/koishi/commits/master  
https://registry.npmjs.org/koishi  
https://registry.npmjs.org/koishi/latest

<a id="cite-12"></a>
**[cite:12] Koishi OneBot 适配器提交与发版**  
访问：2026-11-13，主会话打开 npm 元数据；提交页由并行检索打开。  
https://github.com/koishijs/koishi-plugin-adapter-onebot/commit/4109eb95c34e53d5bdacfeb4970ce26a1e17b0de  
https://registry.npmjs.org/koishi-plugin-adapter-onebot  
https://registry.npmjs.org/koishi-plugin-adapter-onebot/latest

<a id="cite-13"></a>
**[cite:13] Koishi issue 统一口径与长期维护证据**  
访问：2026-11-13，issue 同批关闭数由主会话复核；历史提交由并行检索打开。  
https://api.github.com/search/issues?q=repo%3Akoishijs%2Fkoishi%20is%3Aissue%20created%3A2026-05-13..2026-11-13&per_page=1  
https://api.github.com/search/issues?q=repo%3Akoishijs%2Fkoishi%20is%3Aissue%20is%3Aclosed%20created%3A2026-05-13..2026-11-13&per_page=1  
https://github.com/koishijs/koishi/commit/3ed2648c4cee7a29f64fd5d7e702457a0ea5a3f8

<a id="cite-14"></a>
**[cite:14] Koishi 核心与适配器许可证**  
访问：2026-11-13，官方 LICENSE 由并行检索打开；npm MIT 字段由主会话复核。  
https://github.com/koishijs/koishi/blob/master/LICENSE  
https://github.com/koishijs/koishi-plugin-adapter-onebot/blob/main/LICENSE  
https://registry.npmjs.org/koishi/latest  
https://registry.npmjs.org/koishi-plugin-adapter-onebot/latest

<a id="cite-15"></a>
**[cite:15] Koishi OneBot v11 与 NapCat 接入**  
访问：2026-11-13，主会话复核 02:24—02:35 +08。  
https://raw.githubusercontent.com/koishijs/koishi-plugin-adapter-onebot/main/readme.md  
https://doc.napneko.icu/use/integration

<a id="cite-16"></a>
**[cite:16] Koishi 插件市场动态索引**  
访问：2026-11-13，主会话实际请求并得到 4,879 条。  
https://registry.koishi.chat/index.json

<a id="cite-17"></a>
**[cite:17] Koishi 五类社区插件实例与 Puppeteer 维护记录**  
访问：2026-11-13，并行检索读取市场索引中的包记录；Puppeteer 提交页同日打开。  
https://registry.koishi.chat/index.json  
https://github.com/koishijs/koishi-plugin-puppeteer/commit/c4d8bfe0acf417ca73aa7291a7b9c9e337c189cd

<a id="cite-18"></a>
**[cite:18] Koishi 浏览器渲染**  
访问：2026-11-13，主会话复核 02:24—02:35 +08。  
https://puppeteer.koishi.chat/  
https://koishi.chat/zh-CN/api/message/elements.html

<a id="cite-19"></a>
**[cite:19] Koishi 事件模型与性能证据边界**  
访问：2026-11-13，主会话打开事件文档；`package.json` 和 status 文档由并行检索打开。  
https://koishi.chat/zh-CN/api/service/events.html  
https://github.com/koishijs/koishi/blob/master/package.json  
https://koishi.chat/zh-CN/plugins/console/status.html

### 10.4 NoneBot2

<a id="cite-20"></a>
**[cite:20] NoneBot2 最近提交与 Release**  
访问：2026-11-13，主会话复核 02:24—02:35 +08。  
https://api.github.com/repos/nonebot/nonebot2/commits/master  
https://api.github.com/repos/nonebot/nonebot2/releases/latest

<a id="cite-21"></a>
**[cite:21] NoneBot2 issue 与贡献者**  
访问：2026-11-13，并行检索，分钟未记录。  
https://github.com/nonebot/nonebot2/issues?q=is%3Aissue%20created%3A2026-05-13..2026-11-13  
https://github.com/nonebot/nonebot2/issues?q=is%3Aissue%20is%3Aclosed%20created%3A2026-05-13..2026-11-13  
https://api.github.com/repos/nonebot/nonebot2/contributors?per_page=5

<a id="cite-22"></a>
**[cite:22] NoneBot2 MIT 与 OneBot v11/v12**  
访问：2026-11-13，主会话复核 02:24—02:35 +08。  
https://raw.githubusercontent.com/nonebot/nonebot2/master/LICENSE  
https://onebot.adapters.nonebot.dev/docs/guide/installation/

<a id="cite-23"></a>
**[cite:23] NoneBot 官方 Milky 适配器**  
访问：2026-11-13，主会话实际打开 GitHub 仓库；PyPI 页面抓取失败，未用作证据。  
https://github.com/nonebot/adapter-milky  
https://nonebot.dev/changelog

<a id="cite-24"></a>
**[cite:24] NoneBot 注册表数量与口径**  
访问：2026-11-13，主会话实际请求并得到 955/627；商店说明由并行检索打开。  
https://registry.nonebot.dev/plugins.json  
https://nonebot.dev/docs/tutorial/store

<a id="cite-25"></a>
**[cite:25] NoneBot 五类插件实例**  
访问：2026-11-13，并行检索，分钟未记录。  
https://github.com/KimigaiiWuyi/GenshinUID  
https://github.com/kexue-z/nonebot-plugin-htmlrender  
https://github.com/SanJerry007/nonebot-plugin-chatgpt-api  
https://github.com/lgc-NB2Dev/nonebot-plugin-multincm  
https://github.com/Monarchdos/nonebot_plugin_cloudsign

<a id="cite-26"></a>
**[cite:26] NoneBot 异步事件分发**  
访问：2026-11-13，主会话复核 02:24—02:35 +08。  
https://nonebot.dev/docs/api/message

### 10.5 LittlePaimon

<a id="cite-27"></a>
**[cite:27] LittlePaimon 最近提交与 Release**  
访问：2026-11-13，主会话复核 02:24—02:35 +08。  
https://api.github.com/repos/CMHopeSunshine/LittlePaimon/commits/Bot  
https://api.github.com/repos/CMHopeSunshine/LittlePaimon/releases/latest

<a id="cite-28"></a>
**[cite:28] LittlePaimon issue 与贡献者**  
访问：2026-11-13，并行检索，分钟未记录。  
https://github.com/CMHopeSunshine/LittlePaimon/issues?q=is%3Aissue%20created%3A2026-05-13..2026-11-13  
https://github.com/CMHopeSunshine/LittlePaimon/issues?q=is%3Aissue%20is%3Aclosed%20created%3A2026-05-13..2026-11-13  
https://api.github.com/repos/CMHopeSunshine/LittlePaimon/contributors?per_page=5

<a id="cite-29"></a>
**[cite:29] LittlePaimon 协议、许可证、渲染与功能**  
访问：2026-11-13，主会话打开 `bot.py`/`pyproject.toml`；README、LICENSE、签到源码由并行检索打开。  
https://raw.githubusercontent.com/CMHopeSunshine/LittlePaimon/Bot/bot.py  
https://raw.githubusercontent.com/CMHopeSunshine/LittlePaimon/Bot/pyproject.toml  
https://github.com/CMHopeSunshine/LittlePaimon/blob/Bot/LICENSE  
https://github.com/CMHopeSunshine/LittlePaimon#readme  
https://github.com/CMHopeSunshine/LittlePaimon/tree/Bot/LittlePaimon/plugins/Learning_Chat  
https://github.com/CMHopeSunshine/LittlePaimon/blob/Bot/LittlePaimon/plugins/Paimon_Autobbs/sign_handle.py

### 10.6 Zhenxun

<a id="cite-30"></a>
**[cite:30] Zhenxun 最近提交与 Release**  
访问：2026-11-13，主会话复核 02:24—02:35 +08。  
https://api.github.com/repos/zhenxun-org/zhenxun_bot/commits/main  
https://api.github.com/repos/zhenxun-org/zhenxun_bot/releases/latest

<a id="cite-31"></a>
**[cite:31] Zhenxun issue 与贡献者**  
访问：2026-11-13，并行检索，分钟未记录。  
https://github.com/zhenxun-org/zhenxun_bot/issues?q=is%3Aissue%20created%3A2026-05-13..2026-11-13  
https://github.com/zhenxun-org/zhenxun_bot/issues?q=is%3Aissue%20is%3Aclosed%20created%3A2026-05-13..2026-11-13  
https://api.github.com/repos/zhenxun-org/zhenxun_bot/contributors?per_page=5

<a id="cite-32"></a>
**[cite:32] Zhenxun 协议、许可证、渲染与插件**  
访问：2026-11-13，主会话打开 `pyproject.toml`；其余官方页面由并行检索打开。  
https://raw.githubusercontent.com/zhenxun-org/zhenxun_bot/main/pyproject.toml  
https://github.com/zhenxun-org/zhenxun_bot/blob/main/LICENSE  
https://github.com/zhenxun-org/zhenxun_bot#readme  
https://github.com/zhenxun-org/zhenxun_bot_plugins  
https://github.com/zhenxun-org/zhenxun_bot_plugins/tree/main/plugins/music

### 10.7 Liteyuki

<a id="cite-33"></a>
**[cite:33] Liteyuki 最近提交、稳定版与预发布**  
访问：2026-11-13，主会话复核 02:24—02:35 +08。  
https://api.github.com/repos/LiteyukiStudio/LiteyukiBot/commits/main  
https://api.github.com/repos/LiteyukiStudio/LiteyukiBot/releases/latest  
https://api.github.com/repos/LiteyukiStudio/LiteyukiBot/releases?per_page=5

<a id="cite-34"></a>
**[cite:34] Liteyuki issue 与贡献者**  
访问：2026-11-13，并行检索，分钟未记录。  
https://github.com/LiteyukiStudio/LiteyukiBot/issues?q=is%3Aissue%20created%3A2026-05-13..2026-11-13  
https://github.com/LiteyukiStudio/LiteyukiBot/issues?q=is%3Aissue%20is%3Aclosed%20created%3A2026-05-13..2026-11-13  
https://api.github.com/repos/LiteyukiStudio/LiteyukiBot/contributors?per_page=5

<a id="cite-35"></a>
**[cite:35] Liteyuki 当前架构、插件兼容边界与许可证**  
访问：2026-11-13，主会话复核 02:24—02:35 +08。  
https://raw.githubusercontent.com/LiteyukiStudio/LiteyukiBot/main/docs/architecture/v7.md  
https://raw.githubusercontent.com/LiteyukiStudio/LiteyukiBot/main/README.md  
https://raw.githubusercontent.com/LiteyukiStudio/LiteyukiBot/main/LICENSE

### 10.8 Yunzai 与协议端

<a id="cite-36"></a>
**[cite:36] TRSS-Yunzai 最近提交与无 Release 状态**  
访问：2026-11-13，主会话复核 02:24—02:35 +08。  
https://api.github.com/repos/TimeRainStarSky/Yunzai/commits/main  
https://github.com/TimeRainStarSky/Yunzai/releases

<a id="cite-37"></a>
**[cite:37] TRSS-Yunzai issue 与贡献者**  
访问：2026-11-13，并行检索，分钟未记录。  
https://github.com/TimeRainStarSky/Yunzai/issues?q=is%3Aissue%20created%3A2026-05-13..2026-11-13  
https://github.com/TimeRainStarSky/Yunzai/issues?q=is%3Aissue%20is%3Aclosed%20created%3A2026-05-13..2026-11-13  
https://api.github.com/repos/TimeRainStarSky/Yunzai/contributors?per_page=5

<a id="cite-38"></a>
**[cite:38] TRSS-Yunzai 协议、许可证与渲染依赖**  
访问：2026-11-13，主会话复核 02:24—02:35 +08。  
https://raw.githubusercontent.com/TimeRainStarSky/Yunzai/main/README.md  
https://raw.githubusercontent.com/TimeRainStarSky/Yunzai/main/package.json  
https://raw.githubusercontent.com/TimeRainStarSky/Yunzai/main/LICENSE

<a id="cite-39"></a>
**[cite:39] Miao-Yunzai 最近提交与无 Release 状态**  
访问：2026-11-13，主会话复核 02:24—02:35 +08。  
https://api.github.com/repos/yoimiya-kokomi/Miao-Yunzai/commits/master  
https://github.com/yoimiya-kokomi/Miao-Yunzai/releases

<a id="cite-40"></a>
**[cite:40] Miao-Yunzai issue 与贡献者**  
访问：2026-11-13，并行检索，分钟未记录。  
https://github.com/yoimiya-kokomi/Miao-Yunzai/issues?q=is%3Aissue%20created%3A2026-05-13..2026-11-13  
https://github.com/yoimiya-kokomi/Miao-Yunzai/issues?q=is%3Aissue%20is%3Aclosed%20created%3A2026-05-13..2026-11-13  
https://api.github.com/repos/yoimiya-kokomi/Miao-Yunzai/contributors?per_page=5

<a id="cite-41"></a>
**[cite:41] Miao-Yunzai 协议、许可证与插件证据**  
访问：2026-11-13，并行检索，分钟未记录。  
https://github.com/yoimiya-kokomi/Miao-Yunzai#readme  
https://github.com/yoimiya-kokomi/Miao-Yunzai/blob/master/LICENSE  
https://github.com/yoimiya-kokomi/Miao-Yunzai/tree/master/plugins/genshin  
https://github.com/yoimiya-kokomi/miao-plugin#readme  
https://github.com/ikechan8370/chatgpt-plugin

<a id="cite-42"></a>
**[cite:42] NapCat 与 Lagrange 的当前/历史协议边界**  
访问：2026-11-13，主会话复核 02:24—02:35 +08。  
https://doc.napneko.icu/use/integration  
https://lagrangedev.github.io/Lagrange.Doc/v1/Lagrange.OneBot/  
https://github.com/LagrangeDev/Lagrange.Core#notice  
https://github.com/nonebot/adapter-milky

---

## 11. 最终核对清单

- [x] 覆盖 Go、TypeScript、Python、Node 候选。
- [x] 每个候选均有实际打开的提交、发布、协议、许可证或渲染页面；无法确认的项标“未查证”。
- [x] 活跃度不依赖 star 数，包含提交、Release、issue 和维护者记录。
- [x] 插件数量注明统计口径，没有把商店条目当可用替代品。
- [x] 区分浏览器渲染与纯代码渲染，未虚构内存或吞吐数据。
- [x] 本地 ZeroBot 等目录只按实际启动脚本处理，没有冒充源码基线。
- [x] 明确比较直接采用、二次开发、自研 Go 三条路线及前置条件。
- [x] 事实、推断和未查证项分开。
- [x] 检索记录包含 URL、访问时间、失败与口径修正。
