#!/usr/bin/env python3
"""UVP-GB28181 发布基线的导入与跨方言对账工具。

为什么单独有这个脚本：
  1. 基线脚本必须能在**干净的空库**里从零建起来 —— 开发库看着对，不代表新装环境对。
  2. 四种方言的产物必须表达**同一套** schema 与种子 —— 谁少了表、少了索引、少了行，
     只有把两边都真导进去、按语义（不是按字面）比，才看得出来。
  3. 国产库接入时，"新增一个 profile + 跑一次这里"就是验收路径。

子命令
------
apply     把一份生成的发布脚本导入目标库（空库会先清掉同名表）。
reconcile 把两个已导入的库按语义对比：表集合 / 列 / 索引(列组+唯一性) / 种子行数。

用法
----
  verify_baseline.py apply     --dialect mysql --dsn mysql://root:pw@host:3306/db --script uvp-gb28181.sql
  verify_baseline.py apply     --dialect sqlite --dsn sqlite:///tmp/check.db --script sqlitebaseline/baseline.sql
  verify_baseline.py reconcile --left mysql://root:pw@host:3306/db --right sqlite:///tmp/check.db

DSN 形态：mysql:// / postgresql:// / sqlite:///<绝对路径>
密码里的 @ : / 需要用 urllib.parse.quote 转义。

⚠️ 交付集是 MySQL / PostgreSQL / SQL Server 三份产物，但本工具**只有 mysql 与
postgresql 两条实机路径**：SQL Server 从 2026-09-15 起就没有可用实例（1433 全不通），
加一条跑不到的 `sqlserver` 分支等于把未验证的代码放进验收链路，
反而会给人"SQL Server 验过了"的错觉。SQL Server 侧的最低保障是
`initialization_contract_test.go` 的列级跨方言一致断言 + IDENTITY 区块配对断言。
真要补实机：加一个 meta_sqlserver / connect 分支，并**先**确认有可连的实例。
"""
from __future__ import annotations

import argparse
import re
import sqlite3
import sys
from collections import defaultdict
from urllib.parse import unquote, urlparse

# --------------------------------------------------------------------------
# SQL 语句切分
# --------------------------------------------------------------------------

def split_statements(sql: str, dialect: str = "mysql") -> list[str]:
    """按方言正确切分语句。

    必须处理：`--` 行注释、MySQL 的 `#` 行注释、`/* */` 块注释、
    单/双/反引号字符串（含 '' 与 \\' 两种转义）、PG 的 $tag$ 美元引用。

    反面教材（本项目踩过）：先按 ';\\n' 切，再用 chunk.startswith('--') 过滤。
    一旦某条真语句紧跟在注释行后面，它和注释落在同一个 chunk 里，会被整块丢掉，
    而且**不报错** —— 表现成"库里少了几行"，极难定位。
    """
    out: list[str] = []
    buf: list[str] = []
    i, n = 0, len(sql)

    while i < n:
        ch = sql[i]

        # -- 行注释（MySQL 要求 -- 后面跟空白，否则是运算符）
        if sql.startswith("--", i) and (
            dialect != "mysql" or i + 2 >= n or sql[i + 2] in " \t\r\n"
        ):
            j = sql.find("\n", i)
            i = n if j < 0 else j
            continue

        # MySQL 的 # 行注释
        if ch == "#" and dialect == "mysql":
            j = sql.find("\n", i)
            i = n if j < 0 else j
            continue

        # /* */ 块注释
        if sql.startswith("/*", i):
            j = sql.find("*/", i + 2)
            i = n if j < 0 else j + 2
            continue

        # 引号字符串 / 引号标识符
        if ch in ("'", '"', "`", "["):
            close = "]" if ch == "[" else ch
            buf.append(ch)
            i += 1
            while i < n:
                c2 = sql[i]
                if c2 == "\\" and dialect == "mysql":
                    buf.append(c2)
                    i += 1
                    if i < n:
                        buf.append(sql[i])
                        i += 1
                    continue
                if c2 == close:
                    if i + 1 < n and sql[i + 1] == close:  # '' 转义
                        buf.append(c2)
                        buf.append(c2)
                        i += 2
                        continue
                    buf.append(c2)
                    i += 1
                    break
                buf.append(c2)
                i += 1
            continue

        # PG 美元引用 $tag$ ... $tag$
        if ch == "$" and dialect == "postgresql":
            m = re.match(r"\$[A-Za-z_0-9]*\$", sql[i:])
            if m:
                tag = m.group(0)
                j = sql.find(tag, i + len(tag))
                end = n if j < 0 else j + len(tag)
                buf.append(sql[i:end])
                i = end
                continue

        if ch == ";":
            stmt = "".join(buf).strip()
            if stmt:
                out.append(stmt)
            buf = []
            i += 1
            continue

        buf.append(ch)
        i += 1

    tail = "".join(buf).strip()
    if tail:
        out.append(tail)
    return out


