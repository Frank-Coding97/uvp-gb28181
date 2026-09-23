#!/usr/bin/env python3
"""Capture the release baseline schema from the live development database.

Why this exists
---------------
The release artifacts (``uvp-gb28181.sql`` + the dialect conversions) used to be
maintained partly by hand.  That drifted: the MySQL snapshot was missing real
business tables, and the PostgreSQL / SQL Server files were missing twenty core
tables while still carrying demo tables that no longer existed.

The database is the only complete source of truth, so this script reads the
schema out of it and writes a dialect-neutral intermediate representation:

    schema.ir.json     table/column/index structure for every included table
    seeds/<table>.jsonl  one JSON object per seeded row (only seed tables)

``generate_sql.py`` then turns the IR into the four dialect scripts.  The split
matters: capturing needs a database, generating does not.  That means the
generated SQL can be re-derived and diffed in CI without any database access.

Usage
-----
    python3 capture_schema.py [--check]

``--check`` re-captures into memory and reports whether the checked-in IR is
still in sync with the database, without writing anything.
"""

from __future__ import annotations

import argparse
import base64
import datetime
import decimal
import hashlib
import json
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent
DATABASE_DIR = ROOT.parent           # server/resource/database
REPO_ROOT = DATABASE_DIR.parents[2]  # repository root

POLICY_FILE = ROOT / "policy.json"
IR_FILE = ROOT / "schema.ir.json"
SEEDS_DIR = ROOT / "seeds"


# --------------------------------------------------------------------------
# connection
# --------------------------------------------------------------------------

def parse_config(path: Path) -> dict[tuple[str, str], str]:
    """Flatten ``server/config/config.yml`` into {(section, key): value}.

    Deliberately a line/indent based parser rather than a regex: the file mixes
    two-space section indents with four/eight-space nesting, and regex
    lookaheads over it are easy to get subtly wrong.
    """
    values: dict[tuple[str, str], str] = {}
    section = None
    sub = None
    for raw in path.read_text(encoding="utf-8").splitlines():
        if not raw.strip() or raw.lstrip().startswith("#"):
            continue
        indent = len(raw) - len(raw.lstrip())
        key, _, value = raw.strip().partition(":")
        value = value.strip().strip('"').strip("'")
        if indent == 0:
            section, sub = key, None
            continue
        if indent <= 4:
            if section == "gormv2":
                sub = key
            continue
        if indent >= 8 and section == "gormv2" and sub:
            values[(sub, key)] = value
    return values


def connect(config_path: Path):
    import pymysql

    cfg = parse_config(config_path)
    return pymysql.connect(
        host=cfg[("mysql", "host")],
        port=int(cfg[("mysql", "port")]),
        user=cfg[("mysql", "user")],
        password=cfg[("mysql", "pass")],
        database=cfg[("mysql", "database")],
        charset="utf8mb4",
    )


def resolve_config(explicit: str | None) -> Path:
    """Locate ``server/config/config.yml``.

    The file is gitignored, so a git worktree does not have one.  Main
    checkouts work with no arguments; worktrees pass ``--config`` pointing at
    their primary checkout's copy.
    """
    if explicit:
        path = Path(explicit).expanduser().resolve()
        if not path.is_file():
            raise SystemExit(f"--config 指向的文件不存在: {path}")
        return path
    path = REPO_ROOT / "server" / "config" / "config.yml"
    if not path.is_file():
        raise SystemExit(
            f"找不到 {path}。\n"
            "该文件是 gitignored 的，worktree 里没有；用 --config 指向主工作区的 "
            "server/config/config.yml。"
        )
    return path


# --------------------------------------------------------------------------
# introspection
# --------------------------------------------------------------------------

def fetch_tables(cur) -> list[str]:
    cur.execute(
        """SELECT table_name FROM information_schema.tables
           WHERE table_schema = DATABASE() AND table_type = 'BASE TABLE'
           ORDER BY table_name"""
    )
    return [row[0] for row in cur.fetchall()]


def fetch_table_comments(cur) -> dict[str, str]:
    """表注释。

    单独查一遍而不是复用 ``fetch_tables``：表注释是 ``information_schema.TABLES``
    的列，而表清单被多个地方复用（排除判定、计数），塞进返回值会改动所有调用点。
    """
    cur.execute(
        """SELECT table_name, table_comment FROM information_schema.tables
           WHERE table_schema = DATABASE() AND table_type = 'BASE TABLE'"""
    )
    return {name: (comment or "") for name, comment in cur.fetchall()}


