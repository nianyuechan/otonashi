# TRSS-Yunzai 性能与稳定性热点剖析及基线指标方案

- 分析日期：2026-11-12
- 项目版本：`trss-yunzai 3.1.3`
- 项目根目录：`d:\Toshikoshi_chan\Agent\音无\mysy2\mysy2\home\SERVER2\TRSS_AllBot\TRSS-Yunzai`
- 工作方式：只读源码、配置、日志与 RDB 元信息；未启动机器人、`tsab`、Redis 或 Chromium，未压测，未安装依赖，未构建，未执行 Git 写操作。
- 脱敏：报告不保留账号、群号、Cookie、令牌、WebSocket 鉴权信息、原始消息内容或完整 Redis 业务键。

## 1. 结论摘要

1. **最明确的稳定性故障在 OneBot v11 连接初始化。** `get_cookies` 超时后，代码仍直接读取返回值的 `.cookies` 属性；历史日志的异常栈准确指向该处。2026-08-21 日志中有 **438 次同类未处理异常、438 次断开连接、437 次建立连接**。前 89 个可读异常样本的间隔中位数为 **90.534 秒**。这些是协议重连，不是 PM2 进程重启。
2. **Chromium 浏览器会复用，但页面不会复用，且没有并发上限。** 每次渲染都会同步写 HTML、创建新页面、导航、布局并截图；任何一次截图异常都会强制重启共享浏览器。共享临时 HTML 路径、未等待的 `page.close()` 和全局超时重启共同构成并发风险。
3. **现存历史渲染记录可作为参考，但不是当前实测基线。** 85 条单页截图记录的总耗时时长中位数为 **1612 ms**、P95 为 **2438 ms**、最大为 **6968 ms**；样本仅来自现存未压缩日志中可读取的 2025-11-05 至 2026-04-11 记录。
4. **消息统计会放大 Redis 命令数和键数量。** 一条典型群消息在接收方向执行 16 次串行 `INCR`，一次群回复再执行 16 次；日/月/年/总计及 bot/user/group 维度组合形成高基数键，调用点未设置 TTL。计数 Promise 没有加入主消息链路的 `await`，因此主要风险是 Redis 队列、后台错误与键增长，而不是直接阻塞插件匹配。
5. **日志与缓存存在低成本的治理空间。** `error.log` 没有大小/份数轮转配置，现存单文件为 **113,103,313 字节（107.86 MiB）**；临时文件缓存过期后只替换值、不删除键，唯一文件名会持续留下键元数据。
6. **当前启动链是 Node.js，不是 Bun。** `package.json` 的启动脚本指向 `node`/`pm2`，根目录未发现 `bun*` 文件，检查到的启动脚本未调用 `bun`/`bunx`。因此本方案按 Node.js 进程设计；若另有独立 Bun 服务，应单独建指标，不能混入本进程数据。

## 2. 证据等级与边界

| 标记 | 含义 | 本报告示例 |
|---|---|---|
| A：代码/配置事实 | 直接读取当前工作树可确认 | 串行 `await`、PM2 512M、截图默认参数 |
| B：历史文件证据 | 直接读取日志或文件元信息可确认，但不代表当前运行状态 | 438 次异常、85 条渲染样本、RDB 文件大小 |
| C：风险推断 | 由代码结构推导，尚未通过运行时复现 | 同名 HTML 竞争、监听器累积、任务重叠 |

重要限制：

- 最近写入的主要日志和 `dump.rdb` 停留在 **2026-08-21**，早于分析日期；本报告没有把它们描述为“当前线上状态”。
- 未连接运行中的 Redis，未解析 RDB 键内容，因此没有当前键数、命中率或实时内存数据。
- 未完整展开所有压缩日志；部分文本日志含 NUL 字节，搜索工具会提前停止。所有分布均标明样本范围。
- `Main.sh` 是带压缩载荷的编译包装脚本。本次只确认其释放并执行 `Main.sh.x` 的外层行为，没有释放或运行它，内部进程逻辑不在证据范围内。
- 第三方插件数量较多，外部 API 与同步 I/O 检查以核心链路和高影响代表实现为主，不等价于全插件安全审计。

## 3. 只读文件画像

| 项目 | 结果 | 证据 |
|---|---:|---|
| 日志文件总数 | 290 个 | 文件元信息扫描，B |
| 日志总大小 | 135,440,642 字节，约 129.17 MiB | 同上 |
| 日志格式 | 168 个 `.gz`、106 个 `.zst`、16 个 `.log` | 同上；`.zst` 的具体产生者未由当前 log4js 配置确认 |
| `error.log` | 113,103,313 字节，约 107.86 MiB，占日志总量约 83.5% | 同上 |
| 最新日志写入 | `command.2026-08-21.log`，2026-08-21 11:15 | 同上 |
| `dump.rdb` | 22,361,626 字节，约 21.33 MiB；头为 `REDIS0012`；2026-08-21 11:15 写入 | 仅读取文件头与元信息，B |
| `temp-112148.rdb` | 3,006,464 字节，约 2.87 MiB；头为 `REDIS0012`；2026-04-11 写入 | 同上 |

