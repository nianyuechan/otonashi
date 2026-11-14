from __future__ import annotations

import csv
import json
import math
import os
import re
from collections import Counter, defaultdict
from datetime import datetime
from pathlib import Path

PROJECT_ROOT = Path(
    r"d:\Toshikoshi_chan\Agent\音无\mysy2\mysy2\home\SERVER2\TRSS_AllBot\TRSS-Yunzai"
)
OUTPUT_DIR = Path(__file__).resolve().parent
LOG_NAME_RE = re.compile(r"^command\.(\d{4}-\d{2}-\d{2})\.log$")
ANSI_RE = re.compile(r"\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07]*(?:\x07|\x1b\\))")
TIMESTAMP_RE = re.compile(r"(?<!\d)(\d{4}-\d{2}-\d{2})[T ]\d{2}:\d{2}:\d{2}(?!\d)")
LABEL_PATTERN = r"((?:[^\[\]\r\n]|\[[^\[\]\r\n]*\])*)"
START_RE = re.compile(r"\[" + LABEL_PATTERN + r"\]\[开始处理\]\s*$")
COMPLETE_RE = re.compile(r"\[" + LABEL_PATTERN + r"\]\[完成([^\[\]\r\n]*)\]\s*$")
DURATION_RE = re.compile(
    r"(?:(\d+)天)?(?:(\d+)时)?(?:(\d+)分)?(?:(\d+)秒)?(?:(\d+))?"
)
CRON_FIELD_RE = re.compile(r"^[A-Za-z0-9*/?,#LW-]+$")
PLUGIN_ERROR_RE = re.compile(r"插件加载错误(?:\s+([^\s]+))?")
PLUGIN_TIMEOUT_RE = re.compile(r"插件加载超时\s+([^\s]+)")
MISSING_DEPENDENCY_RE = re.compile(r"([^\s\[\]]+)\s+缺少依赖\s+")
NAME_RE = re.compile(r"\bname\s*[:=]\s*([\"'`])([^\"'`\r\n]+)\1")
FNC_RE = re.compile(r"\bfnc\s*[:=]\s*([\"'`])([^\"'`\r\n]+)\1")
START_MARKER = "[开始处理]"
COMPLETE_MARKER = "[完成"
KINDS = ("command", "task", "command_complete", "task_complete")
COMPLETE_KINDS = ("command_complete", "task_complete")
MAPPING_EXCLUDED_DIRS = {".git", "node_modules"}


def is_cron(value: str) -> bool:
    fields = value.split()
    return len(fields) in (5, 6) and all(CRON_FIELD_RE.fullmatch(field) for field in fields)


def parse_label(label: str) -> tuple[str, str, str] | None:
    if "(" not in label or not label.endswith(")"):
        return None
    name, method = label.rsplit("(", 1)
    method = method[:-1].strip()
    if not name or not method:
        return None
    return name, method, "task" if is_cron(method) else "command"


def parse_duration_ms(value: str) -> int | None:
    match = DURATION_RE.fullmatch(value)
    if not match or not any(match.groups()):
        return None
    days, hours, minutes, seconds, milliseconds = (int(part or 0) for part in match.groups())
    return (
        (((days * 24 + hours) * 60 + minutes) * 60 + seconds) * 1000
        + milliseconds
    )


