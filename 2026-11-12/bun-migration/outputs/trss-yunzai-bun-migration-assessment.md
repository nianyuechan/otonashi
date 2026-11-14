# TRSS-Yunzai 迁移到 Bun 运行时的可行性评估

评估日期：2026-11-12  
评估对象：Windows 工作副本中的 `TRSS-Yunzai` 主实例  
本机对照环境：Windows x64、Bun 1.4.2、Node.js 24.10.0  
项目根目录（下文记为 `<ROOT>`）：`d:\Toshikoshi_chan\Agent\音无\mysy2\mysy2\home\SERVER2\TRSS_AllBot\TRSS-Yunzai`

## 结论摘要

**结论：核心程序具备用 Bun 做受控试验的条件，但“代码基本不改、直接把 node 换成 bun”不能据此判定为全插件可行，不应未经验收就替换生产 Node.js。**

最可行的路径不是同时更换运行时、包管理器和进程管理器，而是：

1. **继续由 pnpm 管理依赖**，不急着执行 `bun install` 或迁移锁文件。
2. **先绕过编译后的总控 `Main.sh` 和 PM2**，在隔离副本中显式执行 `bun app.js start`。
3. **分批启用插件**，先验证核心 HTTP/WebSocket、Redis、Level、SQLite 与纯 JS 插件，再验证 Sharp、Puppeteer 和高风险插件。
4. **保留 Node.js 回退路径**，将 Puppeteer 渲染、`img2pdf/hummus`、依赖 Node V8 序列化的代码，以及 Guoba 的 Windows 热重载列为单独处理项。

关键证据如下：

- 项目根包是 ESM，核心插件加载采用相对路径动态 `import()`；Bun 1.4.2 的隔离探针证明 `#imports`、`createRequire`、相对路径 query 热重载、`process.report.getReport()` 和 `process.execve` 均可用。
- `level/classic-level`、`@karinjs/sqlite3`、`sharp@0.33.5`、`sharp@0.34.5`、`bufferutil` 都通过了本机 Bun 的最小实际调用，不能把这些 N-API 包一概判为不兼容。
- PM2 官方支持 Bun，但现有 `config/pm2.yaml` 未指定解释器；并且 `app.js` 使用顶层 await，PM2 + Bun 仍有相关开放 issue。[cite:2][cite:18]
- `bun pm migrate` 在复制到实验区的完整清单上因 4 个 `link:lib/modules/*` 依赖失败，对应 Bun 的开放 issue。[cite:15]
- Guoba 在 Windows 使用 `file:///…?版本号` 刷新模块，命中 Bun 丢弃 `file:` URL query 的开放 issue；核心加载器的相对路径写法不受同一问题影响。[cite:8]
- `yenai-plugin` 使用 `v8.deserialize` 读取 Node/V8 数据。Bun 官方说明其序列化格式是 JavaScriptCore 而非 V8；本机实验中，Node 生成的数据在 Bun 反序列化时抛出 `TypeError: Unable to deserialize data.`。[cite:1]
- `pixiv-plugin → img2pdf → hummus` 是额外的原生依赖链。当前副本中 Node 和 Bun 都因缺少 `binding/hummus.node` 而无法加载；同时 Hummus 源码直接使用 V8/`node::ObjectWrap`，属于 Bun 尚在补齐的 V8 C++ API 区域，重新构建后也不能默认兼容。[cite:19]

## 1. 范围、方法与证据等级

### 1.1 范围

本报告只评估 `TRSS-Yunzai` 主实例。同目录的 Miao-Yunzai、LittlePaimon、Zhenxun、Liteyuki、Amiya、Sagiri、Yunzai、ZeroBot 等框架未逐一审计，不能共享本报告的兼容性结论。

执行过程中未修改、删除或重命名项目文件，未在项目目录安装依赖、构建、启动机器人或 `tsab`，未执行 Git 写操作。所有实验均位于：

`d:\Toshikoshi_chan\Agent\音无\2026-11-12\bun-migration\work\`

总控 `Main.sh` 是带嵌入压缩 payload 的编译/自释放脚本，只能确认其释放并执行 `$FILE.x` 的外层行为，内部启动逻辑不在本次可审计源码范围内。因此，报告中的“直接 Bun 入口”针对 `TRSS-Yunzai` 实例，不代表已验证总控脚本。

### 1.2 证据等级与状态含义

| 标记 | 含义 |
|---|---|
| 可用（探针） | 在本机 Bun 1.4.2 上执行过最小真实调用，不等于完整业务验收 |
| 可用（官方）/可用（推断） | 官方明确支持，或根据实际调用方式判断不触及已知限制；推断会单独注明 |
| 有坑 | 有官方 issue、配置差异、平台差异或未覆盖的业务路径，需要绕开或专项验收 |
| 无法原样完成 | 特定操作在当前版本或当前数据格式下失败，不表示整个项目永远不能迁移 |
| 未验证 | 没有足够证据下结论，尤其包括完整机器人、外部服务和非 Windows 平台 |

### 1.3 本地隔离实验

| 实验 | 内容 | 结果 |
|---|---|---|
| E1：运行时 API | Bun/Node 分别执行 `runtime-probe/probe.js` | Bun 的 `#imports`、`createRequire`、相对 query、`process.report`、`process.execve`、顶层 await 均通过；`file:` URL query 与 Node 不同 |
| E2：原生模块 | 通过实际 `.pnpm` 路径调用现有包，不写项目目录 | Level 打开/读写/迭代/关闭通过；SQLite 内存查询通过；两个 Sharp 版本写 PNG 并读取 metadata 通过；bufferutil mask/unmask 通过 |
| E3：V8 数据 | Node 序列化，Bun 反序列化 | Bun 抛出 `TypeError: Unable to deserialize data.` |
| E4：pnpm 迁移 | 在 `work/pnpm-migration-probe` 复制根及插件清单，执行不安装包的 `bun pm migrate` | 因相对 `link:` 依赖报不支持并以 `InvalidLockfile` 退出；仅把副本中的 `link:` 改成 `file:` 仍因 lock 缺少对应 package entry 失败 |
| E5：Hummus | Node 与 Bun 分别加载现有 Hummus | 两者均因 `binding/hummus.node` 不存在而失败，不能把该副本的既有缺失误判为 Bun 独有故障 |

