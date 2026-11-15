# 证据与验证约定（2026-11-15）

本文件回答四个问题：结果和日志怎么命名、什么进版本库、什么只留本机、一次验证怎么按 HEAD 追溯。归档位置与清单见 `docs/evidence-index.md`。

## 1. 命名规则

### 1.1 结果 JSON

```text
<variant>-<workload>-<scenario>[-<rate>rps][-<tag>]-run<N>.json
```

| 段 | 取值 | 例 |
|---|---|---|
| `variant` | `go`（Go 版被测）/ `old`（Node 旧版被测） | `go-reply-steady-qpc-run1.json` |
| `workload` | `reply` / `scan` / `render` | `go-render-single-run1.json` |
| `scenario` | `steady` / `burst`，渲染轮用 `single`/`concurrent`/`fallback`/`recovery` | `go-render-concurrent-run1.json` |
| `rate` | 请求速率，容量轮才带 | `go-reply-group-200rps.json` |
| `tag` | 实验/批次标记，如 `qpc`、`qpc2`、`verified`、`wal`、`400target`、`loader-cap` | `go-reply-group-400rps-verified-run1.json` |
| `runN` | 第几轮；同一参数重复测量必须带 | `go-reply-burst-qpc-run2.json` |

规则：

- **参数证据不靠文件名**：新结果 JSON 自记录 `message_type`/`group_count`/`user_count`/`mode`/`rate`，容量判定以 JSON 字段为准；文件名只是人读的标签。
- 已提交的历史结果**不改名、不重写**。
- 同一参数的新一轮必须换 `runN` 或 `tag`，不得覆盖旧结果（脚本已有"显式失败并断言删除成功"的保护）。

### 1.2 跟随日志

结果旁边同名前缀：

| 后缀 | 内容 |
|---|---|
| `.json.log` | 该轮 loadgen 的 stdout（通常为空，不代表失败） |
| `.json.log.err` | 该轮 loadgen / 被测 bot 的 stderr 原始日志（重连、崩溃、慢写都在这里） |
| `.json.bot.log` / `.json.bot.err` | 被测 bot 进程自己的 stdout/stderr（`Invoke-BenchScenario.ps1` 跟随结果名生成，不用固定 `run.err`） |

### 1.3 验证日志

```text
verify-<head>-<yyyyMMdd-HHmmss>[-<level>].log
```

- `head`：运行时 `git rev-parse --short HEAD`（取不到时为 `unknown`）。
- `level`：`fast` 或 `full`（`scripts/Verify-All.ps1 -Level`，见 §4）。
- 例：`verify-daad916-20261115-145342.log`（历史，无 level 后缀，等价 Full）。

一次性检查（如某批的收口核验）可以用 `verify-batch<N>-<stamp>.log`，同属本机工作副本。

### 1.4 其他证据

| 类型 | 命名 | 位置 |
|---|---|---|
| 渲染指标快照 | `render-metrics-<scenario>[-<run>].txt` | `bench/env/results/` |
| goroutine 转储 | `goroutine-<scenario>-midrun.txt` | `bench/env/results/` |
| profile 采集 | `capture-<rate>.json` + `manifest.json` | `bench/env/profiles/<batch>/` |
| 独立 SQLite 诊断原始输出 | `sqlite-diag-<date>.txt` | `bench/env/results/`（代表份归档到 `docs/evidence/`） |

## 2. 什么进版本库

- 代码、脚本、schema、配置模板；
- `docs/` 下的全部项目文档；
- `docs/evidence/` 里的**少量代表文件**：必须同时满足"文档正文直接点名"与"单文件 ≤ 50MB"，一次运行最多 1 个代表文件；
- 文档结论里的每个数字都必须能指向：`docs/evidence/<file>`、某个提交里的结果（按 §3 取回），或明确标注"未验证/无法复核"。

## 3. 什么只留本机

| 内容 | 原因 |
|---|---|
| `bench/env/` 全部（results / profiles / verification / 原始 `.err`、`.log`、`.gz`） | 逐事件 JSON 单轮 65–70MB，profile 与验证记录可由脚本重采；整个目录写入 `.gitignore`，磁盘文件保留在本机 |
| `mysy2/` | 可重建的运行环境 |
| `*.exe`、`*.sqlite`、`*.db`、`*.log` | 构建/运行产物 |

本机副本可以被后续运行覆盖或清理；**可复核性由 `git show` 保证**（见下节），不依赖本机是否还留着。

## 4. 提交前 Fast、收口 Full

| 场景 | 命令 | 覆盖 | 预算 |
|---|---|---|---|
| 每次提交（`pre-commit` 钩子） | `scripts/hooks/pre-commit` → 暂存 `.go` 的 gofmt + `git diff --cached --check` | 只查格式，不构建 | < 1s |
| 每次推送（`pre-push` 钩子） | `scripts/Test-Go.ps1 -Level Fast` | gofmt + build + vet（不跑 `go test`） | 独立实测 29.6–38.5s；曾因本机 IO 停滞在提交内涨到 61.6s |
| 收口 / 合并前 | `scripts/Verify-All.ps1 -Level Full` | 四个目标：gofmt + test + build + vet | 约 68s（历史 Full 实测） |

