#!/usr/bin/env python3
"""把 sys_casbin_rule 的权限增量写进 MySQL 基线SQL。

⛔⛔ 为什么不手工编辑 `uvp-gb28181.sql`（这是本脚本存在的唯一理由）：
   那个文件里`sys_casbin_rule` 的种子是**一条 200+ 行的 INSERT**，
   手工往中间插行必须同时管好三件事：上一行的分号、自己的括号、下一行的衔接。
   实测连试 4 次都错在**不同**的地方（丢分号 / 丢右括号 / 多一个括号 /
   把整块切成三段），而这类错**语法检查不出来**—— 只在 verify_baseline
   跑建库时才报 `near "("`，而报错位置离真正的原因很远。
   ⇒ 这里改成**整块重新生成**：从 JSONL 种子读出全部 p 策略，
   拼成一整条 INSERT 替换掉原来那条。种子是行式的，没有分号/括号问题。

用法：
    python3 apply_casbin_seed_delta.py --sql <uvp-gb28181.sql> --add <delta.jsonl>

delta.jsonl 每行形如：
    {"v1": "/api/sysParam/list", "v2": "GET"}
或（需要时）
    {"v0": "role_3", "v1": "/api/xxx", "v2": "POST"}
"""
from __future__ import annotations

import argparse
import json
import pathlib
import re
import sys

SEED = pathlib.Path(__file__).resolve().parent / "baseline" / "seeds" / "sys_casbin_rule.jsonl"
COLUMNS = ("id", "ptype", "v0", "v1", "v2", "v3", "v4", "v5")


def load_seed() -> list[dict]:
    rows = []
    for line in SEED.read_text(encoding="utf-8").split("\n"):
        if line.strip():
            rows.append(json.loads(line))
    return rows


def read_delta(path: pathlib.Path) -> list[dict]:
    out = []
    for line in path.read_text(encoding="utf-8").split("\n"):
        if line.strip():
            out.append(json.loads(line))
    return out


def max_id(rows: list[dict]) -> int:
    return max((r.get("id", 0) for r in rows), default=0)


def existing_keyset(rows: list[dict]) -> set[tuple[str, str, str, str]]:
    """已存在的策略键：ptype/v0/v1/v2。

    ⛔ 必须按**完整四元组**判重，不能只看 v1 —— 同一路径可能被不同角色
    或不同动作授权（如 POST /api/x 与 DELETE /api/x 都存在）。
    """
    return {(r.get("ptype", ""), r.get("v0", ""), r.get("v1", ""), r.get("v2", "")) for r in rows}


def sql_literal(value: str) -> str:
    """渲染 SQL 字符串字面量。

    ⛔ 不用简单的 f"'{v}'"：路径里可能有单引号（虽然当前没有），
    直接拼会把整条 INSERT 弄坏，而那种错同样是**建库时才报**。
    ⇒ 统一走转义：单引号翻倍 + 反斜杠不转义（基线里没有反斜杠值）。
    """
    return "'" + str(value).replace("'", "''") + "'"


