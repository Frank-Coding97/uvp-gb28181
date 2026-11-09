#!/usr/bin/env python3
"""校验 SDK 里所有语言的签名实现都落在同一份向量上。

这个脚本存在的理由：签名错的表现永远是"401"，而 401 与 AK/SK 填错长得一模一样。
真正容易出事的不是某一门语言写错，而是**只有一门语言被改对了**：
例如有人调了 Java 的 canonical，Python 和 Go 还停在旧逻辑，客户换语言接入就开始报 401。

顺带做地址体检：这个仓库要交给客户，真机地址和内网 IP 不能跟着源码一起发出去。

    python3 sdk/scripts/verify-parity.py                # 夹具对账 + Python + Go
    python3 sdk/scripts/verify-parity.py --with-java    # 再加 Java（需要 mvn）
    python3 sdk/scripts/verify-parity.py --fixture-only # 只查副本有没有漂移（CI 最快档）

退出码：0 全过 / 1 有不一致 / 2 环境缺失
"""

from __future__ import annotations

import argparse
import hashlib
import os
import shutil
import subprocess
import sys

SDK_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
REPO_ROOT = os.path.dirname(SDK_ROOT)
FIXTURE = os.path.join(SDK_ROOT, "fixtures", "signature-v1.json")
SERVER_ORIGINAL = os.path.join(
    REPO_ROOT, "server", "app", "openapi", "auth", "testdata", "signature-v1.json")


def run(label: str, command: list[str], cwd: str | None = None) -> bool:
    print("\n▶ %s" % label)
    result = subprocess.run(command, cwd=cwd, capture_output=True, text=True)
    output = (result.stdout + result.stderr).rstrip()
    if output:
        print(output)
    if result.returncode != 0:
        print("✗ %s 失败（exit=%d）" % (label, result.returncode), file=sys.stderr)
        return False
    return True


def check_fixture_copy() -> bool:
    """副本必须与主仓原件字节一致；SDK 独立后没有原件，这条自动跳过。"""
    if not os.path.isfile(FIXTURE):
        print("✗ 缺少 %s" % os.path.relpath(FIXTURE, REPO_ROOT), file=sys.stderr)
        return False
    if not os.path.isfile(SERVER_ORIGINAL):
        print("· 主仓没有服务端目录（SDK 已独立），跳过副本对账")
        return True

    def digest(path: str) -> str:
        with open(path, "rb") as handle:
            return hashlib.sha256(handle.read()).hexdigest()

    copy_digest = digest(FIXTURE)
    source_digest = digest(SERVER_ORIGINAL)
    if copy_digest != source_digest:
        print("✗ sdk/fixtures/signature-v1.json 与主仓 testdata 不一致\n"
              "  副本: %s\n  原件: %s\n  修复: python3 sdk/scripts/sync-fixture.py"
              % (copy_digest, source_digest), file=sys.stderr)
        return False
    print("✓ 夹具副本与主仓一致 sha256=%s..." % copy_digest[:16])
    return True


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--fixture-only", action="store_true", help="只对账夹具，不跑语言自测")
    parser.add_argument("--with-java", action="store_true", help="额外跑 Java 单测（需要 mvn）")
    args = parser.parse_args()

    ok = check_fixture_copy()
    if args.fixture_only:
        return 0 if ok else 1

    python = shutil.which("python3") or sys.executable
    ok = run("地址体检（对外交付不许出现真实平台/内网 IP）",
             [python, os.path.join(SDK_ROOT, "scripts", "check-no-endpoints.py")]) and ok
    ok = run("Python 自测", [python, os.path.join(SDK_ROOT, "python", "selftest.py")]) and ok

    go = shutil.which("go")
    if go is None:
        print("\n! 未找到 go，跳过 Go 自测（CI 里必须跑）", file=sys.stderr)
        ok = False
    else:
        ok = run("Go 自测", [go, "run", "./cmd/demo", "--self-test"],
                 cwd=os.path.join(SDK_ROOT, "go")) and ok

    if args.with_java:
        mvn = shutil.which("mvn")
        if mvn is None:
            print("\n! 未找到 mvn，跳过 Java 单测（CI 里必须跑）", file=sys.stderr)
            ok = False
        else:
            ok = run("Java 单测（HmacSignerTest）",
                     [mvn, "-B", "-q", "-pl", "uvp-openapi-sdk", "test",
                      "-Dtest=HmacSignerTest", "-DfailIfNoTests=false"],
                     cwd=os.path.join(SDK_ROOT, "java")) and ok

    print("\n%s" % ("✓ 全部一致" if ok else "✗ 存在不一致，见上面输出"))
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
