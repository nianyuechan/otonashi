# `bench` 结果 schema v1

## 1. 机器 schema

- 文件：`bench/schema/result-v1.schema.json`
- `schema_version` 固定为 `"1"`。
- Go 常量：`bench/internal/loadgen.ResultSchemaVersion`。
- `WriteResult` 在写入前调用 `Result.Validate`，缺版本、缺必填字段、计数为负都会拒绝写入。

## 2. 版本分层

- `schema_version`：结果 JSON 的字段/类型契约，由本文件定义。
- `protocol_version`：压测负载使用的 OneBot 协议/负载定义版本，不是结果字段版本。
- 两者不能互相替代。

## 3. 历史结果兼容

- 2026-11-15 之前的结果没有 `schema_version`；`json.Unmarshal` 仍可读取，读取时不调用 `Validate`。
- 新结果必须带 `"schema_version": "1"`；旧结果不能直接作为新写入模板。
- `expected_text_replies` / `missing_text_replies` 是 2026-11-15 新增字段；旧结果缺省为 0。

## 4. 关键口径

- `missing_replies` 仍只覆盖 `Seq > 0` 的带序号消息。
- `missing_text_replies` 是固定文本回复的保守下界，不能当作全量消息零丢失证明。
- `p95` / `p99` 是已回复样本的条件尾延迟，不包含丢失和超时样本。

## 5. 变更规则

1. 新增字段：先更新 schema 和 `Result`，再补兼容测试；旧字段语义不变。
2. 删除/改名/类型变化：升级 `schema_version`，不要直接改 v1。
3. 任何字段语义变化都要同步 `docs/phase0-baseline.md` 的口径说明。