def fetch_columns(cur, table: str) -> list[dict]:
    cur.execute(
        """SELECT column_name, column_type, is_nullable, column_default, extra,
                  column_comment, collation_name, ordinal_position
           FROM information_schema.columns
           WHERE table_schema = DATABASE() AND table_name = %s
           ORDER BY ordinal_position""",
        (table,),
    )
    out = []
    for name, ctype, nullable, default, extra, comment, collation, _pos in cur.fetchall():
        out.append(
            {
                "name": name,
                "type": ctype,
                "nullable": nullable == "YES",
                "default": default,
                "extra": extra or "",
                "comment": comment or "",
                "collation": collation,
            }
        )
    return out


def column_value_range(cur, table: str, column: str) -> tuple:
    cur.execute(f"SELECT MIN(`{column}`), MAX(`{column}`) FROM `{table}`")
    return cur.fetchone()


def resolve_tinyint1(cur, table: str, columns: list[dict], forced_enums: set,
                     forced_integers: set, warnings: list) -> None:
    """Decide whether each ``tinyint(1)`` column is a boolean or a small enum.

    ``tinyint(1)`` is ambiguous in MySQL: GORM emits it for Go ``bool``, but it
    is also a common way to write a small enum.  Mapping an enum to BOOLEAN
    silently breaks cross-dialect behaviour (``sys_menu.type`` holds 1/2/3 for
    directory/page/button), so the decision is made from the data and recorded
    in the IR rather than assumed from the type name.

    The data alone is not enough, though: the dev database is a long-lived
    instance, so a column that *happens* to hold only 1 today looks boolean even
    when the model declares it as ``*int8`` (``sys_users.status`` is 1=enabled /
    2=disabled).  ``forced_integers`` is the authoritative override list, derived
    column-by-column from the Go model field types -- see policy.json.
    """
    for col in columns:
        if col["type"] != "tinyint(1)":
            continue
        qualified = f"{table}.{col['name']}"
        forced_enum = qualified in forced_enums
        forced_int = qualified in forced_integers
        low, high = column_value_range(cur, table, col["name"])
        in_bool_domain = (low is None or low in (0, 1)) and (high is None or high in (0, 1))
        if not in_bool_domain and not forced_enum:
            warnings.append(
                f"{qualified} 的取值超出 0/1（min={low} max={high}），按小枚举处理；"
                f"如果它其实是布尔，请说明，否则建议加进 policy.json 的 tinyint1_enum_columns"
            )
        if forced_int:
            col["boolean"] = False
            col["integer_reason"] = "policy:not_boolean_columns（模型字段为整数类型）"
            continue
        col["boolean"] = in_bool_domain and not forced_enum


def fetch_indexes(cur, table: str) -> list[dict]:
    cur.execute(
        """SELECT index_name, non_unique, seq_in_index, column_name, sub_part
           FROM information_schema.statistics
           WHERE table_schema = DATABASE() AND table_name = %s
           ORDER BY index_name, seq_in_index""",
        (table,),
    )
    grouped: dict[str, dict] = {}
    for index_name, non_unique, _seq, column, sub_part in cur.fetchall():
        entry = grouped.setdefault(
            index_name,
            {"name": index_name, "unique": not non_unique, "columns": []},
        )
        entry["columns"].append({"name": column, "prefix": sub_part})
    return list(grouped.values())


def fetch_foreign_keys(cur, table: str) -> list[dict]:
    """外键及其级联规则。

    规则（``delete_rule`` / ``update_rule``）与列清单在不同视图里：
    ``key_column_usage`` 有列，``referential_constraints`` 有规则，靠约束名 join。
    漏掉规则的后果是静默的 —— 生成的脚本里外键退化成默认 ``NO ACTION``，
    数据照常入库，直到某天发现删父行没级联才暴露。
    """
    cur.execute(
        """SELECT k.constraint_name, k.column_name, k.referenced_table_name,
                  k.referenced_column_name, r.delete_rule, r.update_rule
           FROM information_schema.key_column_usage k
           LEFT JOIN information_schema.referential_constraints r
                  ON r.constraint_schema = k.constraint_schema
                 AND r.constraint_name = k.constraint_name
           WHERE k.table_schema = DATABASE() AND k.table_name = %s
             AND k.referenced_table_name IS NOT NULL
           ORDER BY k.constraint_name, k.ordinal_position""",
        (table,),
    )
    grouped: dict[str, dict] = {}
    for name, column, ref_table, ref_column, delete_rule, update_rule in cur.fetchall():
        entry = grouped.setdefault(
            name, {"name": name, "columns": [], "referenced_table": ref_table,
                   "referenced_columns": [], "delete_rule": delete_rule or "NO ACTION",
                   "update_rule": update_rule or "NO ACTION"},
        )
        entry["columns"].append(column)
        entry["referenced_columns"].append(ref_column)
    return list(grouped.values())


