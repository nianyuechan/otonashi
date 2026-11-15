# 证据索引（2026-11-15）

本文件回答三件事：

1. 哪些结果被长期归档在 `docs/evidence/`，各自支撑哪条文档结论；
2. 其余结果在哪里、为什么不进版本库；
3. 已提交过但退出索引的历史结果，怎么按 HEAD 取回。

命名规则与"什么进库、什么只留本机"的判定标准见 `docs/evidence-conventions.md`。

## 1. 三类位置

| 位置 | 是否进版本库 | 用途 | 生命周期 |
|---|---|---|---|
| `docs/evidence/` | **是** | 文档结论的**可复核快照**，内容与采集时逐字节一致 | 不可变；只新增，不覆盖、不改写 |
| `bench/env/`（results / profiles / verification / 原始日志） | **否**（`.gitignore` 已忽略整个目录） | 本机工作副本：逐事件结果、profile、验证记录、原始日志 | 可被新一轮运行覆盖或被清理 |
| `git show <sha>:<path>` | 历史对象 | 取回已提交过、现已退出索引的结果 | 直到历史被重写为止 |

`bench/env/` 整体退出索引后，磁盘文件**全部保留**；`.gitignore` 的 `bench/env/` 规则只影响此后新文件是否入库，不影响已有文件。

> 换行符说明：仓库 `core.autocrlf=true`，checkout 到工作区时副本可能变为 CRLF；索引中的 blob 内容（`git cat-file -p <sha>:docs/evidence/<file>`）与采集时一致。JSON 语义不受换行符影响，逐字节校验请以 blob 为准。

## 2. 归档清单（`docs/evidence/`，20 个文件）

按结论分组，每行给出"该文件能复核的结论"和"文档出处"。文件保持原始文件名，未改扩展名。

### 2.1 阶段零消息链路对照（`docs/phase0-baseline.md`）

| 文件 | 可复核的结论 | 出处 |
|---|---|---|
| `go-reply-steady-qpc-run1.json` | Go 稳态 20 msg/s：paired 593/593 全样本、p50 0.42ms / p95 0.83ms / p99 1.33ms、CPU 0.3%、RSS 19.4MB | §4.1 |
| `old-reply-steady-run1.json` | 旧版同场景：p50 2.90ms / p95 4.81ms / p99 62.68ms、CPU 4.9%、RSS 127.2MB | §4.1 |
| `go-reply-steady-qpc2-run1.json` | 同批次重采 Go 稳态：p50 0.397ms / p99 1.159ms | §4.4 |
| `old-reply-steady-qpc-run1.json` | 同批次重采旧版稳态：p50 3.159ms / p99 43.61ms | §4.4 |
| `go-reply-burst-qpc-run1.json` | 同批次 Go 突发 500 条：paired 250/250、p50 4.944ms / p99 6.167ms | §4.4 |
| `go-reply-burst-qpc-run2.json` | 同上第二轮：p50 5.308ms / p99 6.249ms | §4.4 |
| `old-reply-burst-qpc-run1.json` | 同批次旧版突发：p50 719.86ms / p99 1035.00ms | §4.4 |

### 2.2 群聊容量（`docs/capacity-100-groups-200rps.md`）

| 文件 | 可复核的结论 | 出处 |
|---|---|---|
| `go-reply-steady-200rps.json` | 100 群/2 万发送者、200 target 私聊档：sent 10666 / paired 5333 / missing 0、p50 0.30ms / p99 0.82ms | §1.1 |
| `go-reply-group-200rps.json` | 同参数群聊档：sent 10671 / paired 5336 / missing 0、p50 0.28ms / p99 0.76ms | §1.1 |
| `go-reply-group-400rps-verified-run1.json` | 400 档：actual injection 327.3/s、reported 280.5、missing 0、p50 0.274ms / p99 0.630ms | §1.2 |
| `go-reply-group-600rps-verified-run1.json` | 600 档：actual injection 437.1/s、reported 374.6、missing 0、p99 0.543ms | §1.2 |
| `go-reply-group-700rps-verified-run1.json` | 700 档：actual injection 543.3/s（最高注入）、reported 465.5、missing 0、p99 0.490ms | §1.2 |
| `go-reply-group-900rps-loader-cap-run1.json` | 900 诊断档：actual injection 639.2/s、reported 547.8、missing 0、p99 0.474ms → 证明"上限在负载器侧" | §1.2、§2 |

