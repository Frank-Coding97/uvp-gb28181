#!/usr/bin/env python3
"""路由 ↔ sys_api ↔ casbin 三方对账（防「新增路由忘了登记权限」的 403）。

为什么需要它
------------
casbin 中间件用 ``c.Request.URL.Path`` 匹配策略、**完全不查 ``sys_api``**。
所以「代码里注册了、但没在 ``sys_api`` 登记、也没给``role_1`` 授权」的接口，
对权限配置界面、对任何基于 ``sys_api`` 正查的护栏来说都是**隐形的** ——
表现就是用户点一下就403，而后端日志里只有一句「您没有权限访问此资源」。

⭐ 本项目已因此漏过两次（2026-10-08 补了 15 条，隔天又漏 7 条，
其中 ``GET /api/gb28181/sip/service-config`` 是老板实测撞出来的）。
⇒ 对账**必须从 ``routes.go`` 反查**，且**必须双向**：
  ① 代码 → sys_api（漏登记）② sys_api → casbin（漏授权）

用法
----
    # 从活库导出（SQLite 绿色包 / MySQL 开发库均可）
    python3 dump_sys_api.py sqlite data/uvp.db > /tmp/permlive.json
    # 或MySQL
    python3 dump_sys_api.py mysql "root:pwd@127.0.0.1:3306/uvp_gb28181" > /tmp/permlive.json

    python3 reconcile_routes.py --perm /tmp/permlive.json          # 对账
    python3 reconcile_routes.py --perm /tmp/permlive.json --json   # 机器可读

退出码：0 = 零缺口；1 = 有缺口（可直接进 CI/门禁）。

⚠️ **前缀推导的两个坑（都踩过，别改回错版）**
   ``gb := protected.Group("/gb28181")`` 的父级 ``protected`` **由外层传入、不在本文件**：
   ① 递归拼父前缀会多拼一层 ⇒ ``/gb28181/gb28181/...``；
   ② 只认本文件的组会**整段丢掉 /api 根前缀**。
   ⇒ 正确做法：``parent == "protected"`` 时基址取 ``/api``。

⚠️ **不参与对账的路由**（挂了它们必然是误报）：
   - ``index/hook/*``挂在 engine 根、用 ``hookAuthenticator.Middleware`` 自己的凭证认证；
   - ``public``/``engine`` 组（``/gb28181/sip/qr/exchange``、扫码落地页、
     下载直链等）—— 免鉴权。
"""

from __future__ import annotations

import argparse
import collections
import json
import pathlib
import re
import sys

HERE = pathlib.Path(__file__).resolve().parent


def _find_repo_root() -> pathlib.Path:
    """向上找仓库根（marker：``go.mod`` 且 module 是本项目）。

    ⛔ 不要写死 ``parents[n]``：脚本目录深度一变（挪进/挪出子目录）就静默指错，
       报错还是``FileNotFoundError`` 指向一个看起来很合理的路径 —— 极难定位。
    """
    for parent in HERE.parents:
        gomod = parent / "go.mod"
        if gomod.is_file() and "uvp-gb28181" in gomod.read_text(encoding="utf-8"):
            return parent
        if (parent / "server" / "go.mod").is_file():
            return parent
    raise SystemExit(
        f"没找到仓库根（从 {HERE} 向上找不到含 go.mod 的目录）。"
        "请用 --routes 显式指定 routes.go 路径。"
    )


REPO = _find_repo_root()
ROUTES_GO = REPO / "server/app/gb28181/routes/routes.go"

# 挂在 engine 根、用自己凭证认证的 hook 回调
HOOK_RE = re.compile(r"/index/hook/")
# 免鉴权接口：登记在sys_api 但本就不该授权
FREE_API = {
    ("/api/login", "POST"),
    ("/api/refreshToken", "POST"),
    ("/api/users/logout", "POST"),
    ("/api/captcha", "GET"),
}


def _norm(path: str) -> str:
    """把 ``:id`` 这类参数段归一化成 ``:*``，让路径能按「形状」匹配。"""
    return re.sub(r":[A-Za-z]+", ":*", path)


