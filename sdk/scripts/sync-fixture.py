#!/usr/bin/env python3
"""把主仓的签名向量同步到 SDK 仓（主仓专用，拆仓后不需要再跑）。

为什么要有副本：SDK 拆出去以后读不到 server/ 目录，客户也不该拿到服务端源码。
为什么要有这个脚本：手抄一份 JSON 一定会漂移，而漂移后的向量仍然"绿"，
直到真实请求全部 401。副本必须带来源，才能被对账脚本和新同事信任。

    python3 sdk/scripts/sync-fixture.py
"""

from __future__ import annotations

import argparse
import datetime
import hashlib
import os
import shutil
import subprocess
import sys

SDK_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
REPO_ROOT = os.path.dirname(SDK_ROOT)
DEFAULT_SOURCE = os.path.join(
    REPO_ROOT, "server", "app", "openapi", "auth", "testdata", "signature-v1.json")
TARGET = os.path.join(SDK_ROOT, "fixtures", "signature-v1.json")
SOURCE_NOTE = os.path.join(SDK_ROOT, "fixtures", "SOURCE.md")


def git_commit() -> str:
    try:
        return subprocess.run(
            ["git", "-C", REPO_ROOT, "rev-parse", "--short", "HEAD"],
            capture_output=True, text=True, check=True).stdout.strip()
    except (subprocess.CalledProcessError, FileNotFoundError):
        return "unknown"


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source", default=DEFAULT_SOURCE)
    args = parser.parse_args()

    if not os.path.isfile(args.source):
        print("✗ 找不到主仓向量: %s\n  如果 SDK 已经独立，副本就是唯一来源，不需要再同步。"
              % args.source, file=sys.stderr)
        return 2

    os.makedirs(os.path.dirname(TARGET), exist_ok=True)
    shutil.copyfile(args.source, TARGET)

    digest = hashlib.sha256(open(TARGET, "rb").read()).hexdigest()
    with open(SOURCE_NOTE, "w", encoding="utf-8") as handle:
        handle.write(
            "# 签名向量的来源\n\n"
            "`signature-v1.json` 不是手写的，是从主仓同步过来的副本。\n"
            "它被 Java / Python / Go 三份实现共用，任何一份对不上向量都算回归。\n\n"
            "| 项 | 值 |\n|---|---|\n"
            "| 来源 | `server/app/openapi/auth/testdata/signature-v1.json` |\n"
            "| 同步时主仓 commit | `%s` |\n"
            "| 同步时间 | %s |\n"
            "| SHA-256 | `%s` |\n\n"
            "同步命令：`python3 sdk/scripts/sync-fixture.py`\n\n"
            "契约（`api/openapi-v1.yaml` 的 `x-uvp-signature-contract`）变了就要重新同步，\n"
            "然后跑 `python3 sdk/scripts/verify-parity.py --with-java` 确认三份实现都还一致。\n"
            % (git_commit(), datetime.datetime.now().strftime("%Y-%m-%d %H:%M:%S"), digest)
        )

    print("✓ 已同步 -> %s" % os.path.relpath(TARGET, REPO_ROOT))
    print("  sha256 = %s" % digest)
    return 0


if __name__ == "__main__":
    sys.exit(main())
