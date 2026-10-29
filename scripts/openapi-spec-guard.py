#!/usr/bin/env python3
"""OpenAPI 契约护栏：守住 `api/openapi-v1.yaml` 与签名夹具、Java 参考实现之间的一致性。

为什么需要这个脚本
------------------
这份 yaml 不只是文档，它是**第三方接入的唯一契约**：服务端契约测试读它、
代码生成器读它、客户端的签名实现也照着它写。历史上已经踩过两类静默失效：

1. **YAML 1.1 把 `on` 当布尔。**
   `properties.on: {type: integer}` 在 Go 的 yaml.v3（YAML 1.2）里是字符串 `on`，
   到了 openapi-generator / PyYAML（YAML 1.1）里变成布尔 `true`，生成的 Java 模型
   直接得到 `private TrueEnum true;`，**编译不过**。服务端契约测试全绿，看不见。
   ⇒ 判据：所有 mapping key 必须是 str；`required` 列表元素必须是 str。

2. **规范声明 3.0.3 却用了 3.1 的 `const`。**
   校验器直接报 "attribute ... is unexpected"，生成器虽然能跳过校验硬生成，
   但语义（必须等于某值）就丢了。⇒ 判据：3.0.x 里不许出现 `const`。

3. **签名示例少拼一行 canonical。**
   canonical 是 11 行（含 Idempotency-Key），少拼一行照样能算出签名，
   但服务端字节不一致 ⇒ **所有请求 401**。这个错误极难从现象反推，
   所以用夹具反算 + 静态核对示例的 `joinLines(...)` 实参个数来锁死。

用法
----
    scripts/openapi-spec-guard.py             # 全量（有 JDK 就跑 Java 自测）
    scripts/openapi-spec-guard.py --no-java   # 只做静态检查，不编译 Java
    scripts/openapi-spec-guard.py --require-java   # CI 用：找不到 JDK 算失败

退出码: 0=全过  1=有问题  2=环境/参数错误
"""

from __future__ import annotations

import argparse
import base64
import hashlib
import hmac
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SPEC = os.path.join(ROOT, "api", "openapi-v1.yaml")
FIXTURE = os.path.join(ROOT, "server", "app", "openapi", "auth", "testdata", "signature-v1.json")
JAVA_EXAMPLE = os.path.join(ROOT, "examples", "openapi-java", "OpenAPISignExample.java")

ERRORS: list[str] = []
NOTES: list[str] = []


def fail(message: str) -> None:
    ERRORS.append(message)


def load_yaml(path: str):
    try:
        import yaml  # noqa: F401  延迟导入：缺 PyYAML 时要给明确报错
    except ImportError:
        print("✗ 缺少 PyYAML：请用带 PyYAML 的解释器运行（quality-gate 会自己定位）", file=sys.stderr)
        sys.exit(2)
    with open(path, encoding="utf-8") as handle:
        # ⚠️ 这里**故意**用 PyYAML 默认加载器：它按 YAML 1.1 解析，
        #    与 openapi-generator 的 SnakeYAML 同族，才能复现代码生成侧的行为。
        return yaml.safe_load(handle)


def walk(node, path: str = ""):
    """递归遍历，产出 (路径, 值) 便于报错定位。"""
    yield path, node
    if isinstance(node, dict):
        for key, value in node.items():
            yield from walk(value, f"{path}/{key}" if path else str(key))
    elif isinstance(node, list):
        for index, value in enumerate(node):
            yield from walk(value, f"{path}[{index}]")


def check_versions(spec: dict) -> None:
    version = str(spec.get("openapi", ""))
    if not version.startswith("3.0"):
        fail(
            f"openapi 版本是 {version!r}：本护栏的 `const` 规则按 3.0.x 写死，"
            "若确实要升到 3.1，请同时修改本脚本与 README 的接入说明"
        )