# --------------------------------------------------------------------------
# 连接
# --------------------------------------------------------------------------

def connect(dsn: str):
    """返回 (conn, dialect, cursor_factory_info)。"""
    u = urlparse(dsn)
    scheme = u.scheme
    if scheme == "sqlite":
        path = u.path or dsn.split("sqlite://", 1)[1]
        return sqlite3.connect(path), "sqlite"

    host = u.hostname or "127.0.0.1"
    port = u.port
    user = unquote(u.username or "")
    password = unquote(u.password or "")
    db = (u.path or "").lstrip("/")

    if scheme == "mysql":
        import pymysql
        return pymysql.connect(host=host, port=port or 3306, user=user, password=password,
                               database=db, charset="utf8mb4"), "mysql"
    if scheme in ("postgresql", "postgres"):
        import psycopg2
        return psycopg2.connect(host=host, port=port or 5432, user=user,
                                password=password, dbname=db), "postgresql"
    raise SystemExit(f"不支持的 DSN 协议: {scheme}")


def execute_all(cur, dialect: str, stmt: str):
    """SQL Server 不接受 IF OBJECT_ID(...) DROP 之外的批；这里逐条执行即可。"""
    cur.execute(stmt)


# --------------------------------------------------------------------------
# apply
# --------------------------------------------------------------------------

def cmd_apply(args) -> int:
    sql = open(args.script, encoding="utf-8").read()
    statements = split_statements(sql, args.dialect)
    conn, dialect = connect(args.dsn)
    cur = conn.cursor()

    if dialect == "mysql":
        cur.execute("SET FOREIGN_KEY_CHECKS = 0")
        cur.execute("SET UNIQUE_CHECKS = 0")
    elif dialect == "postgresql":
        # 必须 autocommit：否则一条语句失败就把整个事务打成 aborted，
        # 后面每一条都报 "current transaction is aborted"，
        # 真正的错误被埋在几百行级联噪声里（踩过）。
        conn.autocommit = True
        cur.execute("SET session_replication_role = replica")
    elif dialect == "sqlite":
        conn.isolation_level = None
        cur.execute("PRAGMA foreign_keys = OFF")

    ok = err = 0
    for idx, stmt in enumerate(statements, 1):
        try:
            execute_all(cur, dialect, stmt)
            ok += 1
        except Exception as exc:  # noqa: BLE001
            err += 1
            if err <= args.max_errors:
                print(f"  ✗ [{idx}/{len(statements)}] {str(exc)[:200]}")
                print(f"      {stmt[:110].replace(chr(10), ' ')}")
            if dialect != "postgresql":
                conn.rollback()

    conn.commit()

    if dialect == "mysql":
        cur.execute("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE()")
    elif dialect == "postgresql":
        cur.execute("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'public'")
    elif dialect == "sqlite":
        cur.execute("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'")
    else:
        cur.execute("SELECT COUNT(*) FROM information_schema.tables WHERE table_type='BASE TABLE'")
    tables = cur.fetchone()[0]

    print(f"{args.script}")
    print(f"  方言={dialect}  语句={len(statements)}  成功={ok}  失败={err}  表数={tables}")
    conn.close()
    return 1 if err else 0


