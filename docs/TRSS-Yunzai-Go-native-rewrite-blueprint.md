# TRSS-Yunzai Go 原生重写可执行蓝图

- 日期：2026-11-13
- 范围：[app.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/app.js)、[lib/](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib)、OneBot 适配器，以及 [plugins/](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/plugins) 下 38 个一级目录；协议端固定为 SnowLuma v1.14.17（win-x64）。插件允许原生化重构，不设置长期 JS 兼容宿主。
- 目标：形成可以按批次实施、离线验证、灰度切换和随时回滚的 Go 架构，而不是做一次机械语法翻译。
- 本次执行边界：只读源码、配置、既有报告与日志；未修改 `mysy2` 或 `D:\Toshikoshi_chan\Agent\SnowLuma` 下任何文件，未安装依赖、构建、启动任何服务，未执行 Git 写操作或访问插件上游网络。

## 0. 决策摘要

**建议采用：Go 单核心 + 进程内静态插件 + 独立渲染/媒体/AI/长任务服务 + 版本化 gRPC 插件契约。**

关键决策如下：

1. **不保留长期 Node 插件宿主。** 原 JS 实现是行为规格和上游差异来源，不是目标运行时。
2. **协议端固定为 SnowLuma，协议适配只保留 OneBot v11。** Go 作为正向 WS 客户端连接 SnowLuma；不实现其他协议适配分支，不重写 SnowLuma。
3. **核心插件默认进程内静态注册。** Windows 不使用 Go `plugin` 动态库；需要独立故障域、动态发布或外部运行时能力的插件做成独立可执行服务。
4. **渲染独立成服务。** Go 核心只调用渲染客户端；Chromium 的并发、页面生命周期、超时和崩溃恢复由渲染服务统一管理。
5. **高频功能按垂直切片迁移。** 先迁移实际命中的规则，不同目录未命中的功能不得凭印象插到高频切片之前；同目录其余启用功能随后补齐。
6. **`miao/genshin` 是共享依赖，不按命令量后置。** 先抽中性领域库，再迁移依赖它们的功能，避免每个游戏插件各写一套。
7. **任何自动 `git pull` 都不得直接覆盖原生实现。** 改为上游锁文件、差异评审、选择性移植和黄金测试。
8. **每个账号/群/会话作用域同一时刻只有一个事件与发送 owner。** 影子模式只记录决策，不回复、不产生业务副作用；回滚必须先移交该作用域的所有权，再恢复旧发送者。

## 1. 证据口径与已知边界

### 1.1 标记约定

- **事实**：当前源码、配置或文件元信息直接支持。
- **历史日志事实**：来自现存日志，只代表对应样本，不代表 2026-11-13 的实时状态。
- **词法事实**：静态扫描命中，属于依赖上限；动态导入、条件分支和运行时装载仍需复核。
- **推断**：由事实推导，尚待最小实验验证。
- **建议**：本蓝图选择的架构或实施策略，不冒充实测结论。

### 1.2 直接复用的既有结论

本次没有重做既有报告中的规模、性能和兼容面分析，主要复用：

- [核心重构评估](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-12/trss-refactor-assessment/outputs/trss-yunzai-refactor-assessment.md)：`lib/` 仅 3,268 物理行，框架与内置范围约 11,421 行；36 个目录有可加载入口；35 个插件目录直接导入核心内部路径；`miao/genshin` 与跨插件导入是主要兼容面。
- [性能与稳定性基线](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-12/perf-baseline/outputs/TRSS-Yunzai-perf-stability-baseline.md)：OneBot 历史初始化异常/重连、Chromium 生命周期、Redis 计数放大和任务重叠风险。

必须保留的核心行为证据：