E2 初次经由表层 `node_modules/<pkg>` 路径加载时，Node 与 Bun 都遇到相同的传递依赖解析失败，因此改用实际 `.pnpm` 包路径。该现象属于当前副本的包链接布局，未作为 Bun 兼容性证据。

## 2. 启动方式与模块系统

### 2.1 当前启动链

| 证据位置 | 客观事实 | 对 Bun 迁移的影响 |
|---|---|---|
| `<ROOT>/package.json:L6-L16` | `main: app.js`、`type: module`；`app/dev/web` 脚本硬编码 `node`，`start/restart` 调用 PM2 | `bun run app` 不等同于明确把脚本中的 `node` 改成 Bun；首个试验应直接执行 `bun app.js` |
| `<ROOT>/TRSS-Yunzai.sh:L7-L10` | 设置 `PUPPETEER_EXECUTABLE_PATH`，循环执行 `node app start` | 可把实例脚本中的命令改成 `bun app.js start`，无需改业务代码；循环和外部 Redis/Chromium 仍须验收 |
| `<ROOT>/config/pm2.yaml:L1-L7` | `script: ./app.js`，无 `interpreter`，设置 `app_type: pm2` 和 512 MiB 重启阈值 | PM2 官方默认 `.js` 通常走 Node；要显式使用 Bun 需配置 `interpreter: bun`，但又必须处理顶层 await issue[cite:2][cite:18] |
| `<ROOT>/app.js:L1-L31` | 顶层动态 import；`daemon` 用 `spawnSync(process.argv[0], …)` 自重启；最终创建全局 `Bot` | Bun 直接运行支持顶层 await 和相应 argv 形态；真实守护/重启循环未启动验证 |
| `<ROOT>/lib/bot.js:L587-L610` | PM2 模式仍执行 `pnpm stop/restart`；其他模式使用 `process.exit`、`process.execve` 或 detached spawn | 即使应用换成 Bun，进程管理脚本仍可保留 pnpm；`process.execve` 在本机探针存在，但实际重启未验证 |

**判断：** 运行时迁移与包管理器迁移可以解耦。保留 pnpm 作为安装和脚本工具，同时让应用进程由 Bun 执行，是比“一键迁移全部工具链”更小、更可控的路径。

### 2.2 CommonJS 与 ESM 混用

根包和多数插件声明 `"type": "module"`，但以下混用广泛存在：

- `level/index.js` 是 CommonJS：`exports.Level = require('classic-level').ClassicLevel`。
- `puppeteer` 同时提供 `import` 和 `require` 条件导出，且包声明 `type: commonjs`。
- `@karinjs/sqlite3` 同时提供 ESM/CJS 入口，内部用 `createRequire` 加载平台二进制。
- `earth-k-plugin`、`San-plugin`、`phi-plugin`、`y-tian-plugin`、`arknights-plugin`、`memz-plugin` 等在 ESM 中使用 `createRequire` 或直接 `require`。
- Sharp、systeminformation、Fuse.js 等传递依赖包含 CommonJS。

Bun 官方将 `require()`、`require.cache`、`require.resolve`、`exports`、`module`、`__dirname` 和 `__filename` 列为已实现；本地 E1 也验证了 `createRequire(import.meta.url)` 与 `#imports`。[cite:1] 因此，**混用本身不是迁移硬阻断**，真正需要逐项规定的是 PM2 的加载钩子、V8 数据格式和模块刷新语义。

## 3. 依赖清单与版本

### 3.1 根包直接依赖

版本来自 `<ROOT>/package.json:L18-L47` 与 `<ROOT>/pnpm-lock.yaml:L15-L98`。标注“纯 JS”表示未发现 Node 原生插件二进制，不表示其所有行为已经 Bun 验收。