# --------------------------------------------------------------------------
# 语义元数据
# --------------------------------------------------------------------------

def _split_top_level(s: str) -> list[str]:
    out, depth, buf, quote = [], 0, [], None
    for ch in s:
        if quote:
            buf.append(ch)
            if ch == quote:
                quote = None
            continue
        if ch in ("'", '"', "`"):
            quote = ch
            buf.append(ch)
        elif ch == "(":
            depth += 1
            buf.append(ch)
        elif ch == ")":
            depth -= 1
            buf.append(ch)
        elif ch == "," and depth == 0:
            out.append("".join(buf))
            buf = []
        else:
            buf.append(ch)
    if buf:
        out.append("".join(buf))
    return [x.strip() for x in out if x.strip()]


def _paren_group(s: str) -> str | None:
    i = s.find("(")
    if i < 0:
        return None
    depth, j = 0, i
    while j < len(s):
        if s[j] == "(":
            depth += 1
        elif s[j] == ")":
            depth -= 1
            if depth == 0:
                return s[i + 1:j]
        j += 1
    return None


def norm_col(raw: str) -> tuple[str, int | None]:
    """把各种方言的索引列写法归一到 (列名, 前缀长度|None)。

    MySQL  sub_part          -> 由 information_schema.statistics 单独给出
    PG     left("path", 191) -> ('path', 191)
    SQLite substr("path",1,191) -> ('path', 191)
    SQL    Server 不支持前缀索引 -> None（对账时降级为只比列名）

    ⚠️ 取元数据时要按方言的"回显原文"来写，别按自己生成的写法想当然：
    PostgreSQL 的 pg_get_indexdef 对表达式索引会**把函数名一起加引号**、并补上
    造型，回显成 `"left"((path)::text, 128)`，而不是我们写进去的 `left("path", 128)`。
    不做这一步归一，会凭空报出"两边索引不一致"的假差异。
    """
    c = raw.strip()
    c = re.sub(r"\s+(ASC|DESC)\s*$", "", c, flags=re.I)
    c = re.sub(r"\s+NULLS\s+(FIRST|LAST)\s*$", "", c, flags=re.I)
    c = re.sub(r"\s+COLLATE\s+\S+$", "", c, flags=re.I)

    m = re.match(r'^"?(?:left|substr|substring)"?\s*\(\s*(.+?)\s*(?:,\s*1)?\s*,\s*(\d+)\s*\)$',
                 c, re.I)
    if m:
        inner = m.group(1).strip()
        inner = re.sub(r"::[\w\s]+$", "", inner).strip()   # 去掉 ::text 造型
        if inner.startswith("(") and inner.endswith(")"):
            inner = inner[1:-1].strip()                    # 去掉 (path) 包裹
        return (inner.strip(' "`[]'), int(m.group(2)))
    return (c.strip(' "`[]'), None)


def sig_equal(a, b, strict: bool) -> bool:
    """索引签名比较；strict=False 时允许一侧前缀长度为 None（该方言表达不了）。"""
    if a == b:
        return True
    if strict:
        return False
    if len(a) != len(b):
        return False
    for (ca, pa), (cb, pb) in zip(a, b):
        if ca != cb:
            return False
        if pa is not None and pb is not None and pa != pb:
            return False
    return True


def index_sets_equal(a: set, b: set, strict: bool) -> bool:
    if a == b:
        return True
    if strict:
        return False
    # 允许一边多出/少掉前缀长度：把双方都投影成"只比列名+唯一性"再比
    pa = {(tuple(c for c, _ in cols), u) for cols, u in a}
    pb = {(tuple(c for c, _ in cols), u) for cols, u in b}
    return pa == pb


