import collections
import json
from pathlib import Path

work = Path(__file__).resolve().parent
data = json.loads((work / "log-command-summary.json").read_text(encoding="utf-8"))
groups = collections.defaultdict(lambda: {"count": 0, "methods": [], "mapping_statuses": set()})
unmapped = []

for item in data["by_command_completion"]:
    mapping = item["directory_mapping"]
    candidates = tuple(mapping["candidate_directories"])
    method = {"method": item["method"], "count": item["count"]}
    if not candidates:
        unmapped.append({"name": item["raw_plugin"], **method})
        continue
    groups[candidates]["count"] += item["count"]
    groups[candidates]["methods"].append(method)
    groups[candidates]["mapping_statuses"].add(mapping["status"])

result = {
    "unique_or_grouped_directories": [
        {
            "candidate_directories": list(candidates),
            "count": value["count"],
            "methods": sorted(value["methods"], key=lambda method: -method["count"]),
            "mapping_statuses": sorted(value["mapping_statuses"]),
        }
        for candidates, value in sorted(groups.items(), key=lambda pair: -pair[1]["count"])
    ],
    "unmapped": sorted(unmapped, key=lambda item: -item["count"]),
    "rule": "唯一映射聚合到目录；歧义目录不拆分、不重复计数；未映射单列。",
}
for group in result["unique_or_grouped_directories"]:
    group["mapping_statuses"] = list(group["mapping_statuses"])

(work / "usage-by-directory.json").write_text(
    json.dumps(result, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
)
print(json.dumps(result, ensure_ascii=False, indent=2))
