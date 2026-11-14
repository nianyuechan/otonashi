import json
import re
from collections import Counter, defaultdict
from pathlib import Path

from inspect_snowluma import ROOT, OUT, sanitize_text

LINE = re.compile(r"^(\d{2}:\d{2}:\d{2})\s+([A-Z]+)\s+(.*)$")
EVENT = re.compile(r"\[Event\]\s*([^\s\[]+)")
JSON_FIELDS = [
    "post_type", "message_type", "sub_type", "notice_type", "request_type", "meta_event_type",
    "action", "retcode", "echo", "message_id", "self_id", "user_id", "group_id", "message",
    "raw_message", "sender", "time",
]
ACTIONS = [
    "send_msg", "send_private_msg", "send_group_msg", "get_login_info", "get_cookies",
    "get_csrf_token", "get_version_info", "get_online_clients", "get_guild_service_profile",
    "get_group_member_list", "get_group_list", "get_friend_list", "delete_msg", "send_like",
]
CONNECTION_PATTERNS = {
    "ws_server": re.compile(r"OneBot\.WS-Server|ws-default|(?<!\d)3001(?!\d)"),
    "ws_client": re.compile(r"OneBot\.WS-Client|Maimai|(?<!\d)7988(?!\d)"),
    "yunzai_historical_listener": re.compile(r"(?<!\d)2536(?!\d)|/OneBotv11"),
    "http_server": re.compile(r"http-default|OneBot\.HTTP|(?<!\d)3000(?!\d)"),
}
MARKERS = ["[图片]", "[视频]", "[语音]", "[文件]", "[表情]", "[回复]", "[At]", "CQ:"]


def walk_json(value, field_counts, value_counts):
    if isinstance(value, dict):
        for key, item in value.items():
            if key in JSON_FIELDS:
                field_counts[key] += 1
                if key in {"post_type", "message_type", "sub_type", "notice_type", "request_type", "meta_event_type", "action", "retcode"}:
                    value_counts[key][str(item)] += 1
            walk_json(item, field_counts, value_counts)
    elif isinstance(value, list):
        for item in value:
            walk_json(item, field_counts, value_counts)


def main():
    result = {
        "scope": "SnowLuma logs/snowluma-YYYY-MM-DD.log；只输出结构、计数和脱敏连接样本",
        "files": {},
        "totals": {},
        "limitations": [
            "日志行是否包含完整 OneBot JSON 由 json_object_lines 与 field_presence 判断；人类可读 Event 行不能替代原始事件。",
            "所有样本均已去除 Event 正文、长数字标识和已知凭据形式；不得把原日志直接作为公开测试夹具。",
        ],
    }
    total_lines = 0
    total_levels = Counter()
    total_events = Counter()
    total_markers = Counter()
    total_actions = Counter()
    total_json_lines = 0
    total_field_counts = Counter()
    total_value_counts = defaultdict(Counter)
    connection_counts = Counter()
    connection_samples = defaultdict(list)

    for path in sorted((ROOT / "logs").glob("snowluma-*.log")):
        per_file = {"levels": Counter(), "events": Counter(), "lines": 0}
        with path.open("r", encoding="utf-8", errors="replace") as handle:
            for raw in handle:
                line = raw.rstrip("\r\n")
                per_file["lines"] += 1
                total_lines += 1
                match = LINE.match(line)
                if match:
                    per_file["levels"][match.group(2)] += 1
                    total_levels[match.group(2)] += 1
                event = EVENT.search(line)
                if event:
                    category = event.group(1)
                    per_file["events"][category] += 1
                    total_events[category] += 1
                    for marker in MARKERS:
                        if marker in line:
                            total_markers[marker] += 1
                for action in ACTIONS:
                    if action in line:
                        total_actions[action] += 1
                for name, pattern in CONNECTION_PATTERNS.items():
                    if pattern.search(line):
                        connection_counts[name] += 1
                        if len(connection_samples[name]) < 24:
                            connection_samples[name].append({"file": path.name, "line": per_file["lines"], "text": sanitize_text(line)})
                if "{" in line:
                    start = line.find("{")
                    try:
                        value, _ = json.JSONDecoder().raw_decode(line[start:])
                    except (ValueError, json.JSONDecodeError):
                        continue
                    total_json_lines += 1
                    walk_json(value, total_field_counts, total_value_counts)
        per_file["levels"] = dict(per_file["levels"])
        per_file["events"] = dict(per_file["events"])
        per_file["bytes"] = path.stat().st_size
        result["files"][path.name] = per_file

    result["totals"] = {
        "lines": total_lines,
        "levels": dict(total_levels),
        "human_readable_event_categories": dict(total_events),
        "human_readable_event_markers": dict(total_markers),
        "action_name_mentions": dict(total_actions),
        "json_object_lines": total_json_lines,
        "json_field_presence": dict(total_field_counts),
        "json_value_counts": {key: dict(value) for key, value in total_value_counts.items()},
        "connection_pattern_counts": dict(connection_counts),
    }
    (OUT / "snowluma-log-summary.json").write_text(
        json.dumps(result, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
    )
    (OUT / "snowluma-connection-evidence-redacted.json").write_text(
        json.dumps(connection_samples, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
    )
    print(json.dumps(result["totals"], ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