| 依赖 | package.json 声明 | lock 实际版本 | 原生/特殊属性 | Bun 初判 |
|---|---|---|---|---|
| art-template | 4.13.2 | 4.13.2 | 纯 JS 模板 | 推断可用，未单测 |
| chalk | ^5.6.2 | 5.6.2 | 纯 JS | 推断可用 |
| chokidar | ^5.0.0 | 5.0.0 | 纯 JS，文件监听 | API 可用性较高；热重载业务未验收 |
| compression | ^1.8.1 | 1.8.1 + patch | 使用 Node zlib；有 zstd 补丁 | 有坑：必须保留补丁及 zstd 行为 |
| es-toolkit | ^1.49.0 | 1.47.0，lock specifier 为 ^1.47.0 | 纯 JS；清单漂移 | 推断可用；先修复清单一致性 |
| express | ^5.2.1 | 5.2.1 | 纯 JS，HTTP | Bun 官方称 Express 可用；项目集成未启动[cite:1] |
| file-type | ^22.0.1 | 22.0.1 | 纯 JS | 推断可用 |
| https-proxy-agent | ^7.0.6 | 7.0.6 | 纯 JS，网络代理 | 推断可用；代理场景未验证 |
| image-size | ^2.0.2 | 2.0.2 | 纯 JS | 推断可用 |
| level | ^10.0.0 | 10.0.0 | **N-API：classic-level 3.0.0 + node-gyp-build** | 可用（E2 探针）；历史 issue 仍需关注[cite:13][cite:14] |
| lodash | link:lib/modules/lodash | 本地 link | 本地 ESM 包 | 运行时可加载；`bun pm migrate` 受阻[cite:15] |
| log4js | ^6.9.1 | 6.9.1 + patch | 纯 JS；stdout/stderr 补丁 | 推断可用；必须保留补丁 |
| md5 | link:lib/modules/md5 | 本地 link | 本地 ESM 包 | 同上 |
| moment | ^2.30.1 | 2.30.1 | 纯 JS | 推断可用 |
| node-fetch | link:lib/modules/node-fetch | 本地 link | 本地 ESM，直接导出全局 fetch | 推断可用；Bun 自带 fetch，无需替换包 |
| node-schedule | ^2.1.1 | 2.1.1 | 纯 JS，定时任务 | 推断可用；时区和长期任务未验证 |
| oicq | link:lib/modules/oicq | 本地 link | 本地 ESM，动态加载可选 icqq segment | 同上；可选 ICQQ 路径另需验收 |
| pm2 | ^7.0.3 | 7.0.1，lock specifier 为 ^7.0.1 | 进程管理器；含 ws/bufferutil 链 | 有坑：配置、清单漂移、顶层 await[cite:18] |
| puppeteer | * | 24.43.0 | **外部 Chromium 二进制 + postinstall** | 有坑：官方有 Bun 安装方式，但 Windows 有随机退出 issue[cite:3][cite:10] |
| redis | ^4.7.1 | 4.7.1 | 纯 JS `redis` 客户端，不是 ioredis | 推断可用；真实 Redis 连接未验证 |
| sequelize | ^6.37.8 | 6.37.8 + @karinjs/sqlite3 0.4.3 | 纯 JS ORM，sqlite dialect 依赖原生驱动 | 驱动探针通过；Sequelize 完整链路未验证 |
| sqlite3 | npm:@karinjs/sqlite3@^0.4.3 | @karinjs/sqlite3 0.4.3 | **N-API v6 平台预编译包** | 可用（E2 内存查询） |
| strip-ansi | ^7.2.0 | 7.2.0 | 纯 JS | 推断可用 |
| ulid | ^3.0.2 | 3.0.2 | 纯 JS | 推断可用 |
| ws | ^8.21.0 | 8.21.0 + bufferutil 4.0.9 | 可选原生 bufferutil；未锁定 utf-8-validate | bufferutil 探针通过；WebSocket 服务未整体验证 |
| yaml | ^2.9.0 | 2.9.0 | 纯 JS | 推断可用 |
| prettier（dev） | ^3.9.4 | 3.8.3，lock specifier 为 ^3.8.3 | 纯 JS；清单漂移 | 与运行时关系低；先修复清单 |

### 3.2 workspace、补丁与锁文件状态

`<ROOT>/pnpm-workspace.yaml:L1-L22` 给出以下事实：

- workspace 范围为 `plugins/**`。
- `body-parser@2`、`compression`、`log4js`、`streamroller` 有本地补丁。
- `classic-level`、`log4js`、`puppeteer`、`sqlite3`、`sharp` 被列入构建允许列表。

补丁并非可有可无：

- `body-parser` 和 `compression` 增加/调整 zstd 解压与压缩。
- `log4js` 替换 stdout/stderr 中的若干控制字符。
- `streamroller` 将日志滚动压缩从 gzip 改为 zstd。

Bun 支持 `patchedDependencies` 和 `bun patch`，但版本键失配仍有“补丁静默消失”的开放 issue，因此迁移安装器后必须核对补丁是否实际写入 `bun.lock` 并进入安装产物。[cite:7][cite:17]

根清单还存在三项明确版本漂移：`es-toolkit`、`pm2`、`prettier`。在这些漂移与 4 个相对 `link:` 依赖未处理前，不能把现有 `pnpm-lock.yaml` 当作可直接冻结转换的输入。