def meta_mysql(dsn: str):
    conn, _ = connect(dsn)
    cur = conn.cursor()
    db = urlparse(dsn).path.lstrip("/")
    cur.execute("SELECT table_name, column_name FROM information_schema.columns "
                "WHERE table_schema=%s ORDER BY table_name, ordinal_position", (db,))
    cols = defaultdict(list)
    for t, c in cur.fetchall():
        cols[t].append(c)
    cur.execute("SELECT table_name, index_name, non_unique, seq_in_index, column_name, sub_part "
                "FROM information_schema.statistics WHERE table_schema=%s "
                "ORDER BY table_name, index_name, seq_in_index", (db,))
    raw = defaultdict(lambda: defaultdict(list))
    uniq = {}
    for t, idx, nonuniq, seq, col, sub in cur.fetchall():
        raw[t][idx].append((seq, col, sub))
        uniq[(t, idx)] = (nonuniq == 0)          # NON_UNIQUE=0 才是唯一
    pk = {}
    idxs = defaultdict(set)
    for t, per in raw.items():
        for idx, members in per.items():
            members.sort()
            colsig = tuple((c, sub) for _, c, sub in members)
            if idx == "PRIMARY":
                pk[t] = colsig
            else:
                idxs[t].add((colsig, uniq[(t, idx)]))
    counts = _count_rows(cur, list(cols), "mysql")
    conn.close()
    return cols, idxs, pk, counts


def meta_postgresql(dsn: str):
    conn, _ = connect(dsn)
    cur = conn.cursor()
    cur.execute("""SELECT table_name, column_name FROM information_schema.columns
                   WHERE table_schema='public' ORDER BY table_name, ordinal_position""")
    cols = defaultdict(list)
    for t, c in cur.fetchall():
        cols[t].append(c)
    cur.execute("""SELECT t.relname, i.relname, ix.indisunique, ix.indisprimary,
                          pg_get_indexdef(ix.indexrelid)
                   FROM pg_index ix
                   JOIN pg_class i ON i.oid = ix.indexrelid
                   JOIN pg_class t ON t.oid = ix.indrelid
                   JOIN pg_namespace n ON n.oid = t.relnamespace
                   WHERE n.nspname='public'""")
    pk = {}
    idxs = defaultdict(set)
    for table, name, is_unique, is_primary, ddl in cur.fetchall():
        inner = _paren_group(ddl)
        if inner is None:
            continue
        colsig = tuple(norm_col(p) for p in _split_top_level(inner))
        if is_primary:
            pk[table] = colsig
        else:
            idxs[table].add((colsig, bool(is_unique)))
    counts = _count_rows(cur, list(cols), "postgresql")
    conn.close()
    return cols, idxs, pk, counts


def meta_sqlite(dsn: str):
    path = dsn.split("sqlite://", 1)[1]
    conn = sqlite3.connect(path)
    cur = conn.cursor()
    cur.execute("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'")
    tables = [r[0] for r in cur.fetchall()]
    cols, pk, idxs = {}, {}, defaultdict(set)
    for t in tables:
        cur.execute(f'PRAGMA table_info("{t}")')
        cols[t] = [r[1] for r in cur.fetchall()]
        cur.execute(f'PRAGMA index_list("{t}")')
        for row in cur.fetchall():
            # (seq, name, unique, origin, partial)
            name, is_unique, origin = row[1], row[2], row[3]
            cur.execute("SELECT sql FROM sqlite_master WHERE type='index' AND name=?", (name,))
            got = cur.fetchone()
            colsig = None
            if got and got[0]:
                inner = _paren_group(got[0])
                if inner:
                    colsig = tuple(norm_col(p) for p in _split_top_level(inner))
            if colsig is None:                       # 表级 UNIQUE 约束没有独立 sql
                cur.execute(f'PRAGMA index_info("{name}")')
                mp = sorted((r[1], r[2]) for r in cur.fetchall())
                colsig = tuple((c, None) for _, c in mp if c)
            if origin == "pk":
                pk[t] = colsig
            else:
                idxs[t].add((colsig, is_unique == 1))
    counts = _count_rows(cur, tables, "sqlite", conn=conn)
    conn.close()
    return cols, idxs, pk, counts


def _count_rows(cur, tables, dialect, conn=None) -> dict:
    q = {"mysql": lambda t: f"`{t}`", "postgresql": lambda t: f'"{t}"',
         "sqlite": lambda t: f'"{t}"'}[dialect]
    out = {}
    for t in tables:
        try:
            cur.execute(f"SELECT COUNT(*) FROM {q(t)}")
            out[t] = cur.fetchone()[0]
        except Exception:  # noqa: BLE001
            if conn:
                conn.rollback()
            out[t] = -1
    return out