def duration_stats(values: list[int]) -> dict:
    if not values:
        return {"sample_count": 0, "median_ms": None, "p95_ms": None}
    ordered = sorted(values)
    size = len(ordered)
    if size % 2:
        median = ordered[size // 2]
    else:
        median = (ordered[size // 2 - 1] + ordered[size // 2]) / 2
    return {
        "sample_count": size,
        "median_ms": median,
        "p95_ms": ordered[math.ceil(size * 0.95) - 1],
    }


def plugin_directory(key: str | None) -> str | None:
    if not key or key.startswith("-") or "\\" in key or "[" in key or "]" in key:
        return None
    parts = key.split("/")
    if len(parts) == 1:
        directory = parts[0]
    elif len(parts) == 2 and parts[1].endswith(".js"):
        directory = parts[0]
    else:
        return None
    if not directory or directory in {".", ".."} or any(ord(char) < 32 for char in directory):
        return None
    return directory


def sorted_counts(counter: Counter) -> list[dict]:
    return [
        {"name": name, "count": count}
        for name, count in sorted(counter.items(), key=lambda item: (-item[1], item[0]))
    ]


def build_mapping_index(plugins_root: Path) -> tuple[dict, dict, int]:
    name_directories = defaultdict(set)
    pair_directories = defaultdict(set)
    files_scanned = 0
    for root, directories, files in os.walk(plugins_root):
        directories[:] = [name for name in directories if name not in MAPPING_EXCLUDED_DIRS]
        root_path = Path(root)
        relative = root_path.relative_to(plugins_root)
        if not relative.parts:
            continue
        plugin_dir = relative.parts[0]
        for filename in files:
            if not filename.endswith(".js"):
                continue
            path = root_path / filename
            text = path.read_text(encoding="utf-8", errors="replace")
            files_scanned += 1
            names = {match.group(2) for match in NAME_RE.finditer(text)}
            methods = {match.group(2) for match in FNC_RE.finditer(text)}
            for name in names:
                name_directories[name].add(plugin_dir)
                for method in methods:
                    pair_directories[(name, method)].add(plugin_dir)
    return name_directories, pair_directories, files_scanned


def map_display_name(name: str, method: str, name_directories: dict, pair_directories: dict) -> dict:
    pair_candidates = pair_directories.get((name, method), set())
    name_candidates = name_directories.get(name, set())
    if len(pair_candidates) == 1:
        directory = next(iter(pair_candidates))
        return {
            "status": "unique_name_and_method",
            "directory": directory,
            "candidate_directories": [directory],
            "method_cross_checked": True,
        }
    if len(pair_candidates) > 1:
        return {
            "status": "ambiguous",
            "directory": None,
            "candidate_directories": sorted(pair_candidates),
            "method_cross_checked": True,
        }
    if len(name_candidates) == 1:
        directory = next(iter(name_candidates))
        return {
            "status": "unique_name_only",
            "directory": directory,
            "candidate_directories": [directory],
            "method_cross_checked": False,
        }
    if len(name_candidates) > 1:
        return {
            "status": "ambiguous",
            "directory": None,
            "candidate_directories": sorted(name_candidates),
            "method_cross_checked": False,
        }
    return {
        "status": "not_found",
        "directory": None,
        "candidate_directories": [],
        "method_cross_checked": False,
    }


def main() -> None:
    logs_dir = PROJECT_ROOT / "logs"
    log_files = []
    for path in logs_dir.glob("command.*.log"):
        match = LOG_NAME_RE.fullmatch(path.name)
        if match and path.is_file():
            log_files.append((match.group(1), path))
    log_files.sort(key=lambda item: item[0])
    if not log_files:
        raise RuntimeError("未发现未压缩 command 日志")
    uncompressed_log_count = len(list(logs_dir.glob("*.log")))

    name_directories, pair_directories, mapping_files_scanned = build_mapping_index(
        PROJECT_ROOT / "plugins"
    )
    counts = {kind: Counter() for kind in KINDS}
    name_methods = {kind: defaultdict(Counter) for kind in KINDS}
    name_dates = {kind: defaultdict(set) for kind in KINDS}
    pair_dates = {kind: defaultdict(set) for kind in KINDS}
    duration_values = {kind: defaultdict(list) for kind in COMPLETE_KINDS}
    hit_dates = {"start": [], "complete": []}
    load_error_counts = Counter()
    timeout_counts = Counter()
    missing_dependency_counts = Counter()
    files_summary = []
    unparsed_start = Counter()
    unparsed_complete = Counter()
    unparsed_load_error = 0
    load_error_banner_lines = 0
    other_complete_lines = 0

    def record_event(
        kind: str, date: str, name: str, method: str, per_file: Counter, duration_ms: int | None
    ) -> None:
        counts[kind][(date, name, method)] += 1
        name_methods[kind][name][method] += 1
        name_dates[kind][name].add(date)
        pair_dates[kind][(name, method)].add(date)
        per_file[f"{kind}_hits"] += 1
        event_class = "complete" if kind.endswith("_complete") else "start"
        hit_dates[event_class].append(date)
        if duration_ms is not None:
            duration_values[kind][(name, method)].append(duration_ms)

    for file_date, path in log_files:
        per_file = Counter()
        with path.open("r", encoding="utf-8", errors="replace") as handle:
            for raw_line in handle:
                per_file["lines"] += 1
                if "\ufffd" in raw_line:
                    per_file["decode_replacement_lines"] += 1
                line = ANSI_RE.sub("", raw_line).rstrip()
                has_start = START_MARKER in line
                has_complete = COMPLETE_MARKER in line

                if "插件加载错误" in line:
                    match = PLUGIN_ERROR_RE.search(line)
                    key = match.group(1) if match else None
                    if key is None or set(key) == {"-"}:
                        load_error_banner_lines += 1
                    else:
                        directory = plugin_directory(key)
                        if directory is None:
                            unparsed_load_error += 1
                        else:
                            load_error_counts[directory] += 1
                            per_file["plugin_load_error_hits"] += 1

                timeout_match = PLUGIN_TIMEOUT_RE.search(line)
                if timeout_match:
                    directory = plugin_directory(timeout_match.group(1))
                    if directory:
                        timeout_counts[directory] += 1
                    else:
                        per_file["unmapped_timeout_lines"] += 1

                dependency_match = MISSING_DEPENDENCY_RE.search(line)
                if dependency_match:
                    directory = plugin_directory(dependency_match.group(1))
                    if directory:
                        missing_dependency_counts[directory] += 1
                    else:
                        per_file["unmapped_missing_dependency_lines"] += 1

                timestamp = TIMESTAMP_RE.search(line)
                date = timestamp.group(1) if timestamp else file_date
                date_source = "timestamp" if timestamp else "filename"

                if has_start:
                    per_file["start_marker_lines"] += 1
                    per_file[f"start_date_from_{date_source}"] += 1
                    start_match = START_RE.search(line)
                    if not start_match:
                        unparsed_start["unrecognized_suffix"] += 1
                        per_file["unparsed_start_lines"] += 1
                    else:
                        parsed = parse_label(start_match.group(1))
                        if not parsed:
                            unparsed_start["invalid_plugin_or_task_label"] += 1
                            per_file["unparsed_start_lines"] += 1
                        else:
                            name, method, base_kind = parsed
                            record_event(base_kind, date, name, method, per_file, None)

                if has_complete:
                    per_file["complete_marker_lines"] += 1
                    complete_match = COMPLETE_RE.search(line)
                    if not complete_match:
                        other_complete_lines += 1
                        per_file["other_complete_lines_excluded"] += 1
                        continue
                    per_file[f"complete_date_from_{date_source}"] += 1
                    label, duration_text = complete_match.groups()
                    parsed = parse_label(label)
                    if not parsed:
                        unparsed_complete["invalid_plugin_or_task_label"] += 1
                        per_file["unparsed_complete_lines"] += 1
                        continue
                    name, method, base_kind = parsed
                    kind = f"{base_kind}_complete"
                    duration_ms = parse_duration_ms(duration_text)
                    if duration_ms is None:
                        unparsed_complete["invalid_duration"] += 1
                        per_file["unparsed_complete_duration_lines"] += 1
                    record_event(kind, date, name, method, per_file, duration_ms)

        files_summary.append(
            {
                "name": path.name,
                "file_date": file_date,
                "bytes": path.stat().st_size,
                **dict(sorted(per_file.items())),
            }
        )

    kind_order = {kind: index for index, kind in enumerate(KINDS)}
    rows = []
    for kind in KINDS:
        for (date, name, method), count in counts[kind].items():
            rows.append((kind, date, name, method, count))
    rows.sort(key=lambda row: (kind_order[row[0]], row[1], row[2], row[3]))

    csv_path = OUTPUT_DIR / "log-command-usage.csv"
    with csv_path.open("w", encoding="utf-8-sig", newline="") as handle:
        writer = csv.writer(handle)
        writer.writerow(["kind", "date", "raw_plugin", "method", "count"])
        writer.writerows(rows)

    def name_entries(kind: str) -> list[dict]:
        is_task = kind.startswith("task")
        name_key = "task_name" if is_task else "raw_plugin"
        method_key = "cron_expressions" if is_task else "methods"
        entries = []
        for name, dates in name_dates[kind].items():
            methods = name_methods[kind][name]
            entry = {
                name_key: name,
                "count": sum(methods.values()),
                "active_date_count": len(dates),
                "first_date": min(dates),
                "last_date": max(dates),
                method_key: sorted_counts(methods),
            }
            if kind in COMPLETE_KINDS:
                values = [
                    duration
                    for (pair_name, _), pair_values in duration_values[kind].items()
                    if pair_name == name
                    for duration in pair_values
                ]
                entry["duration_ms"] = duration_stats(values)
            entries.append(entry)
        entries.sort(key=lambda entry: (-entry["count"], entry[name_key]))
        return entries

    def pair_entries(kind: str) -> list[dict]:
        is_task = kind.startswith("task")
        name_key = "task_name" if is_task else "raw_plugin"
        method_key = "cron" if is_task else "method"
        entries = []
        for (name, method), dates in pair_dates[kind].items():
            entry = {
                name_key: name,
                method_key: method,
                "count": name_methods[kind][name][method],
                "active_date_count": len(dates),
                "first_date": min(dates),
                "last_date": max(dates),
            }
            if kind in COMPLETE_KINDS:
                entry["duration_ms"] = duration_stats(duration_values[kind][(name, method)])
            if kind == "command_complete":
                entry["directory_mapping"] = map_display_name(
                    name, method, name_directories, pair_directories
                )
            entries.append(entry)
        entries.sort(key=lambda entry: (-entry["count"], entry[name_key], entry[method_key]))
        return entries

    command_start_plugins = name_entries("command")
    for entry in command_start_plugins:
        entry["command_count"] = entry.pop("count")
    scheduled_start_tasks = name_entries("task")
    command_complete_pairs = pair_entries("command_complete")
    task_complete_pairs = pair_entries("task_complete")
    command_complete_top20 = command_complete_pairs[:20]

    mapping_pair_counts = Counter()
    mapping_completion_counts = Counter()
    for entry in command_complete_pairs:
        status = entry["directory_mapping"]["status"]
        mapping_pair_counts[status] += 1
        mapping_completion_counts[status] += entry["count"]

    totals = {kind: sum(counts[kind].values()) for kind in KINDS}
    load_error_total = sum(load_error_counts.values())
    all_durations = {
        kind: [
            duration
            for pair_values in duration_values[kind].values()
            for duration in pair_values
        ]
        for kind in COMPLETE_KINDS
    }

    summary = {
        "schema_version": 2,
        "generated_at": datetime.now().astimezone().isoformat(timespec="seconds"),
        "source": {
            "project_root": str(PROJECT_ROOT),
            "log_directory": str(logs_dir),
            "selection": "仅 logs/command.YYYY-MM-DD.log；不匹配或读取 .gz/.zst 等压缩文件",
            "command_file_count": len(log_files),
            "uncompressed_log_file_count": uncompressed_log_count,
            "file_count_note": "目录枚举得到 15 个 command.YYYY-MM-DD.log；连同 error.log 共 16 个未压缩 .log。命令统计仅使用前者，不用压缩日志补足",
            "compressed_files_read": 0,
            "files": files_summary,
        },
        "date_range": {
            "log_files": {"first": log_files[0][0], "last": log_files[-1][0]},
            "parsed_start_hits": {
                "first": min(hit_dates["start"]) if hit_dates["start"] else None,
                "last": max(hit_dates["start"]) if hit_dates["start"] else None,
            },
            "parsed_complete_hits": {
                "first": min(hit_dates["complete"]) if hit_dates["complete"] else None,
                "last": max(hit_dates["complete"]) if hit_dates["complete"] else None,
            },
            "basis": "优先取行内完整时间戳日期；本样本事件行仅有时分秒，因此实际取 command.YYYY-MM-DD.log 文件名日期",
        },
        "total_hits": {
            "command_start": totals["command"],
            "scheduled_task_start": totals["task"],
            "command_complete": totals["command_complete"],
            "scheduled_task_complete": totals["task_complete"],
            "all_start_processing": totals["command"] + totals["task"],
            "all_complete": totals["command_complete"] + totals["task_complete"],
            "plugin_load_error": load_error_total,
        },
        "csv_semantics": {
            "command": "开始处理：date + raw_plugin（显示名）+ method（规则方法名）+ count",
            "task": "定时任务开始：date + raw_plugin（任务名）+ method（cron）+ count",
            "command_complete": "普通规则完成日志：date + raw_plugin（显示名）+ method（规则方法名）+ count",
            "task_complete": "定时任务完成日志：date + raw_plugin（任务名）+ method（cron）+ count",
            "plugin_load_error": "仅写入 JSON，不混入 CSV",
        },
        "by_plugin": command_start_plugins,
        "by_scheduled_task": scheduled_start_tasks,
        "by_plugin_completion": name_entries("command_complete"),
        "by_scheduled_task_completion": name_entries("task_complete"),
        "by_command_completion": command_complete_pairs,
        "by_task_completion": task_complete_pairs,
        "command_complete_top20": command_complete_top20,
        "duration_statistics": {
            "unit": "ms",
            "median": "排序后中位数；偶数样本取中间两数平均",
            "p95": "最近秩（nearest-rank）：排序后第 ceil(0.95*N) 个值",
            "command_complete": duration_stats(all_durations["command_complete"]),
            "task_complete": duration_stats(all_durations["task_complete"]),
            "excluded_invalid_duration_lines": unparsed_complete["invalid_duration"],
        },
        "directory_mapping": {
            "source": "当前 plugins 目录下的 .js 源码静态字面量；排除 .git、node_modules",
            "files_scanned": mapping_files_scanned,
            "rules": {
                "unique_name_and_method": "同一源码文件内 name 与 fnc 字面量组合只指向一个一级插件目录",
                "unique_name_only": "name 字面量只出现于一个一级插件目录，但未找到同文件 fnc 交叉证据",
                "ambiguous": "字面量候选指向多个目录，不选择单一目录",
                "not_found": "当前源码未找到对应字面量，可能是动态名称、历史插件或已删除源码",
            },
            "reliability": "这是当前源码静态映射，不是完成日志内嵌目录，也不保证历史版本完全一致；ambiguous/not_found 不作猜测",
            "all_command_pairs": {
                "pair_count_by_status": dict(sorted(mapping_pair_counts.items())),
                "completion_count_by_status": dict(sorted(mapping_completion_counts.items())),
            },
        },
        "plugin_load_errors": {
            "counted": True,
            "total": load_error_total,
            "by_directory": sorted_counts(load_error_counts),
            "mapping_rule": "仅解析字面标记“插件加载错误 <key>”；依据 loader.js 的 key=目录或目录/文件.js，取首段目录名；横幅及不能可靠映射的行不计数",
            "banner_lines_excluded": load_error_banner_lines,
            "unmapped_lines": unparsed_load_error,
            "related_markers_not_in_total": {
                "plugin_load_timeout": {
                    "total": sum(timeout_counts.values()),
                    "by_directory": sorted_counts(timeout_counts),
                },
                "missing_dependency": {
                    "total": sum(missing_dependency_counts.values()),
                    "by_directory": sorted_counts(missing_dependency_counts),
                },
            },
        },
        "parsing_rules": {
            "ansi": "匹配前剔除 ANSI CSI/OSC 控制序列",
            "timestamp": "优先使用行内完整日期；若行内仅有时分秒，则使用文件名日期，并逐文件记录日期来源数量",
            "command": "末尾 [插件显示名(方法名)][开始处理]；括号内容不像 cron 时归为 command，对应 loader.js L255-L286",
            "scheduled_task": "开始标记中括号内容为 5/6 段 cron 时归为 task，对应 loader.js L499-L535；支持任务名自身带一层方括号",
            "command_complete": "末尾 [插件显示名(方法名)][完成<耗时>] 且方法不是 cron；对应 loader.js L266-L280 的 mark 完成日志",
            "task_complete": "末尾 [任务名(cron)][完成<耗时>] 且括号内容为 5/6 段 cron；对应 loader.js L499-L512",
            "duration": "仅接受 Bot.getTimeDiff 的天/时/分/秒及末尾无单位毫秒组合，转换为整数毫秒；不合法耗时不进入时长样本，但保留完成次数",
            "other_complete_lines": "没有 loader 标签后缀的其他完成日志（例如 Shell Command）不计入 command_complete/task_complete，也不计为无法解析的 loader 行",
            "load_error": "对应 loader.js L123、L128、L658 的字面“插件加载错误”；仅统计可提取目录名的行",
            "privacy": "输出只含聚合名称、目录映射、日期、次数和耗时统计，不保留消息正文、账号、群号、URL、令牌或原始日志行",
        },
        "unparsed_lines": {
            "start_processing": sum(unparsed_start.values()),
            "start_processing_by_reason": dict(sorted(unparsed_start.items())),
            "complete_processing": sum(unparsed_complete.values()),
            "complete_processing_by_reason": dict(sorted(unparsed_complete.items())),
            "complete_invalid_duration_still_counted": unparsed_complete["invalid_duration"],
            "other_complete_lines_excluded": other_complete_lines,
            "plugin_load_error_unmapped": unparsed_load_error,
            "total": sum(unparsed_start.values()) + sum(unparsed_complete.values()) + unparsed_load_error,
        },
        "limitations": [
            "目录中共有 16 个未压缩 .log，其中 15 个是 command.YYYY-MM-DD.log，另一个是 error.log；命令统计只读取 15 个 command 文件，未用压缩日志补足。",
            "样本日期不连续；未读取任何 .gz/.zst 压缩日志，不能外推完整历史或总体使用率。",
            "事件行仅含时分秒，日期取自文件名；若轮转文件实际混入其他日期的记录，本方法无法从行内识别。",
            "command appender 阈值为 warn，普通规则开始为 info、完成为 mark，因此开始数为 0 不代表没有普通命令用量；开始与完成是独立事件计数，不作一一配对。",
            "完成日志表示插件返回非 false 且未抛错后记录了完成，不保证外部 API、签到、更新等业务操作实际成功，更不等于成功业务数。",
            "raw_plugin 是日志显示名而非目录名；目录映射来自当前源码静态字面量，历史变更、动态名称和同名插件可能造成歧义或无法映射。",
            "耗时统计只覆盖格式可可靠解析的完成日志；P95 是样本最近秩，不是全历史性能指标，也不与开始日志配对计算。",
            "定时任务依赖 5/6 段 cron 结构识别；非标准格式可能无法归类，相关行计入无法解析统计。",
            "C 只统计可可靠映射目录名的字面“插件加载错误”行；加载超时、缺少依赖及错误横幅不混入该总数。",
            "日志存在多进程写入、截断、轮转或级别配置差异的可能性；本统计不推断未记录事件。",
        ],
    }

    json_path = OUTPUT_DIR / "log-command-summary.json"
    with json_path.open("w", encoding="utf-8") as handle:
        json.dump(summary, handle, ensure_ascii=False, indent=2)
        handle.write("\n")

    print(
        json.dumps(
            {
                "files": len(log_files),
                **totals,
                "plugin_load_error": load_error_total,
                "unparsed_start": sum(unparsed_start.values()),
                "unparsed_complete": sum(unparsed_complete.values()),
                "other_complete_lines_excluded": other_complete_lines,
                "csv_rows": len(rows),
                "csv": str(csv_path),
                "json": str(json_path),
            },
            ensure_ascii=False,
            indent=2,
        )
    )


if __name__ == "__main__":
    main()