def unescape_check_clause(clause: str) -> str:
    """还原 MySQL 在 ``check_clause`` 里做的反斜杠转义。

    ``information_schema.check_constraints`` 返回的是 ``SHOW CREATE TABLE``
    风格，字符串字面量的引号写成 ``\\'``：``_utf8mb4\\'pending\\'``。
    不还原的话，下游把它当两个字符（反斜杠 + 引号），于是「去掉字符集前缀」
    的正则永远匹配不上 —— 表现成「CHECK 里的 ``_utf8mb4`` 怎么都去不掉」，
    而且生成的脚本里带着反斜杠，导入时直接语法错。
    """
    out: list[str] = []
    index = 0
    while index < len(clause):
        char = clause[index]
        if char == "\\" and index + 1 < len(clause) and clause[index + 1] in {"'", '"', "\\"}:
            out.append(clause[index + 1])
            index += 2
            continue
        out.append(char)
        index += 1
    return "".join(out)


def fetch_check_constraints(cur, table: str) -> list[dict]:
    """表级 CHECK 约束。

    MySQL 8.0 把 CHECK 拆在两个视图里：``table_constraints`` 给归属表，
    ``check_constraints`` 给表达式原文。表达式是 **MySQL 方言**的
    （``_utf8mb4'x'`` 字面量前缀、``regexp_like``、反引号），转换由
    generate_sql.py 按方言做，这里只保证原文被完整取出**且已反转义**。

    ``gb_device_chk_1`` 这种自动命名（MySQL 给匿名 CHECK 起的）也收 ——
    它同样约束数据，丢了就是跨方言少一条约束。
    """
    cur.execute(
        """SELECT c.constraint_name, c.check_clause
           FROM information_schema.check_constraints c
           JOIN information_schema.table_constraints t
             ON t.constraint_schema = c.constraint_schema
            AND t.constraint_name = c.constraint_name
           WHERE c.constraint_schema = DATABASE() AND t.table_name = %s
           ORDER BY c.constraint_name""",
        (table,),
    )
    return [{"name": name, "clause": unescape_check_clause(clause)}
            for name, clause in cur.fetchall()]


# --------------------------------------------------------------------------
# seed rows
# --------------------------------------------------------------------------

def encode_value(value):
    """Encode a value with an explicit tag when JSON cannot round-trip it."""
    if value is None or isinstance(value, (bool, int, float, str)):
        return value
    if isinstance(value, (bytes, bytearray)):
        return {"$hex": bytes(value).hex()}
    if isinstance(value, datetime.datetime):
        return {"$ts": value.isoformat(sep=" ", timespec="microseconds")}
    if isinstance(value, datetime.date):
        return {"$date": value.isoformat()}
    if isinstance(value, datetime.timedelta):
        return {"$dur": value.total_seconds()}
    if isinstance(value, decimal.Decimal):
        return {"$dec": str(value)}
    return {"$raw": base64.b64encode(str(value).encode("utf-8")).decode("ascii")}


def apply_seed_filters(table: str, rows: list[dict], filters: dict) -> list[dict]:
    """Drop environment rows and blank placeholder fields, per policy.json.

    The old release snapshot was cleaned by hand-editing SQL, so the reasoning
    behind each removal was never recorded.  These rules replace that with an
    explicit, reviewable list.
    """
    rule = filters.get(table)
    if not rule:
        return rows

    kept = []
    for row in rows:
        for cond in rule.get("drop_where", []):
            value = row.get(cond["column"])
            if "in" in cond and value in cond["in"]:
                break
            if "matches" in cond and isinstance(value, str) and re.search(cond["matches"], value):
                break
        else:
            kept.append(row)

    overrides = rule.get("set") or {}
    if kept:
        known = set(kept[0])
        unknown = sorted(set(overrides) - known)
        if unknown:
            raise SystemExit(
                f"{table}: seed_filters.set 里的列不存在: {', '.join(unknown)}"
            )
    for row in kept:
        row.update(overrides)
    return kept


def apply_referential_cleanup(seeds: dict, rules: list[dict]) -> list[str]:
    """Drop rows whose referenced row is no longer in the seed set.

    Junction tables store ids only, so removing an excluded menu or API leaves
    orphan bindings behind.  Doing this by rule (rather than by hardcoding id
    whitelists) keeps working when ids drift between environments.
    """
    notes = []
    for rule in rules:
        table, column = rule["table"], rule["column"]
        target, target_column = rule["references"], rule["references_column"]
        if table not in seeds or target not in seeds:
            continue
        alive = {row.get(target_column) for row in seeds[target]}
        before = len(seeds[table])
        seeds[table] = [row for row in seeds[table] if row.get(column) in alive]
        removed = before - len(seeds[table])
        if removed:
            notes.append(f"{table}.{column} 清理悬空引用 {removed} 行（目标 {target}）")
    return notes


