# TRSS-Yunzai 核心重构技术可行性评估

- 评估日期：2026-11-12
- 主实例：`d:\Toshikoshi_chan\Agent\音无\mysy2\mysy2\home\SERVER2\TRSS_AllBot\TRSS-Yunzai`
- 目标：判断用另一种语言重构能否提升性能、稳定性与可维护性，并比较完整重写、新语言核心兼容 JS 插件、保留 Node 核心加侧车三条路线。
- 执行边界：全程只读；未修改项目文件，未安装依赖，未构建或启动机器人，未运行 `tsab`，未执行 Git 写操作，未访问插件上游网络。
- 时间预算口径：不以工作量或工期否决任何路线；结论依据是行为一致性风险、生态维护成本、性能与稳定性收益。

## 1. 结论摘要

### 1.1 直接回答

**可以用另一种语言完整重构，而且完整重写应当作为一等方案。** Go、Rust、Python 都能实现核心；TypeScript 能显著改善类型与维护性，但仍是 Node/V8 运行时，不能视为提升运行时性能的办法。

不过，**换语言本身不会自动带来性能或稳定性提升**。这个实例的主要成本面不是 3 千多行的 `lib/`，而是：

1. 约 1.14 万物理行的框架及内置适配器/插件；
2. 32 个可加载的第三方插件目录，剔除静态资源后约 45.37 万运行时源码物理行，且包含重复目录；
3. 广泛的 JS 动态行为、Node 内置模块、npm 包、跨插件导入和共享全局对象；
4. Chromium、远端 API、Redis 往返、SQLite/LevelDB、子进程等语言外部成本。

### 1.2 推荐决策

| 前提与目标 | 推荐路线 | 理由 |
|---|---|---|
| 接受插件原生化、冻结或分叉 JS 上游，并持续移植需要的上游变化 | **A：Go 完整重写核心与插件** | 长期可获得清晰类型边界、较低部署复杂度、良好网络并发与进程隔离；不以工期为判断依据 |
| 追求更高的 CPU/内存控制，尤其是已测得的图像或计算热点 | **A 或 C 的 Rust 实现** | Rust 适合热点与侧车；但 Chromium、网络和上游延迟不会因 Rust 消失 |
| 必须持续原样跟随现有 JS 插件上游 | **B：新语言核心 + 独立 Node 插件宿主**，或先走 C | 现有插件不是纯数据插件，直接依赖 Node 与核心内部模块；没有 JS 宿主就无法保持行为兼容 |
| 当前主要问题是渲染失败、浏览器资源占用或少量明确热点 | **C：保留 Node，Go/Rust 侧车** | 行为风险最小，能独立验证收益，且侧车可被 A/B 复用；但不会改善 JS 插件本身的维护性 |
| 主要痛点是缺少类型、测试和模块边界，而非运行时瓶颈 | **TypeScript 渐进重写也是有效备选** | 可保留 JS 生态并提升维护性，但性能提升需要靠实现优化而不是 TS 本身 |

**综合建议：**

- 若“完整原生化”是确定的长期目标，选择 **A，优先 Go；Rust 用于已证实的 CPU/图像热点**。完整重写与侧车并不互斥，渲染侧车可以先成为新架构的一部分。
- 若“持续跟随 JS 插件上游”是硬约束，不建议为了换语言而删除 Node。此时应在 **B 的双运行时收益** 与 **C 的较低行为风险** 之间选择，而不是假装完整原生重写还能无损兼容上游。
- 不论选择哪条路线，第一阶段都应建立**契约测试、消息黄金样本、渲染黄金图、数据兼容测试和性能基线**。没有这些证据，重写后的“更快、更稳”无法验证。

## 2. 证据口径与限制

### 2.1 标记约定

- **事实**：由文件内容、目录统计或只读搜索直接支持。
- **历史日志事实**：由既有日志聚合得到，不代表当前运行状态，也不是受控基准。
- **推断**：根据代码结构得出的可能性，需运行时验证。
- **建议/技术判断**：架构与语言选型意见，不冒充实测结论。

### 2.2 统计口径

- 仅统计主实例 `TRSS-Yunzai`；同级的 Miao-Yunzai、LittlePaimon、Zhenxun、Liteyuki、Amiya、Sagiri、Yunzai、ZeroBot、go-cqhttp、Lagrange、Mirai 等没有合并进代码量。
- 体积统计排除 `.git` 和 `node_modules`，包含插件自己的资源、数据和临时文件，因此“体积”不等于源码规模。
- 源码扩展名：`.js/.cjs/.mjs/.jsx/.ts/.tsx/.vue`。
- 行数为**物理行**，包含空行、注释、数据文件和可能的生成代码，不用来估算工期。
- “运行时源码”采用路径启发式，排除 `resources/temp/static/assets/server/public` 及 `.min.js` 等静态浏览器资产；该口径用于区分 Node 侧代码和模板资源，不是 AST 级依赖分析。
- API 扫描同样排除上述静态资产，但仍是词法扫描；`cfg`、`common`、跨插件路径等命中是兼容面上限，需要结合导入语句复核。

### 2.3 未能做的事

- 未启动进程，因此没有当前 CPU、RSS、事件循环延迟、浏览器页签数、消息吞吐或失败率测量。
- 未访问上游仓库，因此只能确认更新机制和远端元数据，不能确认每个上游目前是否活跃、本地是否落后。
- `Main.sh` 是带内嵌压缩数据的自释放脚本；只读取了可见包装层，未执行或解包，不能据此评价其内部逻辑。
- 未输出配置中的凭据、Cookie 或日志正文。

## 3. 代码规模与插件体积

### 3.1 核心及内置模块

| 范围 | 源码文件数 | 物理行 | 说明 |
|---|---:|---:|---|
| `app.js` | 1 | 31 | 启动模式、创建全局 `Bot` |
| `lib/` | 27 | 3,268 | 核心、配置、事件、插件、渲染抽象、工具 |
| `renderers/` | 2 | 311 | Puppeteer 渲染后端 |
| `plugins/adapter/` | 7 | 5,665 | OneBotv11、Milky、Satori、ComWeChat、GSUIDCore、OPQBot、stdin |
| `plugins/other/` | 5 | 636 | 更新、安装、重启等内置功能 |
| `plugins/system/` | 9 | 835 | 系统管理插件 |
| `plugins/example/` | 5 | 675 | 示例插件 |
| **框架与内置范围合计** | **56** | **11,421** | 比单独看 `lib/` 更能代表核心重写范围 |