Bun 从 1.2.23 起已有自动迁移 `pnpm-lock.yaml`/`pnpm-workspace.yaml` 的官方实现记录，但本项目的相对 link 依赖触发了尚未关闭的支持缺口。[cite:16][cite:15] **推断：** 最稳妥的方案是先让 Bun 只承担运行时职责；若以后改用 Bun 包管理器，应重新解析并生成 `bun.lock`，而不是只做文本替换。

### 3.3 插件直接运行时依赖（lock 实际版本）

下表来自 `<ROOT>/pnpm-lock.yaml:L100-L650` 的 workspace importer。空对象表示该 importer 在 lock 中无运行时依赖；插件源码仍可能依赖根包或全局对象。开发依赖未展开，`yenai-plugin` 的 optional `abort-controller@3.0.0` 单独注明。

| 插件 | lock 中的直接运行时依赖 |
|---|---|
| FanSky_Qs | axios 1.3.4、follow-redirects 1.15.2、form-data 2.3.3、markdown-it 2.0.0、oicq 2.3.1、proxy-from-env 1.1.0 |
| Guoba-Plugin | express 5.2.1、jsonwebtoken 9.0.3、multer 2.1.1 |
| San-plugin | @gradio/client 1.15.5、axios 1.16.0、glob 11.0.3、js-yaml 4.1.0、lodash 4.18.1、openai 4.104.0、path 0.12.7、url 0.11.4 |
| Speech-statistics-plugin | express 5.2.1、handlebars 4.7.9、multer 2.1.1、node-fetch 3.3.2、**sharp 0.33.5** |
| StarRail-plugin | 无 lock 运行时依赖 |
| TRSS-Plugin | @highlightjs/cdn-assets 11.11.1、ansi_up 6.0.6、highlightjs-line-numbers.js 2.9.0、markdown-it 14.1.1、qrcode 1.5.4 |
| Yunzai.TRSS.me | 同 TRSS-Plugin |
| ZZZ-Plugin / agent-plugin / liangshi-calc | 无 lock 运行时依赖 |
| ark-plugin | lodash 4.18.1 |
| arknights-plugin | jsdom 25.0.1、moment 2.30.1、qrcode 1.5.4 |
| earth-k-plugin / earth-k-plugin-tmp | fluent-ffmpeg 2.1.3、image-size 1.2.1、systeminformation 5.31.6 |
| mc-plugin | rcon-client 4.2.5、ws 8.20.0 |
| memz-plugin | axios 1.16.0、cheerio 1.2.0、chokidar 4.0.3、cors 2.8.5、express 4.22.1、fuse.js 7.1.0、iconv-lite 0.6.3、ioredis 5.10.1、log4js 6.9.1 + patch、mustache 4.2.0、nodemailer 6.10.1、ping 0.4.4、punycode 2.3.1、systeminformation 5.31.6、whois-json 2.0.4、xlsx 0.18.5、yaml 2.8.4 |
| miao-plugin | cheerio 1.2.0、image-size 1.2.1；dev 另有 request 2.88.2 |
| nai-plugin | adm-zip 0.5.17、axios 1.16.0、cos-nodejs-sdk-v5 2.15.1、form-data 4.0.3、https-proxy-agent 7.0.6 |
| phi-plugin | @vikadata/vika 1.4.5、axios 1.16.0、crypto-js 4.2.0、csv 6.5.1、csvtojson 2.0.10、echarts 5.6.0、echarts-wordcloud 2.1.0、jszip 3.10.1、node-csv 0.1.2、pinyin-pro 3.28.1、**puppeteer 24.43.0**、qrcode 1.5.4、single-line-log 1.1.2、ulid 3.0.1 |
| pixiv-plugin | @pdf-lib/fontkit 1.1.1、axios 1.16.0、cheerio 1.0.0-rc.12、child_process 1.0.2、express 4.22.1、http-proxy-agent 7.0.2、https-proxy-agent 7.0.5、img2pdf 1.0.11（**依赖 hummus 原生模块**）、jsdom 26.1.0、pdf-lib 1.17.1、**puppeteer 24.43.0**、qs 6.14.0、selenium-webdriver 4.43.0、**sharp 0.34.5**、socks-proxy-agent 8.0.5、uuid 11.1.1、yaml 2.8.4 |
| rconsole-plugin | axios 1.16.0、form-data 4.0.3、node-id3 0.2.9、p-queue 8.1.0、qrcode 1.5.4 |
| waves-plugin | axios 1.16.0、p-limit 6.2.0、qs 6.14.0、**sharp 0.34.5** |
| xitian-plugin | acorn 8.16.0 |
| y-tian-plugin | @google/generative-ai 0.24.1、@koa/cors 4.0.0、@koa/router 12.0.2、axios 1.16.0、chalk 1.0.0、crypto-js 4.2.0、dotenv 16.5.0、fetch-undici 3.0.2、form-data 4.0.3、https-proxy-agent 7.0.6、js-tiktoken 1.0.20、js-yaml 4.1.0、jsonwebtoken 9.0.2、keyv 4.5.4、keyv-file 0.2.0、koa 2.16.4、koa-bodyparser 4.4.1、koa-static 5.0.0、markmap-common 0.18.9、markmap-lib 0.18.12、mime-types 2.1.35、node-fetch 3.3.2、openai 4.104.0、p-limit 6.2.0、pdf-parse 1.1.1、public-ip 6.0.2、**puppeteer 24.43.0**、puppeteer-extra 3.3.6、puppeteer-extra-plugin-stealth 2.11.2、request 2.88.2、textract 2.5.0、undici 5.29.0、ws 8.20.0 |
| yenai-plugin | cheerio 1.0.0-rc.12、jimp 0.22.12、node-fetch 3.3.2、systeminformation 5.31.6；optional abort-controller 3.0.0 |
| yunzai-plugin-deer-pipe | **sharp 0.33.5** |

