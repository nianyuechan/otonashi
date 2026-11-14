import csv
import json
import re
from pathlib import Path
from urllib.parse import unquote

WORK = Path(__file__).resolve().parent
OUTPUT = WORK.parent / "outputs" / "TRSS-Yunzai-Go-native-rewrite-blueprint.md"
REPORT = WORK / "validation-report.json"
text = OUTPUT.read_text(encoding="utf-8")
errors = []
checks = {}

summary = json.loads((WORK / "log-command-summary.json").read_text(encoding="utf-8"))
usage = json.loads((WORK / "usage-by-directory.json").read_text(encoding="utf-8"))
command_total = summary["total_hits"]["command_complete"]
usage_total = sum(item["count"] for item in usage["unique_or_grouped_directories"]) + sum(
    item["count"] for item in usage["unmapped"]
)
checks["command_usage_reconciles"] = command_total == usage_total == 2051
if not checks["command_usage_reconciles"]:
    errors.append(f"命令数不对账: summary={command_total}, usage={usage_total}")
checks["task_counts_reconcile"] = (
    summary["total_hits"]["scheduled_task_start"]
    == summary["total_hits"]["scheduled_task_complete"]
    == 5845
)
checks["uncompressed_log_scope"] = (
    summary["source"]["command_file_count"] == 15
    and summary["source"]["uncompressed_log_file_count"] == 16
)

with (WORK.parents[2] / "2026-11-12" / "trss-refactor-assessment" / "work" / "plugin-metrics.csv").open(
    encoding="utf-8-sig", newline=""
) as handle:
    expected_directories = {row["name"] for row in csv.DictReader(handle)}
with (WORK / "migration-batches.csv").open(encoding="utf-8-sig", newline="") as handle:
    batch_rows = list(csv.DictReader(handle))
actual_directories = {row["directory_or_slice"].split("/")[0] for row in batch_rows}
missing = sorted(expected_directories - actual_directories)
extra = sorted(actual_directories - expected_directories)
checks["all_38_directories_covered"] = not missing and not extra and len(expected_directories) == 38
if missing or extra:
    errors.append(f"目录覆盖差异: missing={missing}, extra={extra}")

links = re.findall(r"\[[^\]]+\]\((file:///[^)]+)\)", text)
link_results = []
for link in links:
    target = link[len("file:///") :]
    path_part, _, anchor = target.partition("#")
    path = Path(unquote(path_part))
    result = {"link": link, "exists": path.exists(), "anchor_valid": True}
    if not path.exists():
        errors.append(f"链接不存在: {link}")
    elif anchor:
        match = re.fullmatch(r"L(\d+)(?:-L(\d+))?", anchor)
        if not match:
            result["anchor_valid"] = False
        else:
            first = int(match.group(1))
            last = int(match.group(2) or first)
            line_count = sum(1 for _ in path.open("r", encoding="utf-8", errors="replace"))
            result["anchor_valid"] = 1 <= first <= last <= line_count
        if not result["anchor_valid"]:
            errors.append(f"行号无效: {link}")
    link_results.append(result)
checks["file_links_valid"] = all(item["exists"] and item["anchor_valid"] for item in link_results)
checks["file_link_count"] = len(links)

required_phrases = [
    "Go 项目结构与依赖选型",
    "Go 插件契约",
    "迁移顺序",
    "行为对等：历史日志回放 + 离线重放",
    "里程碑与完成判据",
    "上游分叉策略",
    "会推翻方案的关键假设",
    "单协议端连接并跑通一条消息往返",
    "会话与 Redis 迁移完成",
    "渲染服务独立可用",
    "首批插件可用",
    "全量切换并具备回滚",
]
missing_phrases = [phrase for phrase in required_phrases if phrase not in text]
checks["required_sections_present"] = not missing_phrases
if missing_phrases:
    errors.append(f"缺少章节: {missing_phrases}")

checks["onebot_only_scope"] = not re.search(
    r"Milky|Satori|Lagrange|ComWeChat|GSUIDCore|OPQBot|NapCat|go-cqhttp", text, re.I
)
if not checks["onebot_only_scope"]:
    errors.append("蓝图仍含已排除的协议分支")

