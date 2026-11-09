#!/usr/bin/env python3
"""用仓库里的签名向量自证 Python 实现与平台字节一致。

为什么必须有这个文件：canonical 少拼一行、query 少排一次序，签名照样
算得出来，只是平台一律 401。这种错误从现象上完全反推不出来，只能靠
同一份向量对账。

    python3 sdk/python/selftest.py
"""

from __future__ import annotations

import json
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from uvp_openapi import canonical, sha256_hex, sign  # noqa: E402

FIXTURE = os.path.join(os.path.dirname(os.path.abspath(__file__)),
                       "..", "fixtures", "signature-v1.json")


def main() -> int:
    with open(FIXTURE, encoding="utf-8") as handle:
        fixture = json.load(handle)

    access_key = fixture["accessKey"]
    secret_key = fixture["secretKey"]
    timestamp = fixture["timestamp"]
    nonce = fixture["nonce"]
    audience = fixture["audience"]

    failures = []
    for vector in fixture["vectors"]:
        name = vector["name"]
        body = vector["body"].encode("utf-8")

        produced = canonical(
            method=vector["method"],
            path=vector["path"],
            raw_query=vector["rawQuery"],
            content_type=vector["contentType"],
            body=body,
            access_key=access_key,
            timestamp=timestamp,
            nonce=nonce,
            idempotency_key=vector.get("idempotencyKey", ""),
            audience=audience,
        )
        produced_signature = sign(secret_key, produced)
        produced_bytes = len(produced.encode("utf-8"))

        checks = [
            ("canonical", produced, vector["canonical"]),
            ("canonicalBytes", produced_bytes, vector["canonicalBytes"]),
            ("bodySHA256", sha256_hex(body), vector["bodySHA256"]),
            ("signature", produced_signature, vector["signature"]),
        ]
        for field, got, want in checks:
            if got != want:
                failures.append("%s.%s 不一致\n  实际: %r\n  期望: %r"
                                % (name, field, got, want))
        if not any(item.startswith(name + ".") for item in failures):
            print("  ✓ %s  signature=%s" % (name, produced_signature[:16] + "..."))

    if failures:
        print("\n✗ 签名实现与平台向量不一致：", file=sys.stderr)
        for item in failures:
            print("  " + item, file=sys.stderr)
        return 1

    print("\n✓ Python 签名自测通过：%d 个向量" % len(fixture["vectors"]))
    return 0


if __name__ == "__main__":
    sys.exit(main())