`mora-plugin`、`flower-plugin`、`xianxin-plugin`、`logier-plugin`、`Atlas`、`example` 等没有对应 workspace importer 的插件未被误写成“无代码依赖”；它们不在上述 lock 清单中，需按各自源码和根包解析。

## 4. 原生模块与外部二进制兼容性清单

| 包/机制 | 实际版本与证据 | Bun 状态 | 证据性质与边界 |
|---|---|---|---|
| `level → classic-level` | level 10.0.0；classic-level 3.0.0；`napi-macros`、`node-gyp-build`、多平台 `.node`（lock L10591-L10596、L8104-L8109） | **可用（E2）**，打开、put/get、iterator、close 均通过 | 本机探针；#14686 已关闭并注明 Bun 1.2.0 修复，#21379 的 worker 场景已关闭；长期 DB 和 worker 场景未测[cite:13][cite:14] |
| `sqlite3 → @karinjs/sqlite3` | 0.4.3；按平台提供 `sqlite3-napi-v6-*`，当前存在 win32-x64 `.node`（lock L1262-L1316、L7179-L7188） | **可用（E2）**，`:memory:` 查询返回 42 | 本机探针；Sequelize 事务、备份、并发和真实文件 DB 未测 |
| `sharp` | 0.33.5、0.34.5；`@img/sharp-*` 与 libvips 二进制（lock L12153-L12208） | **基本调用可用（E2），其他场景有坑** | 两版本均在 Windows x64 完成 PNG 文件写入和 metadata；#20372 是 Linux/其他版本崩溃报告，#44109 是 0.35.3 WASM Windows 文件 I/O 问题，不能直接套成当前版本失败，但必须做平台回归[cite:11][cite:12] |
| `bufferutil` | 4.0.9；`node-gyp-build`，win32-x64 预编译 `.node`（lock L7880-L7882） | **可用（E2）** | mask/unmask 探针通过；ws 的完整网络行为另测 |
| `puppeteer` | 24.43.0；postinstall 下载/定位 Chromium；根及 phi、pixiv、y-tian 使用 | **有坑，未做启动验收** | 官方提供 `bun add puppeteer` 和 `bun x puppeteer browsers install`；项目也可指定系统 Chromium。开放 #15683 报告 Windows 随机退出[cite:3][cite:10] |
| `img2pdf → hummus` | img2pdf 1.0.11 → hummus 1.0.118 → @mapbox/node-pre-gyp 2.0.3（lock L9894-L9897、L9939-L9944） | **当前副本无法加载；重新构建后仍属高风险** | Node/Bun 均缺少 `binding/hummus.node`。源码使用 V8 和 `node::ObjectWrap`，Bun 的 V8 C++ API 支持 issue 仍开放[cite:19] |
| `fluent-ffmpeg` | earth-k 两个目录均为 2.1.3 | **有坑/未验证** | 它是外部 FFmpeg 进程的 JS 包装，不是 N-API；迁移主要触及 `child_process`、PATH、参数与退出语义 |
| `selenium-webdriver` | pixiv 使用 4.43.0 | **未验证** | 外部浏览器和 driver；不能把 Puppeteer 的结果套用到 Selenium |
| `ioredis` | memz 使用 5.10.1 | **推断可用，未验证** | 纯 JS Redis 客户端，不应因名称与原生驱动混淆而列入原生阻塞 |
| `canvas` | 只在 jsdom 25/26 的 optional peer 中出现：^2.11.2、^3.0.0（lock L4076-L4092） | **当前无安装项，无需作为现有阻塞** | lock 中无 `canvas@…` 包条目 |
| `better-sqlite3` / `node-pty` / `utf-8-validate` | 当前 lock 未见实际包条目 | **无需按现有依赖处理** | 若后续插件新增，必须重新评估，尤其 node-pty 涉及本地终端和原生 API |

**推断：** 当前原生包的主要风险不是“凡 `.node` 都不能用”，而是不同 API 体系、平台二进制、构建脚本和业务操作覆盖不全。N-API 探针成功的结果不能外推到直接调用 V8 C++ API 的 Hummus。

## 5. Node 专有 API 与插件加载机制

### 5.1 Node API 分布与兼容性

本次搜索覆盖 `app.js`、`lib/`、36 个插件/内置目录；以下行号均相对 `<ROOT>`。