def scan_forbidden(table: str, rows: list[dict], forbidden: list[str]) -> list[str]:
    """Guard against environment data leaking back into a release baseline."""
    blob = json.dumps(rows, ensure_ascii=False)
    return [token for token in forbidden if token in blob]


def fetch_rows(cur, table: str, rule: dict) -> list[dict]:
    """Read seed rows.  ``soft_delete`` filters on ``deleted_at`` when present.

    Not every seed table has a ``deleted_at`` column, so the filter is applied
    conditionally rather than assuming the column exists.
    """
    columns = [c["name"] for c in fetch_columns(cur, table)]
    sql = "SELECT " + ", ".join(f"`{c}`" for c in columns) + f" FROM `{table}`"
    if rule.get("soft_delete") and "deleted_at" in columns:
        sql += " WHERE `deleted_at` IS NULL"
    sql += " ORDER BY " + ", ".join(f"`{c}`" for c in columns[:1])
    cur.execute(sql)
    return [dict(zip(columns, (encode_value(v) for v in row))) for row in cur.fetchall()]


# --------------------------------------------------------------------------
# IR assembly
# --------------------------------------------------------------------------

def schema_fingerprint(payload: dict) -> str:
    """Stable hash over the schema-bearing part of the IR.

    Deliberately excludes anything time-based: the IR has to be reproducible so
    that ``--check`` can detect real drift instead of a new capture timestamp.
    """
    canonical = json.dumps(payload, ensure_ascii=False, sort_keys=True,
                           separators=(",", ":"))
    return hashlib.sha256(canonical.encode("utf-8")).hexdigest()


def build_ir(cur, policy: dict, warnings: list) -> dict:
    excluded = policy["excluded_tables"]
    seed_tables = policy["seed_tables"]
    forced_enums = set(policy.get("tinyint1_enum_columns", []))
    forced_integers = set(policy.get("not_boolean_columns", []))
    drop_columns = policy.get("drop_columns", {})

    live = fetch_tables(cur)
    included = [t for t in live if t not in excluded]
    table_comments = fetch_table_comments(cur)

    cur.execute("SELECT VERSION()")
    server_version = cur.fetchone()[0]

    tables = []
    for name in included:
        columns = fetch_columns(cur, name)
        spec = drop_columns.get(name)
        if spec:
            dropped = set(spec["columns"])
            present = [c["name"] for c in columns if c["name"] in dropped]
            missing = sorted(dropped - set(present))
            if missing:
                warnings.append(
                    f"{name}: policy 要求剥掉 {missing}，但库里已经没有这些列，"
                    f"请同步清理 policy.json 的 drop_columns"
                )
            columns = [c for c in columns if c["name"] not in dropped]
        resolve_tinyint1(cur, name, columns, forced_enums, forced_integers, warnings)
        indexes = fetch_indexes(cur, name)
        if spec:
            kept = {c["name"] for c in columns}
            dangling = sorted(
                {c["name"] for i in indexes for c in i["columns"] if c["name"] not in kept}
            )
            if dangling:
                warnings.append(
                    f"{name}: drop_columns 剥掉的列仍被索引引用 {dangling}，"
                    f"索引会把已删的列建回来 —— 请一并处理"
                )
            if name in seed_tables:
                warnings.append(
                    f"{name}: 同时出现在 drop_columns 与 seed_tables，"
                    f"种子行会带着已剥掉的列，需先确认取数口径"
                )
        primary = [i for i in indexes if i["name"] == "PRIMARY"]
        uniques = [i for i in indexes if i["unique"] and i["name"] != "PRIMARY"]
        plain = [i for i in indexes if not i["unique"] and i["name"] != "PRIMARY"]
        tables.append(
            {
                "name": name,
                "comment": table_comments.get(name, ""),
                "columns": columns,
                "primary_key": primary[0]["columns"] if primary else [],
                "uniques": [
                    {
                        "name": u["name"],
                        "columns": [
                            {"name": c["name"], "prefix": c["prefix"]} for c in u["columns"]
                        ],
                    }
                    for u in uniques
                ],
                "indexes": [
                    {
                        "name": i["name"],
                        "columns": [
                            {"name": c["name"], "prefix": c["prefix"]} for c in i["columns"]
                        ],
                    }
                    for i in plain
                ],
                "foreign_keys": fetch_foreign_keys(cur, name),
                "checks": fetch_check_constraints(cur, name),
                "seeded": name in seed_tables,
            }
        )

    seeds = {k: v for k, v in seed_tables.items() if not k.startswith("_")}
    return {
        "_comment": (
            "生成物，请勿手工编辑。由 capture_schema.py 从开发库抽取，"
            "由 generate_sql.py 消费。改结构请先在库里改，再重新捕获。"
        ),
        "meta": {
            "source_database": "uvp_gb28181",
            "source_server_version": server_version,
            "live_table_count": len(live),
            "included_table_count": len(included),
            "excluded_table_count": len(excluded),
            "schema_fingerprint": schema_fingerprint({"tables": tables, "seed_tables": seeds}),
            "policy_rev": policy.get("_rev", 1),
        },
        "excluded_tables": excluded,
        "seed_tables": seeds,
        "tables": tables,
    }