def check_schema_keys(spec: dict) -> None:
    """所有 mapping key 必须是字符串，`required` 必须是字符串列表。"""
    for path, node in walk(spec):
        if isinstance(node, dict):
            for key in node:
                if not isinstance(key, str):
                    # PyYAML 把 `on`/`off`/`yes`/`no` 解析成布尔，键名就变成了 True/False
                    fail(
                        f"{path} 出现非字符串键 {key!r}：YAML 1.1 会把 on/off/yes/no 当布尔，"
                        "代码生成器会生成非法字段名。给键名加单引号（如 'on':）"
                    )
        if path.endswith("/required") and isinstance(node, list):
            for item in node:
                if not isinstance(item, str):
                    fail(
                        f"{path} 含非字符串元素 {item!r}：同上，裸写的 on/off/yes/no 会被当布尔"
                    )


def check_const(spec: dict) -> None:
    for path, node in walk(spec):
        if isinstance(node, dict) and "const" in node:
            fail(
                f"{path} 使用了 3.1 的 const：3.0.3 不认。"
                "字符串改成 {type: string, enum: [值]}，布尔改成 {type: boolean, enum: [true]}"
            )


def canonical_line_count(spec: dict) -> int:
    contract = spec.get("x-uvp-signature-contract") or {}
    lines = ((contract.get("canonical") or {}).get("lines")) or []
    return len(lines)


def check_fixture(spec: dict) -> None:
    """夹具必须自洽，且行数与规范声明的 canonical 行数一致。"""
    if not os.path.isfile(FIXTURE):
        fail(f"找不到签名夹具：{os.path.relpath(FIXTURE, ROOT)}")
        return
    with open(FIXTURE, encoding="utf-8") as handle:
        fixture = json.load(handle)

    expected_lines = canonical_line_count(spec)
    vectors = fixture.get("vectors") or []
    if not vectors:
        fail("签名夹具没有 vectors")

    secret = fixture.get("secretKey", "")
    try:
        key_bytes = base64.urlsafe_b64decode(secret + "=" * (-len(secret) % 4))
    except Exception as exc:  # noqa: BLE001  夹具损坏要原样报出来
        fail(f"夹具 secretKey 不是合法 base64url：{exc}")
        return
    if len(key_bytes) != 32:
        fail(f"夹具 secretKey 解码后是 {len(key_bytes)} 字节，规范要求 32 字节")

    for index, vector in enumerate(vectors):
        name = vector.get("name", f"vectors[{index}]")
        canonical = vector.get("canonical", "")
        actual_lines = len(canonical.split("\n")) if canonical else 0
        if actual_lines != expected_lines:
            fail(
                f"夹具 {name} 的 canonical 是 {actual_lines} 行，"
                f"但规范 x-uvp-signature-contract.canonical.lines 声明 {expected_lines} 行"
            )
        if canonical.endswith("\n"):
            fail(f"夹具 {name} 的 canonical 末尾多了换行（规范要求无结尾 LF）")
        if len(canonical.encode("utf-8")) != vector.get("canonicalBytes"):
            fail(
                f"夹具 {name} 的 canonicalBytes 是 {vector.get('canonicalBytes')}，"
                f"实际 {len(canonical.encode('utf-8'))} 字节"
            )
        body_hash = hashlib.sha256(vector.get("body", "").encode("utf-8")).hexdigest()
        if body_hash != vector.get("bodySHA256"):
            fail(f"夹具 {name} 的 bodySHA256 与实际算出的 {body_hash} 不一致")
        signature = hmac.new(key_bytes, canonical.encode("utf-8"), hashlib.sha256).hexdigest()
        if signature != vector.get("signature"):
            fail(
                f"夹具 {name} 的 signature 用夹具 SK 复算不上："
                f"算出 {signature}，夹具写的是 {vector.get('signature')}"
            )


def joinlines_arity(source: str) -> int | None:
    """数 `return joinLines(...)` 的实参个数（顶层逗号 + 1）。

    不引 Java 解析器，只处理这个文件里的事实形态：实参全是字符串字面量或标识符，
    字符串里没有逗号。返回 None 表示没找到调用点。
    """
    match = re.search(r"return\s+joinLines\(", source)
    if not match:
        return None
    start = match.end()
    depth = 1
    args = 1
    in_string = False
    index = start
    while index < len(source):
        char = source[index]
        if in_string:
            if char == "\\":
                index += 2
                continue
            if char == '"':
                in_string = False
        elif char == '"':
            in_string = True
        elif char == "(":
            depth += 1
        elif char == ")":
            depth -= 1
            if depth == 0:
                return args
        elif char == "," and depth == 1:
            args += 1
        index += 1
    return None