核心入口可见 [app.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/app.js#L1-L31)，插件与适配器运行关系见 [bot.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/bot.js#L256-L273)。

### 3.2 插件总量

| 指标 | 结果 |
|---|---:|
| `plugins/` 一级目录 | 38 |
| 加载器可识别入口的目录 | 36 |
| 含 `package.json` 的目录 | 26 |
| 含 `.git` 元数据的目录 | 30 |
| 排除 `.git/node_modules` 后的全部文件 | 33,203 |
| 总体积 | 5,527,361,777 字节，约 **5.15 GiB** |
| 全部源码文件 / 物理行 | 3,231 / 651,097 |
| 运行时源码文件 / 物理行 | **2,464 / 461,532** |
| 静态浏览器资产源码文件 / 物理行 | 767 / 189,565 |
| 第三方插件运行时物理行，扣除 adapter/other/system/example | **约 453,721**，含重复目录 |

体积分布：

| 单目录体积 | 目录数 | 合计 | 目录 |
|---|---:|---:|---|
| `<10 MiB` | 14 | 0.05 GiB | adapter、agent-plugin、bin、example、mc-plugin、other、plugins、San-plugin、system、TRSS-Plugin、xianxin-plugin、xitian-plugin、yunzai-plugin-deer-pipe、Yunzai.TRSS.me |
| `10–50 MiB` | 10 | 0.24 GiB | ark-plugin、genshin、Guoba-Plugin、logier-plugin、memz-plugin、nai-plugin、pixiv-plugin、rconsole-plugin、Speech-statistics-plugin、ZZZ-Plugin |
| `50–100 MiB` | 4 | 0.28 GiB | arknights-plugin、FanSky_Qs、liangshi-calc、yenai-plugin |
| `100–500 MiB` | 6 | 1.69 GiB | earth-k-plugin、earth-k-plugin-tmp、flower-plugin、StarRail-plugin、waves-plugin、y-tian-plugin |
| `>=500 MiB` | 4 | 2.90 GiB | Atlas、miao-plugin、mora-plugin、phi-plugin |

**事实：** `earth-k-plugin` 与 `earth-k-plugin-tmp` 的文件数、源码行数几乎完全相同；`TRSS-Plugin` 与 `Yunzai.TRSS.me` 的统计也相同。**推断：** 它们是复制或临时目录的可能性较高，但本次未做逐文件哈希证明。迁移前必须由人决定去重、保留还是分别兼容，不能擅自删除。

## 4. 架构现状与 Node.js 强绑定

### 4.1 启动与进程模型

**事实：**

- `app.js` 支持 `stop/daemon/pm2/start` 等模式；`daemon` 用同步子进程循环守护，最终创建 `global.Bot` 并调用 `Bot.run()`，见 [app.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/app.js#L1-L31)。
- `Bot.run()` 依次执行初始化、HTTP/HTTPS 监听、插件加载、事件与适配器加载，然后发出 `online`，见 [bot.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/bot.js#L256-L273)。
- PM2 配置为单应用 `TRSS-Yunzai`，`max_memory_restart: 512M`，指数退避重启延迟 1000ms，见 [pm2.yaml](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/config/pm2.yaml#L1-L7)。
- 初始化会设置上海时区、捕获 `uncaughtException/unhandledRejection`，并初始化 Redis，见 [init.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/config/init.js#L17-L63)。

**技术判断：** 当前是所有 JS 插件共享一个主进程和全局状态的结构。新语言可以改善核心故障隔离，但如果继续把所有插件放进同一个 JS 宿主，插件间的故障域仍然共享。

### 4.2 插件发现、加载、生命周期和热更新

[loader.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/plugins/loader.js#L45-L168) 显示：

1. 扫描 `plugins/` 一级目录；有 `index.js` 就加载入口，否则加载目录顶层每个 `.js` 文件。
2. 通过 ESM 动态 `import()` 加载，支持导出单个类或 `apps` 集合。
3. 用 `Promise.allSettled` 并发导入，配置的超时为 60 秒。
4. 每个插件类先实例化一次执行 `init()`、收集 `task`，再实例化一次处理规则与 `handler`。
5. `priority` 升序排列；`rule.reg` 统一转成 JS `RegExp`。
6. 全局注入 `plugin` 基类和 `segment`。
7. 顶层松散 JS 插件通过 `chokidar` 监听；变更时使用带时间戳查询参数的 `import()` 绕过入口模块缓存，见 [loader.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/plugins/loader.js#L639-L718)。

**必须保留或明确改变的细节：**

- 加载超时只是 `Promise.race`，不会取消正在执行的动态导入。
- 两次实例化及其副作用属于现有行为。
- ESM 查询参数只直接改变入口模块标识，不能简单等同于“整个依赖树都无缓存重载”。
- 任务函数、`handler` 函数、上下文闭包、类原型和动态字段不是 JSON 可直接表达的插件描述。

插件基类还提供 `reply`、`setContext/getContext/awaitContext/finish` 和 `renderImg`，见 [plugin.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/plugins/plugin.js#L10-L121)。

### 4.3 适配器、消息事件与分发

**适配器即插件。** 七个适配器文件通过 `Bot.adapter.push(...)` 注册，之后由监听器调用 `load()`，见 [listener/loader.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/listener/loader.js#L10-L50) 和 [OneBotv11.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/plugins/adapter/OneBotv11.js#L5-L43)。

消息路径：

1. 协议端 WebSocket/HTTP 事件进入适配器。
2. 适配器构造账号、好友、群、成员对象和消息段，调用 `Bot.em()`。
3. `Bot.em()` 对 `message.group.normal` 这类名称逐级发射完整事件及父事件，见 [bot.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/bot.js#L317-L375)。
4. `message` 监听器调用 `PluginsLoader.deal(e)`，见 [message.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/events/message.js#L6-L13)。
5. 分发器执行黑白名单、冷却、事件字段规范化、`Runtime.init`、上下文、`accept`、规则匹配、权限和回复包装，见 [loader.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/plugins/loader.js#L188-L290)。

**Node 强绑定：** `EventEmitter`、可变事件对象、非枚举属性、getter、`Proxy`、动态方法绑定、JS 正则、异步 Promise、`Map/Set`、Buffer，以及账号选择时的动态回退。`Bot` 本身还是一个代理到工具方法与具体账号对象的 `Proxy`，见 [bot.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/bot.js#L66-L81)。

### 4.4 配置与热重载

[config.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/config/config.js#L51-L179) 的事实：

- `config/default_config/` 提供默认值，缺失文件会复制到 `config/config/`。
- 默认配置与用户配置通过对象展开做**浅合并**，不是递归深合并。
- 群配置按 `default`、Bot 默认、群、Bot+群的顺序覆盖。
- YAML 有缓存；文件变化后删除缓存，带 5 秒防抖，并可触发指定 `change_*` 回调。
- `file_watch: false` 时会替换监听实现以关闭监听。
- 配置对象也是 `Proxy`，允许 `cfg.bot` 这类动态访问。

### 4.5 Redis、LevelDB 与 SQLite

- **Redis：** 启动时创建全局客户端；连接本地失败时可启动 Redis 子进程，远端连接失败可走退出路径；错误时断开并重连，见 [redis.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/config/redis.js#L8-L113)。用途包括消息统计、上线冷却、重启状态、Chromium WS endpoint 和各插件自己的缓存/状态。
- **LevelDB：** `Bot.getMap()` 用 `level` 打开 `${dir}-leveldb`，将数据库包装成带异步持久化 `set/delete` 的 JS `Map`，见 [util.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/util.js#L88-L188)。
- **SQLite/Sequelize：** 当前配置为 SQLite，路径 `data/db/data.db`。`genshin` 插件直接创建 Sequelize 实例并自动认证，见 [BaseModel.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/plugins/genshin/model/db/BaseModel.js#L1-L25)。模型包括 `UserDB`、`MysUserDB`、`UserGameDB`，字段中有 JSON 字符串和自定义 getter/setter，不能仅复制表结构就算迁移完成。

### 4.6 渲染与日志

- 渲染器由目录动态加载，要求 `id/type/render`，见 [renderer/loader.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/renderer/loader.js#L21-L63)。
- `art-template` 生成 HTML，写入 `temp/html`，模板有缓存和变更监听，见 [Renderer.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/renderer/Renderer.js#L19-L81)。
- Puppeteer 后端启动或连接 Chromium，使用 Redis 缓存 WS endpoint，每 100 次截图尝试重启，失败时关闭重启，见 [puppeteer.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/renderers/puppeteer/lib/puppeteer.js#L51-L118)。
- 日志使用 `log4js + chalk`，有控制台、按天 command 日志和 error 文件，command 保留 180 份并压缩；`logger` 是全局对象，见 [log.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/config/log.js#L14-L91)。

## 5. 插件上游与真实 API 兼容面

### 5.1 是否持续跟随上游

**事实：**

- 更新插件支持核心与插件的 `git pull`，强制更新还包含硬重置；批量更新遍历插件并在包变化后安装依赖，见 [update.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/plugins/other/update.js#L89-L130) 和 [update.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/plugins/other/update.js#L196-L217)。
- 当前 `config/config/bot.yaml` 的 `update_time` 为 1440 分钟，`update_cron` 为空；默认配置相同，见 [bot.yaml](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/config/config/bot.yaml#L14-L25)。
- 30 个插件目录有 `.git/config` 和远端 URL；远端列表已保存在中间产物中。

**判断：** 该实例具备并已配置周期性跟随上游的机制，但本次未联网，不能把“配置了自动更新”表述成“所有插件均更新成功或上游均活跃”。

**迁移含义：**

- 完整原生翻译后，如果继续接受 JS 上游更新，必须有人/agent 持续将上游差异移植到原生实现。这是长期生态维护成本，与一次性工作量无关。
- 若不希望承担双实现维护，必须选择冻结版本、维护原生分叉，或保留 JS 插件宿主。这个选择是人工产品决策。

### 5.2 实际框架接口使用范围

对 2,464 个运行时源码文件的词法扫描结果：

| 接口或能力 | 命中插件目录数 | 文件数 | 引用数 | 解释 |
|---|---:|---:|---:|---|
| Node 内置模块 | 36 | 661 | 1,195 | 文件、路径、进程、网络、加密等 |
| 直接导入核心内部路径 | 35 | 359 | 491 | 不只是调用公开 API |
| `rule` 生命周期字段 | 35 | 433 | 500 | 命令规则普遍存在 |
| `logger.*` | 35 | 562 | 4,060 | 全局日志 |
| 继承/实例化 `plugin` | 34 | 416 | 420 | 不等于实际加载类数 |
| `e.reply` / `this.e.reply` | 32 | 444 | 3,998 | 事件对象行为契约 |
| `Bot.*` | 32 | 200 | 1,052 | 共识别 76 种成员名 |
| `segment.*` | 30 | 255 | 808 | 消息段构造 |
| 子进程相关引用 | 30 | 61 | 66 | 词法上限，需逐个确认用途 |
| 跨插件路径字符串/导入 | 29 | 436 | 1,046 | 并非全部为 import，作为风险上限 |
| `cfg.*` | 28 | 341 | 1,165 | 含插件自己的 cfg，属词法上限 |
| `common.*` | 28 | 216 | 618 | 含不同来源 common，属词法上限 |
| 全局 `redis.*` | 27 | 200 | 803 | 数据访问直接进入插件 |
| `puppeteer.*` | 22 | 156 | 381 | 另有 10 个目录直接 import `puppeteer` |
| `task` 字段 | 22 | 44 | 51 | 框架定时任务 |
| `e.runtime` | 19 | 33 | 58 | Runtime 服务接口 |
| 上下文接口 | 18 | 53 | 198 | 会话状态与超时 |
| `setInterval` | 9 | 11 | 14 | 绕过框架 task 的常驻计时器 |
| `handler` 字段 | 7 | 20 | 30 | 插件间事件处理 |
| `#miao` 别名 | 6 | 115 | 169 | 跨插件包导入映射 |
| 直接 `node-schedule` | 5 | 7 | 13 | 绕过统一调度器的插件存在 |

实际 `Bot` 成员不只是 `sendMsg`：

| 类别 | 搜索命中的成员示例 |
|---|---|
| 账号与通讯录 | `uin`（20 个插件目录）、`gl`、`fl`、`gml`、`bots`、`pickGroup`、`pickFriend`、`pickUser`、`pickMember`、`getGroupList`、`getFriendList`、`getGroupMap`、`getFriendMap` |
| 消息构造与发送 | `makeForwardMsg`、`makeForwardArray`、`sendForwardMsg`、`sendMasterMsg`、`sendFriendMsg`、`sendGroupMsg`、`sendUni` |
| 工具 | `fsStat`、`sleep`、`exec`、`String`、`makeLog`、`Loging`、`getTimeDiff`、`Buffer`、`download`、`fileType`、`glob`、`rm`、`mkdir`、`makeError` |
| 事件与进程 | `on`、`once`、`emit`、`em`、`adapter`、`restart`、`exit` |
| HTTP/WS 与文件 | `express`、`server`、`wsf`、`fileToUrl` |
| 账号/协议特定接口 | `nickname`、`logger`、`getGroupMemberInfo`、`sendLike`、`cookies`、`bkn`、`getSystemMsg`、`sendApi`、`setEssenceMessage`、`getMsg`、`getForwardMsg` 等 |

完整 76 项及命中文件见中间产物 `bot-api.csv`。

### 5.3 npm 与跨插件生态

- 运行时扫描识别出 **141 个不同的导入根标识**，包括 Node 内置模块、内部别名和 npm 包。
- `oicq` 出现在 24 个插件目录，`node-fetch` 22 个，`axios` 10 个，直接 `puppeteer` 10 个，`icqq` 8 个，`express` 6 个，`sharp` 4 个。
- `miao-plugin` 自身通过 `#miao` 暴露组件与模型，根 `package.json` 也注册 `#miao` 和 `#miao.models`，见 [package.json](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/package.json#L49-L52)。
- `Runtime` 直接尝试导入 `genshin` 的 Mys 模型和 `#miao`，见 [runtime.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/plugins/runtime.js#L14-L24)。
- `genshin` 直接使用 Sequelize；`sharp` 已是原生图像库，重写它的 JS 调用包装并不会自动获得同比例提速。

**结论：** “只实现 `load/deal/reply` 就兼容所有插件”不成立。路线 B 必须兼容核心相对导入、全局对象、npm 与 Node 内置模块；路线 A 则必须把共享插件库和这些外部能力一起纳入原生契约。

## 6. 关键性能与稳定性路径

### 6.1 Chromium 与模板渲染

**事实：**

- 核心渲染器每次截图新建 page，执行模板写入、页面导航、尺寸计算、截图，并在 100 次后重启浏览器，见 [puppeteer.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/renderers/puppeteer/lib/puppeteer.js#L159-L287)。
- 默认 `page.goto` 超时为 120 秒，见 [config_default.yaml](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/renderers/puppeteer/config_default.yaml#L22-L30)。
- 插件并非都走核心渲染器。比如 `yenai-plugin` 自己启动浏览器、等待 `networkidle0`、截图并每 100 次重启，见 [插件 puppeteer.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/plugins/yenai-plugin/lib/puppeteer/puppeteer.js#L44-L153)。
- 10 个插件目录直接导入 `puppeteer`，因此只优化核心单例不能覆盖全部浏览器路径。

**历史日志事实：** 从 2025-11-05 至 2026-08-21 的现存日志中提取到 86 条带耗时渲染记录：

| 样本数 | 最小 | 中位数 | 均值 | P95 | 最大 |
|---:|---:|---:|---:|---:|---:|
| 86 | 864ms | 1,607ms | 1,739.98ms | 2,438ms | 6,968ms |

这些样本不是完整请求集，也不是同环境受控基准，不能用来计算换语言后的百分比收益。

**技术判断：** Chromium 页面加载、布局、字体、截图和图像编码通常不会因控制语言从 JS 换成 Go/Rust 而消失。侧车的主要收益应是**有界并发、队列、浏览器池、故障隔离和资源回收**；原始截图速度是否提升必须实测。

### 6.2 外部 HTTP/API

- 核心 `Bot.Buffer()` 对 URL 直接 `fetch()` 并读完整 `arrayBuffer()`，此处没有显式超时或流式大小限制，见 [util.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/util.js#L273-L327)。
- OneBot API 请求有 60 秒 echo 超时并终止 WebSocket，见 [OneBotv11.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/plugins/adapter/OneBotv11.js#L17-L43)。
- 插件使用 `node-fetch`、全局 `fetch`、axios 和自有代理封装，行为不统一。
- 历史日志存在网络超时/重置匹配行和 Redis 错误匹配行，但日志行不是独立事故数，不能据此计算事故率。

**应测指标：** 上游耗时与本地处理耗时拆分、连接复用、并发数、超时率、重试次数、响应体大小、队列等待、DNS/TLS 时间。换语言不能降低远端服务自身延迟。

### 6.3 Redis 消息统计

[loader.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/plugins/loader.js#L467-L488) 对日/月/年/累计四种周期构造总计、Bot、用户、群维度键，并逐键 `await redis.incr`。群消息且字段齐全时一次 `saveCount` 可形成 16 个键；接收和回复路径都会计数。

**推断：** 这是值得优先测量的 Redis 往返热点。**建议：** 先在现有 Node 中用 pipeline/批量 Lua、键结构审查和指标验证收益；该优化不依赖换语言，并可移植到新核心。

### 6.4 消息分发与正则

- 分发按优先级遍历插件和规则，命中后停止；还有上下文和 `accept` 前置流程。
- 运行时扫描到 420 处插件基类继承/实例化，不等于线上实际加载数量，但说明规则面较宽。
- **应测指标：** 每条事件的候选插件数、正则测试次数、各插件耗时、总分发耗时、慢正则、每会话乱序率。
- Go 的 RE2 和 Rust 默认 `regex` 都不支持 JS 的后顾/回溯引用等全部语义，直接替换正则引擎可能改变匹配结果。

### 6.5 定时任务、上下文与常驻内存

- 框架用 `node-schedule` 创建任务并按名称去重，见 [loader.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/plugins/loader.js#L490-L535)。
- 22 个插件目录出现 `task`，9 个出现 `setInterval`，34 个出现 `setTimeout`。
- 自动更新和重启也通过计时器实现；上下文状态保存在进程内对象中，重启会丢失。
- `fileToUrl` 会将文件 Buffer 放进内存表并设置过期替换，见 [bot.js](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/bot.js#L275-L315)。

**应测指标：** 任务重叠执行、任务耗时、漂移、错过执行、活动计时器数、上下文数量、文件缓存字节数、重启后状态差异。

### 6.6 进程与故障隔离

- PM2 的 512M 内存重启阈值是现有保护线，不是内存使用证明。
- 全局异常捕获能记录错误，但不能证明进程在异常后仍处于一致状态。
- 历史日志没有匹配到 `uncaughtException/unhandledRejection/JavaScript heap/out of memory`，这只能说现存日志未命中，不能证明从未发生。
- 同一进程内多个插件还能各自启动 Chromium、Express 和子进程，核心 PM2 只能整体重启。

## 7. 完整重写必须重新实现的运行时契约

下表是路线 A 的最低契约清单，也是路线 B 的兼容层清单。库名是候选，不表示已做依赖验证。

| 契约 | 现有实现与 Node 绑定 | Go 替代 | Rust 替代 | Python 替代 |
|---|---|---|---|---|
| 启动与生命周期 | ESM 顶层 await、全局 `Bot`、信号、PM2/daemon、退出码 | `main`、显式 `App` 生命周期、`os/signal`、外部 supervisor | `#[tokio::main]`、显式状态机、signal、supervisor | `asyncio.run`、显式 App、signal、supervisor |
| 插件发现与加载 | 目录扫描、动态 `import()`、ESM 导出、类原型、顶层副作用 | 编译期注册表；动态需求用子进程 gRPC 或 WASM。Go 原生 plugin 不适合作为 Windows 通用方案 | 静态注册、Wasmtime 或子进程 IPC；动态 dylib ABI 风险高 | `importlib`、入口点、子进程隔离；动态导入最接近现状 |
| 初始化与卸载 | 两次实例化、`init`、规则编译、任务收集、热更新缓存语义 | 显式 `Plugin` 接口、构造/启动/停止、worker 重启 | trait、生命周期所有权、worker/WASM 实例重建 | 类/协议生命周期、模块重载或 worker 重启 |
| 规则与优先级 | JS 类字段、`priority`、`accept`、上下文、权限、首个命中即停 | typed rule + 扩展元数据；保持排序与返回语义 | typed rule + trait 回调；保持排序与短路语义 | dataclass/协议 + 动态钩子，较容易模拟 |
| 正则 | JS `RegExp`、flags、lastIndex、后顾和回溯引用 | `regexp` 是 RE2，语义不足时用 `dlclark/regexp2` 或 PCRE2，并做差异测试 | 默认 `regex` 不含后顾/回溯引用；评估 `fancy-regex`/PCRE2 | `re` 较接近部分特性，但 Unicode/flags/JS 语义仍需测试 |
| 消息与事件 | 可变对象、getter/Proxy、非枚举字段、层级 EventEmitter、异步监听器 | typed canonical event + `map[string]any` 扩展、显式层级发射、消息段接口 | Serde 类型 + 扩展 map、显式 emitter、所有权边界 | dataclass/Pydantic + 动态属性、async emitter |
| Bot/好友/群/成员 | 动态代理、方法绑定、账号回退、Map/Set、懒加载 | interface + account registry + typed ID；保留回退规则 | trait + registry + typed ID；明确借用/并发 | 动态对象或 Protocol，保留回退规则 |
| 协议适配器 | 七个 JS 适配器、WS echo 关联、HTTP/WS 服务 | `net/http`、WebSocket 库、context 超时、request map | axum/tungstenite、tokio timeout、request map | aiohttp/websockets、asyncio timeout、request map |
| 回复与消息段 | `segment`、Buffer、base64/file/HTTP、转发、撤回、文件缓存 | typed Segment、`[]byte`、流式文件引用、RPC/本地 sender | enum Segment、`Bytes`、流式引用、sender trait | Segment 类、bytes、文件引用、async sender |
| Runtime 与 Handler | `cfg/common/puppeteer/Mys*`、`Handler.call/has`、跨插件函数调用 | 版本化服务接口和事件总线；共享游戏库独立模块 | 版本化 trait/service、事件总线、共享 crate | 服务注册表、事件总线、共享 Python 包 |
| 模板与截图 | `art-template`、同步模板 IO、缓存、Puppeteer、HTML 临时文件 | chromedp + 模板兼容层或重写模板；`html/template` 不是语法等价替代 | chromiumoxide + Tera/兼容层；需验证 CDP 功能覆盖 | Playwright + Jinja2/兼容层；浏览器自动化生态较直接 |
| Redis | 全局 node-redis、任意插件直接调用、TTL、重连、键命名 | go-redis、pipeline、统一数据服务或允许直接客户端 | redis crate、pipeline、连接管理 | redis-py/async client、pipeline |
| LevelDB | `level` + 异步持久化 Map 包装 | goleveldb + Map 等价 repository；必须验证既有文件兼容性 | LevelDB crate + repository；必须验证兼容性 | Plyvel/LevelDB 绑定 + repository |
| SQLite/Sequelize | 模型同步、字段 getter/setter、JSON 字符串、自动建表 | database/sql/sqlc/sqlx + 显式 schema/migration | sqlx/rusqlite + Serde + migration | SQLAlchemy/sqlite3 + 显式模型/migration |
| 定时任务与上下文 | node-schedule、六段 cron、setTimeout/Interval、进程内上下文 | robfig/cron 或兼容调度器、context/timer、状态仓库 | tokio-cron-scheduler/croner、tokio timer、状态仓库 | APScheduler/asyncio timer、状态仓库 |
| 配置与热重载 | YAML、浅合并、群覆盖顺序、Proxy、chokidar 5 秒防抖 | yaml.v3/goccy + fsnotify + typed snapshot + 扩展 map | serde_yaml + notify + typed snapshot | PyYAML + watchdog + 动态配置视图 |
| 日志 | log4js、chalk、全局 logger、mark 等级、180 份压缩 | slog/logrus + 滚动文件、结构化字段、敏感信息过滤 | tracing + rolling appender | logging + 滚动/压缩 handler |
| HTTP/WS/进程工具 | Express 扩展点、鉴权、静态 File、exec/spawn、PM2 | net/http 中间件、os/exec、明确超时与资源限制 | axum、tokio process、明确权限与资源限制 | aiohttp、asyncio subprocess |
| npm/原生模块 | sqlite3、sharp、canvas、oicq/icqq 等直接导入 | 逐项选择 Go 库、保留外部进程或原生翻译；不能假设等价 | 逐项选择 crate/C ABI/侧车 | 逐项选择 Python 包/C 绑定/侧车 |

### 7.1 两个不能忽略的契约问题

**动态插件模型。** 完整原生重写不等于必须继续支持任意 JS 动态导入。可选择：

1. 核心内置插件静态注册，获得最强类型与部署一致性；
2. 原生插件作为独立进程，通过版本化 IPC/gRPC 运行，获得热重启和故障隔离；
3. WASM 插件获得可移植沙箱，但需要重新设计宿主 API，不能直接运行现有 Node 插件。

路线 B 若要保留现有行为，则应让原有 ESM 插件继续运行在 Node 宿主中，而不是把 Boa、QuickJS 或一个普通 JS 引擎误认为 Node 兼容环境。

**事件与调用引用。** 现有插件会修改事件对象、调用其方法、保存上下文并通过跨插件函数引用协作。普通 JSON RPC 只能传值，不能自动保留对象引用、getter、函数和原型。路线 B 需要本地事件对象 + 远程服务代理/对象句柄，或者明确缩小兼容范围。

## 8. 路线 A：另一种语言完整重写核心与插件

### 8.1 执行形态

推荐目标架构：

- Go 核心负责账号、协议、事件、配置、调度、日志、数据与插件生命周期。
- 内置核心插件可采用静态注册；需要动态安装/隔离的插件采用独立进程和版本化 RPC，或明确选择 WASM。
- 共享的 `miao/genshin` 能力先形成独立原生库，避免每个插件重复翻译。
- Chromium 作为独立渲染服务；模板和静态资源保持为独立资产。
- 保留外部协议端，go-cqhttp、Lagrange、Mirai 等不因核心换语言而必须重写。

### 8.2 收益

| 维度 | 可能收益 | 反例/边界 |
|---|---|---|
| 性能 | 更强的核心并发、较低 IPC/部署开销、 typed 数据处理、容易做池化和背压 | Chromium、远端 API、原生 sharp、SQLite 等外部成本不会按语言替换比例下降；正则兼容引擎也可能更慢 |
| 稳定性 | 明确错误处理、插件进程隔离、资源上限、统一超时、可关闭 worker、静态类型减少动态错误 | 把同一套全局共享状态机械翻译过去，仍会保留逻辑竞争；外部 Chromium/协议端故障仍存在 |
| 可维护性 | 明确 schema、版本化 API、模块边界、测试与数据迁移可审计 | 若继续维持 JS 和原生两套功能实现，长期维护成本会上升；机械直译动态 JS 不会自然得到好架构 |

### 8.3 主要风险

1. **行为一致性：** 正则、事件顺序、动态对象、插件加载副作用、上下文、热更新和协议边界都可能出现细微差异。
2. **生态维护：** 自动上游更新不能直接更新原生翻译；需要冻结、分叉或持续差分移植。
3. **共享依赖：** `miao/genshin` 被其他插件引用，先翻译叶子插件会造成重复实现和语义分叉。
4. **动态插件分发：** 静态编译、原生进程插件和 WASM 各自改变安装与开发方式，必须明确产品契约。
5. **数据兼容：** LevelDB、Sequelize 模型默认值/JSON getter、Redis key/TTL 都需要迁移验证。

### 8.4 多 agent 并行切分

| 工作流 | 可并行内容 | 必须先冻结/协调的边界 |
|---|---|---|
| 契约与测试 agent | 事件 schema、插件接口、黄金消息、差分框架 | 是所有其他 agent 的前置共享契约，由单一负责人合并 |
| 核心 agent | 配置、日志、调度、Redis、插件注册、进程管理 | App 生命周期、错误模型、并发/顺序语义 |
| 适配器 agents | OneBotv11；Milky/Satori；ComWeChat/GSUIDCore/OPQBot/stdin | canonical event、Segment、账号接口和 API 超时语义冻结后并行 |
| 数据 agent | Redis key、LevelDB、SQLite schema 与迁移 | 数据格式与回滚方案单点负责，避免多个 agent 改同一迁移 |
| 渲染 agent | 模板清单、HTML 快照、浏览器服务、图片对比 | 模板兼容策略、字体/资源路径、截图容差 |
| 共享库 agents | `miao` 组件、`genshin` Mys/DB/Runtime | 必须先于依赖它们的插件翻译；公共 API 由独立评审 |
| 插件批次 agents | 按游戏/工具/Web/AI 等领域并行翻译 | 每批只能依赖已冻结的共享接口；不得各自修改核心契约 |
| 资产 agents | 静态 JS/CSS/图片路径整理、资源加载兼容 | 不需要翻译全部浏览器资产，但需保持 URL 与 HTML 行为 |
| 独立验证 agent | 契约覆盖、差分运行、故障注入、性能报告 | 不修改实现来“迁就”测试，失败回交对应负责人 |

**人工决策节点：** 原生上游策略、动态插件模型、正则与历史怪癖是否严格保留、重复目录处理、数据切换、远程命令能力、渲染像素容差和最终切换标准。

## 9. 路线 B：新语言核心，保持现有 JS 插件兼容

### 9.1 推荐执行形态

- Go/Rust/Python 新核心负责协议端、账号、HTTP/WS、配置、数据、调度和生命周期。
- **保留一个独立 Node 插件宿主**，加载原有 ESM 插件及 npm 依赖。
- 新核心与 Node 宿主通过版本化协议交换事件、回复、服务调用、任务和健康状态。
- Node 宿主内保留同一全局空间和跨插件导入，避免把每个插件拆进不同 JS worker 后改变共享状态。
- 渲染可继续由 Node 宿主执行，也可调用路线 C 的侧车。

### 9.2 必须实现的兼容层

1. `global.Bot/plugin/segment/logger/redis/Renderer` 等全局 shim。
2. 与原始相对路径对应的兼容模块，例如 `lib/config/config.js`、`lib/common/common.js`、`lib/puppeteer/puppeteer.js`、`lib/plugins/runtime.js`；35 个插件目录直接导入核心内部路径，只有全局变量不够。
3. `Bot` 的 76 种已识别成员和 Proxy 回退，优先覆盖真实调用，长尾通过契约测试补齐。
4. 可变事件对象、回复/撤回、群/好友/成员对象、Handler、Runtime、上下文和任务函数。
5. ESM 动态导入、`#miao` 映射、npm 包和必要的原生扩展。
6. 对象句柄/远程代理或明确的 RPC 值语义；不能假设 JSON 序列化可以传递函数、Buffer、Map 或类实例。
7. 插件宿主崩溃后的状态恢复策略：接受丢失、快照恢复或事件重放，三者行为不同。

### 9.3 收益与代价

| 维度 | 判断 |
|---|---|
| 行为一致性 | 插件仍在 Node 中运行，保留 npm/ESM 是最主要优势；但核心服务和对象跨越进程后，引用与顺序语义仍有高风险 |
| 性能 | 新核心协议/数据路径可能改善；每次服务调用跨进程的序列化与等待也可能抵消收益，插件 CPU 仍在 V8 |
| 稳定性 | 新核心和插件宿主可分别重启、限制资源，故障隔离优于单进程；宿主重启会丢失内存状态，需专门处理 |
| 可维护性 | 上游 JS 插件可继续使用；但同时维护新核心、Node 宿主、RPC 和 shim，长期系统复杂度最高 |
| 生态 | 最适合持续跟随 JS 上游；Node 与 npm 部署并未消除 |

### 9.4 多 agent 并行切分

- **桥接协议组**先定义版本化消息、对象句柄、错误、取消、背压和重连语义；该协议不能由各插件 agent 各自发明。
- **Node 宿主组**负责 ESM 加载、全局 shim、相对路径兼容包和跨插件状态。
- **核心服务组**并行实现账号/适配器、配置、Redis/DB、调度和日志。
- **API 兼容组**按 Bot、segment、Runtime、Renderer、cfg/common 分批实现，每组用真实插件样本验证。
- **验证组**对同一消息记录新旧调用轨迹、回复和副作用，特别检查顺序与重复调用。

**人工决策节点：** 单 Node 宿主还是多 worker、允许损失的兼容 API、宿主崩溃后的状态策略、哪些调用必须同步、是否接受双运行时长期存在。

## 10. 路线 C：保留 Node 核心，热点用 Go/Rust 侧车

### 10.1 优先候选

1. **渲染侧车：** 有界队列、浏览器池、页面级超时、资源限制、健康检查、失败隔离；接收 HTML/模板上下文并返回图片。
2. **图像处理侧车：** 仅在 profile 证明图像处理是热点后实施；若当前已用 sharp/libvips，应先比较现有原生路径。
3. **HTTP/API 网关：** 统一连接复用、超时、重试、缓存和指标，但需保持各 API 的签名、Cookie 与错误语义。
4. **计算热点 worker：** 仅针对测量出的 CPU 密集计算，不建议无证据地重写所有业务逻辑。
5. **不一定需要侧车的优化：** Redis pipeline、消息分发计时、缓存与慢正则治理可直接在 Node 中先做。

### 10.2 收益与边界

- **行为风险最低：** 插件 API、ESM、npm、上游更新和共享全局对象保持原样。
- **性能收益局部：** 只改善被侧车接管且经测量确认的路径；增加一次进程通信也可能让简单任务更慢。
- **稳定性收益明确：** 浏览器或计算 worker 可以独立崩溃、重启和限资源，避免拖垮整个机器人。
- **维护性改善有限：** 核心动态 JS 和插件兼容面不变，但侧车契约可以成为 A/B 的稳定组件。

### 10.3 多 agent 并行切分

- 协议/契约 agent：定义渲染或计算 API、超时、取消、错误与版本。
- Go/Rust worker agents：浏览器池、图像/计算实现，可独立并行。
- Node 薄客户端 agent：只替换调用出口，保持 `render/screenshot` 返回语义。
- 可观测性 agent：队列、耗时、RSS、页签、崩溃、失败率指标。
- 验证 agent：HTML/图片黄金对比、并发与故障注入、旧路径回退验证。

**人工决策节点：** 首个热点是否有足够测量证据、Go 或 Rust、图片一致性容差、浏览器并发/内存上限、失败时是否自动回退旧渲染器。

## 11. 语言选择理由与反例

| 语言 | 适用理由 | 主要反例与风险 | 本项目建议 |
|---|---|---|---|
| **Go** | 网络并发、单二进制部署、显式错误、生态覆盖 Redis/SQLite/WS/cron/fsnotify；适合核心和独立服务 | GC 仍存在；RE2 与 JS 正则不兼容；Windows 动态 Go plugin 不是通用答案；Chromium 成本不变 | **完整重写核心首选之一，渲染/HTTP 侧车也合适** |
| **Rust** | 内存安全、无 GC、CPU/图像处理与资源控制强、适合高可靠 worker | 异步/所有权和动态插件 ABI 增加实现约束；CDP 与模板生态需验证；不会修复 Chromium 自身问题 | **已证实 CPU/内存热点、侧车或追求最高控制时选择** |
| **Python** | `importlib` 接近动态插件、Playwright/SQLAlchemy/APScheduler 生态完整、适合 agent 辅助翻译和快速行为对齐 | CPU 密集路径受解释器/GIL 约束，打包与类型纪律需额外建设；动态类型可能保留原有维护问题 | **重视动态插件与开发一致性时可选完整重写，不以最高吞吐为目标时合理** |
| **TypeScript** | 保留 ESM/npm/JS 插件，类型化契约和渐进改造最容易，行为差异最小 | 仍使用 V8 与 Node；类型不会减少 Chromium、网络或 Redis 时间；错误的数据结构仍会影响性能 | **维护性优先、性能不是主要矛盾时的强备选** |

**关键反例总结：**

- Go/Rust 重写 `fetch()` 不会降低远端 API 的服务端延迟。
- Rust 无 GC 不等于整个系统无内存增长，因为 Chromium、图像缓冲和插件队列仍可能增长。
- Python 动态导入方便不等于消息分发一定更快。
- TypeScript 类型更多不等于运行时吞吐更高。
- sharp 等库已经是原生实现，换调用语言可能只改变包装层。
- 多 agent 可以消除实施时间约束，但不能消除行为差异、上游分叉和长期双实现维护成本。

## 12. 三路线横向比较

| 路线 | 重写范围 | 行为一致性风险 | 上游生态成本 | 性能收益潜力 | 稳定性收益 | 长期维护性 |
|---|---|---|---|---|---|---|
| **A 完整原生重写** | 核心、适配器、内置插件、第三方插件、共享库、数据与模板兼容 | 迁移期高；需大量差分测试 | 高：冻结/分叉或持续移植 | 核心与计算路径高，外部成本不变 | 高：可做真正故障域与资源边界 | 原生单栈且边界清晰时高；双实现并存时下降 |
| **B 新核心 + JS 宿主** | 新核心、RPC、Node 宿主、路径/API shim；插件尽量不改 | 很高：对象引用、顺序、全局状态和核心服务跨界 | JS 上游成本较低，但兼容层持续跟随核心变化 | 不确定：核心改善与 RPC 开销并存 | 较高：进程隔离，但状态恢复复杂 | 双运行时和双协议维护成本最高 |
| **C Node + 热点侧车** | 侧车与 Node 薄客户端，插件主体不动 | 低：可逐路径对比和回退 | 最低：原 JS 生态保留 | 局部且可测量 | 对浏览器/计算故障隔离明显 | 核心维护性基本不变，侧车可复用 |

**不是按工作量排序，而是按系统属性排序：** A 用最高的生态与行为验证责任换取原生架构收益；B 用持续双运行时复杂度换取 JS 上游兼容；C 用有限的架构变化换取可验证的局部收益。

## 13. 分阶段路线与验证门禁

### 阶段 0：确认产品与生态决策

- 决定 A/B/C 是否组合，以及目标语言。
- 决定是否必须持续原样跟随 JS 上游，还是允许冻结/分叉/选择性移植。
- 决定动态插件必须采用静态、原生进程、WASM 还是 Node 宿主。
- 决定重复目录、旧协议、远程命令和历史兼容怪癖的处理范围。

**门禁：** 决策记录明确，不能由实施 agent 各自假设。

### 阶段 1：建立基线与契约，不更换核心

- 汇总插件实际加载清单、规则、优先级、任务、Handler、导入和外部进程。
- 为每个适配器录制脱敏事件与 API 响应样本；为每个插件建立至少正常、权限拒绝、异常、边界输入样本。
- 记录消息分发、Redis 命令、渲染、HTTP、SQLite/LevelDB 和进程指标。
- 固化 YAML 合并、事件层级、回复/撤回、上下文、任务和数据格式契约。

**门禁：** 黄金样本可重复；每个迁移契约都有“保留”或“有意改变”的说明；性能报告能区分本地处理与外部等待。

### 阶段 2：实施可复用、可回退的基础改进

- 建立渲染侧车和图片/HTML 差分测试；根据基线决定是否同时做 Redis pipeline。
- 保持 Node 插件 API 不变，用薄客户端和旧实现回退开关验证。
- 该阶段产物可供 A/B 使用；若最终选 C，也可直接作为最终架构的一部分。

**门禁：** 图片与返回消息语义通过；并发、超时、浏览器崩溃和内存上限测试通过；相对旧实现有明确测量结果，无结果不扩展侧车范围。

### 阶段 3A：完整原生重写

1. 先实现核心 schema、配置、日志、调度、数据和一个端到端适配器。
2. 再并行其余适配器、共享 `miao/genshin` 库和渲染服务。
3. 共享库稳定后按插件批次原生翻译；重复目录先由人决定。
4. 用影子流量对比新旧规则命中、回复、Redis/DB 副作用、任务与渲染。
5. 按插件逐个启用，不做没有差分证据的一次性切换。

**门禁：** 每插件黄金行为通过；共享库使用方全部通过；数据可回滚；无非预期上游功能缺失；性能与故障注入达到预定标准。

### 阶段 3B：新核心 + Node 插件宿主

1. 先在 Node 宿主内完成全局对象和相对导入 shim，再连接新核心。
2. 优先实现真实使用频率高的 Bot API，再覆盖 76 项长尾与 Proxy 回退。
3. 验证对象引用、事件顺序、跨插件调用、热更新、任务和宿主重启状态。
4. 分别压测核心本地调用与跨 RPC 调用，确认没有把热点从计算变成序列化等待。

**门禁：** 原插件不修改业务代码即可加载；黄金消息和调用轨迹一致；宿主崩溃策略符合决策；双运行时指标可观测且可独立回退。

### 阶段 3C：持续热点侧车

- 只在测量支持时增加图像、HTTP 或计算侧车。
- 每增加一个侧车，都维持旧路径、契约测试和资源预算。

**门禁：** 每项改动都有独立收益与故障隔离证据；没有收益的侧车不进入长期架构。

### 阶段 4：灰度、回滚与长期上游策略

- 采用只读影子、少量账号/群、单插件灰度，再逐步扩大。
- 数据写入采用双写校验或可重放迁移，禁止不可逆地直接覆盖唯一数据副本。
- 完整原生路线需建立上游 diff → 原生移植 → 黄金测试的固定流程；兼容路线需跟踪 shim API 变化。
- 保留明确回退版本和配置/数据兼容窗口。

**门禁：** 黄金行为全部通过、性能不劣于约定阈值、资源与故障指标受控、回滚演练成功；未达标则不切换。

## 14. 明确不该先动的部分

1. **不要先重写外部协议端。** go-cqhttp、Lagrange、Mirai 等已经是独立进程，核心换语言并不要求它们一起换。
2. **不要先翻译所有静态浏览器资产。** 先保持 HTML/CSS/JS/字体/图片路径和渲染结果一致。
3. **不要直接改写唯一 Redis/LevelDB/SQLite 数据。** 先做格式读取、快照、迁移和回滚验证。
4. **不要先统一删除重复插件目录。** 先确认实际加载需求与数据来源。
5. **不要把 Go/Rust 的默认正则直接替换 JS 正则。** 先做规则语料与兼容性测试。
6. **不要把每个 JS 插件随意拆到不同 Node worker。** 共享全局对象、跨插件导入和上下文会改变行为。
7. **不要在差分测试期间让新旧实现同时无控制地自动更新。** 上游版本漂移会使对比失效。
8. **不要把 sharp 等现有原生库列为“必须换语言才能优化”的证据。** 先测调用与数据拷贝开销。
9. **不要以代码物理行推断工期，也不要以多 agent 为由跳过公共契约。** 并行越多，schema、共享库和测试负责人越重要。

## 15. 最终结论

**完整重写技术上可行，且在不限实施时间、允许多 agent 长期并行的前提下，不应被“工作量大”否决。** 对该项目，完整重写能否成功主要取决于两个选择：

1. 是否愿意把插件生态转为原生实现，并承担冻结、分叉或持续移植上游的责任；
2. 是否用契约与差分测试约束 JS 动态行为，而不是仅完成语法翻译。

若两个条件都成立，**Go 完整重写 + 独立渲染/热点服务**是合理的主推荐；**Rust**适合需要更强 CPU/内存控制的部分；**Python**适合把动态插件加载和翻译便利性放在最高优先级；**TypeScript**则是维护性优先、运行时性能不是主要矛盾时的现实备选。

若必须持续原样跟随 JS 插件上游，**保留 Node 插件宿主是必要条件**。此时应在“新核心 + Node 宿主”的双运行时收益和“Node 核心 + Go/Rust 侧车”的低风险局部收益之间做选择。无论哪条路线，最先建设的都应是**可重复的行为契约与测量体系**，而不是立即替换全部代码。

---

## 附录 A：全部插件目录统计

说明：体积排除 `.git/node_modules`；总源码行包含静态资产；运行时行采用第 2 节启发式；入口数为加载器可识别入口模块数，0 表示本次静态扫描未发现入口。

| 目录 | 全部文件 | 源码文件 | 体积 MiB | 总源码行 | 运行时文件 | 运行时行 | 入口数 |
|---|---:|---:|---:|---:|---:|---:|---:|
| adapter | 7 | 7 | 0.16 | 5665 | 7 | 5665 | 7 |
| agent-plugin | 30 | 22 | 0.10 | 2548 | 22 | 2548 | 1 |
| ark-plugin | 119 | 36 | 17.23 | 5933 | 34 | 5864 | 1 |
| arknights-plugin | 200 | 50 | 70.31 | 8833 | 49 | 8824 | 1 |
| Atlas | 1766 | 10 | 643.62 | 927 | 10 | 927 | 1 |
| bin | 0 | 0 | 0.00 | 0 | 0 | 0 | 0 |
| earth-k-plugin | 983 | 59 | 275.76 | 22497 | 57 | 21685 | 1 |
| earth-k-plugin-tmp | 983 | 59 | 275.76 | 22497 | 57 | 21685 | 1 |
| example | 6 | 5 | 0.02 | 675 | 5 | 675 | 5 |
| FanSky_Qs | 475 | 96 | 86.98 | 62166 | 95 | 62150 | 1 |
| flower-plugin | 546 | 23 | 344.98 | 2924 | 23 | 2924 | 1 |
| genshin | 422 | 57 | 28.02 | 12670 | 54 | 12607 | 1 |
| Guoba-Plugin | 453 | 316 | 11.38 | 10095 | 101 | 8929 | 1 |
| liangshi-calc | 1590 | 790 | 74.36 | 60151 | 781 | 59247 | 1 |
| logier-plugin | 181 | 40 | 36.99 | 5039 | 39 | 5015 | 1 |
| mc-plugin | 50 | 19 | 8.41 | 2019 | 18 | 1995 | 1 |
| memz-plugin | 124 | 55 | 16.80 | 10068 | 54 | 10044 | 1 |
| miao-plugin | 7827 | 506 | 860.71 | 44889 | 122 | 18450 | 1 |
| mora-plugin | 868 | 22 | 553.67 | 2603 | 22 | 2603 | 1 |
| nai-plugin | 55 | 23 | 30.60 | 2185 | 22 | 2161 | 1 |
| other | 5 | 5 | 0.02 | 636 | 5 | 636 | 5 |
| phi-plugin | 1477 | 79 | 912.65 | 101916 | 74 | 15867 | 1 |
| pixiv-plugin | 53 | 32 | 15.84 | 4067 | 32 | 4067 | 1 |
| plugins | 0 | 0 | 0.00 | 0 | 0 | 0 | 0 |
| rconsole-plugin | 108 | 47 | 36.92 | 15436 | 47 | 15436 | 1 |
| San-plugin | 35 | 19 | 1.12 | 3237 | 19 | 3237 | 1 |
| Speech-statistics-plugin | 8774 | 23 | 14.58 | 11363 | 23 | 11363 | 1 |
| StarRail-plugin | 1698 | 54 | 333.77 | 8791 | 52 | 8737 | 1 |
| system | 9 | 9 | 0.02 | 835 | 9 | 835 | 9 |
| TRSS-Plugin | 38 | 16 | 8.07 | 1965 | 16 | 1965 | 1 |
| waves-plugin | 421 | 39 | 207.50 | 6341 | 37 | 6272 | 1 |
| xianxin-plugin | 113 | 26 | 4.25 | 7283 | 25 | 7282 | 1 |
| xitian-plugin | 74 | 17 | 9.78 | 2642 | 17 | 2642 | 1 |
| y-tian-plugin | 2771 | 317 | 288.21 | 164782 | 190 | 92436 | 1 |
| yenai-plugin | 318 | 104 | 53.96 | 15388 | 100 | 14782 | 1 |
| yunzai-plugin-deer-pipe | 31 | 12 | 7.88 | 1023 | 12 | 1023 | 1 |
| Yunzai.TRSS.me | 38 | 16 | 8.07 | 1965 | 16 | 1965 | 1 |
| ZZZ-Plugin | 555 | 221 | 32.80 | 19043 | 218 | 18989 | 1 |

## 附录 B：可复核中间产物

- [规模统计 JSON](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-12/trss-refactor-assessment/work/metrics.json)
- [核心统计 CSV](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-12/trss-refactor-assessment/work/core-metrics.csv)
- [全部插件统计 CSV](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-12/trss-refactor-assessment/work/plugin-metrics.csv)
- [框架 API 扫描 CSV](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-12/trss-refactor-assessment/work/framework-api.csv)
- [Bot 成员扫描 CSV](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-12/trss-refactor-assessment/work/bot-api.csv)
- [包导入扫描 CSV](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-12/trss-refactor-assessment/work/package-imports.csv)
- [插件上游元数据 CSV](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-12/trss-refactor-assessment/work/plugin-upstream.csv)
- [历史日志聚合 JSON](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-12/trss-refactor-assessment/work/log-summary.json)
- [历史渲染耗时 CSV](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-12/trss-refactor-assessment/work/historical-render-durations.csv)
- [只读统计脚本](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-12/trss-refactor-assessment/work/collect-metrics.ps1)
- [只读 API 扫描脚本](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-12/trss-refactor-assessment/work/collect-api-evidence.ps1)
- [只读日志聚合脚本](file:///d:/Toshikoshi_chan/Agent/音无/2026-11-12/trss-refactor-assessment/work/collect-log-metrics.ps1)