| API/机制 | 项目证据与分布 | Bun 判断 | 证据性质 |
|---|---|---|---|
| 顶层 await、ESM | `app.js:L1-L31`；根包 `type: module` | **直接 Bun 可用；PM2 有坑** | E1 直接运行通过；PM2 + Bun 顶层 await issue 开放[cite:18] |
| `require` / `createRequire` | `@karinjs/sqlite3/dist/index.mjs:L1-L20`；earth-k、San、phi、y-tian、arknights、xianxin 等多个插件在 ESM 中加载 CJS | **可用（官方 + E1）** | Bun 官方列出 require/cache/resolve；E1 的 `createRequire` 解析成功[cite:1] |
| `__dirname` / `__filename` / `import.meta.url` | `plugins/miao-plugin/tools/path.js:L5-L13` 用 `fileURLToPath` 构造 `__dirname`；ark、y-tian、memz、logier 等也有类似代码 | **可用** | Bun 官方实现两个全局；E1 中为 string。`fileURLToPath(import.meta.url)` 是跨运行时更稳妥的既有写法[cite:1] |
| `child_process` | `app.js:L11-L17`；`lib/util.js:L5,L329-L370`；`lib/config/redis.js:L3,L64-L93`；adapter、miao、Guoba、agent、rconsole、phi、y-tian 等广泛使用 | **可用性较高，进程边界有坑** | Bun 官方缺少 gid/uid、部分 IPC socket handle 等能力；已检查调用主要是 exec/execFile/spawn/spawnSync，未见这些缺失项。detached restart、PID、退出码和外部命令仍须整体验证[cite:1] |
| `worker_threads` | 未在本轮第一方源码搜索中命中；Sharp 等依赖可能间接受影响 | **当前不构成直接阻塞** | Bun 官方有选项限制，但不能据此推断项目核心使用 worker[cite:1] |
| `node:vm` | `plugins/arknights-plugin/utils/common.js:L6,L89-L110` 使用 `new Script(...).runInNewContext(dom.getInternalVMContext())` | **基础 API 可用，JSDOM 集成未验证** | 本机 `new Script('1 + 1')` 返回 2；Bun 官方列出核心 VM API。JSDOM 内部上下文与第三方 SDK 仍需专项测试[cite:1] |
| `process.binding` | 本轮第一方源码搜索未命中 | **当前无直接影响** | Bun 官方只称部分实现，但项目未显示依赖点[cite:1] |
| `node:v8` 序列化 | `yenai-plugin/lib/common/common.js:L216-L219`、`model/GroupAdmin.js:L482-L485`、`apps/groupAdmin/groupVote.js:L207-L210` | **Node 数据无法原样读取（E3）** | Bun 使用 JavaScriptCore wire format 而非 V8；E3 复现反序列化失败。三处均有空 `catch`，可能静默留下空数组并改变权限判断[cite:1] |
| `process.report.getReport()` | `lib/bot.js:L113-L120` 用于 `/status` | **可用（E1）** | 本机返回含 header 的报告；Windows 实现 issue 已关闭[cite:9] |
| `process.execve` / `process.argv[0]` | `app.js:L13`；`lib/bot.js:L598-L607` | **API 存在，重启语义未验收** | E1 中 `process.execve` 为 function，argv 指向探针脚本；未真正执行重启[cite:1] |
| `package.json#imports` | 根 `#miao/#miao.models`；miao 的 `#miao.path`；Guoba 的 `#guoba.*` | **可用（E1）** | 隔离探针成功解析 `#probe`；Bun 的模块解析支持该机制 |
| 相对路径动态 `import()` | `lib/plugins/loader.js:L45-L74,L80-L132`；监听器和 renderer 也动态导入 | **可用（E1）** | 核心初次加载和相对路径模块实例化未发现 Node 专属限制 |
| 相对路径 + query 热重载 | `lib/plugins/loader.js:L639-L641,L662-L718` 使用 `../../plugins/${key}?时间戳` | **可用（E1）** | Bun 与 Node 都生成两个不同模块实例；Bun 的 `file:` URL 问题不适用此写法[cite:8] |
| `file:` URL + query | `plugins/Guoba-Plugin/utils/guobaImport.js:L49-L64` 在 Windows 转为 `file:///` 后追加版本号 | **有坑：刷新可能失效** | Bun 开放 issue 与 E1 均显示不同 query 返回同一模块；初次导入可工作，但版本刷新可能继续复用旧模块[cite:8] |
| `chokidar` 文件监听 | `lib/plugins/loader.js:L662-L718` 监听 change/add/unlink | **推断可用，事件验收缺失** | Bun 的 fs 实现较完整，但未启动项目验证 Windows/Linux 事件、防抖和重启联动 |

### 5.2 核心插件发现与加载调用链

核心加载过程可以从源码完整还原：