### 2.3 渲染链路（`docs/phase0-baseline.md` §9.4）

| 文件 | 可复核的结论 | 出处 |
|---|---|---|
| `go-render-single-run1.json` | 单次渲染：sent 60、text_replies 0、action_errors 0 → 60/60 图片段成功 | §9.4 |
| `go-render-concurrent-run1.json` | 并发 2 / 队列 2：sent 478 = 234 图片 + 244 条降级文本（1:1） | §9.4 |
| `go-render-fallback-run1.json` | 渲染服务停止：sent 10、text_replies 10 → 10/10 降级文本、无崩溃 | §9.4 |
| `go-render-recovery-run1.json` | 重启 go-render 后：sent 30、text_replies 0 → 30/30 自动恢复 | §9.4 |
| `goroutine-fallback-midrun.txt` | 降级轮 bot 主进程 goroutine 总数 9，转储内**无任何 go-render 栈帧** → "渲染器有界性未验证"的唯一依据 | §9.4、`docs/plan.md` 阶段零表 |

### 2.4 存储与写路径

| 文件 | 可复核的结论 | 出处 |
|---|---|---|
| `sqlite-diag-2026-11-15.txt` | 默认 FULL 在 G: 机械盘 9.3–11.8 提交/s（p50 72–84ms）；WAL+NORMAL 22,411–28,708 提交/s；C: NVMe FULL 298.8–314.4 提交/s | `docs/phase0-baseline.md` §9.6 |
| `go-reply-group-realwrite-200-wal-run2.json.log.err` | Z2 最差轮（200 target 30 分钟，WAL+NORMAL）的原始 bot stderr：7 次重连尝试、8 条读循环提前结束 → 支撑"9 个丢失窗口、missing 13855（7.34%）"的机制判断 | `docs/phase0-baseline.md` §9.3 |

## 3. 未归档的结果（`bench/env/results/`）

### 3.1 现状

- 磁盘上 **145 个文件**；其中 **89 个曾进入索引**（本轮退出），**56 个一直只在工作区**（主要是 `*.log` 规则忽略的 0 字节日志与 bot 输出）。
- 2026-11-15 二次清理：`bench/env` 下另有 **68 个已跟踪文件**（47 份 profile、16 份 `verification-2026-11-15`、4 个 `.err`、1 个 json）一并退出索引；两轮合计 **157 个**，磁盘 18,311 个文件一个未删。
- 目录总大小约 **374.6MB**，其中 4 个 Z2 逐事件结果各 67–70MB。
- 本机目录现在整体被 `.gitignore` 忽略；`git status` 不会再显示它们。

### 3.2 为什么不全部归档

- 逐事件结果（`events` 数组）体积大：单个 30 分钟 Z2 轮次 67–70MB，复制 20 个就是数百 MB，与"版本库只放可复核小块"的目标冲突。
- 文档结论只需要 `summary` 字段与少量原始日志；完整逐事件数组用于复算时再按第 4 节取回。

### 3.3 明确未做快照的大文件

| 文件 | 大小 | 处理 |
|---|---|---|
| `go-reply-group-realwrite-200-wal-run2.json` | 67.8MB | 不入 `docs/evidence/`；用同轮的 `.log.err`（35KB）替代归档；`summary` 数字按第 4 节从历史取回 |
| `go-reply-group-realwrite-200-wal-run3.json` | 70.4MB | 同上（未归档，历史可取回） |
| `go-reply-group-realwrite-200-wal-run4.json` | 70.5MB | 同上 |
| `go-reply-group-realwrite-400target-wal-run1.json` | 70.3MB | 同上 |