META = {"mysql": meta_mysql, "postgresql": meta_postgresql, "sqlite": meta_sqlite}


def cmd_reconcile(args) -> int:
    ld = urlparse(args.left).scheme
    rd = urlparse(args.right).scheme
    ld = "postgresql" if ld == "postgres" else ld
    rd = "postgresql" if rd == "postgres" else rd
    lcols, lidx, lpk, lrows = META[ld](args.left)
    rcols, ridx, rpk, rrows = META[rd](args.right)
    strict = (args.prefix_mode == "strict")

    print(f"对照：左={ld}  右={rd}  前缀比对={'严格' if strict else '宽松'}\n")

    lset, rset = set(lcols), set(rcols)
    print(f"表：{ld}={len(lcols)}  {rd}={len(rcols)}")
    if lset - rset:
        print(f"  仅 {ld} 有: {sorted(lset - rset)}")
    if rset - lset:
        print(f"  仅 {rd} 有: {sorted(rset - lset)}")

    shared = sorted(lset & rset)
    colbad = [(t, [c for c in lcols[t] if c not in rcols[t]],
                  [c for c in rcols[t] if c not in lcols[t]])
              for t in shared if lcols[t] != rcols[t]]
    print(f"列：{len(shared) - len(colbad)}/{len(shared)} 一致")
    for t, a, b in colbad[:20]:
        print(f"  ✗ {t}: 仅{ld}={a} 仅{rd}={b}")

    ishared = sorted(set(lidx) & set(ridx))
    idxbad = [t for t in ishared if not index_sets_equal(lidx[t], ridx[t], strict)]
    print(f"索引（非主键，含唯一性）：{len(ishared) - len(idxbad)}/{len(ishared)} 一致")
    for t in idxbad[:25]:
        print(f"  ✗ {t}")
        print(f"      仅{ld}  : {sorted(lidx[t] - ridx[t])}")
        print(f"      仅{rd}: {sorted(ridx[t] - lidx[t])}")

    pshared = sorted(set(lpk) & set(rpk))
    pkbad = [t for t in pshared if not sig_equal(lpk[t], rpk[t], strict)]
    print(f"主键：{len(pshared) - len(pkbad)}/{len(pshared)} 一致")
    for t in pkbad[:20]:
        print(f"  ✗ {t}: {ld}={lpk[t]} {rd}={rpk[t]}")

    rowbad = [(t, lrows[t], rrows[t]) for t in shared if lrows.get(t) != rrows.get(t)]
    print(f"种子行数：{len(shared) - len(rowbad)}/{len(shared)} 一致")
    for t, a, b in rowbad[:20]:
        print(f"  ✗ {t}: {ld}={a} {rd}={b}")

    fail = bool(lset - rset or rset - lset or colbad or idxbad or pkbad or rowbad)
    print("\n结论:", "✗ 存在差异" if fail else "✓ 全部一致")
    return 1 if fail else 0


# --------------------------------------------------------------------------

def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = ap.add_subparsers(dest="cmd", required=True)

    a = sub.add_parser("apply", help="把发布脚本导入目标库")
    a.add_argument("--dialect", required=True,
                   choices=["mysql", "postgresql", "sqlite"])
    a.add_argument("--dsn", required=True)
    a.add_argument("--script", required=True)
    a.add_argument("--max-errors", type=int, default=10)
    a.set_defaults(func=cmd_apply)

    r = sub.add_parser("reconcile", help="两个已导入的库按语义对比")
    r.add_argument("--left", required=True)
    r.add_argument("--right", required=True)
    r.add_argument("--prefix-mode", choices=["strict", "loose"], default="loose",
                   help="loose=允许 SQL Server 这种不支持前缀索引的方言降级比列名")
    r.set_defaults(func=cmd_reconcile)

    args = ap.parse_args()
    return args.func(args)


if __name__ == "__main__":
    sys.exit(main())