def render_ir(ir: dict) -> str:
    return json.dumps(ir, ensure_ascii=False, indent=2, sort_keys=False) + "\n"


def render_seed(rows: list[dict]) -> str:
    return "".join(
        json.dumps(row, ensure_ascii=False, sort_keys=False) + "\n" for row in rows
    )


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--check", action="store_true",
                        help="report drift instead of writing")
    parser.add_argument("--config", help="path to server/config/config.yml")
    args = parser.parse_args()

    policy = json.loads(POLICY_FILE.read_text(encoding="utf-8"))
    conn = connect(resolve_config(args.config))
    warnings: list[str] = []
    try:
        cur = conn.cursor()
        ir = build_ir(cur, policy, warnings)
        filters = policy.get("seed_filters", {})
        forbidden = policy.get("forbidden_literals", [])
        seeds = {}
        violations = []
        for name, rule in policy["seed_tables"].items():
            if name.startswith("_"):
                continue
            rows = fetch_rows(cur, name, rule)
            rows = apply_seed_filters(name, rows, filters)
            seeds[name] = rows
        cleanup_notes = apply_referential_cleanup(seeds, policy.get("referential_cleanup", []))
        violations = []
        for name, rows in seeds.items():
            hits = scan_forbidden(name, rows, forbidden)
            if hits:
                violations.append(f"{name}: 命中禁词 {', '.join(hits)}")
    finally:
        conn.close()

    if violations:
        print("⛔ 种子数据里出现禁止的环境数据，请在 policy.json 的 seed_filters 里补充清洗规则:")
        for line in violations:
            print("   ", line)
        return 1

    ir_text = render_ir(ir)
    seed_texts = {name: render_seed(rows) for name, rows in seeds.items()}

    drift = []
    if not args.check:
        IR_FILE.write_text(ir_text, encoding="utf-8")
        if not SEEDS_DIR.is_dir():
            SEEDS_DIR.mkdir(parents=True, exist_ok=True)
        for path in SEEDS_DIR.glob("*.jsonl"):
            if path.stem not in seed_texts:
                path.unlink()
                drift.append(f"removed stale seed file {path.name}")
        for name, text in seed_texts.items():
            (SEEDS_DIR / f"{name}.jsonl").write_text(text, encoding="utf-8")
    else:
        if not IR_FILE.exists() or IR_FILE.read_text(encoding="utf-8") != ir_text:
            drift.append("schema.ir.json differs from the database")
        for name, text in seed_texts.items():
            path = SEEDS_DIR / f"{name}.jsonl"
            if not path.exists() or path.read_text(encoding="utf-8") != text:
                drift.append(f"seeds/{name}.jsonl differs from the database")

    total_rows = sum(len(r) for r in seeds.values())
    meta = ir["meta"]
    print(f"实库 {meta['live_table_count']} 表 → 收录 {meta['included_table_count']} "
          f"/ 排除 {meta['excluded_table_count']}")
    for name, rows in sorted(seeds.items(), key=lambda kv: -len(kv[1])):
        print(f"   种子 {name:26s} {len(rows):6d} 行")
    print(f"   种子合计 {total_rows} 行")

    if args.check:
        if drift:
            print("\n⛔ 与实库不一致:")
            for line in drift:
                print("   ", line)
            return 1
        print("\n✅ 与实库一致")
        return 0

    for line in cleanup_notes:
        print("   " + line)
    for line in drift:
        print("   ", line)
    print(f"\n已写出 {IR_FILE.relative_to(REPO_ROOT)} 与 {len(seed_texts)} 个种子文件")
    return 0


if __name__ == "__main__":
    sys.exit(main())
