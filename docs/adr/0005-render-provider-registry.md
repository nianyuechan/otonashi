# ADR 0005：渲染后端用进程内注册表按名字选择

- 状态：已采纳（2026-11-15，Q4 落地）
- 相关代码：`go-render/internal/renderer/registry.go`（`Provider`、`RegisterProvider`、`LookupProvider`、`ProviderNames`）、`go-render/internal/renderer/provider_chromedp.go`（chromedp 自注册）、`go-render/cmd/renderer/main.go`（`-provider` 参数）
- 相关测试：`go-render/internal/renderer/registry_test.go`
- 取代关系：补完 ADR 0002 里"provider 选择仍硬编码在 main.go"的已知缺口；三层接口本身不变

## 背景

ADR 0002 已把渲染实现切成 `Page` / `Browser` / `BrowserFactory` 三层，`Service` 只依赖最外层。但**选择**这一步仍然写死在 `main.go` 的一行闭包里：`renderer.NewChromedpBrowser(ctx, *chromium)`。后果是部署层想换实现（换浏览器后端、接一个 mock 服务、做灰度对比）必须改这行代码并重新编译，而不是换个参数。

Q4 要解决的就是这一步：接口已经有了，缺的是"按名字找到实现"的机制。

## 决定

在 `internal/renderer` 里加一张进程内注册表：

```go
type ProviderConfig struct {
    ChromiumPath string
}

type ProviderFactory func(context.Context, ProviderConfig) (Browser, error)

type Provider struct {
    Name        string
    Description string
    Factory     ProviderFactory
}

func RegisterProvider(Provider) error
func LookupProvider(name string) (Provider, error)
func ProviderNames() []string
```

四条约定：

1. **注册发生在实现文件自己的 `init()` 里**，不在 `main.go`。`provider_chromedp.go` 是当前唯一的注册方，所以"新增一个后端"= 新增一个带 `init` 的文件，`Service`、HTTP 层、指标层、`main` 的分支逻辑都不用动。
2. **`ProviderConfig` 用字段扩展，不加参数**。以后某个后端需要代理地址、需要离线镜像路径，就往结构体里加字段；已有 provider 的工厂函数签名不变，不会因为这轮加字段而全部改一遍。
3. **注册失败是错误，不是覆盖**。空名字、`nil` 工厂、重名都会被拒绝并返回哨兵错误（`ErrProviderInvalid` / `ErrProviderRegistered`），避免两个文件互相顶掉而没人发现。
4. **`LookupProvider` 对未知名字报错并列出可用名字**。部署时打错名字，日志里直接能看到 `available: chromedp`，而不是一句 `browser is unavailable`。

`main.go` 只加一个 `-provider` 参数（默认 `chromedp`）：默认路径与改动前完全一致，行为没有变化。

## 后果

- 换实现不再需要改 `main.go` 的分支逻辑：加文件 + 用 `-provider` 选名字即可。
- **已知边界一**：如果新后端放在**别的包里**（例如独立模块的 mock 服务），Go 不会因为没人引用而编译它，`main.go` 仍需一行 `_ "…/mockprovider"` 的空白导入。这行导入是"让实现参与编译"，不是"改选择逻辑"，与 ADR 0002 的承诺不冲突，但必须写清楚。
- **已知边界二**：`main.go` 仍无条件要求 `-chromium` 非空（沿用改动前的校验），所以现在还选不了"不需要浏览器"的后端。等出现第二个真实后端、需要不同配置时再拆这层校验，避免这一轮为了假想需求放松已有的启动校验。
- 注册表是进程级的，无法在运行中替换后端。热切换需要连接池、双实例、流量切换，属于另一件事（阶段九控制面），不在 Q4 范围。
- 测试固定了六件事：注册后能解析、空名字/空工厂被拒、重名被拒且不覆盖、未知名字报错含可用列表、名字列表有序且返回副本（调用方改返回值不影响注册表）、chromedp 已注册且空路径会被拒。

## 备选方案与被否原因

| 方案 | 为什么不用 |
|---|---|
| 用配置文件（YAML/JSON）选后端 | 需要一个 schema、一套校验、一份文档，而现在只有一个真实后端；这轮只补"选择机制"，不引入配置契约（Q6 才做）。 |
| 把注册表挂在 `Service` 上 | 会让每个调用方自己接线，而 `Service` 根本不关心后端叫什么；注册表属于部署层，不属于服务层。 |
| 编译期 build tag 选后端 | 换后端要重新编译不同 tag 的产物，部署时反而更麻烦；运行期名字更适合灰度与对照。 |
| 依赖注入容器（wire/dig） | ADR 0002 已否决：只有一个依赖点，容器带来的生命周期语义远超收益。 |
| 允许重名覆盖，便于测试 | 生产里两个 provider 文件重名会静默顶替，出问题时无从发现；测试用唯一名字更便宜。 |

## 如果重来一次

仍然会用注册表，但会把 `-provider` 参数和 `ProviderConfig` 一起设计得更早一点：ADR 0002 落地时就留出这个参数位，部署层就不必经历"先改代码、后改参数"的两段式。另一个会提前的是**未知名字的报错必须带可用列表**——这个细节能省掉一次部署排查。