分两层的理由：把完整 Fast 塞进 `pre-commit` 后，实际提交要等 61.6s；人会用 `--no-verify` 绕过，门禁反而失效。提交只拦"格式/空白"这类零成本错误，构建级检查移到推送前。

```powershell
# 提交前
& .\scripts\Test-Go.ps1 -Level Fast

# 收口（默认 Level=Full），日志写到 bench/env/results/verify-<head>-<stamp>-full.log
& .\scripts\Verify-All.ps1

# 只跑快速检查并留日志
& .\scripts\Verify-All.ps1 -Level Fast
```

判定口径：

- `Fast` 只回答"没弄坏格式/编译/静态检查"，**不能证明测试通过**；
- `Full` 是唯一可以作为"这批收口"的门禁；
- 两者都必须串行（`-Parallelism 1` 默认）：本机内存偏紧，包级并行会 OOM 或触发停滞超时。
- 钩子安装：`scripts/Install-GitHooks.ps1`（把 `scripts/hooks/pre-commit` 与 `pre-push` 复制到 `.git/hooks`）。仓库目前没有 remote，钩子是本地唯一自动门禁；接上 remote 后由 `.github/workflows/verify.yml` 承担同一角色（见 §7）。
- 临时跳过推送门禁：`YZGO_SKIP_GATE=1 git push`（会打印提示，不是静默跳过）。

## 5. 按 HEAD 追溯一次验证

1. **找日志**：`Verify-All.ps1` 打印 `HEAD=<sha>` 与 `日志=<path>`；日志名本身带 HEAD 与时间。
2. **找结果**：结果文件名带轮次；文档引用时应同时给出文件名与文档位置（做法见 `docs/evidence-index.md` §2）。
3. **取回已退出索引的文件**：

```powershell
# 列出某提交时 bench/env/results 下有哪些文件
git -C "G:\ALL WORK\音无\音无" ls-tree --name-only <sha> bench/env/results

# 取回单份（用 cat-file 而不是 >，避免 PS 5.1 写 UTF-16）
git -C "G:\ALL WORK\音无\音无" cat-file -p "<sha>:bench/env/results/<file>" |
    Out-File -Encoding utf8 -LiteralPath "$env:TEMP\<file>"
```

4. **复现同一轮**：按文档记录的参数重跑（`bench/scripts/Invoke-BenchScenario.ps1`），结果写到 `bench/env/results/`，不复用旧文件名。
5. **对不上就停**：文档数字与原始 `summary` 不一致时，先查清证据，不要改文档把差异抹平。

## 6. 评审清单（写/改证据前）

- [ ] 文件名符合 §1，带轮次，不覆盖旧文件；
- [ ] 结果 JSON 有 `schema_version`（新结果必须，历史除外）；
- [ ] 数字能回到 `summary` 字段或 `events` 首尾 `sent_ns` 复算；
- [ ] 未做的测量写成"未验证 + 原因"，不写成"已达标"；
- [ ] 只有"文档正文引用 + ≤ 50MB"的文件才进 `docs/evidence/`，并在 `docs/evidence-index.md` 登记；
- [ ] `git diff --check` 通过，`git status` 只包含本批预期变更。

## 7. CI 与工具链解析

本地与 CI 跑的是同一个脚本，差异只在"Go 从哪来、缓存写到哪"：

| 项 | 解析顺序 | 本机结果 | CI 结果 |
|---|---|---|---|
| Go 可执行文件 | `-GoExecutable` → `YZGO_GO_BIN` → 工作区父目录 `tools\go\bin\go.exe` → `PATH` | 命中工作区自带 `tools\go` | 命中 `setup-go` 放进 `PATH` 的 `go` |
| 缓存根 | `-CacheRoot` → `YZGO_CACHE_ROOT` → `RUNNER_TEMP` → 工作区父目录 | 落在 G 盘工作区 | 落在 runner 临时目录 |

- 工作流文件：`.github/workflows/verify.yml`。运行矩阵：**默认分支 push 与 PR 跑 Full**（含单元测试，单测失败不能合并）；其他分支 push 跑 Fast（快速反馈）；`workflow_dispatch` 可选 Level。默认分支名取自 `github.event.repository.default_branch`，不写死 `master`。
- Go 版本只认一个来源：`go-render/go.mod` 的 `go` 指令（四个模块里最高），不在工作流里另写死版本号。
- **未验证**：本机从未在"没有 `tools\go`、只能走 PATH"的环境跑过这个脚本；该分支要等第一次 CI 执行才算实测。
- 如果 CI 与本机结果不一致，先比对 `go version` 与 `GOFLAGS`，再怀疑代码——不要直接改测试让它变绿。
