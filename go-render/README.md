# Go Render

独立的 Chromium HTML 渲染服务。核心进程只通过 HTTP 提交 HTML 和渲染选项；本服务独占浏览器与页面生命周期，不依赖 Node.js，也不会下载浏览器内核。

## 已实现

- 单浏览器进程复用，固定数量的工作页面池。
- 并发上限、有界请求队列、排队与渲染统一超时。
- `load`、`dom_content_loaded`、`network_idle` 和 CSS 选择器等待。
- PNG 输出，支持视口尺寸与设备像素比。
- 区分参数非法、超时、页面崩溃、浏览器不可用、队列满和服务关闭。
- 崩溃页面自动替换；浏览器断连后作废旧代页面，后续请求自动创建新浏览器与页面。
- `slog` JSON 结构化日志、Prometheus 指标、健康检查。
- HTTP 与浏览器/page 的优雅关闭；超过关闭期限后强制取消剩余渲染。

## 环境要求

- Go 1.27.4。
- 一个**已经安装**的 Chromium、Chrome 或 Edge 可执行文件。
- Windows 已验证；代码只使用 Go 与 Chromium/CDP。

本次使用：

```text
go version go1.27.4 windows/amd64
C:\Program Files\Google\Chrome\Application\chrome.exe
```

旧终端若找不到 `go`，可重开终端，或使用：

```text
C:\Users\14036\AppData\Local\Programs\go\bin\go.exe
```

## 启动

在项目目录执行：

```powershell
$env:CHROMIUM_PATH = "C:\Program Files\Google\Chrome\Application\chrome.exe"
go run ./cmd/renderer -addr 127.0.0.1:8080 -concurrency 2 -queue-size 16
```

也可以先构建：

```powershell
go build -o renderer.exe ./cmd/renderer
.\renderer.exe -chromium "C:\Program Files\Google\Chrome\Application\chrome.exe"
```

启动时即创建浏览器和全部工作页面；浏览器不可用时进程启动失败并输出结构化错误日志。

### 配置项

| 参数 | 默认值 | 含义 |
|---|---:|---|
| `-addr` | `127.0.0.1:8080` | HTTP 监听地址 |
| `-chromium` | 环境变量 `CHROMIUM_PATH` | 现有浏览器可执行文件路径；必填 |
| `-concurrency` | `2` | 工作页面数，也是最大同时渲染数；必须大于 0 |
| `-queue-size` | `16` | 等待页面的最大请求数；0 表示不缓冲排队 |
| `-max-timeout` | `30s` | 单请求排队加渲染的超时上限 |
| `-browser-start-timeout` | `20s` | 每次浏览器或页面创建/恢复的超时 |
| `-body-limit` | `4194304` | JSON 请求体上限，单位字节 |
| `-shutdown-timeout` | `15s` | HTTP 与渲染服务优雅关闭总期限 |

### 并发、排队与超时语义

1. 最多只有 `concurrency` 个请求占用页面执行 CDP 渲染。
2. 页面占满后，请求进入最多 `queue-size` 个位置的有界队列；队列满时不阻塞，立即返回 `429 queue_full`。
3. 请求的 `timeout_ms` 从调用渲染服务时开始计算，**包含排队和实际渲染**，不能超过 `-max-timeout`。
4. 超时返回 `504 timeout`。已超时但仍在队列中的任务会在取到页面之前被丢弃，不再执行渲染。
5. 当前请求发生页面或浏览器崩溃时直接返回错误，不透明重试。后续请求触发页面替换或浏览器重建，避免调用方看到重复结果。
6. 收到 Ctrl+C、SIGTERM 或 Windows Ctrl+Break 后停止接收新 HTTP 请求，等待已开始/已排队请求完成；超过 `-shutdown-timeout` 则取消剩余任务并关闭页面和浏览器。

## HTTP 接口

### `POST /v1/render`

请求体：

```json
{
  "html": "<!doctype html><html><body><div id=\"ready\">Hello</div></body></html>",
  "options": {
    "width": 320,
    "height": 200,
    "device_scale_factor": 2,
    "timeout_ms": 15000,
    "wait_until": "network_idle",
    "wait_selector": "#ready"
  }
}
```

字段规则：

| 字段 | 默认值 | 规则 |
|---|---:|---|
| `html` | 无 | 必填，非空白 HTML 字符串 |
| `width` | `800` | 1–8192 CSS 像素 |
| `height` | `600` | 1–8192 CSS 像素 |
| `device_scale_factor` | `1` | 0.1–4；输出像素尺寸为视口尺寸乘以该值 |
| `timeout_ms` | `10000` | 大于 0，且不超过服务端 `-max-timeout` |
| `wait_until` | `load` | `load`、`dom_content_loaded` 或 `network_idle` |
| `wait_selector` | 空 | 可选 CSS 选择器，最长 2048 字节 |

等待条件：

- `dom_content_loaded`：等待 `document.readyState !== "loading"`。
- `load`：等待 `document.readyState === "complete"`，并等待页面字体加载完成。
- `network_idle`：在满足 `load` 后，再等待连续 500ms 没有在途网络请求；`data:` URL 不计入在途请求。
- `wait_selector`：在上述条件后继续等待对应 CSS 节点就绪。

成功时返回 `200 OK`，`Content-Type: image/png`，响应体为 PNG 二进制。

PowerShell 示例：

