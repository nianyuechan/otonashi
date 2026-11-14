import json
import re
from datetime import datetime
from pathlib import Path

ROOT = Path(r"D:\Toshikoshi_chan\Agent\SnowLuma")
OUT = Path(__file__).resolve().parent
SENSITIVE_KEY = re.compile(
    r"(?:token|secret|password|passwd|authorization|cookie|credential|api[-_]?key|access[-_]?key|session[-_]?key|private[-_]?key|sign(?:ature)?)",
    re.I,
)
TEXT_KEY = re.compile(r"^(?:raw_message|message|content|text|prompt|stacktrace|exception)$", re.I)
BEARER = re.compile(r"(?i)(bearer\s+)[^\s,\"'}]+")
LONG_NUMBER = re.compile(r"(?<!\d)\d{5,12}(?!\d)")
URL_SECRET = re.compile(r"(?i)([?&](?:token|access_token|key|secret|sign|signature)=)[^&\s\"]+")
PASSWORD_VALUE = re.compile(r"(?i)(password=)[^\s,\"']+")
KEY_VALUE_SECRET = re.compile(r"(?i)\b(accessToken|token|secret|authorization)([=:]\s*)[^\s,\"'}]+")


def sanitize_value(key, value):
    if SENSITIVE_KEY.search(str(key)):
        return "***"
    if isinstance(value, dict):
        return {k: sanitize_value(k, v) for k, v in value.items()}
    if isinstance(value, list):
        return [sanitize_value(key, item) for item in value]
    if TEXT_KEY.match(str(key)):
        return "<text>"
    if isinstance(value, str):
        return sanitize_text(value)
    return value


def sanitize_text(value):
    if "initial credentials:" in value:
        return value.split("initial credentials:", 1)[0] + "initial credentials: ***"
    if "[Event]" in value:
        return value.split("[Event]", 1)[0] + "[Event] <event-text-redacted>"
    value = BEARER.sub(r"\1***", value)
    value = PASSWORD_VALUE.sub(r"\1***", value)
    value = KEY_VALUE_SECRET.sub(r"\1\2***", value)
    value = URL_SECRET.sub(r"\1***", value)
    value = LONG_NUMBER.sub("<id>", value)
    return value


def config_dump(name):
    path = ROOT / "config" / name
    data = json.loads(path.read_text(encoding="utf-8-sig"))
    if isinstance(data, dict):
        return {key: sanitize_value(key, value) for key, value in data.items()}
    return sanitize_value("root", data)


def main():
    configs = {
        "onebot": config_dump("onebot_2406354517.json"),
        "webui": config_dump("webui.json"),
    }
    raw_onebot = json.loads((ROOT / "config" / "onebot_2406354517.json").read_text(encoding="utf-8-sig"))
    networks = raw_onebot["networks"]
    configs["connection_evidence"] = {
        "http_access_token_present": bool(networks["httpServers"][0].get("accessToken")),
        "ws_access_token_present": bool(networks["wsServers"][0].get("accessToken")),
        "http_clients_count": len(networks["httpClients"]),
        "ws_clients_count": len(networks["wsClients"]),
    }
    (OUT / "snowluma-config-redacted.json").write_text(
        json.dumps(configs, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
    )

    samples = {}
    metadata = []
    for path in sorted((ROOT / "logs").glob("*.log")):
        stat = path.stat()
        metadata.append({
            "name": path.name,
            "bytes": stat.st_size,
            "last_write": datetime.fromtimestamp(stat.st_mtime).astimezone().isoformat(timespec="seconds"),
        })
        lines = path.read_text(encoding="utf-8", errors="replace").splitlines()
        nonempty = [line for line in lines if line.strip()]
        selected = nonempty[:20]
        if len(nonempty) > 40:
            selected += ["<middle omitted>"] + nonempty[-20:]
        elif len(nonempty) > 20:
            selected += nonempty[20:]
        samples[path.name] = [sanitize_text(line) for line in selected]
    (OUT / "snowluma-log-metadata.json").write_text(
        json.dumps(metadata, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
    )
    (OUT / "snowluma-log-samples-redacted.txt").write_text(
        "\n\n".join(f"===== {name} =====\n" + "\n".join(lines) for name, lines in samples.items()) + "\n",
        encoding="utf-8",
    )
    print(json.dumps({"configs": list(configs), "logs": metadata}, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