required_snowluma_details = [
    "127.0.0.1:3001/", "Authorization: Bearer ***", "messageFormat=array",
    "reportSelfMessage=false", "httpClients=[]", "2536 仅作旧行为对照",
]
checks["snowluma_topology_documented"] = all(detail in text for detail in required_snowluma_details)
required_live_gates = ["[需 SnowLuma 实例]", "blocked-not-run", "不能宣布 M6 完成"]
checks["live_gates_explicit"] = all(gate in text for gate in required_live_gates)

snowluma_summary = json.loads((WORK / "snowluma-log-summary.json").read_text(encoding="utf-8"))
snowluma_totals = snowluma_summary["totals"]
action_summary = json.loads((WORK / "snowluma-action-structures.json").read_text(encoding="utf-8"))
checks["snowluma_log_stats"] = (
    snowluma_totals["lines"] == 149437
    and snowluma_totals["human_readable_event_categories"].get("群") == 12012
    and snowluma_totals["action_name_mentions"].get("send_msg") == 31
    and snowluma_totals["json_object_lines"] == 0
    and action_summary["record_count"] == 31
    and action_summary["missing_echo_count"] == 31
    and action_summary["missing_message_id_count"] == 31
    and action_summary["missing_retcode_count"] == 31
)
checks["snowluma_log_stats_documented"] = all(
    value in text for value in ["149,437", "12,012", "`send_msg` 31 次提及", "日期不重叠"]
)

# 只报告是否泄漏，不把敏感值写入验证报告或错误信息。
sensitive_values = []
snowluma_root = Path(r"D:\Toshikoshi_chan\Agent\SnowLuma")
raw_onebot = json.loads((snowluma_root / "config" / "onebot_2406354517.json").read_text(encoding="utf-8-sig"))
for network_type in ("httpServers", "wsServers"):
    for item in raw_onebot["networks"][network_type]:
        value = item.get("accessToken")
        if isinstance(value, str) and value:
            sensitive_values.append(value)
raw_webui = json.loads((snowluma_root / "config" / "webui.json").read_text(encoding="utf-8-sig"))
for key in ("passwordHash", "passwordSalt"):
    value = raw_webui.get(key)
    if isinstance(value, str) and value:
        sensitive_values.append(value)
stdout_text = (snowluma_root / "logs" / "stdout.log").read_text(encoding="utf-8", errors="replace")
initial_password = re.search(r"initial credentials:.*?password=([^\s]+)", stdout_text)
if initial_password:
    sensitive_values.append(initial_password.group(1))

checked_texts = {"blueprint": text}
for path in WORK.glob("snowluma-*"):
    if path.suffix in {".json", ".txt"} and path.name != "validation-report.json":
        checked_texts[path.name] = path.read_text(encoding="utf-8", errors="replace")
secret_hits = 0
secret_locations = []
for name, content in checked_texts.items():
    file_hits = sum(1 for value in sensitive_values if len(value) >= 4 and value in content)
    file_hits += len(re.findall(r"Bearer\s+(?!\*\*\*)[A-Za-z0-9._~+/=-]+", content))
    file_hits += len(re.findall(r"(?i)password=(?!\*\*\*)\S+", content))
    if file_hits:
        secret_locations.append({"file": name, "hits": file_hits})
    secret_hits += file_hits
checks["credentials_redacted"] = secret_hits == 0
if secret_hits:
    errors.append(f"检测到 {secret_hits} 处疑似未脱敏凭据；敏感值未写入本报告；位置={secret_locations}")

for name in ("onebot_only_scope", "snowluma_topology_documented", "live_gates_explicit", "snowluma_log_stats", "snowluma_log_stats_documented"):
    if not checks[name]:
        errors.append(f"新增校验失败: {name}")

report = {
    "ok": not errors and all(value for value in checks.values() if isinstance(value, bool)),
    "checks": checks,
    "errors": errors,
    "blueprint_bytes": OUTPUT.stat().st_size,
    "blueprint_lines": len(text.splitlines()),
    "batch_rows": len(batch_rows),
    "links": link_results,
}
REPORT.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
print(json.dumps({key: value for key, value in report.items() if key != "links"}, ensure_ascii=False, indent=2))
raise SystemExit(0 if report["ok"] else 1)
