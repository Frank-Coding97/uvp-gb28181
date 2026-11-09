#!/usr/bin/env python3
"""对外发布前的地址体检：SDK 里不许出现真实平台或客户内网的 IP。

为什么要有这一条：这个仓库是交给客户的。开发时图方便写进 README 的真机地址、
内网 Nexus、联调平台 IP，会跟着源码一起交付出去——既是信息泄露，
也会让客户照着抄一个打不通的地址。

    python3 sdk/scripts/check-no-endpoints.py

退出码：0 干净 / 1 发现可疑地址
"""

from __future__ import annotations

import ipaddress
import os
import re
import sys

SDK_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))

# 故意保留：本机回环、通配、掩码，以及内网交付用的 Nexus（客户在内网确实要用到）。
ALLOWED = {
    "127.0.0.1",
    "0.0.0.0",
    "255.255.255.255",
    "192.168.10.220",   # 内网 Nexus，README 里作为交付渠道给出，属于有意公开
}

SKIP_DIRS = {"target", "node_modules", ".cache", ".git", "__pycache__"}
SUFFIXES = {".md", ".java", ".py", ".go", ".yml", ".yaml", ".sh", ".xml", ".json", ".properties"}

IPV4 = re.compile(r"(?<![\d.])(?:\d{1,3}\.){3}\d{1,3}(?![\d.])")


def is_ipv4(text: str) -> bool:
    try:
        ipaddress.IPv4Address(text)
        return True
    except ValueError:
        return False


def main() -> int:
    findings = []
    for root, dirs, files in os.walk(SDK_ROOT):
        dirs[:] = [name for name in dirs if name not in SKIP_DIRS]
        for name in files:
            if os.path.splitext(name)[1].lower() not in SUFFIXES:
                continue
            path = os.path.join(root, name)
            try:
                with open(path, encoding="utf-8") as handle:
                    lines = handle.readlines()
            except (UnicodeDecodeError, OSError):
                continue
            for number, line in enumerate(lines, 1):
                for match in IPV4.finditer(line):
                    address = match.group(0)
                    if not is_ipv4(address) or address in ALLOWED:
                        continue
                    findings.append((os.path.relpath(path, os.path.dirname(SDK_ROOT)),
                                     number, address, line.strip()[:100]))

    if not findings:
        print("✓ 未发现真实平台或内网 IP（白名单：%s）" % ", ".join(sorted(ALLOWED)))
        return 0

    print("✗ 发现 %d 处疑似真实地址，交付前请替换成占位符：" % len(findings), file=sys.stderr)
    for path, number, address, snippet in findings:
        print("  %s:%d  %s" % (path, number, address), file=sys.stderr)
        print("     %s" % snippet, file=sys.stderr)
    print("\n  建议：文档与注释统一写成 http://<平台IP>:51002 / http://<平台IP>:8280；\n"
          "        如果是内网交付必需的地址（如 Nexus），加进本脚本的 ALLOWED 并注明理由。",
          file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main())