def render_insert(rows: list[dict]) -> str:
    """把策略行渲染成一条完整的 INSERT。"""
    lines = [
        "INSERT INTO `sys_casbin_rule` "
        "(`id`, `ptype`, `v0`, `v1`, `v2`, `v3`, `v4`, `v5`) VALUES"
    ]
    for n, r in enumerate(rows, start=1):
        closing = ";" if n == len(rows) else ","
        values = [str(r.get(c, "") if r.get(c) is not None else "") for c in COLUMNS]
        rendered = ", ".join(sql_literal(v) if i else v for i, v in enumerate(values))
        lines.append(f"({rendered}){closing}")
    return "\n".join(lines)


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--sql", required=True, help="uvp-gb28181.sql 路径")
    ap.add_argument("--add", help="要追加的策略 JSONL")
    ap.add_argument("--sync", action="store_true",
                    help="只把 SQL 与种子对齐（不追加）。用于种子被外部改动后重整 SQL。")
    args = ap.parse_args()
    if not args.add and not args.sync:
        ap.error("需要 --add 或 --sync 之一")

    sql_path = pathlib.Path(args.sql)
    delta_path = pathlib.Path(args.add) if args.add else None

    seed_rows = load_seed()
    delta = read_delta(delta_path) if delta_path else []
    if not delta and not args.sync:
        print("增量为空，无事可做")
        return 0

    seen = existing_keyset(seed_rows)
    next_id = max_id(seed_rows)

    to_add: list[dict] = []
    for d in delta:
        ptype = d.get("ptype", "p")
        v0 = d.get("v0", "role_1")
        v1 = d["v1"]
        v2 = d["v2"]
        key = (ptype, v0, v1, v2)
        if key in seen:
            print(f"  跳过（已存在）: {v0} {v2} {v1}")
            continue
        seen.add(key)
        next_id += 1
        to_add.append({
            "id": next_id, "ptype": ptype, "v0": v0, "v1": v1, "v2": v2,
            "v3": d.get("v3", "*"), "v4": d.get("v4", ""), "v5": d.get("v5", ""),
        })
        print(f"  + {v2:6s} {v1}({v0}) id={next_id}")

    if to_add:
        # 1) 种子 jsonl 追加
        seed_text = SEED.read_text(encoding="utf-8").rstrip("\n")
        add_text = "\n".join(
            json.dumps(r, ensure_ascii=False, separators=(", ", ": ")) for r in to_add
        )
        SEED.write_text(f"{seed_text}\n{add_text}\n", encoding="utf-8")
        print(f"\n种子已更新: {SEED}（+{len(to_add)} 条）")
    else:
        # ⛔⛔ 即使没有增量也要重整 SQL：种子可能被外部改过（回滚、手工编辑），
        #   而脚本按"已存在就跳过"返回 ⇒ SQL 就永远停在旧内容上。
        #   实测踩过：git checkout 回滚种子后重跑，脚本说"全部已存在"，
        #   但 SQL 里还缺着被删掉的那条 ⇒ 校验立刻报 403。
        #   ⇒ 声明一个 --sync 就够：SQL 始终由种子决定，不存在"两边不一致"。
        print("\n无增量，但按 --sync 用种子重整 SQL")

    # 2) 重写 SQL 里**所有** casbin INSERT 段
    sql_text = sql_path.read_text(encoding="utf-8", errors="replace")
    all_rows = seed_rows + to_add

    # ⛔⛔ MySQL 导出把大表的种子**分成多段 INSERT**（实测本文件是 3 段：
    #    200 + 200 + 81 行 = 481 条）。只替换第一段会留下两段旧的，
    #    新增的权限进不去；而按"插入点"去手工编辑更是错法百出
    #    （我连踩 4 次：丢分号 / 丢括号 / 多括号 / 把块切三段）。
    #   ⇒ 这里**删除所有旧段、在第一段的位置写一整段新的**，其余段删掉。
    head = re.compile(
        r"INSERT INTO `sys_casbin_rule`\s*\([^)]*\)\s*VALUES", re.MULTILINE
    )
    segments: list[tuple[int, int]] = []   # (start, end) 覆盖整条语句含结尾 ');'
    pos = 0
    while True:
        m = head.search(sql_text, pos)
        if not m:
            break
        close = sql_text.find(");", m.end())
        if close < 0:
            print("❌ 某段 casbin INSERT 找不到结尾 ');' —— 源文件被手工改坏了？"
                  "请先 git checkout 该文件再重试", file=sys.stderr)
            return 1
        # 吃掉结尾的换行（若有），避免留下空行堆积
        end = close + 2
        while end < len(sql_text) and sql_text[end] == "\n":
            end += 1
        segments.append((m.start(), end))
        pos = end

    if not segments:
        print("❌ 在 SQL 里找不到 sys_casbin_rule 的 INSERT", file=sys.stderr)
        return 1

    print(f"\n原 SQL 里有 {len(segments)} 段 casbin INSERT，"
          f"共 {sum(sql_text[a:b].count(chr(10)) for a, b in segments)} 数据行")

    # 从后往前删，避免偏移量失效
    for a, b in reversed(segments):
        sql_text = sql_text[:a] + sql_text[b:]

    # 在第一段原来的位置插入新段
    first = segments[0][0]
    new_stmt = render_insert(all_rows) + "\n"
    sql_path.write_text(sql_text[:first] + new_stmt + sql_text[first:], encoding="utf-8")
    print(f"SQL 已重写：{len(segments)} 段合并为 1 段，共 {len(all_rows)} 条策略")

    sync_sys_api(sql_path)
    sync_sys_users(sql_path)
    return 0


# ------------------------------------------------------------ sys_users ----
USERS_SEED = pathlib.Path(__file__).resolve().parent / "baseline" / "seeds" / "sys_users.jsonl"


