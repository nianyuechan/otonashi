# ADR 0002：渲染器用 Page / Browser / BrowserFactory 三层接口

- 状态：已采纳（接口已落地，2026-11-15 复审确认）；provider 注册/选择机制已由 ADR 0005 补齐
- 相关代码：`go-render/internal/renderer/service.go`（`Page`、`Browser`、`BrowserFactory`、`Service`）、`go-render/internal/renderer/browser.go`（`NewChromedpBrowser`）、`go-render/cmd/renderer/main.go`
- 相关测试：`go-render/internal/renderer/service_test.go`（fake provider）

## 背景

`go-render` 是一个独立渲染服务：HTTP 层收 HTML → 渲染层出图片。渲染层既要能跑真实 Chromium/EdgeCore，又要能在单元测试里验证并发、队列、超时、启动失败、崩溃重建、关闭排空——如果测试必须启动真实浏览器，这些用例在低内存本机上不可靠（本机已多次出现 30s 级 IO 停滞）。

同时部署层需要"换一个渲染实现"（例如换浏览器后端、加 mock 服务）时不改核心代码。Q4 的缺口正在这里：接口已经存在，但 provider 选择仍硬编码在 `main.go`。

## 决定

把渲染实现切成三个正交的抽象，`Service` 只依赖最外面那一层：

```go
type Page interface {
    Render(context.Context, api.Request) ([]byte, error)
    Close() error
}

type Browser interface {
    NewPage(context.Context) (Page, error)
    Done() <-chan struct{}
    Close() error
}

type BrowserFactory func(context.Context) (Browser, error)
```

理由逐层说明：

1. **`Page` 只负责"一次渲染 + 自身关闭"**。页面是会被重建的最小单位；崩溃检测、队列超时都只需要 `Render` 的返回值与 `Close`。
2. **`Browser` 负责"页面工厂 + 死亡通知 + 整体关闭"**。`Done()` 是浏览器进程崩溃/退出的信号通道，`Service` 靠它触发重建；`NewPage` 让页面池可以按需创建而不是预先占满。
3. **`BrowserFactory` 是一个函数类型，不是接口**。它只需要"用 ctx 造一个 Browser"，用一个函数就能表达；引入接口/容器只会增加注册样板。
4. `Service` 构造时注入 factory（`renderer.NewService(factory, Config{...}, logger, metrics)`）；`NewChromedpBrowser` 是当前唯一生产实现，在 `cmd/renderer/main.go` 里被填入。

## 后果

- 单元测试完全不启动 Chromium：`service_test.go` 用 fake `Page`/`Browser` 覆盖并发上限、队列满、排队超时、页面崩溃重建、浏览器崩溃重建、Close 超时后 worker 仍在跑的语义。这是阶段零 Z3 能"分阶段测渲染"的前提。
- 换实现只需提供新的 `BrowserFactory`；`Service`、HTTP 层、指标层都不用改。
- **已知边界（Q4 已解决）**：provider 的"选择"曾写死在 `main.go`（一行 `renderer.NewChromedpBrowser`）。2026-11-15 由 ADR 0005 的进程内注册表 + `-provider` 参数补齐；`main.go` 不再直接引用具体实现。
- `Service.Close` 超时返回后 worker 可能仍在运行（`service_test.go` 固定了该语义），契约尚未写明调用方如何处理——登记为 P2-7，不在本 ADR 解决。

## 备选方案与被否原因

| 方案 | 为什么不用 |
|---|---|
| `Service` 内部直接 `new` chromedp 浏览器 | 无法测并发/队列/崩溃/关闭语义；测试要么依赖真实浏览器（本机不可靠），要么这些行为永远没有回归。 |
| 一个 `Renderer` 大接口（Render + Close + NewPage + Done 全在一起） | 页面与浏览器生命周期不同：页面可频繁重建、浏览器要长期存活。合成一个接口后 `Close` 的语义会歧义（关页面还是关浏览器），fake 也必须实现用不到的方法。 |
| 用 `interface{ Render(...) }` 单一方法抽象，忽略浏览器生命周期 | 队列/并发/重建都需要"浏览器死亡"这个信号；只抽象 Render 会让 `Service` 重新耦合底层进程管理。 |
| 引入依赖注入容器（wire/dig 等） | 这里只有一个依赖（factory），容器带来的注册/生命周期语义远超收益，违反"不引入看起来规范的重构"。 |
| 用配置文件选 provider（本轮就做） | 需要一个注册表 + 名称解析 + 错误处理 + 文档，是 Q4 的工作量；本轮只立规矩不加能力。 |

## 如果重来一次

仍然会保留这三层，但会把 `BrowserFactory` 的注入点提前到"可注册的 provider 表"，而不是 `main.go` 里的一行常量。也就是说：三层抽象是对的，缺的是**选择机制**——这正好是 Q4 要补的那一半。