1. `lib/bot.js:L256-L263` 在 `run()` 中先初始化配置和 HTTP 服务，再执行 `PluginsLoader.load()` 与 `ListenerLoader.load()`。
2. `lib/plugins/loader.js:L45-L74` 读取 `plugins/` 下的目录：若有 `index.js` 只加入该入口，否则加入目录第一层的所有 `.js` 文件。
3. `lib/plugins/loader.js:L80-L132` 通过 `Promise.allSettled` 并发执行 `import(file.path)`，并对缺包和超时分别记录。
4. `lib/plugins/loader.js:L134-L168` 实例化插件类，执行 `init()`，收集 cron 任务，按 priority 注册规则及 handler。
5. `lib/plugins/loader.js:L639-L718` 通过 chokidar 监听文件变化，并用时间戳 query 再次 `import()` 以绕过 ESM 缓存。
6. `lib/listener/loader.js:L10-L49` 动态加载事件和适配器；`lib/renderer/loader.js:L27-L54` 动态加载渲染后端。

**判断：** 核心“发现 → 动态导入 → 实例化 → 注册 → 热重载”不依赖 `process.binding`、私有 CJS loader 或 `module._cache` 等只有 Node 才具备的语义。E1 进一步证明核心使用的相对 query 写法可在 Bun 形成新模块实例。因此，**核心加载机制属于可迁移部分**。

但插件不是天然独立子系统。核心向插件提供 `global.Bot`、`plugin`、`segment`、`logger`、`redis` 等对象，许多插件还在导入时读取配置、连接服务或启动子进程。把某个插件单独拿到 Bun 中执行通常需要适配器或完整宿主，不能把“依赖包可加载”误当成“插件可独立运行”。

### 5.3 插件自有加载器的差异

- Guoba 的 `GI()` 不仅是普通动态导入，还自行维护 `instanceVersion`。在 Windows，它把路径转换成 `file:///` 后拼接 query，因此其“通过递增版本刷新模块”的设计会受到 Bun #46900 影响。[cite:8]
- `memz-plugin/index.js:L42-L64` 递归扫描 `apps/`，通过 `pathToFileURL(fullPath).href` 导入；没有追加 query，不触发同一缓存绕过问题。
- `lib/modules/oicq/index.js:L34-L41` 使用 `file://${process.cwd()}/...` 加载可选 ICQQ segment，也没有 query，不能与 Guoba 的情况混同。
- `yenai-plugin` 的问题不是模块发现，而是持久化二进制格式。空 `catch` 会隐藏不兼容，验收必须检查行为，而不能只看进程是否启动。

## 6. 必须绕开或单独处理的项目

| 项目 | 原样替换的后果 | 建议边界 |
|---|---|---|
| PM2 + 顶层 await | 可能在 PM2 的 Bun 容器/加载钩子中报 `require() async module ... is unsupported` | 第一阶段绕过 PM2，直接 Bun；若必须保留 PM2，单独验证动态 import wrapper 和监控指标，不把 issue 评论中的 workaround 当作已验证方案[cite:18] |
| `bun pm migrate` | 当前在相对 `link:` 依赖处失败，简单改为 `file:` 也无法沿用原 lock | 保留 pnpm；以后改为受支持的本地包形式并重新生成 `bun.lock`，不要只改文本[cite:15] |
| Guoba Windows 热重载 | 版本 query 被忽略，可能继续使用旧模块 | 初次加载可试验；热重载应禁用、留 Node，或改成 Bun 支持的“绝对路径 + query”并回归验证[cite:8] |
| `yenai-plugin` 的 V8 数据 | 反序列化失败被吞掉，可能静默改变权限相关行为 | 在改为 JSON/跨运行时格式并验证前保留 Node；禁止仅凭“没有报错”判定成功 |
| `img2pdf/hummus` | 当前副本 Node/Bun 都缺二进制；直接 V8 C++ API 又是 Bun 高风险区域 | 不将 Bun 重新构建作为首选；可先修复 Node 基线，再考虑保留 Node sidecar 或改用项目已有的 `pdf-lib`，后者需要代码改造[cite:19] |
| Puppeteer 渲染 | 浏览器自动下载、启动、长时间截图和 Windows 随机退出均可能影响稳定性 | 首批迁移不纳入；在独立进程中做反复 launch/connect、导航、截图和关闭测试，必要时保留 Node 渲染服务[cite:3][cite:10] |
| 编译版 `Main.sh` | 内层解释器和进程调用无法从源码确认 | 在获得可审计启动源前，从实例脚本直接启动 Bun，不修改或反编译总控 |

## 7. 可行范围与分阶段迁移建议

### 7.1 可以优先跑在 Bun 上的部分

1. **核心进程的受控精简配置**：保留 pnpm 依赖，绕过 PM2 和总控，在隔离副本中只保留目标插件，验证配置、日志、Express、WebSocket、Redis、定时任务及动态插件加载。
2. **Level 与 SQLite 数据路径**：N-API 最小调用已通过，可优先做真实数据副本的读写和迭代验收。Sequelize 仍需单独覆盖。
3. **Sharp 相关插件的候选路径**：两个实际版本均通过基础文件 I/O，可在目标操作系统上继续做格式、缩放、合成、并发与内存验收。
4. **纯 JS 工具和后续独立脚本**：Bun 可直接运行 JS/TS，适合新增不依赖 `global.Bot` 的批处理、维护脚本和小型服务。现有插件因全局宿主耦合，不能无适配地拆出。
5. **`lib/tools/web.js` 的后续候选**：代码本身主要是 Express、静态文件和模板渲染，但当前根 package/lock 未声明且 `node_modules` 未找到 `express-art-template`，所以它不是“无需处理即可优先迁移”的现成候选；应先完成依赖基线审计。

