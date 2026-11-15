# ADR 0001：结果 JSON 使用独立的 schema_version

- 状态：已采纳（2026-11-15，Q1，提交 `bdadc08`）
- 相关代码：`bench/internal/loadgen/metrics.go`（`ResultSchemaVersion`、`Result`、`Result.Validate`、`WriteResult`）
- 相关文档：`docs/result-schema-v1.md`、`bench/schema/result-v1.schema.json`

## 背景

`bench` 的每次运行把指标写成一份 JSON（`summary` + `events` + `samples`）。在 Q1 之前，这份 JSON 只有 `protocol_version` 一个版本字段，它表示**压测负载**用的 OneBot 协议/消息定义版本，而不是结果字段的契约版本。结果是：

- 结果字段靠 Go 结构体直接扩展，没有机器可读的契约；
- 新旧结果是否可比较只能靠人读文档；
- 2026-11-14 的审计已经踩过一次：`missing_replies` 只覆盖 `Seq > 0` 的消息，但报告被读成"全量消息零丢失"，因为结果里没有任何字段能表达覆盖范围。

同时仓库里有几十份历史 JSON 已经提交，不能重写（证据不可变）。所以需要一种"新结果可校验、旧结果仍可读"的机制。

## 决定

1. 在 `Result` 里新增 `schema_version`，常量 `ResultSchemaVersion = "1"`，与 `protocol_version` **并列且互不替代**：
   - `schema_version` 约束结果字段/类型；
   - `protocol_version` 约束负载协议与消息样本定义。
2. `Result.Validate()` 校验版本、`protocol_version` 非空、`mode`/`workload`/`kind` 枚举、时间字段非空、`summary.sent > 0`、所有计数字段非负；`WriteResult()` 在落盘前调用，校验失败直接返回错误、不写文件（临时文件 + rename 保证原子性）。
3. 机器 schema 只保留一份：`bench/schema/result-v1.schema.json`；文档 `docs/result-schema-v1.md` 解释口径，不复制字段清单。
4. **兼容策略**：
   - 新增字段视为兼容（v1 内允许追加），例如 `expected_text_replies` / `missing_text_replies`，旧结果缺省按 0 读；
   - 删除、改名、类型变化、语义变化都必须升级 `schema_version`，不得直接改 v1；
   - 历史 JSON 读取**不调用** `Validate()`（它们没有 `schema_version`），但也不能被当作新结果的写入模板。

## 后果

- 新结果一定能被机器判定版本；缺失版本的新写入会被拒绝（有测试 `TestWriteResultRejectsMissingSchemaVersion`）。
- 旧结果继续可读（`TestLegacyResultJSONReads`），文档里"历史 JSON 没有新字段"成为明确事实而不是猜测。
- 结论口径必须写在文档里，例如 `missing_text_replies` 是"固定文本回复的保守下界"，不能当全量零丢失证明。
- 维护成本：每次改字段要同时动 schema、`Result`、测试和 `docs/result-schema-v1.md`——这是有意的，防止"代码改了文档没改"。

## 备选方案与被否原因

| 方案 | 为什么不用 |
|---|---|
| 复用 `protocol_version` 当结果版本 | 两者变化频率不同：加一个统计字段不该强迫负载协议升级，协议升级也不该让历史结果格式失效。混用会让任何一边的变更都被另一边绑死。 |
| 每次加字段就升 `schema_version` | 代价过高。结果 JSON 的可扩展性主要来自"追加字段"，每次都升版会让版本号失去信号价值（v1 到 v7 只差几个统计项）。 |
| 只用 Go struct、不写 JSON Schema | 外部工具（Python/jq/CI）无法校验；也无法在测试里比对"schem 文件与常量一致"（现有 `TestResultSchemaFileIsValidJSON`）。 |
| 校验历史结果并回填 `schema_version` | 违反"历史证据不可变"：改写已提交结果会让文档引用的数字失去可追溯性。只允许读，不允许改写。 |
| 用 protobuf/自定义二进制格式 | 当前结果主要给人看、给脚本读；JSON + Schema 的通用性更高，改造收益不足。 |

## 如果重来一次

仍然会把"结果字段版本"和"负载协议版本"拆成两个字段。这次审计暴露的问题不是"缺一个版本号"，而是"缺一个能说明字段语义边界的机制"；独立 `schema_version` + 单一 JSON Schema + 文档口径三者配套，才能同时回答"这份结果能不能机器校验"和"这个数字覆盖了什么"。