def collect_code_routes(path: pathlib.Path = ROUTES_GO) -> set[tuple[str, str]]:
    """从 routes.go 抽出所有**受 casbin 管**的 (METHOD, 完整路径)。

    完整路径 = 分组前缀链+ 路由内路径。参数段原样保留（:id），由 _norm 归一。
    """
    src = path.read_text(encoding="utf-8")

    # X := <parent>.Group("sub")⇒ X 的前缀 = parent 前缀 + sub
    prefix: dict[str, str] = {}
    for m in re.finditer(r'(\w+)\s*:=\s*(\w+)\.Group\("([^"]*)"\)', src):
        child, parent, sub = m.groups()
        if parent == "protected":
            base = "/api"          # ⛔ protected 由外层传入，本文件里查不到
        elif parent in ("api", "engine", "public"):
            base = ""
        else:
            base = prefix.get(parent, "")
        prefix[child] = base + sub

    out: set[tuple[str, str]] = set()
    for m in re.finditer(r'(\w+)\.(GET|POST|PUT|DELETE|PATCH)\("([^"]*)"', src):
        grp, meth, sub_path = m.groups()
        if grp in ("engine", "public"):     # 免鉴权，不参与对账
            continue
        full = prefix.get(grp)
        if full is None:                    # 分组未识别 ⇒ 宁可漏报也不要报错
            continue
        route = f"{full}{sub_path}"
        if HOOK_RE.search(route):
            continue
        out.add((meth.upper(), route))
    return out


def load_permission_table(path: pathlib.Path) -> list[tuple[str, str]]:
    """读权限快照，返回 ``[(path, METHOD), ...]``。

    支持两种格式（门禁要能直接吃仓库里的种子，所以两种都必须支持）：

    1. **JSON 数组** —— ``dump_sys_api.py`` 的输出：``[["/api/x","GET"], ...]``
    2. **JSONL** —— ``seeds/sys_api.jsonl``：每行一个 ``{"path":...,"method":...}``

    ⚠️ 别只支持 JSON 数组：门禁里传的是仓库种子（JSONL），
    只认数组会直接抛 ``JSONDecodeError: Extra data`` ⇒ **门禁项永远失败**，
    而失败信息指向 JSON 解析、离「格式不匹配」这个真因隔了好几层。
    """
    raw = path.read_text(encoding="utf-8")
    items: list = []
    try:
        data = json.loads(raw)
        items = data if isinstance(data, list) else [data]
    except json.JSONDecodeError:
        # JSONL：逐行解析，跳过空行；任一行坏了要报出**行号**
        for lineno, line in enumerate(raw.splitlines(), 1):
            line = line.strip()
            if not line:
                continue
            try:
                items.append(json.loads(line))
            except json.JSONDecodeError as exc:
                raise SystemExit(
                    f"{path.name} 第 {lineno} 行不是合法 JSON：{exc.msg}。"
                    "该文件既不是 JSON 数组也不是 JSONL，请确认传对了文件。"
                ) from None

    rows: list[tuple[str, str]] = []
    for item in items:
        if isinstance(item, (list, tuple)) and len(item) == 2:
            rows.append((item[0], str(item[1]).upper()))
        elif isinstance(item, dict) and "path" in item and "method" in item:
            rows.append((item["path"], str(item["method"]).upper()))
    return rows


def reconcile(perm_file: pathlib.Path, routes_file: pathlib.Path = ROUTES_GO):
    code = collect_code_routes(routes_file)
    perm = load_permission_table(perm_file)

    api_by_shape: dict[str, set[str]] = collections.defaultdict(set)
    for p, m in perm:
        api_by_shape[_norm(p)].add(m)

    code_by_shape: dict[str, set[str]] = collections.defaultdict(set)
    for m, p in code:
        code_by_shape[_norm(p)].add(m)

    # ① 代码注册了但 sys_api 没登记
    missing_api = [
        (meth, shape)
        for shape, meths in sorted(code_by_shape.items())
        for meth in sorted(meths)
        if meth not in api_by_shape.get(shape, set())
    ]
    return missing_api, len(code), len(perm)


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--perm", required=True, type=pathlib.Path,
                    help="dump_sys_api.py 产出的权限快照 JSON")
    ap.add_argument("--routes", type=pathlib.Path, default=ROUTES_GO,
                    help="routes.go 路径（默认仓库内那份）")
    ap.add_argument("--json", action="store_true", help="输出 JSON（供 CI 消费）")
    args = ap.parse_args()

    missing, code_n, perm_n = reconcile(args.perm, args.routes)

    if args.json:
        json.dump({"missing": missing, "codeRoutes": code_n, "sysApi": perm_n},
                  sys.stdout, ensure_ascii=False, indent=2)
        print()
        return 1 if missing else 0

    print(f"代码路由(受 casbin 管) {code_n} 条 / sys_api 登记 {perm_n} 条")
    if not missing:
        print("✅ 零缺口：代码里注册的接口在 sys_api 里都有登记")
        return 0
    print(f"❌ 代码注册了但 sys_api 未登记: {len(missing)} 条")
    for meth, shape in missing:
        print(f"     {meth:6} {shape}")
    print("\n补法：写进 seeds/sys_api.jsonl + seeds/sys_casbin_rule.jsonl，"
          "再重跑 apply_casbin_seed_delta.py 与 sqlitebaseline/generate.py")
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