def check_java_example(spec: dict) -> None:
    if not os.path.isfile(JAVA_EXAMPLE):
        fail(f"找不到 Java 签名示例：{os.path.relpath(JAVA_EXAMPLE, ROOT)}")
        return
    with open(JAVA_EXAMPLE, encoding="utf-8") as handle:
        source = handle.read()
    expected = canonical_line_count(spec)
    arity = joinlines_arity(source)
    if arity is None:
        fail("Java 示例里找不到 canonical 的 joinLines(...) 调用，护栏判据失效，请同步修改脚本")
    elif arity != expected:
        fail(
            f"Java 示例的 canonical 拼了 {arity} 行，规范要求 {expected} 行"
            "（少拼一行照样算得出签名，但服务端会一律 401）"
        )
    if "idempotencyKey" not in source:
        fail("Java 示例没有处理 idempotencyKey：canonical 第 10 行缺失")


def locate_java() -> tuple[str, str] | None:
    javac = shutil.which("javac")
    java = shutil.which("java")
    if javac and java:
        return javac, java
    return None


def run_java_self_test(require_java: bool) -> None:
    pair = locate_java()
    if pair is None:
        message = "未找到 JDK（javac/java），跳过 Java 示例自测"
        if require_java:
            fail(message + "；CI 里必须跑，请确认 runner 装了 JDK")
        else:
            NOTES.append(message)
        return
    javac, java = pair
    with tempfile.TemporaryDirectory() as out_dir:
        compile_result = subprocess.run(  # noqa: S603
            [javac, "-d", out_dir, JAVA_EXAMPLE],
            capture_output=True,
            text=True,
        )
        if compile_result.returncode != 0:
            fail("Java 示例编译失败：\n" + (compile_result.stderr or compile_result.stdout).strip()[:800])
            return
        run_result = subprocess.run(  # noqa: S603
            [java, "-cp", out_dir, "OpenAPISignExample", "--self-test"],
            capture_output=True,
            text=True,
            cwd=ROOT,
        )
        if run_result.returncode != 0:
            fail(
                "Java 示例自测失败（签名与夹具对不上）：\n"
                + (run_result.stderr or run_result.stdout).strip()[:800]
            )


def main() -> int:
    parser = argparse.ArgumentParser(description="OpenAPI 契约护栏")
    parser.add_argument("--quiet", action="store_true", help="只输出结论")
    parser.add_argument("--no-java", action="store_true", help="跳过 Java 示例编译与自测")
    parser.add_argument("--require-java", action="store_true", help="找不到 JDK 视为失败（CI 用）")
    args = parser.parse_args()

    if not os.path.isfile(SPEC):
        print(f"✗ 找不到规范文件：{SPEC}", file=sys.stderr)
        return 2

    spec = load_yaml(SPEC)
    if not isinstance(spec, dict):
        print("✗ 规范文件不是 YAML mapping", file=sys.stderr)
        return 2

    check_versions(spec)
    check_schema_keys(spec)
    check_const(spec)
    check_fixture(spec)
    check_java_example(spec)
    if not args.no_java:
        run_java_self_test(args.require_java)

    for note in NOTES:
        print(f"· {note}")
    if ERRORS:
        print(f"✗ OpenAPI 契约护栏发现 {len(ERRORS)} 个问题：")
        for item in ERRORS:
            print(f"  - {item}")
        return 1
    if not args.quiet:
        print(
            "✓ OpenAPI 契约护栏通过："
            f"{canonical_line_count(spec)} 行 canonical / "
            f"{len(spec.get('paths') or {})} 条路径 / YAML 1.1 键名与 3.0.3 关键字均合规"
        )
    return 0


if __name__ == "__main__":
    sys.exit(main())
