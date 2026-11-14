from __future__ import annotations

import csv
import json
import re
from collections import Counter, defaultdict
from pathlib import Path

ROOT = Path(r"d:\Toshikoshi_chan\Agent\音无\mysy2\mysy2\home\SERVER2\TRSS_AllBot\TRSS-Yunzai")
PLUGINS = ROOT / "plugins"
OUT = Path(__file__).resolve().parent
SOURCE_SUFFIXES = {".js", ".cjs", ".mjs", ".jsx", ".ts", ".tsx"}
EXCLUDED_PARTS = {".git", "node_modules", "resources", "static", "assets", "public", "dist"}
IMPORT_PATTERNS = [
    re.compile(r"(?m)^\s*(?:import|export)\s+(?:[^;\n\"']*?\s+from\s+)?[\"']([^\"']+)[\"']"),
    re.compile(r"\bimport\s*\(\s*[\"']([^\"']+)[\"']\s*\)"),
    re.compile(r"\brequire\s*\(\s*[\"']([^\"']+)[\"']\s*\)"),
]
FEATURE_PATTERNS = {
    "child_process": re.compile(r"\b(?:child_process|execSync|spawnSync|execFile|spawn)\b"),
    "http_server": re.compile(r"\b(?:express|createServer|\.listen\s*\()"),
    "browser": re.compile(r"\b(?:puppeteer|chromium|playwright)\b"),
    "redis": re.compile(r"\bredis\s*\."),
    "sqlite": re.compile(r"\b(?:sequelize|sqlite3|SQLite)\b"),
    "leveldb": re.compile(r"\b(?:getMap|leveldb|LevelDB)\b"),
    "scheduler": re.compile(r"\b(?:node-schedule|scheduleJob|setInterval)\b"),
    "framework_task": re.compile(r"\btask\s*[:=]"),
    "context": re.compile(r"\b(?:setContext|getContext|awaitContext|finish)\s*\("),
}
ALIASES = {
    "#miao": "miao-plugin",
    "#miao.models": "miao-plugin",
    "#miao.path": "miao-plugin",
    "#liangshi": "liangshi-calc",
    "#guoba.platform": "Guoba-Plugin",
    "#guoba.framework": "Guoba-Plugin",
    "#guoba.utils": "Guoba-Plugin",
    "#guoba.adapter": "Guoba-Plugin",
    "#guoba.libs": "Guoba-Plugin",
}


def source_files(directory: Path):
    for path in directory.rglob("*"):
        if not path.is_file() or path.suffix.lower() not in SOURCE_SUFFIXES:
            continue
        relative = path.relative_to(directory)
        if any(part in EXCLUDED_PARTS for part in relative.parts) or path.name.endswith(".min.js"):
            continue
        yield path


def package_root(specifier: str) -> str:
    if specifier.startswith("node:"):
        return specifier
    if specifier.startswith("."):
        return "relative"
    parts = specifier.split("/")
    return "/".join(parts[:2]) if specifier.startswith("@") else parts[0]


def cross_plugin(specifier: str, file: Path, own: str) -> str | None:
    if specifier in ALIASES:
        target = ALIASES[specifier]
        return target if target != own else None
    if not specifier.startswith("."):
        return None
    resolved = (file.parent / specifier).resolve()
    try:
        relative = resolved.relative_to(PLUGINS)
    except ValueError:
        return None
    if not relative.parts:
        return None
    target = relative.parts[0]
    return target if target != own else None