### 3.4 其余未入 `docs/evidence/` 的已点名文件

`go-reply-group-250rps.json`、`go-reply-group-300rps.json`（200–300 档趋势已由 400–900 档覆盖）、各档 `*-run2.json`（与 run1 同参数重复轮）、`render-metrics-*.txt`（数值已写入文档表格）、`verify-*.log`（本机验证日志，`*.log` 规则本就不入库）等，均只保留在本机工作副本，需要时按第 4 节从历史取回。完整点名清单位于 `docs/phase0-baseline.md`、`docs/capacity-100-groups-200rps.md`、`docs/plan.md`。

## 4. 按 HEAD 追溯

### 4.1 列出某提交时的结果清单

```powershell
git -C "G:\ALL WORK\音无\音无" ls-tree --name-only d4daa47 bench/env/results
```

`d4daa47` 是"证据减负"前的最后一个提交（`docs: record phase one execution`），此时 89 个结果仍在索引内。

### 4.2 取回一个已退出索引的结果

```powershell
git -C "G:\ALL WORK\音无\音无" cat-file -p "d4daa47:bench/env/results/go-reply-group-realwrite-200-wal-run2.json" |
    Out-File -Encoding utf8 -LiteralPath "$env:TEMP\wal-run2.json"
```

用 `cat-file -p` + `Out-File -Encoding utf8` 而不是 `>`：PowerShell 7 的 `>` 写 UTF-8 无 BOM 尚可，Windows PowerShell 5.1 的 `>` 会写 UTF-16LE，解析 JSON 前必须转码。

### 4.3 只读 summary，不落盘（67MB 文件也很快）

```powershell
git -C "G:\ALL WORK\音无\音无" cat-file -p "d4daa47:bench/env/results/go-reply-group-realwrite-200-wal-run2.json" |
    python -c "import sys,json; s=json.load(sys.stdin)['summary']; print({k:s[k] for k in ('sent','expected_replies','paired_replies','missing_replies','action_errors')})"
```

### 4.4 复算一次容量档的 actual injection

`*-verified-run*.json` 不重写，其 `summary` 无 `actual_injection_eps` 字段；按 `sent / (最后一条 sent_ns − 第一条 sent_ns)` 从 `events` 复算（700 档 run1 得 543.3/s，与文档一致）。

## 5. 归档选择规则（为什么是这 20 个）

1. **只算"文档明确点名"的文件**：`docs/phase0-baseline.md`、`docs/plan.md`、`docs/capacity-100-groups-200rps.md` 三份文档中写出完整文件名的结果与日志，共 33 个候选。
2. **上限 20 个**，按"每条文档结论至少有 1 个可复核文件"取舍：
   - 同类重复轮（run2、run3）优先让位给 run1；
   - 同一结论的两批测量（第一批 §4.1 与同批次 §4.4）各留 1 个代表；
   - 单文件 > 50MB 的逐事件 JSON 不复制，改用同轮原始日志；
   - 已被后续批次覆盖的早期档（250/300）让位给 400–900 验证批。
3. **归档文件内容不得改写**：复制是逐字节拷贝，SHA256 与源文件一致；历史结果作为证据是不可变的。

## 6. 新增证据时怎么做

1. 先按 `docs/evidence-conventions.md` 命名，结果落到 `bench/env/results/`（默认不入库）。
2. 只有"文档正文直接引用、且体积 ≤ 50MB"的文件才考虑复制到 `docs/evidence/`；一次运行最多 1 个代表文件。
3. 在本文件第 2 节追加一行（文件 + 结论 + 出处），再在引用它的文档里写 `docs/evidence/<文件名>`。
4. 不为归档改写历史文件；需要更正结论时改文档并注明"依据为历史版本，见 commit"。