```powershell
$payload = @{
  html = '<html><body><div id="ready">Hello</div></body></html>'
  options = @{
    width = 320
    height = 200
    device_scale_factor = 2
    timeout_ms = 15000
    wait_until = 'load'
    wait_selector = '#ready'
  }
} | ConvertTo-Json -Depth 5 -Compress

$response = Invoke-WebRequest `
  -Uri 'http://127.0.0.1:8080/v1/render' `
  -Method Post `
  -ContentType 'application/json' `
  -Body $payload `
  -OutFile "$env:TEMP\render.png" `
  -PassThru
$response.StatusCode
```

错误响应统一为 JSON：

```json
{
  "error": {
    "code": "timeout",
    "message": "render exceeded its deadline"
  }
}
```

| 错误码 | HTTP 状态 | 含义 |
|---|---:|---|
| `invalid_argument` | 400 | JSON、HTML 或渲染选项非法 |
| `invalid_argument` | 413 | 请求体超过 `-body-limit` |
| `timeout` | 504 | 排队或渲染超过请求期限 |
| `page_crashed` | 502 | 当前页面崩溃或 Target 关闭 |
| `browser_unavailable` | 502 | 浏览器崩溃、断连或恢复失败 |
| `queue_full` | 429 | 页面和队列均已满 |
| `unavailable` | 503 | 服务未启动、正在关闭或任务被取消 |
| `internal` | 500 | 其他渲染错误；内部细节只写日志 |

### 健康检查与指标

- `GET /healthz`：返回 `{"status":"ok"}`。
- `GET /metrics`：Prometheus 文本格式。

主要指标：

| 指标 | 类型 | 含义 |
|---|---|---|
| `render_active_requests` | Gauge | 当前占用页面的渲染数 |
| `render_queue_length` | Gauge | 当前排队长度 |
| `render_duration_seconds` | Histogram | 含排队的端到端耗时分布，按结果分类 |
| `render_requests_total` | Counter | 各结果请求数 |
| `render_browser_starts_total` | Counter | 浏览器启动次数，包含崩溃恢复 |

日志为 JSON Lines，包含请求 ID、方法、路径、状态和耗时；不会记录 HTML 请求体。

## 测试

### 无浏览器单元测试

桩测试覆盖参数校验、HTTP 状态和错误码、并发上限、队列满、排队超时、页面替换、浏览器重建、优雅关闭以及指标标签：

```powershell
go test ./... -count=1 -timeout=60s
```

### 真实 Chromium 冒烟

设置现有浏览器路径后执行；未设置时该测试自动跳过：

```powershell
$env:RENDER_SMOKE_CHROMIUM = "C:\Program Files\Google\Chrome\Application\chrome.exe"
go test ./internal/renderer -run TestChromedpSmoke -v -count=1 -timeout=60s
```

该测试启动隔离服务与浏览器，验证 `network_idle`、选择器等待、PNG 解码及 DPR 后的尺寸，并关闭浏览器。

## 本次实际验证记录

验证日期：2026-11-13。以下均为本机实际输出，不是性能推算。

### 格式化、单元测试和构建

```text
> go fmt ./...
（无文件被重新格式化）

> go test ./... -count=1 -timeout=60s
?    go-render/cmd/renderer [no test files]
ok   go-render/internal/httpapi 0.805s
ok   go-render/internal/renderer 0.978s
ok   go-render/pkg/render/api 0.382s

> go build ./...
（无输出，退出码 0）

> go vet ./...
（无输出，退出码 0）
```

额外的 `go test -race ./...` 未记为通过：当前环境 `CGO_ENABLED=0`，Go 返回 `-race requires cgo`；未为此安装额外软件。

### 真实浏览器 Go 冒烟

```text
=== RUN   TestChromedpSmoke
    browser_smoke_test.go:65: smoke render succeeded: duration=543.6194ms bytes=5989 dimensions=640x400
--- PASS: TestChromedpSmoke (1.20s)
PASS
ok   go-render/internal/renderer 2.039s
```

### 独立进程 HTTP 冒烟与优雅关闭

使用构建出的 `cmd/renderer`、本机 Chrome、2 个页面和容量 4 的队列，真实请求 `/v1/render`：

```text
renderer_pid=157668
health=ok status=200 content_type=image/png elapsed_ms=556 bytes=7675 dimensions=640x400 sha256=A8FB333B524782A48CB446CA7C9973D639EEFD534C43D66B548F9BBC854934F5
metric=render_active_requests 0
metric=render_duration_seconds_count{result="success"} 1
metric=render_queue_length 0
metric=render_requests_total{result="success"} 1
service_graceful_exit=true service_exit_code=0
renderer_child_chrome_processes=0
```

服务日志依次确认 `renderer started`、`HTTP server listening`、三个 200 请求、`shutdown signal received` 和 `renderer stopped`；标准错误为空。

这些结果是**单次冒烟**，只证明隔离服务可用，不能当作 P50/P95 性能结论，也未与历史 puppeteer 样本做统计 benchmark。

## 目录结构

```text
cmd/renderer          独立进程入口、配置、HTTP/信号生命周期
internal/httpapi      JSON API、错误映射、访问日志、健康与指标端点
internal/renderer     页面池、队列、崩溃恢复、chromedp/CDP、指标
pkg/render/api        可复用的渲染请求、选项和公共错误契约
```

## 部署边界

默认只监听 loopback，且当前没有鉴权或 HTML 网络访问隔离，适用于受信任的本机核心调用。不要直接暴露到公网；如需跨机调用，应另行增加认证、传输保护和出站网络限制。

本次验证只启动了隔离的 renderer/Chromium，没有启动 SnowLuma、没有操作 WSL；对 `mysy2` 仅做了只读路径调查，未修改其中任何文件。