def main() -> None:
    imports_rows = []
    summary = {}
    graph = defaultdict(Counter)
    packages = defaultdict(Counter)
    feature_counts = defaultdict(Counter)

    for directory in sorted((p for p in PLUGINS.iterdir() if p.is_dir()), key=lambda p: p.name.lower()):
        own = directory.name
        files = list(source_files(directory))
        summary[own] = {
            "source_files_scanned": len(files),
            "cross_plugin_dependencies": {},
            "package_imports": {},
            "feature_lexical_hits": {},
            "core_relative_import_files": 0,
        }
        core_import_files = set()
        for file in files:
            text = file.read_text(encoding="utf-8", errors="replace")
            relative_file = file.relative_to(ROOT).as_posix()
            seen_imports = set()
            for pattern in IMPORT_PATTERNS:
                for match in pattern.finditer(text):
                    specifier = match.group(1)
                    line = text.count("\n", 0, match.start()) + 1
                    key = (specifier, line)
                    if key in seen_imports:
                        continue
                    seen_imports.add(key)
                    target = cross_plugin(specifier, file, own)
                    root = package_root(specifier)
                    if target:
                        graph[own][target] += 1
                    elif root != "relative":
                        packages[own][root] += 1
                    if root == "relative":
                        resolved = (file.parent / specifier).resolve()
                        try:
                            resolved.relative_to(ROOT / "lib")
                            core_import_files.add(relative_file)
                        except ValueError:
                            pass
                    imports_rows.append({
                        "plugin": own,
                        "file": relative_file,
                        "line": line,
                        "specifier": specifier,
                        "kind": "cross_plugin" if target else ("core_relative" if relative_file in core_import_files else "package_or_local"),
                        "target": target or "",
                    })
            for feature, pattern in FEATURE_PATTERNS.items():
                hits = len(pattern.findall(text))
                if hits:
                    feature_counts[own][feature] += hits
        summary[own]["cross_plugin_dependencies"] = dict(sorted(graph[own].items()))
        summary[own]["package_imports"] = dict(sorted(packages[own].items()))
        summary[own]["feature_lexical_hits"] = dict(sorted(feature_counts[own].items()))
        summary[own]["core_relative_import_files"] = len(core_import_files)

    with (OUT / "plugin-imports.csv").open("w", encoding="utf-8-sig", newline="") as handle:
        writer = csv.DictWriter(handle, fieldnames=["plugin", "file", "line", "specifier", "kind", "target"])
        writer.writeheader()
        writer.writerows(sorted(imports_rows, key=lambda row: (row["plugin"], row["file"], row["line"], row["specifier"])))

    with (OUT / "plugin-dependencies.csv").open("w", encoding="utf-8-sig", newline="") as handle:
        fields = ["plugin", "source_files_scanned", "cross_plugin_dependencies", "package_imports", "feature_lexical_hits", "core_relative_import_files"]
        writer = csv.DictWriter(handle, fieldnames=fields)
        writer.writeheader()
        for own, data in sorted(summary.items()):
            writer.writerow({
                "plugin": own,
                "source_files_scanned": data["source_files_scanned"],
                "cross_plugin_dependencies": json.dumps(data["cross_plugin_dependencies"], ensure_ascii=False),
                "package_imports": json.dumps(data["package_imports"], ensure_ascii=False),
                "feature_lexical_hits": json.dumps(data["feature_lexical_hits"], ensure_ascii=False),
                "core_relative_import_files": data["core_relative_import_files"],
            })

    payload = {
        "scope": "plugins 下源码的只读词法导入扫描；排除 .git/node_modules/resources/static/assets/public/dist 和 .min.js",
        "limitations": [
            "不是 AST 或运行时加载图；动态拼接 import、条件导入和字符串路径可能漏报。",
            "cross_plugin 仅统计可静态解析的别名与相对路径，不包含 Handler/Runtime 等服务调用。",
            "feature_lexical_hits 是引用上限，不代表功能已启用或任务实际加载。",
        ],
        "plugins": summary,
    }
    (OUT / "plugin-dependency-summary.json").write_text(json.dumps(payload, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(json.dumps({
        "plugin_directories": len(summary),
        "import_rows": len(imports_rows),
        "cross_plugin_edges": {own: dict(targets) for own, targets in sorted(graph.items()) if targets},
    }, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
