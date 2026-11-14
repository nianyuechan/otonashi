import json
import re
from pathlib import Path

from inspect_snowluma import sanitize_text

ROOT = Path(r"D:\Toshikoshi_chan\Agent\SnowLuma\logs")
OUT = Path(__file__).resolve().parent / "snowluma-action-structures.json"
records = []
for path in sorted(ROOT.glob("snowluma-*.log")):
    with path.open("r", encoding="utf-8", errors="replace") as handle:
        for line_number, line in enumerate(handle, 1):
            if "send_msg" not in line:
                continue
            keys = sorted(set(re.findall(r"([A-Za-z_][A-Za-z0-9_]*)\s*[:=]", line)))
            shape = re.sub(r"(?i)(message|text|content|raw_message)=[^,)]+", r"\1=<text>", line)
            records.append({
                "file": path.name,
                "line": line_number,
                "keys": keys,
                "redacted_shape": sanitize_text(shape),
                "segment_types": re.findall(r"type\s*:\s*['\"]([^'\"]+)", line),
                "has_success_word": bool(re.search(r"\b(?:success|succeeded|OK)\b", line)),
                "has_error_word": bool(re.search(r"\b(?:error|failed)\b", line)),
                "has_message_id": "message_id" in line,
                "has_echo": "echo" in line,
                "has_retcode": "retcode" in line,
                "line_length": len(line),
            })
            if len(records) >= 31:
                break
    if len(records) >= 31:
        break
summary = {
    "record_count": len(records),
    "missing_echo_count": sum(not item["has_echo"] for item in records),
    "missing_message_id_count": sum(not item["has_message_id"] for item in records),
    "missing_retcode_count": sum(not item["has_retcode"] for item in records),
    "note": "仅统计日志中 send_msg 提及对应的动作摘要行；缺失表示该行未提供字段，不推断协议实际响应不存在。",
    "records": records,
}
OUT.write_text(json.dumps(summary, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
print(json.dumps({key: value for key, value in summary.items() if key != "records"}, ensure_ascii=False, indent=2))