| 契约 | 当前事实 | 目标处理 |
|---|---|---|
| 启动 | `Bot.run()` 依次初始化、监听 HTTP(S)、加载插件、加载事件与适配器，见 [bot.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/bot.js#L256-L273) | 显式 `App` 生命周期；适配器只有在插件注册完成后才接流量 |
| 进程守护 | `app.js` 有 stop/daemon/pm2/start；PM2 为单应用、512M 阈值，见 [app.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/app.js#L1-L31) 和 [pm2.yaml](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/config/pm2.yaml#L1-L7) | 过渡期可保留外部 PM2；目标由部署环境的 supervisor 管理 Go 与服务进程 |
| 外层脚本 | `TRSS-Yunzai.sh` 启动 Dragonfly/Redis、设置 Chromium 路径并循环启动，见 [TRSS-Yunzai.sh](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/TRSS-Yunzai.sh#L1-L11) | Redis/Chromium 改独立服务；Go 启动器只做预检，不隐式修改系统状态 |
| [Main.sh](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/Main.sh#L2-L2) | 可见层会释放并执行 `Main.sh.x`；未执行或解包，不能评价内部逻辑 | 不将其内部行为写成既定事实；切换脚本需单独人工确认 |
| 分发 | 黑白名单/冷却 → 事件规范化 → Runtime → context → accept → 按优先级和规则顺序匹配，见 [loader.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/plugins/loader.js#L188-L290) | 单事件内保持顺序；规则返回 `false` 才继续；权限拒绝也有明确回复和停止语义 |
| 跨事件并发 | 消息监听器没有等待 `plugins.deal(e)`，见 [message.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/events/message.js#L6-L13) | 默认允许不同事件并发；不擅自增加全局或每会话串行队列 |
| 层级事件 | `Bot.em()` 逐级发射完整事件及父事件，见 [bot.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/bot.js#L367-L375) | 用显式事件展开器保持 `a.b.c → a.b → a` 顺序 |
| 配置 | 默认/用户配置浅合并；群配置按 default、Bot 默认、群、Bot+群覆盖；变化防抖 5 秒，见 [config.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/config/config.js#L107-L179) | `atomic.Value` 配置快照；严格保留覆盖顺序和浅合并，不改成隐式深合并 |
| 上下文 | 进程内 `stateArr`，支持超时、等待和 finish，见 [plugin.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/plugins/plugin.js#L72-L117) | 新会话使用 Redis；旧内存会话无法从 RDB 恢复，切换前排空或明确过期 |
| Redis 计数 | 典型群消息每方向 16 个串行 `INCR`，见 [loader.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/plugins/loader.js#L467-L488) | pipeline 降低往返，但不擅自变成事务；逐键对账 |
| OneBot | echo 超时 60 秒并终止 WS；初始化多处错误后仍读字段，见 [OneBotv11.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/plugins/adapter/OneBotv11.js#L17-L43) 和 [OneBotv11.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/plugins/adapter/OneBotv11.js#L1012-L1057) | typed API 结果、显式错误检查、echo 清理、有限重连退避；Cookie 请求按依赖拆分 |
| 渲染 | 每次写 HTML、新建 page、导航和截图；无页面并发上限，见 [Renderer.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/renderer/Renderer.js#L38-L81) 和 [puppeteer.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/renderers/puppeteer/lib/puppeteer.js#L159-L189) | 唯一请求目录或内存 HTML、有界队列、每请求超时、`finally` 关页、致命故障才重启浏览器 |
| 数据 | Redis 之外还有 LevelDB 和 Sequelize/SQLite；SQLite 模型含 JSON 字符串与自定义 getter/setter | 先副本读取和格式验证，再迁移；禁止直接改写唯一数据副本 |

### 1.2.1 SnowLuma 部署与 OneBot 连接事实

**部署事实：**实际协议端是 [SnowLuma](file:///D:/Toshikoshi_chan/Agent/SnowLuma) v1.14.17（win-x64 打包版），OneBot 配置为 [onebot_2406354517.json](file:///D:/Toshikoshi_chan/Agent/SnowLuma/config/onebot_2406354517.json)，WebUI 配置为 [webui.json](file:///D:/Toshikoshi_chan/Agent/SnowLuma/config/webui.json)。SnowLuma 日志最后写入 2026-09-24 12:48，Yunzai 日志最后写入 2026-08-21；**两者当前都未运行**。WSL 中其他机器人和容器与本项目无关，不纳入架构、回放或切换范围。

**配置事实（所有凭据均打码）：**

| 项目 | 当前配置与日志证据 | 对 Go 的约束 |
|---|---|---|
| 连接方式 | `networks.wsServers` 的 `ws-default` 监听 `127.0.0.1:3001/`，SnowLuma 日志也有 `listening 127.0.0.1:3001/`，见 [脱敏配置](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-13/go-rewrite-blueprint/work/snowluma-config-redacted.json) 和 [原始日志](file:///D:/Toshikoshi_chan/Agent/SnowLuma/logs/snowluma-2026-09-23.log#L42-L43) | 本蓝图把“框架主动连接协议端 WS Server”称为**正向 WS**：Go → SnowLuma `ws://127.0.0.1:3001/`。不增加其他协议或 HTTP 上报分支 |
| 鉴权 | `ws-default.accessToken` 非空，值为 `***`；日志出现 `rejected unauthorized WebSocket upgrade`，见 [脱敏连接证据](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-13/go-rewrite-blueprint/work/snowluma-connection-evidence-redacted.json) | 只实现 OneBot 标准 `Authorization: Bearer ***`；凭据从环境变量或受限 secret 注入。鉴权拒绝可离线 mock，真实成功握手必须待实例启动验证 |
| 消息与角色 | `messageFormat=array`、`role=Universal`、`reportSelfMessage=false` | canonical event 以 array 消息段为输入；事件与 API 共用 Universal WS；自消息不上报是默认行为，需专门回归 |
| HTTP | `http-default` 为 `127.0.0.1:3000/`，`enableWebSocket=false`；`httpClients=[]` | 没有 HTTP 上报客户端配置；3000 的 HTTP 服务不是本方案新增的 HTTP 上报链路，首期事件与动作只走上述 WS |
| 其他 WS 外连 | `wsClients` 中名为 Maimai 的项目连接其他本地端口，日志为其独立重连错误 | 不属于 Yunzai ↔ SnowLuma 链路，不纳入迁移或回放 |
| WebUI | 配置含密码哈希和盐，均记为 `***`；stdout 还证明历史日志可能含初始凭据 | WebUI 不是机器人数据面；其凭据和日志凭据行不得进入测试夹具或报告 |

**拓扑差异与未决验证：**旧 Yunzai 源码是在 `2536` 提供 `/OneBotv11` 的 WS Server，见 [bot.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/bot.js#L147-L186) 和 [server.yaml](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/config/default_config/server.yaml#L1-L14)，即旧协议端主动连接框架的反向 WS；当前 SnowLuma 配置则提供 3001 的 WS Server，且没有指向 2536 的 `wsClients` 项。因此，**Go 目标按最新 SnowLuma 配置采用正向 WS，2536 仅作旧行为对照，不保留为第二条长期协议分支。** 两天 SnowLuma 样本没有可用于证明 Yunzai 与 `ws-default` 成功握手的记录，不能把“配置存在”写成“联合链路已验证”。

### 1.3 本次新增日志用量证据

**历史日志事实：**目录中共有 16 个未压缩 `.log`，其中 15 个 `command.YYYY-MM-DD.log`，另一个是 [error.log](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/logs/error.log)。本次命令统计只读取这 15 个 command 文件，日期为 2025-11-05 至 2026-08-21，日期不连续；未展开 `.gz/.zst` 补足样本。

普通规则的“开始处理”是 `info`，“完成”是 `mark`，而 command appender 阈值为 `warn`，见 [log.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/config/log.js#L51-L55) 和 [loader.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/plugins/loader.js#L266-L280)。因此：

- 普通规则开始数为 0，**不能解释为零用量**。
- 有效用量代理是 **2,051 条普通规则完成日志**。
- 定时任务有 **5,845 条开始和 5,845 条完成日志**，与用户命令严格分开。
- “完成”表示规则返回非 `false` 且未抛错，不保证签到、更新、下载等外部业务实际成功。
- 原始聚合见 [log-command-summary.json](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-13/go-rewrite-blueprint/work/log-command-summary.json)，可复算脚本为 [aggregate_command_logs.py](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-13/go-rewrite-blueprint/work/aggregate_command_logs.py)。

目录级结果：

| 目录或映射组 | 普通规则完成数 | 主要方法 | 证据限制 |
|---|---:|---|---|
| `logier-plugin` | 790 | 戳一戳 600、今日运势 107、签到 43、今日老婆 35、其他 5 | 当前源码 name+fnc 唯一映射 |
| `rconsole-plugin` | 786 | bili 565、douyin 169、general 42、其他 10 | 唯一映射 |
| `earth-k-plugin` + `earth-k-plugin-tmp` | 247 | jrys 111、bzjx 57、jrlp 25、其他 54 | **歧义合计**；不得拆成两份或双计 |
| `yenai-plugin` | 47 | thumbUp 41、其他 6 | 唯一映射 |
| `xianxin-plugin` | 34 | searchCos 25、其他 9 | 唯一映射 |
| `yunzai-plugin-deer-pipe` | 29 | lu 28、leaderboard 1 | 唯一映射 |
| `FanSky_Qs` | 20 | MagicCrystalSign 16、其他 4 | 唯一映射 |
| `genshin` | 10 | bingUid 7、其他 3 | 唯一映射 |
| `system` | 7 | recall 6、status 1 | 唯一映射 |
| `liangshi-calc` | 6 | GS 6 | 唯一映射 |
| `memz-plugin` | 4 | whoAtme 4 | 唯一映射 |
| `Speech-statistics-plugin` | 4 | 三个方法 | 仅名称级映射，实施前复核 |
| `StarRail-plugin` | 4 | StarRail 2、update 2 | 唯一映射 |
| `flower-plugin` / `mora-plugin` / `phi-plugin` | 各 2 | 见聚合文件 | 唯一映射 |
| `ZZZ-Plugin` | 1 | handleRule 1 | 唯一映射 |
| 未映射显示名 | 56 | “喵喵”候选 55、“小飞插件_代发言”1 | **不归入任何目录的确证用量**；先确认历史/动态名称 |

完整目录聚合见 [usage-by-directory.json](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-13/go-rewrite-blueprint/work/usage-by-directory.json)。**样本未命中不等于无人使用。** 全量切换前仍需用实际启用清单和未来授权的只读运行指标复核。

定时任务证据只用于隔离和资源规划：

- FanSky 仓库更新 2,783 次、ZZZ 两类任务各 1,116 次、memz 仓库更新 748 次。它们是轮询任务，不是高频用户命令；仓库轮询由统一发布检查替代。
- mora 资源更新 14 次，完成耗时样本中位数约 23.2 分钟；森空岛签到 14 次，中位数约 10.3 分钟。样本小，但足以把“长任务隔离、恢复和防重”列为验收项，而不是声称实时性能结论。
- ZZZ 提醒历史日志存在多个长耗时完成记录；是否有实际重叠需在测试环境计量，不把静态风险写成既成事故。

### 1.4 本次新增依赖证据

[plugin-imports.csv](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-13/go-rewrite-blueprint/work/plugin-imports.csv) 和 [plugin-dependency-summary.json](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-13/go-rewrite-blueprint/work/plugin-dependency-summary.json) 是只读词法扫描，不是 AST 或运行时加载图。

关键事实：

- `genshin` 对 `miao-plugin` 有 15 个静态跨插件导入命中，例如 [user.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/plugins/genshin/model/user.js#L11-L12)。
- StarRail 对 genshin/miao 有多处直接导入，例如 [challenge.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/plugins/StarRail-plugin/apps/challenge.js#L4-L9)；ZZZ 也直接导入 genshin 的 User/MysApi/NoteUser，例如 [plugin.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/plugins/ZZZ-Plugin/lib/plugin.js#L1-L8)。
- ark 对 miao/genshin 有较高静态扇出，但命中含 `backup/` 路径；不能把 45 次命中当成生产模块数，实施时必须判活。
- liangshi 直接使用 miao 的 ProfileDmg、CharCfg、Common 与数据别名，证据见 [plugin-imports.csv](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-13/go-rewrite-blueprint/work/plugin-imports.csv#L2686-L2722)。
- `rconsole-plugin` 未命中跨插件 import，但自身媒体/进程依赖很重：[tools.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/plugins/rconsole-plugin/apps/tools.js#L1-L12) 同时引入网络、子进程和核心 Puppeteer，多个工具封装 FFmpeg、BBDown、yt-dlp。
- `logier-plugin` 的高频戳一戳只依赖自身配置/工具、消息段和可选 GPT，见 [Poke.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/plugins/logier-plugin/apps/Poke.js#L4-L50)；但今日运势和签到直接启动 Puppeteer，见 [Jrys.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/plugins/logier-plugin/apps/Jrys.js#L206-L221) 和 [Sign.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/plugins/logier-plugin/apps/Sign.js#L171-L188)。
- `logier` 对 `other` 的唯一静态跨插件边来自自身更新文件，不是高频功能的业务依赖，见 [plugin-imports.csv](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-13/go-rewrite-blueprint/work/plugin-imports.csv#L3839-L3839)。

**推断：**“高用量 + 浅依赖”适合先做垂直切片；“高用量 + 重外部工具”适合先做独立服务再迁移。以下批次因此不是单纯按次数降序，也不以代码行数代替依赖深度。

## 2. 目标架构

```text
SnowLuma v1.14.17（win-x64，独立进程）
        ▲ 正向 WS：127.0.0.1:3001/，Bearer ***，array
        │ OneBot v11 事件与 API
cmd/yunzai（Go 核心，WS 客户端）
  adapter → canonical event → dispatcher → static plugin registry
                                  │             ├─ 进程内业务插件
                                  │             └─ 共享 game/domain 库
                                  ├─ Session/KV/SQL repositories
                                  ├─ Scheduler（单点调度、任务幂等）
                                  ├─ Render client ───────► cmd/renderer
                                  │                           ├─ 模板/资产版本
                                  │                           ├─ 有界队列与页面生命周期
                                  │                           └─ Chromium/CDP
                                  ├─ Media client ────────► cmd/media-worker
                                  │                           └─ FFmpeg/BBDown/yt-dlp 等
                                  └─ Plugin RPC client ───► 独立 AI/Web/长任务插件
                                                               │
                                                               ▼
                                                       受权 capability API
                                                       （存储/渲染/消息动作）
```

### 2.1 核心边界

- **核心拥有：**账号、协议连接、事件顺序、权限、规则路由、会话键、任务调度、消息最终发送、配置快照、数据迁移和审计。
- **插件拥有：**业务规则、领域输入校验、业务状态转换和领域输出；不得修改核心全局对象。
- **服务拥有：**浏览器、外部下载器、FFmpeg、AI provider、长任务和独立 HTTP 端点的资源生命周期。
- **共享库拥有：**Mys/User/Profile/游戏数据/计算模型。通过显式 Go 包或服务接口复用，不通过文件路径跨插件 import。
- **最终发送只在核心。** 独立插件返回消息动作，由核心校验权限、大小和顺序后发送，避免多发送者与审计缺口。

## 3. Go 项目结构与依赖选型

### 3.1 建议仓库布局

```text
/cmd
  /yunzai                 主核心启动、信号处理、退出码
  /renderer               独立渲染服务
  /media-worker           下载/转码/媒体工具隔离
  /plugin-agent           需要独立进程的可选插件启动器
/internal
  /app                    组合根、启动/停止顺序、健康状态
  /config                 YAML、浅合并、群覆盖、快照与热重载
  /lifecycle              supervisor 协议、健康检查、优雅退出
  /adapter
    /onebotv11            唯一协议适配器；SnowLuma 正向 WS 客户端、Bearer、echo、重连
    /stdin                离线调试入口，不是线上协议分支
  /event                   canonical event、层级展开、扩展字段
  /message                Segment、回复、转发、文件与撤回
  /account                Bot/好友/群/成员、账号选择与通讯录
  /dispatch               context、accept、权限、规则匹配与短路
  /plugin                 注册表、描述符、生命周期、capability 鉴权
  /session                对话状态、TTL、恢复、finish/取消
  /schedule               六段 cron、去重、重叠策略、任务审计
  /storage
    /redis                客户端、pipeline、旧键兼容层
    /sqlite               repository、事务与迁移
    /leveldb              旧格式只读验证和迁移
    /migrate              副本迁移、校验、回滚与版本记录
  /render                 渲染客户端、超时、取消、结果类型
  /media                  媒体 worker 客户端、配额与取消
  /observability          slog、指标、审计、敏感信息过滤
/pkg
  /plugin/sdk             对插件公开的稳定 Go 契约
  /domain/mys             Mys/User/NoteUser/签名与凭据使用
  /domain/profile         面板请求、CD、数据归一化
  /domain/gamedata        版本化游戏数据与别名
  /domain/calc            伤害/计算黄金模型
  /render/api             渲染公共类型（不包含核心内部对象）
/plugins
  /builtin                system/other 的原生替代
  /logier
  /rconsole               核心薄插件；媒体逻辑调用 worker
  /yenai
  /xianxin
  /deer
  /miao
  /genshin
  /liangshi
  /...                    其余按批次加入
/api
  /plugin/v1              protobuf、动作、错误、能力、版本
  /render/v1              渲染请求/响应
  /media/v1               媒体任务/进度/结果
/assets
  /templates              按插件和版本管理的 Go 模板
  /static                 原静态浏览器资源，不盲目翻译 JS/CSS
/migrations               SQL 与数据版本迁移
/testdata
  /events                 脱敏 canonical event
  /rules                  正则正负例
  /replies                消息动作黄金样本
  /http                   外部 API 录制响应（严格脱敏）
  /render                 HTML/图片黄金样本与容差
  /storage                Redis/LevelDB/SQLite 副本夹具
/upstream                 上游锁文件、差异评审记录（实施时生成）
```

`internal` 与 `pkg` 不互相反向引用。`pkg/domain` 不依赖具体插件；`plugins` 不直接读其他插件目录。原 `Runtime`、`Handler` 变成版本化领域服务或事件服务，不能保留任意函数引用穿越 RPC。

### 3.2 依赖候选与验证门禁

以下是**建议候选**，不是本次已安装或已验证版本。实施时先在独立新仓库做小型 spike，再锁版本；不得在主实例内安装。

| 能力 | 首选 | 备选/边界 | 采用前最小验证 |
|---|---|---|---|
| HTTP | `net/http`；路由可加 `github.com/go-chi/chi/v5` | 不引入重型 Web 框架 | 中间件、鉴权、body 限额、优雅关闭 |
| WebSocket | `github.com/coder/websocket` | 自写一层薄 OneBot 客户端，不套完整机器人框架 | 连接 `127.0.0.1:3001/`、Bearer 鉴权、写超时、关闭握手、Windows |
| OneBot | 项目内薄适配器 | 只实现 SnowLuma 当前使用的 array/Universal 形态；其他框架仅作协议参考 | echo、retcode、array 消息段、`reportSelfMessage=false`、60 秒旧超时与可配置新超时、重连 |
| Redis | `github.com/redis/go-redis/v9` | — | 旧键类型/TTL、pipeline 部分失败、Dragonfly 兼容、断线重连 |
| SQLite | `database/sql` + `modernc.org/sqlite` | 若实测/部署需要 CGO，可评估 `mattn/go-sqlite3`，但会破坏纯静态交叉编译便利 | 只读打开副本、schema、JSON 字段、锁与繁忙超时、事务 |
| SQL 迁移 | 显式版本脚本；可选 `pressly/goose` | 不让 ORM 自动修改生产 schema | 前向/回滚副本、旧 Node 可读性、迁移版本表 |
| LevelDB | `github.com/syndtr/goleveldb/leveldb` | 只用于兼容/迁移，不继续当通用 Map 暴露 | Node `level` 生成副本的读写、迭代顺序、值编码 |
| CDP | `github.com/chromedp/chromedp` | 若个别 CDP 能力不对等，补原始 CDP 调用；不立即引入第二浏览器框架 | `setContent`/导航、选择器截图、多页、字体、超时、Target closed、浏览器崩溃 |
| 模板 | `html/template` + 原静态资源 | `art-template` 与 `html/template` 语法不等价，必须逐模板转换 | 三类代表模板 HTML/图片黄金对比；禁止全局机械替换 |
| YAML | `gopkg.in/yaml.v3` | — | JS YAML 与 Go 对日期、数字、null、锚点的解析差异；浅合并 |
| 文件监听 | `github.com/fsnotify/fsnotify` | — | 5 秒防抖、编辑器原子替换、关闭后无 goroutine 泄漏 |
| 定时任务 | `github.com/robfig/cron/v3` | 六段格式使用 seconds parser；旧 `?` 等写法做兼容归一化 | 全部任务 cron 正负例、Asia/Shanghai、重叠与停机策略 |
| 正则 | 新规则用 Go `regexp`；旧规则按兼容清单选 `dlclark/regexp2` | RE2 不支持 JS 后顾/回溯引用；regexp2 也不是天然 JS 完全等价 | 全规则静态分类 + Node/Go 正负语料差分 + 超时 |
| RPC | `google.golang.org/grpc` + protobuf | 首期本机 loopback；跨机时再增加 mTLS | 版本协商、deadline、取消、重连、背压、消息上限、鉴权 |
| 日志 | `log/slog`；滚动可配 `lumberjack` | 不复制全局 logger；通过 context 注入 | 日/大小轮转、错误签名限流、敏感字段过滤 |
| 指标 | `prometheus/client_golang` | 也可用 stdout JSONL，避免初期多余服务 | 低基数 label、直方图、无用户/群/完整 URL/原始键 |
| 图像 | 简单合成优先原生 Go 图像库；复杂 SVG/字体交渲染服务 | 不假设 sharp 换语言会自动加速 | 与 sharp 输出尺寸/像素/字体对账，测内存和耗时 |

## 4. Go 插件契约

### 4.1 进程内 API

以下是**提议的新接口**，不是现有代码：

```go
package sdk

import (
    "context"
    "io"
    "time"
)

type Plugin interface {
    Descriptor() Descriptor
    Register(Router) error
    Start(context.Context) error
    Stop(context.Context) error
}

type Descriptor struct {
    ID          string
    Version     string
    Priority    int           // 数值小先执行；同优先级按注册顺序
    Events      []string
    Capabilities []Capability // 最小权限声明
}

type Router interface {
    Rule(Rule, HandlerFunc) error
    Accept(AcceptFunc) error
    ContextHook(ContextHookFunc) error
    Task(Task, TaskFunc) error
}

type Rule struct {
    ID         string
    Event      string
    Pattern    string
    Engine     RegexEngine // RE2 或明确标记的 Legacy
    Permission Permission  // all/master/owner/admin
    Log        bool
}

type Result uint8
const (
    Stop Result = iota // 当前规则已处理；停止后续规则
    Next               // 对应 JS return false：继续后续规则
)

type HandlerFunc func(context.Context, *Context) (Result, error)

type AcceptDecision uint8
const (
    AcceptNextPlugin AcceptDecision = iota
    AcceptBreak                    // 停止 accept 循环，但继续规则匹配
    AcceptReturn                   // 终止整个事件
)

type Task struct {
    Name     string
    Cron     string
    Timeout  time.Duration
    Overlap  OverlapPolicy // 兼容期允许 Allow；另提供 Skip/Queue
}

type Context struct {
    Event   *Event
    Sender  Sender
    Session Sessions
    Store   KVStore       // 自动限定插件命名空间
    Render  Renderer
    Config  ConfigView
    Logger  Logger
    Clock   Clock         // 测试注入固定时间
    Random  Random        // 测试注入确定性随机数
}

type KVStore interface {
    Get(context.Context, string) ([]byte, error)
    Set(context.Context, string, []byte, time.Duration) error
    Delete(context.Context, string) error
}

type Renderer interface {
    Render(context.Context, RenderRequest) (Image, error)
}

type RenderRequest struct {
    TemplateID string
    Data       any
    AssetVersion string
    Viewport   Viewport
    Output     ImageOptions
}

type Image struct {
    MIME string
    Data io.ReadCloser
    Width, Height int
}
```

### 4.2 必须冻结的语义

1. **注册与优先级：**插件按 `priority` 升序稳定排序；规则保持声明顺序。禁止为了“优化”提前做全局前缀索引，除非差分证明 accept/context/返回语义不变。
2. **权限：**master 绕过规则权限；owner/admin 的拒绝文案和是否继续匹配以 JS 为规格。
3. **规则返回：**`Next` 只对应 JS `return false`；其他正常返回停止。错误被记录后的停止行为也要一致。
4. **accept：**分别表达“继续下一个 accept”“跳出 accept 循环继续规则”“终止事件”，不可压缩成 bool。
5. **context：**键至少包含插件、Bot、群/用户；支持超时、完成、取消和等待唤醒。Redis 只存可序列化状态，不存 JS 事件对象或函数。
6. **会话超时：**默认 120 秒及超时文案按各插件现状配置；同一键只能有一个有效代次，旧定时器不得删除新代次。
7. **任务：**以 `name+cron` 去重；统一使用 Asia/Shanghai；兼容期默认允许重叠并计量，业务确认后才改为 Skip/Queue。
8. **持久化：**旧键使用 legacy adapter 保持名称、JSON 和 TTL；新键使用 `yzgo:<plugin>:v1:<...>`。禁止插件随意写其他插件命名空间。
9. **渲染：**请求必须带模板和资产版本；返回图片或 typed error，不返回隐式 `false`/`""` 混合类型。
10. **外部副作用：**消息、Redis、DB、HTTP、进程调用都进入可记录 trace；测试用 fake capability，不需要线上协议端。

### 4.3 进程内与独立服务插件的边界

| 形态 | 适用场景 | 本项目的建议对象 | 不适合/禁止 |
|---|---|---|---|
| 进程内静态插件 | 规则明确、低延迟、共享 Go 领域库、故障可单测、无需独立资源生命周期 | logier、deer、xianxin、yenai 的功能切片；miao/genshin/liangshi；多数游戏命令 | 不可信脚本、无界子进程、自有 Chromium、长时下载、崩溃需独立恢复 |
| 独立服务插件 | 媒体工具、AI provider、独立 HTTP 服务、长任务、需要独立发布/限资源/隔离崩溃 | rconsole 媒体 worker；Speech/Guoba Web；arknights/mora/waves 长任务；agent/nai/y-tian；必要时 pixiv/San | 高频小调用不宜无证据拆分；不能传 JS 函数、对象引用、Prototype 或全局单例 |
| 独立基础服务 | 多插件共享且有明确资源边界 | renderer、media-worker | 不允许各插件再私自启动第二个 Chromium/FFmpeg 管理器 |

独立插件 RPC 的强制约束：

- protobuf 只传 canonical event、值对象和消息动作；跨进程共享能力通过受权 `Capability` API，不伪造对象引用。
- 启动时协商 `protocol_version/plugin_version/capabilities`；不兼容不得接流量。
- 每个请求带 `event_id/rule_id/deadline/cancellation`；大文件用受限临时文件引用或流式传输，不塞进 JSON。
- 核心调度器调用 `RunTask`，避免核心与 worker 各自 cron 造成双跑。
- 插件回复以动作流返回，核心执行；重试必须携带幂等键。**发生过部分回复后默认不自动重放**，除非该动作支持去重。
- worker 崩溃只影响其故障域；核心按规则返回既有错误文案或明确的新错误，不静默吞掉。

## 5. 迁移顺序

### 5.1 排序规则和并行轨道

排序键依次为：

1. **门槛依赖**：协议/事件/会话/存储/渲染是否已具备。
2. **普通规则完成数**：优先高用量，但歧义数不拆、未映射数不冒领。
3. **依赖深度**：无跨插件业务依赖、无 Chromium、无子进程的切片先行。
4. **共享扇出**：miao/genshin/liangshi 提前建设，供后续批次复用。
5. **故障域**：媒体、长任务、Web、AI 先建立服务边界再迁业务。

允许三条轨道并行：

- **核心轨道**：B0 → B1/B2 → B3/B4 → 全量切换。
- **共享领域轨道**：B0 契约冻结后立即开始 B5，必须在 B6/B7 完整游戏功能前完成。
- **独立服务轨道**：renderer、media-worker、长任务 worker 与对应插件并行，但插件只有在服务验收后才能启用。

每批都先迁移表中观察到的切片；同目录其他**实际启用**功能在该批尾段迁移，表中明确分配到后续批次的切片除外。未启用功能可以记录退役，但必须有配置/人工依据，不能因日志未命中就删除。

### 5.2 分批名单与前置依赖

| 批次 | 名单与顺序 | 用量/依赖证据 | 前置依赖与原生形态 |
|---|---|---|---|
| **B0 核心** | adapter（仅 OneBot/SnowLuma 正向 WS）→ system 注册骨架 → other 核心生命周期；example 仅测试 | OneBot 历史故障和核心路径；更新机制是框架能力 | canonical event、账号、配置、权限、Redis/会话、调度、渲染/媒体客户端。system 业务规则归 B1；other 的 miao 版本展示后置 B5 |
| **B1 首批浅切片** | **logier 戳一戳** → system recall/status | 戳一戳 600，为单一最高规则；system 完成 7；[Poke.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/plugins/logier-plugin/apps/Poke.js#L27-L50) 无 Redis/Chromium | B0；固定 RNG、配置、图片下载与 GPT fake；进程内 |
| **B2 高频状态/图像切片** | logier 今日运势 → 签到 → 今日老婆 → 其余已观测 → deer | logier 其余已映射 190；deer 29。logier 直接 Puppeteer；deer 用 Redis 与 sharp 合成，见 [core.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/plugins/yunzai-plugin-deer-pipe/utils/core.js#L50-L149) | B1 + Redis 旧键 + 固定时钟 + 独立 renderer/图像对等；进程内业务 |
| **B3 高频媒体** | rconsole **bili → douyin → general → 其他已观测 → 其余启用功能** | 总计 786；bili 565、douyin 169。无跨插件 import 命中，但 [tools.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/plugins/rconsole-plugin/apps/tools.js#L1-L12) 有子进程/渲染/网络 | 独立 media-worker、工具版本清单、下载/转码配额、临时文件回收；核心薄插件 |
| **B4 中量浅领域切片** | yenai thumbUp 等 → xianxin mys 等 → memz whoAtme 等 | 47、34、4；yenai 有点赞规则与额外事件入口，[thumbUp.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/plugins/yenai-plugin/apps/thumbUp.js#L5-L18)；xianxin 有群转发/私聊差异 | B0/B2；账号好友、转发、HTTP fake。memz 的仓库轮询先由统一发布检查替代 |
| **B5 共享领域底座** | miao 共享库 → genshin Mys/User/DB → liangshi 计算 → 三者已观测命令 | miao 的 55 条仅为未确认候选；genshin 10、liangshi 6。真实 import 扇出见第 1.4 节 | B0；冻结中性 domain API、SQLite 副本、游戏数据版本、渲染；不得让各插件重复实现 Mys/Profile |
| **B6 高量歧义与 FanSky 非 Teyvat** | earth-k 已观测功能 → FanSky 打卡等非 Teyvat 功能 → earth 其余启用功能 | earth 两目录歧义合计 247；FanSky 20 + 仓库轮询 2,783 | **先由操作员选定 earth 唯一来源**；完整 earth 业务需要 B5；FanSky Teyvat 明确归 B7；发布检查与业务任务分离 |
| **B7 游戏/资源套件** | FanSky Teyvat → StarRail/ZZZ → ark/liangshi 使用方 → flower/mora → phi | StarRail 4、ZZZ 1、flower/mora/phi 各 2；ark 样本未命中但共享扇出高。ZZZ 高频任务、mora 长任务单列证据 | B5/B6；游戏数据、共享 Profile、独立提醒/资源 worker；ark 的 backup 和缺失可选依赖先判活 |
| **B8 长任务与独立 Web** | arknights 签到 → Atlas 资源 → waves 签到/任务 → Speech → Guoba | arknights/Atlas 任务各 14，waves 两类各 6；Speech 4；Guoba 样本未命中。Express/Puppeteer/sharp 证据见依赖统计 | 独立 worker、任务恢复、HTTP 鉴权/上传限额、统计口径；Guoba 是否阻断全量切换由操作员确认 |
| **B9 长尾与高风险能力** | agent、mc、nai、pixiv、San、TRSS/Yunzai.TRSS.me、xitian、y-tian | 样本无普通命令命中；子进程、动态脚本、AI、Web/代理等词法依赖较重 | 只迁实际启用功能；独立沙箱/AI 网关；TRSS 两目录先选唯一来源；高危远程命令逐项白名单 |
| **不迁移** | `bin`、`plugins` 两个空目录；未被选定的重复目录 | 入口与源码统计为 0；重复目录统计相同或近乎相同 | 不擅自删除；在迁移清单中标记 retired/frozen |

逐目录执行表见 [migration-batches.csv](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-13/go-rewrite-blueprint/work/migration-batches.csv)。

## 6. 行为对等：历史日志回放 + 离线重放

### 6.1 样本来源、缺失字段与判定口径

建立 `contract-replay`，以 SnowLuma 与 Yunzai 的历史日志为**用例来源之一**，再由 JS 源码规格补齐字段和预期。日志不是完整 OneBot 报文数据库，不能单独承担严格行为证明。

#### 6.1.1 两类日志能提供的证据

| 来源 | 可提取内容 | 不能据此声称 |
|---|---|---|
| SnowLuma `snowluma-2026-09-23.log`、`snowluma-2026-09-24.log` | 共 149,437 行；人类可读事件包括群消息 12,012、群撤回 107、入群 12、群戳 165、表情回应 18、精华 3、退群 6、群邀请 1、login 1；图片标记 3,328、表情 206、视频 31、语音 37；`send_msg` 31 次提及；未提取到可解析的完整 OneBot JSON 对象；WS 监听、鉴权拒绝、socket reset 和会话关闭，见 [日志结构聚合](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-13/go-rewrite-blueprint/work/snowluma-log-summary.json) | 不能把人类可读摘要当作完整 OneBot JSON；不能把 Maimai 外连错误当作 Yunzai 链路；不能证明当前实例仍在运行 |
| SnowLuma `Bridge.Action` | 可提取 action 名、`message_type`、群目标及 message 摘要；31 个 `send_msg` 动作摘要行均未记录 echo、响应 message_id 或 retcode（仅指日志未提供，不推断协议实际无响应），见 [动作结构](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-13/go-rewrite-blueprint/work/snowluma-action-structures.json) | 不能可靠还原完整回复内容、API 响应或请求—响应因果关系 |
| Yunzai 15 个 command 日志与 error.log | 插件显示名/方法、完成与耗时、定时任务/cron、部分渲染模板和错误栈；普通规则完成 2,051、任务开始/完成各 5,845 | command appender 为 warn，普通消息与规则开始的 info 不完整；不能把完成日志当作原始输入或外部业务成功 |
| 两侧联合 | 可提供协议事件形态、插件规则名称、动作类型和故障类别 | **日期不重叠**：SnowLuma 为 2026-09-23/24，Yunzai 最新为 2026-08-21；又没有共同 echo/message_id，不能按“时间接近”拼成同一次消息往返 |

原始日志可能包含消息正文、账号/群标识、Cookie、token 或 WebUI 初始凭据。提取器必须在本地完成脱敏，报告和公开夹具只保留结构、计数和伪名；不得把原日志直接复制进测试仓库。

#### 6.1.2 字段覆盖与缺失处理

| 字段/行为 | 日志覆盖 | 重放处理 |
|---|---|---|
| 文件日期、时分秒、级别、事件大类 | 可直接提取 | 保留相对时间；需要绝对时间时用固定测试时钟 |
| 群/用户/消息 ID、昵称和群名 | 部分人类可读行存在 | 用数据集内稳定伪名，如 `group-0001/user-0001`；伪名映射不进入报告或公开夹具 |
| 文本命令 | 部分 Event 摘要可见，但不保证完整或统一编码 | 仅在本地用 JS 规则识别后保留命令字面量和必要参数；其余正文替换为 `<redacted-text>`，用作负例 |
| 图片/视频/语音/表情 | 只有类型标记，缺少原始 URL、文件、MD5、尺寸和完整 mixed-segment 顺序 | 转成 `fixture://image/...` 等受控资源；缺少顺序时标 `partial`，不得声称完整段数组对等 |
| notice（撤回、戳一戳、入群等） | 有人类可读类别，缺少统一 `post_type/notice_type/sub_type` 原文 | 按 OneBot 与现有适配器源码人工映射；所有映射字段标 `synthetic_fields`，并由另一条完整合成用例验证 |
| 私聊、自消息 | 本批 SnowLuma 样本未见私聊类别；配置又默认 `reportSelfMessage=false` | 不能从该样本归纳；按 JS 规格补造私聊完整用例，自消息分别测默认不上报和显式改变配置后的行为 |
| `send_msg` 响应、echo、retcode、响应 message_id | 样本缺失 | 由 mock SnowLuma 按请求 echo 生成合成响应；标记为合成，不冒充历史响应 |
| Redis/会话初始状态、外部 HTTP 响应、完整模板数据 | 两类日志均不能完整提供 | 从插件规格构造初始状态与 fake 响应；关键状态迁移另用数据副本夹具 |

#### 6.1.3 用例集构造流程

1. **本地提取：**逐行读取 SnowLuma 与 Yunzai 日志，先剔除/打码凭据，再提取事件类别、动作、规则、任务和错误签名。原日志只读，不修改。
2. **规则识别：**在本地用当前 JS 规则匹配文本，保留能改变分发结果的命令前缀和参数；URL 改用保留域名或 fixture ID，外部响应使用录制后脱敏的 fake。
3. **伪名化：**账号、群、用户、消息 ID 使用稳定伪名；昵称/群名替换为角色占位符。不得只使用可被字典反查的无盐 QQ 哈希作为“脱敏”。
4. **补 canonical event：**按现有 OneBotv11 源码补齐 `post_type/message_type/sub_type`、array 段、sender 和 Bot 字段；每个补出的字段记入 `synthetic_fields`，无法确定的值记 `missing_fields`，不得留隐形默认值。
5. **生成 manifest：**每条用例记录 `source_file/source_line/content_hash`、`coverage`、`redaction`、输入事件、预期动作、缺失/合成字段。敏感原文和伪名映射不写入 manifest。
6. **建立三层参考：**静态规格层提取规则/权限/消息段/Redis/任务；受控 JS 参考层只运行副作用审查过的叶子模块并注入 fake；Go 层重放同一 fixture。无法安全导入的 JS 模块标 `reference=static-spec`，不伪装成双实现已执行。

示例 manifest（全部为合成值）：

```json
{
  "case_id": "logier/poke/self/no-gpt",
  "source": {"kind": "snowluma-event+js-spec", "content_hash": "<hash>"},
  "coverage": "partial",
  "synthetic_fields": ["post_type", "notice_type", "target_id"],
  "event": {"type": "notice.group.poke", "target_id": "bot-self", "user_id": "user-0001"},
  "expected": {
    "matched": ["logier.poke"],
    "replies": [{"segments": [{"type": "image", "source": "fixture://emoji/1"}]}],
    "redis": [],
    "errors": []
  }
}
```

#### 6.1.4 如何判定“对等”

| 级别 | 适用用例 | 通过条件 | 不能证明的内容 |
|---|---|---|---|
| **严格对等** | 字段完整的历史用例或由源码补齐并经评审的合成用例 | 规则命中、优先级/短路、回复顺序、段类型与内容、动作参数、错误类别、Redis/DB 副作用全部符合预期；额外回复或额外写入即失败 | 不证明真实 SnowLuma 握手或真实外部服务当前可用 |
| **结构对等** | 只有人类可读摘要的 `partial` 历史用例 | 只比较日志实际覆盖的事件类别、目标类型、可见段类型、插件方法和错误类别；缺失字段不得参与“猜中即通过” | 不证明完整段顺序、原始文本、响应 message_id 或端到端因果一致 |
| **聚合对等** | 规则次数、任务次数、渲染样本和故障签名 | 在相同筛选口径下对账；耗时只作基线参考，不要求跨日期的绝对值相等 | 不用合计次数证明单条行为正确，也不把历史性能当作当前实测 |

所有关键迁移规则至少各有一条**严格对等**合成用例；只有 `partial` 历史证据的规则不能直接获得 M4/M5 的业务对等门禁。历史回放失败按“输入提取错误 / 合成字段错误 / Go 行为差异 / JS 规格未决”分类，不能通过修改预期来消除差异。

#### 6.1.5 纯离线协议重放

实现一个 mock SnowLuma WS Server：监听测试临时端口，要求 `Authorization: Bearer ***`，使用 array/Universal，并模拟 `reportSelfMessage=false`。Go 以正向 WS 客户端连接，重放 canonical event；mock 按请求 echo 返回合成 retcode/message_id，同时注入鉴权失败、超时、reset、乱序响应和断连。该过程不需要活体 SnowLuma/Yunzai，但需要在以后获准的实施环境运行测试进程；本次没有启动它。

### 6.2 每批对等检查矩阵

每批除表列用例外，所有规则默认覆盖：正常输入、权限允许/拒绝、空输入、畸形输入、外部超时、依赖返回空值、重复事件。无权限或不调用外部服务的规则标 N/A，不伪造测试。

| 批次 | 输入用例 | 输出/副作用比对 | 边界错误与特殊门禁 |
|---|---|---|---|
| **B0** | SnowLuma 历史事件 + 合成群/私聊文本、at/别名、图片/回复/文件段、notice；正确/错误 Bearer；array；两个同优先级插件；规则 Next；accept 三种返回；context 超时；master/admin/owner | canonical event 字段、父子事件顺序、规则顺序、OneBot action JSON、合成 echo/message_id、回复 quote/at/recall、Redis 计数 | 正向 WS 握手/鉴权/reset 用 mock 验证；`reportSelfMessage=false`；未知消息段保留扩展；断连清理 echo map；真实 SnowLuma 握手不在离线判据内 |
| **B1** | Poke：target=self/他人；随机阈值两侧；无 GPT key；GPT 成功/失败；表情类别黑名单；空图片。system：合法/非法撤回、权限拒绝 | 图片或 GPT 文本的段类型、来源和顺序；戳他人时不回复；system 动作与拒绝文案 | null 图片、GPT 非 JSON/超时、配置缺项。JS 中未等待的异步回复是否改为等待需登记为“保留”或“有意变更” |
| **B2** | 固定日期首次/同日签到、悔签、跨日、跨月、并列排名；deer 补签/戒签、未来日/0 日、次数 >99；固定 RNG | Redis JSON/键/TTL、群排名、文本 fallback、图片 MIME/尺寸/感知差异、quote/at | 损坏 JSON、远程图片/一言失败、渲染失败、旧键存在。deer 图像标题、日历周起点和 `x99+` 必须对账 |
| **B3** | BV/b23/各 B 站 URL、抖音短链/视频/图集、general 支持域；录制 HTTP 200/403/404/429/5xx；媒体大小阈值 | 文本/图片/视频/文件/转发顺序、重试次数、代理选择、临时文件、最终错误回复 | 非法 URL、FFmpeg 非零退出、部分下载、超时取消、磁盘不足、工具缺失；测试后无残留进程和文件 |
| **B4** | thumbUp 自己/at、好友/陌生人开关、API code 0/1/51/其他、`#全部赞我`；xianxin 私聊/群聊、索引越界、空图片；memz 已观测规则 | 点赞循环次数和文案、at/图片段；xianxin 私聊逐图+600ms 与群合并转发；memz 动作轨迹 | 额外 `Bot.on` 入口不得与规则双执行；空成员列表、API 异常、转发为空、 strangers 配置缺失 |
| **B5** | UID/CK 绑定、无 CK、跨游戏、资料空/错误页；Sequelize JSON 字段；Profile CD；伤害计算固定向量 | Mys/User repository、Redis TTL、SQLite 行、API 签名、Profile 归一化、模板数据、计算数值 | UID 用字符串防精度变化；HTML 错误页、非法 JSON、数据库 busy、旧 getter/setter 差异；共享 API 的所有调用方一起回归 |
| **B6** | earth 的 jrys/bzjx/jrlp 等固定用例；FanSky 首次/重复打卡、清除状态；仓库有/无新提交 | 消息/媒体/Redis/转发；FanSky 打卡状态和推送去重 | 同一 earth 来源只运行一次；重复目录不得双回复；发布检查失败不能影响业务命令 |
| **B7** | 各游戏已观测规则、资料为空/展柜关闭、提醒到点、资源已最新/有新版、任务执行中超时 | 游戏资料文本/图片、计算/面板、提醒接收者、资源版本与下载动作 | 可选依赖缺失、跨游戏 UID 混用、任务重入、部分账号失败；提醒和资源任务必须幂等且可恢复 |
| **B8** | 签到部分账号失败、上传合法/超限、统计空数据/跨日、Web 未鉴权、资源下载中断 | HTTP status/schema、统计图与数据、签到部分成功结果、任务续跑点 | worker 崩溃不拖垮核心；无重复签到/统计；上传内容类型、鉴权、路径穿越和大小限制；Guoba 配置 schema 与旧值兼容 |
| **B9** | 每个实际启用规则的正常/权限/边界/外部错误；AI provider mock；动态脚本白名单；TRSS 选定目录用例 | 消息动作、provider 请求/响应、脚本输出限额、服务健康状态 | 非白名单命令拒绝、进程超时/取消、secret 不进入日志；未选定重复目录不执行；未启用功能有退役记录 |

### 6.3 专项差分

- **正则：**从全部启用规则提取 pattern/flags，静态标记后顾、回溯引用、`g` 状态等；为每条规则至少准备命中、近 miss、空串、Unicode、多行和长输入。Node 结果与 Go RE2/regexp2 对比，任何差异必须修复或登记。
- **配置：**同一 YAML fixture 比较 JS/Go 解析值、浅合并和群覆盖顺序；重点测前导零 ID、超大整数、日期样字符串、null、数组和锚点。
- **Redis：**在隔离副本上记录命令级 trace。计数总值、键名和 TTL 对账；pipeline 不掩饰部分失败。会话测试覆盖进程重启后恢复、超时、finish 和旧定时器代次。
- **SQLite/LevelDB：**只读复制夹具，验证 schema、迭代、值编码和旧 Node 回读。任何迁移先写副本并输出逐表/逐键差异，唯一原始数据不动。
- **渲染：**先比规范化 HTML，再比尺寸、格式与感知差异。像素容差由代表模板基线决定并经人工确认，不能随意设一个“看起来差不多”的统一阈值。
- **任务：**使用 fake clock 触发 cron，验证六段表达式、去重、时区、重叠策略、异常、跳过时区和恢复；不让测试等待真实分钟/小时。

## 7. 里程碑与完成判据

### 7.1 无活体实例的验证分级

- **A｜当前只读即可完成：**配置/源码分析、历史日志提取与脱敏、回放 manifest、缺失字段清单、静态规格、迁移和 owner 路由设计。本次只做了这一级，没有启动任何服务。
- **B｜实施期隔离环境可完成，不需要活体 SnowLuma/Yunzai：**Go 单测、mock SnowLuma 正向 WS、JS/Go 差分、隔离 Redis、renderer/Chromium、media-worker 和故障注入。它们仍需以后明确授权运行相应测试进程，不能写成已经执行。
- **C｜只能等实际实例或服务显式启动后完成：**真实 SnowLuma 鉴权握手与消息往返、真实配置生效情况、生产 Redis/会话切换、真实外部 API、灰度 owner 转移、实际故障率及真实回滚演练。没有活体实例时只能准备用例和 mock 演练，不得宣布这些项目通过。

### 7.2 里程碑表

| 里程碑 | 交付内容 | 离线完成判据（A/B） | 只能待实例启动或额外授权的项目（C） |
|---|---|---|---|
| **M0 契约与黄金框架** | canonical event、消息动作、日志 manifest、规则/权限/配置/数据规格；`contract-replay` | B0/B1 用例可重复；历史样本均有 coverage/missing/synthetic 标记；每个差异有“保留/有意改变/未解决”状态 | 无；不需要活体实例 |
| **M1 单协议端连接并跑通一条消息往返** | OneBot v11 正向 WS 客户端 + mock SnowLuma + 最小 echo 插件 | mock 群消息只产生一次 `send_msg`；Bearer、array、合成 echo/message_id、超时、reset、重连和 echo 清理全部通过 | **[需 SnowLuma 实例]** 连接真实 `127.0.0.1:3001/`，验证真实 token、角色、当前配置、真实消息往返及 2536 旧链路的停用方式 |
| **M2 会话与 Redis 迁移完成** | go-redis、旧键 adapter、pipeline、Redis SessionStore、副本校验 | 在隔离 Redis/数据副本上，计数、旧键 JSON/TTL、会话恢复、finish、代次和回滚读取全部通过 | **[需实际 Redis/旧实例]** 生产数据切换、旧进程活动会话排空、真实配置连接和恢复后状态核对 |
| **M3 渲染服务独立可用** | `cmd/renderer`、Go 客户端、模板/资产版本、CDP 生命周期 | **[需启动隔离 renderer/Chromium，不需要机器人]** 三类代表模板、并发隔离、页面有界、超时、崩溃恢复和无遗留进程通过 | 实际部署路径、字体和现有资源在目标机器上的最终复核；未获启动授权前只有静态模板与 golden HTML |
| **M4 首批插件可用** | B1 logier 戳一戳 + system 目标规则；随后 B2 首个状态/图像切片 | B1 严格对等用例全通过；partial 历史行只做结构对等；命令一次命中、GPT/图片故障 fallback、单独关闭开关通过 | **[需实际实例灰度]** 真实群/用户中的插件启停、回复接收、owner 转移和旧实现回退 |
| **M5 高频与共享底座可用** | B2-B5；media-worker；miao/genshin/liangshi 共享库 | B2-B5 严格矩阵通过；媒体子进程有界可取消；共享调用方同步回归；数据副本差异清零或有签字豁免 | **[需真实 provider/实例]** 外部站点当前签名/API、真实媒体发送、真实账号游戏数据及端到端性能 |
| **M6 全量切换并具备回滚** | B6-B9 实际启用范围、灰度、旧版本保留、回滚手册与演练 | 离线只能完成清单 100%、critical diff 清零、mock owner 转移和 mock 回滚；**不能宣布 M6 完成** | **[需 SnowLuma + 新旧框架 + Redis 显式启动]** 单账号/群灰度、无双回复、真实任务切换、实际数据恢复、旧发送者与任务回滚全部通过 |

M6 的“全量”指**经清单确认需要保留的功能**，不是把空目录、重复副本和明确退役能力也机械翻译一遍。无活体实例时，所有 C 项保持 `blocked-not-run`，不得以 mock 结果替代。

## 8. 上游分叉策略

### 8.1 固定流程

1. 每个来源记录 `upstream.lock`：URL、commit、许可证、原生对应版本、启用功能、黄金样本版本、最后移植差异。
2. 上游检查只生成报告，不直接改工作树，不执行强制 reset，不自动安装依赖。
3. 将差异分为：游戏/资源数据、外部 API 适配、行为修复、新功能、JS 宿主特有风险。
4. 数据可由版本化资产管道更新；行为修复/API 变化进入原生移植；新功能需产品验收；JS 宿主特有风险不盲目移植。
5. 移植后运行该模块及全部调用方的黄金测试，再更新锁文件。
6. 两个重复目录未选定前，不建立双上游，不猜测同步关系。

### 8.2 分类建议

| 策略 | 目录 | 原因 |
|---|---|---|
| **原生分叉，选择性跟踪上游差异** | logier、rconsole、yenai、xianxin、earth 选定源、miao、genshin、liangshi、FanSky、ark、StarRail、ZZZ、flower、mora、phi、arknights、waves | 有高用量/定时证据、共享扇出或外部站点/游戏 API 变化；需要持续移植业务修复 |
| **按本地契约直接重写替代，不承诺逐行跟随** | adapter、system、other、deer、memz、Speech、Guoba、agent、mc、nai、pixiv、San、TRSS 选定源、xitian、y-tian | 核心协议/管理面/服务边界应重新设计，或样本未命中且高风险能力多；只实现本地实际启用契约 |
| **资源管道优先，代码按需** | Atlas，以及各游戏插件的静态游戏数据/图片 | 大体积资源不应和 Go 代码版本强耦合；用清单、哈希和增量更新 |
| **冻结/不迁移** | bin、plugins、earth 未选源、TRSS/Yunzai.TRSS.me 未选源、明确停用的旧功能 | 空目录或重复来源；保留原目录供回滚，不擅自删除 |

**建议：**即使选择“原生分叉”，也不保留运行时自动更新。上游修复通过上述差异流程进入；否则一次上游 `git pull` 就可能让 JS 规格和 Go 实现失去可比较基线。

## 9. 灰度、数据切换与回滚预案

### 9.1 切换步骤

**执行前置：**以下真实切换与回滚步骤均属于第 7.1 节 C 类，必须另行获得启动 SnowLuma、旧 Yunzai、Go 和 Redis 的明确授权。无活体实例时只能准备 owner 路由表并用 mock 演练，不执行生产操作。

**所有权模型：**消息以 `adapter + self_id + group_id/user_id` 确定 owner，定时任务以 `task_id` 确定 owner；路由表把每个键明确分给 `node` 或 `go`。同一事件只能进入 owner 侧业务插件，非 owner 侧不得以主动 listener 再消费。非灰度作用域仍由旧 Node 服务，灰度作用域由 Go 服务，因而可以按账号/群并行灰度而不产生双回复。

1. 冻结新旧版本和配置，生成启用规则/任务/路由清单及上述 owner 路由表；停止差分期间的双侧自动更新。
2. 所有作用域的初始 owner 均为旧 Node；Go 以 fixture 或脱敏录制事件做 shadow，只记录“将命中/将回复/将写入”，不发送、不写生产、不执行定时业务副作用。
3. 对账规则未命中、不同命中、回复差异、外部调用差异和数据差异；critical 未解决不得灰度。
4. 数据采用副本迁移、只读校验、必要的双写对账或可重放 outbox。迁移窗口只做过加字段/新键等旧实现可忽略的变化。
5. 选定单测试账号/群：停止该作用域旧事件入口与任务 → 确认 in-flight 已排空 → 原子切换 owner 到 Go → 启动该作用域 Go 事件入口与发送 → 验证后逐作用域、逐插件扩大。任何时刻同一 owner 键只有一个发送者。
6. 每批保留旧二进制、配置、数据快照和回退开关；任务切换也要按 `task_id` 防双跑。

### 9.2 立即回滚触发器

- 任意关键命令无回复、双回复、错误命中或权限绕过。
- 数据出现无法解释的差异、旧实现无法回读，或迁移写入不可逆。
- 协议稳态重连超过既有基线报告的严重阈值（20/h），或同类初始化 TypeError 的 Go 等价错误持续出现。
- 渲染失败率超过 1%、活动页面/媒体进程失控、worker 崩溃影响核心。
- 定时任务重复执行、跨批次双跑或无法停止。

### 9.3 回滚动作

1. 关闭回滚作用域的 Go 插件和任务，再停 Go 发送者；确认没有 in-flight 回复/任务。
2. 停止新建 Go 会话；排空可完成的对话，或将其余会话代次明确作废并取消对应超时任务，记录“有意过期、旧 Node 不恢复这些对话”。确保旧 Go 定时器之后不能写入或回复。
3. 导出或暂停 outbox；对新写入做逐键/逐表对账。旧实现不认识的写入先重放为旧格式，否则不得启动旧写入者。
4. 恢复旧配置和旧 Node 进程，确认 Redis/文件/DB 路径指向预期副本；原子把 owner 路由切回 `node`。
5. 恢复该作用域旧发送者和任务，执行一条只读命令与一条受控回复验证。
6. 保存脱敏差异、版本、会话处置和触发原因；未定位前不再次扩大灰度。

## 10. 会推翻方案的关键假设与最低成本验证

| 假设/失效条件 | 为什么可能推翻当前设计或排序 | 最小成本验证（在获准的隔离环境） | 失败后的调整 |
|---|---|---|---|
| 历史日志能代表当前主要功能 | Yunzai 样本不连续且止于 2026-08-21；SnowLuma 仅有 2026-09-23/24，两侧又不重叠；若当前热点不同，B1-B4 排序失效 | 先用现有日志完成结构回放；未来授权后采集至少 7 天结构化 rule 名称和次数，不采集正文/ID | 按新证据重排，不改核心契约 |
| SnowLuma 3001 正向 WS 就是实际联合链路 | 当前配置与旧 Yunzai 2536 反向 WS 不同，且日志无成功 Yunzai 握手证据；若拓扑理解错误，真实切换会失败 | 离线已完成配置/日志核对；真实 token、握手和消息往返必须等实例显式启动后验证 | 未验证前冻结真实切换；由操作员确认或修正 SnowLuma 配置，仍只保留 OneBot，不新增其他协议分支 |
| 高频 logier 切片没有隐藏全局/共享依赖 | 若 Poke 间接依赖运行时副作用，首批不再浅 | 对叶子 import 做闭包审查；用 fake 运行最小参考 harness | 把缺失能力抽成显式服务，或后移切片 |
| JS 正则可被 RE2/regexp2 覆盖 | 若大量规则依赖不兼容语义，分发行为和性能都失去保证 | 全规则静态分类 + 正负语料差分 + 长输入超时 | 为少量规则写解析器/手工匹配；仍不行则该插件独立使用兼容正则服务并重新评估 |
| miao/genshin 可拆成无循环领域库 | 若共享对象和双向函数引用无法分离，B5 之后所有批次都会返工 | 先实现 NoteUser/MysApi/Profile 三条窄接口及调用方测试 | 临时合并为一个 domain 模块，再逐步拆；禁止复制实现 |
| LevelDB/SQLite 数据能被 Go 正确读取且旧 Node 可回滚 | 若不兼容，全量切换不能安全进行 | 对最小副本做只读迭代、写入副本、旧 Node 回读和哈希/行级 diff | 改为导出/导入迁移；保留只读旧库，不直接原地转换 |
| Redis 会话能提供所需恢复语义 | 旧内存上下文无法直接迁；多实例还可能出现代次竞争 | 两个 Go 实例 + 隔离 Redis 的 TTL/CAS/重启测试 | 单实例会话所有权或显式 sticky routing；不强求无状态多实例 |
| chromedp 与转换模板能达到业务可接受对等 | 若 CDP/字体/模板差异过大，大量图片插件不能通过 | 三类代表模板做 HTML/图片差分和浏览器故障注入 | 修模板/资源；必要时仅渲染驱动保留独立替代实现，仍不恢复 JS 插件宿主 |
| 媒体 worker 能覆盖现有下载/转码行为 | 站点签名、工具版本和临时文件语义可能使 rconsole 无法对等 | bili/douyin 各选最小录制响应与工具 fixture，跑超时/取消/非零退出 | 按站点拆 provider；保留受控外部工具进程，不重写所有协议算法 |
| 独立插件 IPC 不引入不可接受延迟/复杂度 | 若小命令频繁跨进程，独立化可能比单进程更差 | 用 10,000 个本地 fixture 测进程内与 RPC 的 P50/P95、序列化和失败恢复 | 小型纯 Go 功能回到进程内；只保留资源边界明确的 worker |
| 缺失/重复/backup 路径可安全判活 | 错选 earth/TRSS 来源或把 backup 当生产，会双回复或迁错规格 | 操作员确认启用清单；静态入口闭包；未来只读加载清单核对 | 冻结批次，不猜测；未确认前不上线对应功能 |
| 外部 API 的签名、Cookie、错误语义可重实现 | 若关键 provider 无稳定契约，对应插件可能无法原生化 | 用脱敏录制请求/响应做契约测试；人工核对鉴权流程 | 将该 provider 独立为受控适配服务，或明确退役该能力 |
| 操作员接受“原生分叉/替代”而非自动同步 JS 上游 | 若后来又要求无损自动跟随全部 JS 上游，本产品前提被推翻 | 在 M0 签署上游策略和启用功能清单 | 重新评估双运行时；不得偷偷保留一个不受控 JS 宿主 |
| Windows/目标 Linux 都能部署所选依赖 | CGO、浏览器路径、文件锁或进程模型可能破坏单二进制假设 | 在新仓库做一次跨平台 spike 和最小集成测试，不触碰主实例 | 换纯 Go 依赖或按平台拆分构建/服务 |

## 11. 实施入口清单

核心开工顺序如下；第 5.1 节允许的并行轨道可提前启动，但未通过对应门禁的插件不得提前启用：

1. 建立独立 Go 新仓库和 `contract-replay`，不在主实例内改代码。
2. 用 SnowLuma/Yunzai 历史日志生成脱敏 manifest，再冻结 B0/B1 的 canonical event、Segment、动作 trace、配置和权限规格。
3. 用符合当前配置的 mock SnowLuma 正向 WS/Bearer/array 完成 M1 离线验收；随后完成 M2，再并行推进 M3，真实握手保持 `blocked-not-run`。
4. 以 logier Poke 作为首个真实业务切片，验证“规则命中—回复—故障—关闭”完整链路。
5. 继续推进并验收 B0 契约冻结后已启动的共享领域和 media-worker 轨道；每批只依赖已冻结接口。
6. 请操作员尽早决定：earth/TRSS 重复来源、Guoba 是否阻断切换、未命中功能哪些必须保留、渲染图片容差、旧会话排空策略。
7. 每批生成证据包： fixtures、预期/实际 trace、数据 diff、故障注入、启用清单、回滚结果。没有证据包不进入下一批。

---

## 附录 A：38 个目录去向核对

| 分类 | 目录 |
|---|---|
| B0 | adapter、system、other、example（仅测试） |
| B1/B2 | logier-plugin、yunzai-plugin-deer-pipe |
| B3 | rconsole-plugin |
| B4 | yenai-plugin、xianxin-plugin、memz-plugin |
| B5 | miao-plugin、genshin、liangshi-calc |
| B6 | earth-k-plugin、earth-k-plugin-tmp（二选一）、FanSky_Qs |
| B7 | FanSky_Qs（仅 Teyvat）、StarRail-plugin、ZZZ-Plugin、ark-plugin、flower-plugin、mora-plugin、phi-plugin |
| B8 | arknights-plugin、Atlas、waves-plugin、Speech-statistics-plugin、Guoba-Plugin |
| B9 | agent-plugin、mc-plugin、nai-plugin、pixiv-plugin、San-plugin、TRSS-Plugin、Yunzai.TRSS.me（与前者二选一）、xitian-plugin、y-tian-plugin |
| 空目录不迁移 | bin、plugins |

合计 38 个一级目录；两个空目录不计入 36 个可加载入口目录。所有目录都有明确去向，没有因样本未命中而被静默遗漏。

## 附录 B：可复核产物

- [历史命令/任务明细 CSV](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-13/go-rewrite-blueprint/work/log-command-usage.csv)
- [历史日志聚合 JSON](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-13/go-rewrite-blueprint/work/log-command-summary.json)
- [目录用量聚合](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-13/go-rewrite-blueprint/work/usage-by-directory.json)
- [日志聚合脚本](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-13/go-rewrite-blueprint/work/aggregate_command_logs.py)
- [目录用量聚合脚本](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-13/go-rewrite-blueprint/work/aggregate_usage_by_directory.py)
- [静态导入明细](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-13/go-rewrite-blueprint/work/plugin-imports.csv)
- [插件依赖汇总 CSV](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-13/go-rewrite-blueprint/work/plugin-dependencies.csv)
- [插件依赖汇总 JSON](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-13/go-rewrite-blueprint/work/plugin-dependency-summary.json)
- [依赖扫描脚本](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-13/go-rewrite-blueprint/work/scan_plugin_dependencies.py)
- [逐目录迁移批次表](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-13/go-rewrite-blueprint/work/migration-batches.csv)
- [SnowLuma 脱敏配置与 token 存在性](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-13/go-rewrite-blueprint/work/snowluma-config-redacted.json)
- [SnowLuma 日志结构聚合](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-13/go-rewrite-blueprint/work/snowluma-log-summary.json)
- [SnowLuma 脱敏连接证据](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-13/go-rewrite-blueprint/work/snowluma-connection-evidence-redacted.json)
- [SnowLuma 动作字段结构](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-13/go-rewrite-blueprint/work/snowluma-action-structures.json)
- [SnowLuma 配置/日志脱敏脚本](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-13/go-rewrite-blueprint/work/inspect_snowluma.py)
- [SnowLuma 日志聚合脚本](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-13/go-rewrite-blueprint/work/analyze_snowluma_logs.py)
