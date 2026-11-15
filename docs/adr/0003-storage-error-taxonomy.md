# ADR 0003：存储用类型化错误，默认保持 FULL 同步

- 状态：已采纳（错误分类与默认值均已落地）；WAL+NORMAL 仍是显式实验开关
- 相关代码：`go-yunzai/pkg/plugin/sdk/storage.go`（`KVStore`、哨兵错误）、`go-yunzai/internal/storage/backend.go`（`Backend`）、`go-yunzai/internal/storage/sqlite.go`（`experimentPragmas`）、`go-yunzai/internal/storage/redis.go`、`legacy_redis.go`
- 相关证据：`docs/phase0-baseline.md` §9.6、`docs/evidence/sqlite-diag-2026-11-15.txt`

## 背景

插件需要持久化自己的状态（`#note` 就是用它验证的），而上层（插件、事件上下文、后台任务）不能知道底层是 SQLite、Redis 还是旧核心的 Redis 键空间。在错误分类之前有两个具体风险：

1. **靠字符串匹配判错**：一旦换后端或换包装层，`if strings.Contains(err.Error(), "not found")` 就失效。
2. **错误泄露敏感内容**：把底层错误原样透传会把 key、value、数据库路径带进日志甚至回复文本。

存储的持久性设置同样是"不能拿吞吐换正确性"的问题：本机 G: 机械盘上默认 FULL 只有 ~9–11 提交/s，而 200 msg/s 目标需要 ~222 写/s，差距 20 倍以上。WAL+NORMAL 能到 2.2–2.9 万提交/s，但进程崩溃或断电可能丢掉最后若干未 checkpoint 的事务。

## 决定

### 1. 六个哨兵错误，上层用 `errors.Is`

```go
ErrKeyNotFound      // 键不存在或已过期
ErrStoreUnavailable // 后端不可用（打开失败、连接断开、IO 错误）
ErrStoreCommand     // 命令执行失败（Redis 返回错误等）
ErrStoreClosed      // 句柄已关闭（插件停止/应用关闭后）
ErrInvalidKey       // 键不合法（1–1024 字节）
ErrInvalidTTL       // TTL 不合法（负数或不可表示）
```

- 三个后端（SQLite、Redis、旧 Redis 兼容层）都只返回这六个错误之一，或用 `%w` 包装后仍可 `errors.Is` 命中；`context.Canceled`/`DeadlineExceeded` 原样返回（取消不是"没有数据"）。
- **错误文本不含 key/value**：`sqlite_test.go`、`redis_test.go` 显式断言错误串里不出现敏感 key/value 与地址。
- "不存在"永远返回 `ErrKeyNotFound`，绝不用 `nil, nil` 表示失败——`KVStore` 的注释写明"Errors never imply a successful read of no data"。

### 2. 应用级接口只暴露 Scope

```go
type Backend interface {
    Scope(string) sdk.KVStore
    Close() error
}
```

生命周期归应用所有，插件只拿到一个命名空间绑定后的 `KVStore`（`yzgo:<pluginID>:v1`）。插件无法通过 key 拼接访问别人的数据（SQLite 用独立列 + 绑定参数，Redis 用前缀）。

### 3. 默认 `synchronous(FULL)` + 回滚日志

`experimentPragmas()` 的默认分支只设 `busy_timeout(2500)` 与 `synchronous(FULL)`，保持回滚日志；只有显式设置 `YZGO_STORAGE_EXPERIMENT=wal-normal` 才追加 `journal_mode(WAL)` + `synchronous(NORMAL)`。

补充约束：SQLite 单连接（`SetMaxOpenConns(1)`），因为插件 KV 的写入模型是"短事务、低并发"，多连接只会放大锁竞争。

## 后果

- 插件可以写 `if errors.Is(err, sdk.ErrKeyNotFound)` 做分支，换后端行为不变。
- 插件停止后句柄被撤销（`ErrStoreClosed`），但已持久化的数据保留；重启可以读回。
- **默认 FULL 是本机的已知容量边界**：sqlite-diag 实测 9.3–11.8 提交/s（p50 72–84ms），Z4-A 的 10 msg/s 低速 30 分钟窗口能无损承接（actual 9.93/s 已贴上限），但 200 msg/s 下 FULL 会积压到 dispatcher 占满、连接断开。这不是 bug，是"默认持久性优先"的直接代价。
- 提升写吞吐的唯一合规路径是：显式实验开关（WAL+NORMAL）、换后端（C: NVMe 上 FULL 约 300 提交/s）、或另立产品决策——不得偷偷改默认。
- WAL+NORMAL 的代价写进文档：崩溃/断电可能丢最后若干未 checkpoint 事务；`journal_mode(WAL)` 启动时需短暂独占锁，快速重启竞争场景不适用。

## 备选方案与被否原因

| 方案 | 为什么不用 |
|---|---|
| 错误用字符串/错误码匹配 | 换后端就失效；也无法保证错误文本不泄露 key。 |
| 直接透传底层错误（`sql.ErrNoRows`、redis.NIL） | 上层会被迫依赖具体后端；错误串还会带路径/值/地址。 |
| 用 `nil, nil` 表示"不存在" | 无法区分"读成功但没数据"和"读失败"；调用方极易把故障当空值继续。 |
| 默认 WAL+NORMAL（为了吞吐） | 断电丢最后若干事务，不能作为默认；这一条已写进阶段零目标表的"回退门槛"。 |
| 把 `Backend.Scope` 也暴露给插件 | 插件会自己选命名空间，隔离边界失效；生命周期也会被插件控制。 |
| SQLite 多连接提升并发 | 当前模型是单写者短事务，多连接收益低、锁竞争与 `database is locked` 风险高；实测数据（sqlite-diag）也显示瓶颈在 fsync 而不在连接数。 |

## 如果重来一次

仍然会先做错误分类再考虑吞吐：错误边界一旦被字符串匹配污染，后续每换一个后端都要重审调用方。默认值也仍然选 FULL——性能目标可以靠"换盘/换后端/显式开关"解决，而"默认会丢事务"是用户不可见的风险。唯一会调整的是：更早把 `sqlite-diag` 独立跑起来，让"FULL 只有 ~9/s"这条边界在阶段零之前就可见，而不是在 Z2 失败之后才补证。