### 7.2 建议保留 Node 或最后迁移的部分

- Puppeteer 高频渲染和 Selenium。
- `img2pdf/hummus` PDF 生成链。
- 依赖 Node/V8 序列化数据的 `yenai-plugin` 相关功能。
- Guoba 的 Windows 热重载。
- 必须在 PM2 下运行且不能接受加载器改写的部署方式。

如果最终不能整体迁移，较合理的混合架构是：**核心或若干独立工具跑 Bun，Node 保留渲染、PDF 和特殊插件服务**。跨进程服务化需要新增适配层，不属于“只换 node 为 bun”的零改代码范围。

### 7.3 推荐执行顺序

**阶段 A：固定 Node 基线**  
记录 Node 下可启动插件、实际版本、外部命令、数据格式和现有故障。特别先处理当前副本中 Hummus 二进制缺失与根 lock 漂移，否则无法区分既有问题和迁移问题。

**阶段 B：只换运行时**  
继续使用 pnpm 安装的依赖，在测试副本执行：

```bash
bun app.js start
```

不执行 `bun install`，不更换 PM2，不迁移生产数据。先使用精简插件集合，再逐步加入插件。

**阶段 C：专项验收高风险项**  
分别验证 PM2、Guoba 热重载、Puppeteer、Sharp、Hummus、V8 数据、外部 FFmpeg 和进程重启。任何一项未通过都应保留 Node 回退，而不是修改结论为“基本兼容”。

**阶段 D：可选的包管理器迁移**  
只有在确实需要 Bun 安装器收益时才处理相对 link、本地补丁、构建白名单和锁文件。生成新的 `bun.lock` 后，核对 4 个补丁、平台 optionalDependencies、Puppeteer 浏览器下载及原生包安装结果。Bun 默认不执行任意依赖生命周期脚本，`trustedDependencies` 或等效配置必须明确。[cite:5]

## 8. 可能收益与不能预设的收益

| 收益方向 | 判断 | 证据与限制 |
|---|---|---|
| 冷启动 | **潜在收益，未量化** | 本次只运行了小探针，没有测量完整机器人。大量动态插件导入、原生模块、Redis 和 Chromium 可能主导启动时间 |
| 内存 | **不能保证下降** | 未做同数据、同插件、同请求负载对照。现有 512 MiB PM2 阈值是配置，不是 Bun 内存收益证据；Chromium 和原生库内存也不能忽略 |
| 包安装速度 | **官方有性能主张，本项目尚未获得该收益** | Bun 官方宣传安装速度，并给出 workspace 基准；但当前自动迁移被相对 link 阻断，且未进行本项目安装基准[cite:5][cite:6] |
| TS 直跑 | **能力存在，当前直接收益有限** | Bun 官方支持 TypeScript、顶层 await 和 `.ts` 扩展导入；项目核心及已检查插件主要是 JS，因此不是本次迁移的必要收益[cite:4] |
| 减少业务代码改动 | **核心有现实基础** | ESM、CJS 混用、相对 query、`#imports` 和多个 N-API 包已通过探针；高风险点集中在少数明确机制 |
| 工具链整合 | **可选，而非必须** | 不必同时采用 Bun 安装器、测试器、打包器或单文件编译；扩大范围会增加原生模块与补丁兼容风险 |

## 9. 验收清单与最终边界

迁移试验至少应记录以下结果，并同时给出 Node 对照：

- 冷启动到 `online` 的时间、常驻内存、相同消息负载下的内存与吞吐量。
- `stop`、定时/手动 restart、异常退出后重启、detached 子进程及 PM2 监控指标。
- HTTP、HTTPS、WebSocket upgrade、Redis 断线重连和外部 Redis 启动。
- 既有 LevelDB 数据副本的完整迭代与写回；真实 SQLite 文件的 Sequelize 查询、事务和并发。
- 插件新增、修改、删除后的热重载；Guoba 需单独检查新版本模块是否真正加载。
- Sharp 各实际使用格式和操作，而不只是 PNG metadata；记录长时间运行的原生崩溃与内存。
- Puppeteer 反复启动、连接、导航、截图、关闭和浏览器异常退出恢复。
- `yenai-plugin` 权限相关路径，确认反序列化失败不会被误认为功能正常。
- `img2pdf` 的真实 PDF 生成，先确认 Node 基线本身可用。
- 目标 Linux/MSYS 或实际部署平台的重新验证；Windows x64 探针结果不能替代目标平台结果。

**最终判断：** “只把 node 换成 bun”可以走到核心及一部分插件的受控运行试验，且原生依赖已有积极证据；但它目前不是可证明的全量、零改动、生产级迁移方案。最稳妥的工程定位是 **Bun 运行时候选 + pnpm 保持不变 + 插件分批验收 + 特殊子系统保留 Node**。

## Sources

<a id="cite-1"></a>[cite:1] [Bun — Node.js compatibility](https://bun.sh/docs/runtime/nodejs-apis)

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