def sync_sys_users(sql_path: pathlib.Path) -> None:
    """把 sys_users 种子同步进基线 SQL（admin 的默认头像走这条路）。

    为什么必须同步：绿色包的库是**首次启动时从这份基线建的**，
    只改 JSONL 不改 SQL ⇒ 新建出来的库头像仍是空的。
    """
    rows = [json.loads(l) for l in USERS_SEED.read_text(encoding="utf-8").split("\n") if l.strip()]
    if not rows:
        return
    cols = ["id", "username", "password", "email", "status", "dept_id", "phone", "sex",
            "nick_name", "avatar", "description", "created_by"]
    text = sql_path.read_text(encoding="utf-8", errors="replace")
    head = re.compile(r"INSERT INTO `sys_users`\s*\([^)]*\)\s*VALUES", re.MULTILINE)
    m = head.search(text)
    if not m:
        print("\n⚠️ SQL 里找不到 sys_users 的 INSERT，跳过")
        return
    close = text.find(");", m.end())
    end = close + 2
    while end < len(text) and text[end] == "\n":
        end += 1

    lines = [f"INSERT INTO `sys_users` ({', '.join('`' + c + '`' for c in cols)}) VALUES"]

    def lit(col, r):
        v = r.get(col, "")
        if col in ("id", "status", "dept_id", "created_by"):
            return str(v if v not in ("", None) else 0)
        if col == "password":
            # bcrypt 的 $ 前缀在 SQL 里没有特殊含义，原样单引号包裹即可
            return "'" + str(v).replace("'", "''") + "'"
        return "'" + str(v).replace("'", "''") + "'"

    for n, r in enumerate(rows, 1):
        lines.append("(" + ", ".join(lit(c, r) for c in cols) + ")"
                     + (";" if n == len(rows) else ","))
    new_stmt = "\n".join(lines) + "\n"
    sql_path.write_text(text[:m.start()] + new_stmt + text[end:], encoding="utf-8")
    avatars = [r.get("username") for r in rows if (r.get("avatar") or "").strip()]
    print(f"sys_users 已同步：{len(rows)} 个用户"
          + (f"（含头像：{', '.join(avatars)}）" if avatars else ""))


# ------------------------------------------------------------- sys_api ----
#
# ⛔⛔ 为什么必须同步 sys_api（实测踩过，与固件仓库那次完全同型）：
#   路由注册了但 sys_api 表里没有这条登记 ⇒
#   ① 基线护栏的「全量对比」**抓不到它**（它只扫表里有记录的东西）；
#   ② 管理端列表页依赖 sys_api，接口也不会出现在权限配置界面里。
#   实测：`/api/gb28181/sip/service-config` 的父路径（GET/PUT）就是这样漏的 ——
#   34 条子路径（playback-settings/sdp-extension/…）全都有，
#   **唯独父路径本身没登记、也没授权** ⇒ admin 打开就是 403。
API_SEED = pathlib.Path(__file__).resolve().parent / "baseline" / "seeds" / "sys_api.jsonl"
API_COLUMNS = ("id", "method", "path", "api_group", "api_name", "remark",
               "created_by", "created_at", "updated_at")


def sync_sys_api(sql_path: pathlib.Path) -> None:
    """把 sys_api 种子里的登记同步进基线 SQL（同样整段重渲染）。"""
    rows = [json.loads(l) for l in API_SEED.read_text(encoding="utf-8").split("\n") if l.strip()]
    # 只同步种子里有的列：SQL 里的 INSERT 列名必须与实际渲染一致，
    # 多写一个不存在的列会直接让建库失败。
    cols = [c for c in API_COLUMNS if any(c in r for r in rows)]
    text = sql_path.read_text(encoding="utf-8", errors="replace")
    # ⛔⛔ 必须处理**全部段**，不能只替换第一段（与 casbin 当初同一个坑）。
    #   实测源 SQL 里 sys_api 也有 3 段（438 + 200 + 36 行）—— MySQL 导出的分批。
    #   只改第一段 ⇒ 另两段残留 ⇒ id 与第一段撞车 ⇒ 建库时报
    #   `UNIQUE constraint failed: sys_api.id`，而报错位置离真因很远。
    head = re.compile(r"INSERT INTO `sys_api`\s*\([^)]*\)\s*VALUES", re.MULTILINE)
    segments: list[tuple[int, int]] = []
    pos = 0
    while True:
        m = head.search(text, pos)
        if not m:
            break
        close = text.find(");", m.end())
        if close < 0:
            print("❌ 某段 sys_api INSERT 找不到结尾');' —— 请先 git checkout 再重试",
                  file=sys.stderr)
            return
        end = close + 2
        while end < len(text) and text[end] == "\n":
            end += 1
        segments.append((m.start(), end))
        pos = end
    if not segments:
        print("\n⚠️ SQL 里找不到 sys_api 的 INSERT，跳过同步")
        return
    total_before = sum(text[a:b].count("\n") for a, b in segments)
    for a, b in reversed(segments):
        text = text[:a] + text[b:]
    first = segments[0][0]

    lines = [f"INSERT INTO `sys_api` ({', '.join('`' + c + '`' for c in cols)}) VALUES"]
    for n, r in enumerate(rows, 1):
        def lit(col):
            v = r.get(col, "")
            if col in ("id", "created_by"):
                return str(v if v not in ("", None) else 0)
            return "'" + str(v).replace("'", "''") + "'"
        lines.append("(" + ", ".join(lit(c) for c in cols) + ")"
                     + (";" if n == len(rows) else ","))
    new_stmt = "\n".join(lines) + "\n"

    sql_path.write_text(text[:first] + new_stmt + text[first:], encoding="utf-8")
    print(f"sys_api 已同步：{len(segments)} 段合并为 1 段，共 {len(rows)} 条登记"
          f"（原 {total_before} 行）")


if __name__ == "__main__":
    sys.exit(main())