日志配置本身见 [log.js:L14-L56](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/config/log.js#L14-L56)，RDB 数值仅来自文件系统元信息和前 16 字节，不包含键值内容。

## 4. 请求与消息处理链路

### 4.1 端到端路径

```text
OneBot v11 makeMessage
  → 解析消息并记录日志
  → Bot.prepareEvent / Bot.em
  → EventEmitter 的 message/notice/request 监听器
  → PluginsLoader.deal
  → 黑白名单、冷却、消息字段标准化、回复包装、Runtime.init
  → context hook → accept → 逐插件逐 rule 正则匹配
  → await 命中的插件方法
  → e.reply
  → OneBot makeMsg：逐段文件/图片处理
  → 合并转发 → 文件逐个发送 → 普通消息发送
  → sendApi 等待协议端响应；60 秒超时并终止 WebSocket
```

协议入口与事件发射见 [OneBotv11.js:L1070-L1120](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/plugins/adapter/OneBotv11.js#L1070-L1120)，事件展开见 [bot.js:L317-L375](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/bot.js#L317-L375)，监听器到插件分发见 [message.js:L6-L13](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/events/message.js#L6-L13)。

**关键并发语义（A）：** `messageEvent.execute` 调用 `this.plugins.deal(e)` 时没有 `await` 或 `return`，EventEmitter 本身也不等待异步监听器。因此：

- **单条事件内部**按下文所述串行执行。
- **不同事件之间**可以在 `await` 点交错，核心层没有全局消息队列。
- 某个插件自行设置的锁仍可能造成额外串行，这不属于核心层统一行为。

### 4.2 插件匹配与执行

[loader.js:L188-L288](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/plugins/loader.js#L188-L288) 给出主流程：

1. 接收计数、黑白名单和冷却检查。
2. 同步拆分文本、图片、at、回复、文件等字段。
3. 包装 `e.reply`，然后 `await Runtime.init(e)`。
4. 按优先级过滤插件，串行执行上下文 hook。
5. 串行执行各插件 `accept`。
6. 逐插件、逐 rule 执行同步 `reg.test(e.msg)`；命中后创建插件实例并 `await` 插件方法。
7. 插件方法抛错会被记录，当前匹配流程随后返回；返回 `false` 时可继续后续 rule。

规则正则在插件加载时已编译，见 [loader.js:L134-L156](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/plugins/loader.js#L134-L156)，所以成本主要是**遍历、同步正则执行、实例分配和插件方法的串行等待**，不是每次重新编译正则。通用 Handler 也按注册顺序逐个 `await`，见 [handler.js:L39-L60](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/plugins/handler.js#L39-L60)。

**C：性能推断。** 插件数量和规则数量增加会提高未命中消息的匹配成本；但在取得事件分阶段数据前，不应优先重写优先级/插件匹配语义。先做命令前缀或事件类型索引虽然可能提速，却容易改变 accept、优先级和返回值的既有行为。

### 4.3 Redis 在消息路径中的实际位置

接收和发送计数分别由 [loader.js:L188-L200](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/plugins/loader.js#L188-L200) 与 [loader.js:L425-L471](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/plugins/loader.js#L425-L471) 触发。核心实现位于 [loader.js:L474-L488](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/plugins/loader.js#L474-L488)：

- 时间桶为日、月、年、总计。
- 每个时间桶按 total、bot、user、group 组合键。
- 典型群消息且这些 ID 均存在时，每个方向为 `4 × 4 = 16` 个键，逐个 `await redis.incr(...)`。
- 实际配置 `msg_type_count: false`，见 [bot.yaml:L35-L36](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/config/config/bot.yaml#L35-L36)。默认情况下不按每个消息段额外计数；若开启，命令数还会增加。
- 一次接收加一次群回复，典型情况下合计 **32 次 INCR**。
- 外层 `count` 没有被主流程等待，故这些串行 Redis 往返不直接延长 `deal` 的插件匹配等待；但会占用同一 Redis 客户端命令队列。Redis 拒绝时，未等待 Promise 还可能进入全局未处理拒绝日志。

**A：** 单个 `INCR` 是原子操作，但这 16 个操作不是事务；不能据此断言计数已损坏。**C：** 部分成功、后台错误日志和命令排队是应监测的风险。

其他有明确 TTL/语义的键：

- `Yz:chromium:browserWSEndpoint:<设备标识>`：30 天 TTL，见渲染章节。
- `Yz:loginMsg:<bot>`：上线通知冷却键，实际 `online_msg_exp: 1440` 分钟，即 24 小时，见 [connect.js:L12-L24](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/events/connect.js#L12-L24)。
- `Yz:restart`：保存重启状态，读取后删除；写入为 JSON，见 [restart.js:L94-L128](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/plugins/other/restart.js#L94-L128)。

### 4.4 回复与发送的串行环节

`e.reply` 会等待原始发送函数并捕获异常，见 [loader.js:L414-L464](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/plugins/loader.js#L414-L464)。OneBot 发送路径见 [OneBotv11.js:L45-L113](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/plugins/adapter/OneBotv11.js#L45-L113)：

- 消息段逐个处理，含文件的内容逐个等待 `makeFile`。
- Buffer 转 Base64 是同步 CPU/内存操作；该调用的默认大小参数为 10 MiB。
- 先等待合并转发，再逐个等待文件发送，最后等待普通消息发送。
- `sendApi` 的超时为 **60,000 ms**，超时后 reject、记录并 `ws.terminate()`；echo Map 在 `finally` 中清理，见 [OneBotv11.js:L10-L43](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/plugins/adapter/OneBotv11.js#L10-L43)。

因此“用户感知回复延迟”必须包括文件获取/Base64、协议 API 响应和可能的渲染，而不能只统计插件函数耗时。

## 5. Puppeteer / Chromium 渲染链路

### 5.1 配置与浏览器复用

渲染器默认选择 `puppeteer`，见 [loader.js:L27-L60](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/renderer/loader.js#L27-L60)。项目中不存在 `renderers/puppeteer/config.yaml`，所以该加载器不会读取 `config_default.yaml` 作为运行配置；后者是复制配置的示例。当前代码默认值位于 [puppeteer.js:L15-L46](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/renderers/puppeteer/lib/puppeteer.js#L15-L46)：

| 参数 | 当前默认/实际配置 |
|---|---|
| `headless` | `"new"` |
| 启动参数 | `--disable-gpu`、`--disable-setuid-sandbox`、`--no-sandbox`、`--no-zygote` |
| `puppeteerTimeout` | 当前 bot 配置为空，回退为 `0`，即该全局截图定时器不启用 |
| `page.goto` | 默认 `timeout: 120000`、`waitUntil: "networkidle2"`；调用方可覆盖 |
| 浏览器重启计数 | 每 100 次截图检查一次；队列非空时推迟 |
| 页面并发上限 | **未见上限或排队信号量** |

`TRSS-Yunzai.sh` 会设置 `PUPPETEER_EXECUTABLE_PATH`，见 [TRSS-Yunzai.sh:L7-L10](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/TRSS-Yunzai.sh#L7-L10)。因此实际 Chromium 路径还受启动方式和环境影响，不能仅凭 YAML 断言。

浏览器初始化见 [puppeteer.js:L48-L119](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/renderers/puppeteer/lib/puppeteer.js#L48-L119)：

1. 已存在 `this.browser` 时直接复用。
2. 否则尝试从 Redis 读取 WebSocket Endpoint 并 `puppeteer.connect`；失败则删除该键。
3. 无可用实例时 `puppeteer.launch`。未固定 `userDataDir` 时会先递归删除 `temp/puppeteer`，再创建 ULID 子目录。
4. 新实例 Endpoint 写入 Redis，TTL 为 30 天。
5. `disconnected` 事件触发强制重启。

**结论：不是每次渲染都启动浏览器。** 成本分为冷启动/重连与温浏览器渲染两类，基线必须分开统计。另一个边界是：初始化锁存在时，`browserInit` 直接返回 `false`，不是等待初始化完成，见 [puppeteer.js:L51-L55](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/renderers/puppeteer/lib/puppeteer.js#L51-L55)。

### 5.2 每次渲染的成本来源

单次截图流程见 [puppeteer.js:L159-L277](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/renderers/puppeteer/lib/puppeteer.js#L159-L277)，模板处理见 [Renderer.js:L38-L68](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/renderer/Renderer.js#L38-L68)。

| 阶段 | 代码事实 | 成本性质 |
|---|---|---|
| 模板读取 | 首次模板 `readFileSync`，之后缓存；文件变化时清除缓存 | 冷缓存同步磁盘 I/O |
| 模板渲染与写入 | 每次执行 `template.render`，每次 `writeFileSync` | 同步 CPU + 每次同步磁盘 I/O |
| 页面 | 每次 `browser.newPage()`，没有页面池 | CDP 调用、新渲染目标 |
| 导航 | `page.goto(file://...)`，默认等待 `networkidle2` | 本地文件及模板引用的子资源、网络安静等待 |
| 布局 | 查询 `#container` 或 `body`，再读取 `boundingBox` | 样式、字体、布局 |
| 截图 | 默认 JPEG、quality 90；PNG 时删除 quality | 光栅化与编码，受实际像素面积影响 |
| 多页 | 默认每页高度 4000；滚动、改 viewport、逐页截图；页数大于 2 时每次循环还等待 200 ms | 多次布局/截图/编码与固定等待 |
| 清理/重启 | 成功后调用 `page.close()` 但不等待；每 100 次检查重启；任何异常强制重启共享浏览器 | 页面回收、周期性冷启动、并发故障扩散 |

尺寸方面，普通截图使用 `#container/body` 的实际 bounding box；代码未显式固定 `deviceScaleFactor`、`fullPage` 或 `clip`。多页时才把 viewport 设为内容宽度和 `pageHeight + 100`，最后一页设为剩余高度，见 [puppeteer.js:L192-L255](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/renderers/puppeteer/lib/puppeteer.js#L192-L255)。因此不能从静态代码给出一个统一像素尺寸，运行基线应记录宽、高、页数、输出字节数。

### 5.3 历史渲染样本（B）

统计口径：现存 `command.*.log` 中同时含图片大小和总耗时的单页成功记录；不将多页分片伪造成完整请求，不将历史样本当作当前线上测试。

| 指标 | 样本值 |
|---|---:|
| 样本数 | 85 |
| 文件日期范围 | 2025-11-05 至 2026-04-11 |
| 最小 / 中位数 / 均值 | 864 / 1612 / 1748 ms |
| P90 / P95 / P99 / 最大 | 2301 / 2438 / 6968 / 6968 ms |
| 图片大小中位数 / P95 / 最大 | 249.15 / 1609.11 / 1865.90 KiB |

分模板示例：`UserCard` 20 条，中位数 1511 ms、P95 2357 ms；`miao-plugin/character/profile-detail` 13 条，中位数 1719 ms、P95 1839 ms；`miao-plugin/help/index` 10 条，中位数 1206 ms，但 P95 达 4949 ms。样本证明“模板类型和长尾”必须进入标签与分位数统计，单看全局平均值会掩盖问题。

历史 `error.log` 还出现导航 20 秒超时和 `Target closed`，例如 [error.log:L55332-L55351](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/logs/error.log#L55332-L55351)。20 秒是历史调用覆盖或当时配置的表现，不是当前 120 秒默认值。

### 5.4 并发与生命周期风险

- **C：同名 HTML 竞争。** 默认路径是 `./temp/html/${name}/${saveId}.html`，`saveId` 默认等于 `name`。两个同名渲染可先后覆盖同一路径，而写文件与 `page.goto` 之间存在异步页面创建步骤，见 [Renderer.js:L39-L67](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/renderer/Renderer.js#L39-L67) 和 [puppeteer.js:L159-L189](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/renderers/puppeteer/lib/puppeteer.js#L159-L189)。结果为错图的风险尚未在本机复现。
- **A/C：故障扩散。** 任一截图异常都会强制关闭共享浏览器；若启用全局截图定时器，它也会重启浏览器并清空整个 `shoting` 数组。其他进行中页面可能因此失败，历史 `Target closed` 与该风险一致，但日志本身不能单独证明每次关闭的调用者。
- **A：页面关闭不可靠。** 成功路径没有 `await page.close()`；异常路径没有独立的页面 `finally close`，主要依赖浏览器重启。
- **A/C：队列计数不准确。** `shoting` 用 `push(name)` 和 `pop()` 管理，并发完成顺序不保证 LIFO，可能移除错误名称；它也没有容量限制。
- **A/C：调用覆盖会残留。** `lodash.extend(this.pageGotoParams, data.pageGotoParams || {})` 会修改共享默认对象，某次调用的覆盖可能持续影响后续渲染。

## 6. 资源管理

### 6.1 进程模型与重启

[pm2.yaml:L1-L7](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/config/pm2.yaml#L1-L7) 只声明一个应用条目：

- `script: ./app.js`
- `max_memory_restart: 512M`
- `exp_backoff_restart_delay: 1000`
- `env.app_type: pm2`

`instances`、`exec_mode`、`max_restarts`、`kill_timeout` 等未显式配置，不能仅凭该文件断言集群数量或其他 PM2 运行值；应在未来已授权运行的实例上用 `pm2 jlist` 读取。`app.js` 另提供 daemon 循环，使用同步 `spawnSync` 等待子进程退出并决定是否重启，见 [app.js:L9-L29](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/app.js#L9-L29)。项目 shell 还包含外层循环和 `node app start`，见 [TRSS-Yunzai.sh:L1-L11](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/TRSS-Yunzai.sh#L1-L11)。不同启动方式不能混为同一进程树。

512M 是该 PM2 应用的内存重启阈值，**不是 Node + Chromium + Redis/Dragonfly 的总内存上限**。Redis 可由脚本单独启动，Chromium 也是独立进程，基线必须分别采集。

Node/Bun 事实见 [package.json:L8-L16](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/package.json#L8-L16)：`start/stop/restart` 均调用 PM2，开发入口为 `node . dev`。未发现 Bun 启动声明。

### 6.2 Redis 连接与键空间

[redis.js:L8-L21](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/config/redis.js#L8-L21) 初始化一个挂载到 `global.redis` 的 node-redis 客户端。连接错误时会断开并重连；本地地址连接失败时可启动配置的 Redis 进程，见 [redis.js:L37-L100](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/config/redis.js#L37-L100)。本次没有执行这些路径。

键空间特征：

- `Yz:count:*` 是最高基数的已确认模式，时间桶与 bot/user/group 交叉增长，调用点没有 TTL。
- `Yz:chromium:*` 为浏览器 Endpoint，30 天 TTL。
- `Yz:loginMsg:*` 为上线通知冷却，当前 24 小时 TTL。
- `Yz:restart` 是单次状态键，读取后删除。

RDB 文件只证明落盘体积和格式版本，不能据此给出键数量、各前缀比例或命中率。

### 6.3 定时任务数量与频率

插件加载器收集 `plugin.task`，按“名称 + cron”跳过重复项，再用 `node-schedule` 创建任务，见 [loader.js:L490-L535](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/plugins/loader.js#L490-L535)。

本次扫描范围内得到 **30 个 cron 候选点**。复核发现其中包含块注释、miao 基类通用字段，以及给 `task.push` 赋值而非调用它的非有效调度代码；同时还存在条件开关和动态值。典型噪声见 [Guoba update.js:L41-L55](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/plugins/Guoba-Plugin/apps/update.js#L41-L55) 与 [memz puppeteer.js:L79-L87](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/plugins/memz-plugin/apps/puppeteer.js#L79-L87)。此外，`other/update.js`、`other/restart.js` 的 4 个候选点可按配置数组生成 0..N 个任务。因此 **30 只是静态候选数，不能换算成实际加载数或去重后的调度数**。未来应在已运行实例内读取 `Object.keys(schedule.scheduledJobs).length` 并保留名称与 cron 清单，作为实际调度数。

最近日志的可读片段中实际出现 9 种任务调度名：

| 任务 | cron | 名义频率 |
|---|---|---|
| FanSky_Qs 仓库更新检测 | `0 0/4 * * * ?` | 每 4 分钟 |
| ZZZ 自动检测更新 | `0 0/10 * * * ?` | 每 10 分钟 |
| ZZZ 防卫战/强袭战提醒 | `0 */10 * * * ?` | 每 10 分钟 |
| memz 定时检查仓库更新 | `0 */15 * * * *` | 每 15 分钟 |
| 清除打卡状态 | `0 0 0 * * *` | 每日 00:00 |
| 点赞任务 | `0 0 0 * * ?` | 每日 00:00 |
| 森空岛签到 | `0 2 0 * * ?` | 每日 00:02 |
| Waves 自动签到 | `0 10 0 * * ?` | 每日 00:10 |
| Waves 自动任务 | `0 10 0 * * ?` | 每日 00:10 |

代表定义见 [MonitorTask.js:L25-L32](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/plugins/FanSky_Qs/apps/MonitorTask.js#L25-L32)、[remind.js:L78-L82](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/plugins/ZZZ-Plugin/apps/remind.js#L78-L82) 和 [UpdateTask.js:L25-L29](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/plugins/memz-plugin/apps/UpdateTask.js#L25-L29)。9 种是日志下界，不是加载总数。

核心自动更新另有递归 `setTimeout`：实际 `update_time: 1440` 分钟、`restart_time: 0`，`update_cron` 和 restart/stop/start cron 为空，见 [bot.yaml:L14-L25](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/config/config/bot.yaml#L14-L25)。自动更新在上一次 Promise `finally` 后才注册下一次，见 [update.js:L42-L67](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/plugins/other/update.js#L42-L67)，这一路径不会与自身按固定间隔重叠。

**C：任务重叠风险。** `startTask` 虽会 `await i.fnc()`，但任务对象没有 running 锁、跳过策略或执行超时；对于执行时间超过 cron 周期的任务，代码层未见防重叠机制。应通过 `task_overlap_total` 实测，而不是直接宣称已发生重叠。

### 6.4 日志写入

[log.js:L14-L56](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/config/log.js#L14-L56) 的事实是：

- `command`：log4js `dateFile`，按日轮转，`numBackups: 180`，`compress: true`。
- `error`：普通 `file` appender，固定写入 `logs/error.log`，未配置大小或份数轮转。
- error 类别同时进入 `stderr`、`command` 和 `error` 三个 appender；同一故障会形成多路输出和堆栈放大。

现存文件数/大小是历史状态，不能仅根据 `numBackups` 推断所有旧文件的清理方式。

## 7. 稳定性风险清单

### 7.1 已有日志证明的故障

#### OneBot 初始化异常与重连循环

`sendApi` 超时为 60 秒并终止 WebSocket；初始化又连续等待登录信息、频道信息、在线客户端、版本、Cookie 和 CSRF 等请求。Cookie 部分包括首个域和最多 18 个后续域的串行请求，见 [OneBotv11.js:L1012-L1057](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/plugins/adapter/OneBotv11.js#L1012-L1057)。多个 `.catch(i => i.error)` 后直接读取 `.data`、`.cookies`、`.clients` 或 `.token`，缺少统一的失败结果检查。

2026-08-21 日志中：

- `get_cookies` 请求超时后，在 `.cookies` 处抛出 TypeError；首处栈见 [command.2026-08-21.log:L13-L26](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/logs/command.2026-08-21.log#L13-L26)。
- 同类未处理异常计数 **438**。
- “断开连接” **438**，“建立连接” **437**。
- 前 89 个可读异常覆盖 00:00:18.703 至 02:13:05.540，间隔中位数 90.534 秒、P95 90.545 秒；全文件计数与可读文本样本分开统计。

这构成代码与日志相互印证的热点。串行初始化还放大了每个协议请求的等待时间；其精确总时长需在修复前基线中按 action 分段测量。

#### 全局异常处理不会主动退出进程

[init.js:L28-L37](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/config/init.js#L28-L37) 对 `uncaughtException` 和 `unhandledRejection` 主要调用 `Bot.makeLog("error", ...)`；只有日志处理再次失败时才退出。因此上述 438 次异常不能直接解释成 438 次 PM2 重启。继续运行可避免单次错误直接终止进程，但也可能保留部分初始化状态，必须依靠指标和修复源头，而不是简单改成“所有异常都退出”。

历史日志还出现外部 `fetch failed`、响应头超时进入 `unhandled` 的记录，见 [error.log:L185-L230](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/logs/error.log#L185-L230)。这说明至少有外部请求失败突破了局部错误边界；具体调用来源应结合对应时间段的完整栈另行定位。

### 7.2 有保护但实现不同的外部 API

Milky HTTP 实现使用 `AbortController`，实际默认 `http_timeout` 为 15 秒，可配置；它处理非 2xx、JSON 解析错误和中断，并在 `finally` 清理定时器，见 [Milky.js:L365-L421](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/plugins/adapter/Milky.js#L365-L421) 与 [Milky.js:L17-L20](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/plugins/adapter/Milky.js#L17-L20)。OneBot 则采用 60 秒 echo 超时和断开连接。这两个实现均未在该函数中看到自动重试，不能外推到所有第三方插件。

### 7.3 缓存、监听器与并发写入

| 对象 | 代码事实 | 风险判断 |
|---|---|---|
| 临时文件缓存 `Bot.fs` | 默认 1 分钟后把 `this.fs[file.name]` 替换成 timeout 对象，而不是 `delete`； unnamed 文件使用 ULID 名称，见 [bot.js:L275-L315](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/bot.js#L275-L315) | **C：键元数据无界增长。** Buffer 引用会被释放，但唯一键名会保留；同名文件还需防止旧定时器覆盖新值 |
| OneBot `request_list` | 每次请求事件 `push(data)`，本文件只见 getter，未见容量/TTL/删除，见 [OneBotv11.js:L983-L985](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/plugins/adapter/OneBotv11.js#L983-L985) 与 [OneBotv11.js:L1441-L1446](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/plugins/adapter/OneBotv11.js#L1441-L1446) | **C：长连接期间数组可增长。** 重连创建新 bot 对象会重置它，因此不能称为跨重连永久泄漏 |
| 对话文本监听 | `getTextMsg` 注册 `Bot.on("message")`，只在收到匹配文本后移除，没有超时或取消信号，见 [bot.js:L517-L539](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/bot.js#L517-L539) | **C： abandoned conversation 可累积监听器和闭包。** 需用 listenerCount 验证 |
| OneBot echo Map | 每个请求设置超时，Promise `finally` 删除 echo，见 [OneBotv11.js:L17-L43](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/plugins/adapter/OneBotv11.js#L17-L43) | 已有明确清理，不应列为无界缓存 |
| 通用事件等待 | `promiseEvent` 在 `finally` 中解绑并清定时器，见 [util.js:L393-L409](file:///d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai/lib/util.js#L393-L409) | 已有明确清理 |
| 渲染 HTML | 同名默认路径，同步写后还有异步步骤才导航 | **C：并发写同一文件及错图风险。** 静态分析未证明实际发生 |
| Redis 计数 | 单键 INCR 原子，多键循环非事务 | 不一概视为并发写错误；重点监测部分完成和后台拒绝 |

**没有直接证据时不宣称内存泄漏。** RSS 增长、缓存键增长和监听器增长必须分开测量。

## 8. 可测量基线指标方案

### 8.1 采集原则

1. **本阶段不启动本机机器人。** 以下方案只在以后获得授权、实例已经正常运行时实施，或在独立测试环境实施；不通过压测凑样本。
2. **先外部只读采集，再少量埋点。** PM2/Redis/文件元信息可先采集；完整事件链路需要在后续单独变更中增加时间戳和指标输出。
3. **正常流量至少覆盖 7 天。** 保留工作日/周末、冷/温浏览器、渲染/非渲染、不同模板和不同任务；样本不足时只报告样本量，不伪造 P99。
4. **统一单调时钟。** 单段耗时使用 `performance.now()`；跨日志关联同时保存 wall-clock ISO 时间和随机 `event_id`。
5. **低基数标签。** 只使用 `adapter`、`event_type`、`plugin`、`rule`、`template`、`result`、`error_class`、`cold_start` 等受控标签；不得把 QQ、群号、用户 ID、echo、完整 URL、原始 Redis 键或消息正文放入指标。
6. **同时保留绝对与相对阈值。** 下表是首轮告警起点，不是承诺 SLA。延迟稳定后可采用“连续 3 个 5 分钟窗口超过 `max(初始阈值, 7 日基线 P95 × 1.5)`”的劣化告警。

### 8.2 指标、采集方式与建议阈值

| 指标 | 定义与标签 | 采集方式 | 首轮建议阈值 |
|---|---|---|---|
| `event_to_plugin_ms` | 协议入口到命中插件方法入口；按 adapter/event_type/plugin | OneBot `makeMessage`、loader `deal`、插件调用前打时间戳 | 非渲染 P95 > 1500 ms 或 P99 > 5000 ms，连续 3 窗口 |
| `event_to_first_reply_ack_ms` | 协议入口到第一条发送 API 成功/失败返回；按 render/non-render、result | 在事件对象携带只读时间戳，`sendApi finally` 记录；无回复事件不计 | 非渲染 P95 > 2000 ms；渲染 P95 > 8000 ms；失败单独计数 |
| `plugin_exec_ms` | 插件函数进入到返回；按 plugin/rule/result | 当前 loader 已有开始/完成日志，可改为结构化直方图 | 先建基线；日 P95 较基线上升 > 50% 告警 |
| `plugin_match_ms` | 进入 accept 前到 rule 匹配结束；按 event_type、是否命中 | loader 内单独计时，记录扫描插件数/rule 数 | P95 > 250 ms 或扫描数持续上升 > 30% 时评估索引 |
| `render_total_ms` | 进入 screenshot 到 Buffer 返回；按 template、multiPage、cold_start、result | 复用现有总耗时并结构化；保留 P50/P90/P95/P99/max | 温浏览器 P95 > 5000 ms、P99 > 15000 ms；冷启动单列 |
| `render_phase_ms` | browser_init、template、new_page、goto、bounding_box、screenshot、close、restart 分阶段 | Puppeteer 内埋点；每阶段不记录用户数据 | 任一阶段日 P95 较基线上升 > 50%；goto 长期占主导时再改等待策略 |
| `render_active_pages` / `render_queue_ms` | 实际打开页面数、等待信号量的时间 | 页面创建/关闭计数；加入并发限制后记录队列 | active pages 不超过设定上限；queue P95 > 2000 ms 警告 |
| `render_failures_total` / `browser_restarts_total` | 按 timeout/target_closed/navigation/launch/disconnected 等原因分类 | 错误分类计数，不导出原始堆栈到指标标签 | 渲染失败率 > 1%、超时率 > 0.1%；非计划浏览器重启 > 3/h |
| `node_rss_bytes` / `heap_used_bytes` / `external_bytes` | Node 主进程内存；同时记录 Chromium 子进程 RSS | `process.memoryUsage()` 每 30 秒；PM2/OS 每 60 秒采集进程树 | Node RSS > 410 MiB 持续 5 分钟警告，> 460 MiB 严重；对应 512M 的约 80%/90% |
| `eventloop_delay_ms` | 事件循环延迟直方图 | `perf_hooks.monitorEventLoopDelay()`，每 30 秒重置并聚合 | P99 > 100 ms 警告，> 250 ms 严重 |
| `heap_growth_slope` | 预热后每小时 heapUsed 中位数趋势；与缓存/监听器数量联合判断 | 至少 6 小时滚动窗口；不在生产强制 GC | > 5 MiB/h 持续 6 小时且缓存/监听器同步增长，进入堆快照复核 |
| `bot_fs_keys` / `request_list_length` / `message_listeners` / `echo_map_size` | 关键缓存和监听器基数 | 定期只读计数；按 bot 聚合，不导出 ID | `bot_fs_keys` 在文件发送停止后仍单调增长即告警；其他项超过 7 日同负载 P95 的 1.5 倍 |
| `redis_keyspace_hit_ratio` | `keyspace_hits / (hits + misses)`；同时保存原始分子分母 | 已运行实例 `INFO stats` 每 60 秒；零命中窗口记 N/A | 不设通用 99% 门槛；1 小时均值比自身基线下降 > 10 个百分点告警，读缓存业务可暂用 85% 下限 |
| `redis_db_keys` / `redis_prefix_keys` | 当前 DB 与 `Yz:count`、登录、渲染等前缀键数 | `INFO keyspace`/`DBSIZE`；`SCAN COUNT 500..1000` 在低谷或副本聚合，禁止 `KEYS`；只导出前缀计数 | 同负载周同比增长 > 20%，或日增长 > 10% 连续 3 天；TTL 策略变更前先做保留期评审 |
| `redis_used_memory` / `rdb_last_bgsave_status` / `rdb_size_bytes` | Redis 内存、落盘状态和 RDB 增长 | `INFO memory/persistence` 每 60 秒；文件元信息每日 | bgsave 失败 > 0 立即告警；RDB 同周期增长 > 50% 或超过自身基线 2 倍复核 |
| `pm2_restarts_total` | PM2 `restart_time` 的单调差分；进程启动代次单独标记 | 已运行实例 `pm2 jlist` 每 60 秒，仅保留 name/status/restart_time/uptime/memory/cpu，过滤 env | 非计划重启 ≥ 1/天警告，≥ 3/小时严重；与 512M 内存阈值关联 |
| `adapter_reconnects_total` / `api_timeout_total` | 协议重连与 action 超时；按 adapter/action/error_class | WebSocket connect/disconnect 和 sendApi 结构化计数 | 稳态重连 > 5/h 警告、> 20/h 严重；API 超时率 > 0.1% |
| `task_exec_ms` / `task_overlap_total` | 每次计划任务耗时及同任务并发实例数 | `startTask` 增加 running gauge、超时和重叠计数 | 同任务重叠 > 0；P95 执行时长 > cron 周期的 80% |
| `error_signature_rate` / `error_log_bytes` | 归一化错误签名的速率与日志体积 | 本地日志聚合，签名去变量并脱敏；文件元信息每日 | 同签名 > 10 次/分钟警告；单 error 文件 > 100 MiB 或日增量 > 50 MiB |

### 8.3 具体采集落地顺序

**阶段 0：现有文件，已完成。**

- 建立日志体积、RDB 大小、历史渲染分布和错误签名样本。
- 结论只作先验参考，不替代运行基线。

**阶段 1：已运行实例的外部只读数据，未来授权后执行。**

- 每 60 秒读取一次 PM2 选择性字段；不要原样导出包含环境变量的完整 `jlist`。
- 每 60 秒读取 Redis `INFO`；每小时或低谷期在副本上用 `SCAN` 聚合前缀。原始键只在本地计数，不进入报告。
- 每日记录日志/RDB 文件元信息；保留文件时间和分析时区。
- 这一步仍无法得到完整事件到回复延迟，需阶段 2。

**阶段 2：最小结构化埋点，作为后续独立改动。**

- 在 OneBot 入口、loader、sendApi、Puppeteer 各阶段添加 `event_id` 和单调时间戳。
- 输出 JSONL 或 Prometheus 指标；Prometheus label 使用白名单，堆栈只进脱敏日志。
- 对渲染记录 `width`、`height`、`pages`、`bytes`、`cold_start`，以区分“大图成本”和“等待成本”。
- 对内存同时记录 Node、Chromium 和 Redis，避免 512M 单进程阈值掩盖进程树增长。

**基线报告必须包含：** 样本量、时间范围、版本/配置摘要、流量区间、P50/P90/P95/P99/max、失败率、冷温分组、采集缺口。优化前后只在相同模板、相近流量和相同启动方式下比较。

## 9. 按收益/改动成本排序的前五项

排序同时考虑证据强度和故障影响，不是未经测量的精确 ROI 数值。

| 排名 | 优化点 | 预期收益 | 改动成本 | 验收指标 |
|---:|---|---|---|---|
| 1 | **修复 OneBot 初始化失败处理，并缩短串行初始化。** 对每个 API 返回先验证，再读字段；Cookie/元数据按依赖拆分，独立请求采用有上限的并发；超时后避免无界重连循环 | 很高：直接对应 438 次异常和持续断连，减少重复初始化与日志放大 | 低到中 | cookies 属性 TypeError 降为 0；协议重连回到自身基线；初始化各 action 耗时可解释 |
| 2 | **隔离每次渲染并建立页面/浏览器生命周期边界。** 唯一临时文件或改用内存 HTML；加入小型并发上限；`await page.close()` 且放入 finally；按单次任务超时；只对致命故障重启共享浏览器；修复 `shoting` 和共享参数修改 | 很高：降低错图、Target closed、页面堆积及故障扩散，兼具稳定性与延迟收益 | 中 | 渲染失败率 < 1%；active pages 有界；Target closed 不再集中出现；P95/P99 对比同模板基线 |
| 3 | **给错误日志加轮转和错误签名限流。** 保留首次完整堆栈，后续相同签名按窗口聚合计数；command/error 仍保持必要级别 | 高：减少磁盘、控制台/文件输出和故障期额外开销；现有 error 单文件已超 100 MiB | 低 | error.log 日增量、同签名速率、故障期间 eventloop delay；不丢失首次诊断信息 |
| 4 | **将消息统计改为 pipeline/批量提交，并制定计数键保留期。** 保留原子 INCR 语义；日/月键 TTL 需先确认统计查询需求 | 中到高：典型群消息收发可减少 32 次独立往返，抑制键基数增长 | 低到中 | Redis 命令 RTT/事件、命令队列、`Yz:count` 键数/RDB 增长；计数与旧口径对账一致 |
| 5 | **给缓存和监听器加确定性生命周期。** 文件缓存到期删除且校验代次；request_list 设容量/消费删除；getTextMsg 增加超时/取消和 finally 解绑 | 中到高：降低长期运行内存与监听器累积风险，改动点明确 | 低到中 | `bot_fs_keys` 不再单调残留；监听器/request_list 在同负载下回落；内存曲线不单独作为唯一判据 |

下一候选而非当前前五：

- 将 `networkidle2` 改为模板显式 ready 信号，并按阶段数据决定图片尺寸、quality 和多页策略；应先确认 goto 占比，避免为缩短等待而截断字体/图片。
- 为计划任务增加 running 锁、重叠计数、超时和错峰；先验证是否实际重叠。
- 插件匹配索引化暂缓，先采集 `plugin_match_ms` 和扫描规则数，避免高成本重写却改变既有语义。

## 10. 最终判断

当前最有把握的收益不是笼统“换运行时”或“重写插件系统”，而是：

1. 消除 OneBot 可复现于历史日志的初始化异常与重连放大。
2. 给共享 Chromium 和同名模板建立并发边界。
3. 控制错误日志、Redis 计数键和长期缓存的资源增长。
4. 用分阶段、分模板、分冷温的指标验证后续优化。

Bun 并未出现在已检查的启动链中，因此“迁移 Bun”不是本次证据能支持的性能结论。即使未来单独评估 Bun，也应先用本报告基线确认瓶颈位于 Node 运行时，而不是协议等待、Chromium、磁盘日志或 Redis 键设计。
