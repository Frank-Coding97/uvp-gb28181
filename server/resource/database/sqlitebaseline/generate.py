#!/usr/bin/env python3
"""Build the SQLite baseline for the UVP standalone green package.

为什么需要它
------------
绿色安装包的目标机不装 MySQL，只带一个 SQLite 文件。而仓库里唯一的全量建库
脚本是 MySQL 方言（``uvp-gb28181.sql``，由 ``capture_schema.py`` 从活库派生）。
本脚本把那份脚本转成 SQLite 方言，输出 ``baseline.sql`` + ``manifest.json``。

转换 vs 手写
------------
MySQL 基线有 111 张表 / 5409 行种子、444 处 COLLATE、92 处 AUTO_INCREMENT。
手写 SQLite 版必然漏项、且两边会随时间漂移 —— 所以从源脚本**自动转换**。

三条硬规则
----------
1. ⛔ **不丢自增**：单列整数主键 → ``INTEGER PRIMARY KEY AUTOINCREMENT``。
2. ⛔ **不丢引用完整性**：外键原样搬运。且必须知道 SQLite 的
   ``foreign_keys`` PRAGMA **默认为 OFF** —— DDL 头部显式打开，
   应用侧 DSN 也要带（见 gormhelper/sqlite.go 的 sqlitePragmas）。
3. ⛔ **能补的语义用CHECK 补**：MySQL 的 unsigned / varchar(n) / tinyint(1) / json
   在 SQLite 里没有对应类型，用 CHECK 回补。补不了的（如复合主键 + 自增）
   **在注释里写明**，不假装能做到。
"""

from __future__ import annotations

import hashlib
import json
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parent
SOURCE = ROOT.parent / "uvp-gb28181.sql"
POLICY = ROOT.parent / "baseline" / "policy.json"
VERSION = "sqlite-baseline-20261008-r1"


# --------------------------------------------------------------------------
# 语句切分
# --------------------------------------------------------------------------

def split_sql(text: str) -> list[str]:
    """按分号切分语句，跳过字符串字面量与注释里的分号。

    ⛔ 直接 ``text.split(';')`` 会在种子数据上炸掉：菜单标题、URL 里都可能带分号
    （带查询串的回调地址就有）。切出来的片段既不完整也不报错，
    表现为「转换结果少了几条 INSERT」—— 极难定位。
    """
    statements: list[str] = []
    buffer: list[str] = []
    quote: str | None = None
    i, n = 0, len(text)

    while i < n:
        ch = text[i]

        if quote is None and ch == "-" and text.startswith("--", i):
            nl = text.find("\n", i)
            i = n if nl < 0 else nl + 1
            continue
        if quote is None and ch == "/" and text.startswith("/*", i):
            end = text.find("*/", i)
            i = n if end < 0 else end + 2
            continue

        if quote is not None:
            if ch == "\\" and quote == "'":
                buffer.append(ch); i += 1
                if i < n:
                    buffer.append(text[i]); i += 1
                continue
            if ch == quote:
                if i + 1 < n and text[i + 1] == quote:   # '' 转义
                    buffer.append(ch); buffer.append(text[i + 1]); i += 2
                    continue
                quote = None
            buffer.append(ch); i += 1
            continue

        if ch in "'\"":
            quote = ch; buffer.append(ch); i += 1; continue
        if ch == ";":
            statements.append("".join(buffer)); buffer = []; i += 1; continue
        buffer.append(ch); i += 1

    tail = "".join(buffer).strip()
    if tail:
        statements.append(tail)
    return [s.strip() for s in statements if s.strip()]


def split_top_level(text: str) -> list[str]:
    """按顶层逗号切分，跳过括号内与引号内的逗号。"""
    parts: list[str] = []
    buffer: list[str] = []
    depth = 0
    quote: str | None = None
    i, n = 0, len(text)

    while i < n:
        ch = text[i]
        if quote is not None:
            if ch == "\\" and quote == "'":
                buffer.append(ch); i += 1
                if i < n:
                    buffer.append(text[i]); i += 1
                continue
            if ch == quote:
                if i + 1 < n and text[i + 1] == quote:
                    buffer.append(ch); buffer.append(text[i + 1]); i += 2; continue
                quote = None
            buffer.append(ch); i += 1; continue
        if ch in "'\"":
            quote = ch; buffer.append(ch); i += 1; continue
        if ch == "(":
            depth += 1
        elif ch == ")":
            depth -= 1
        if ch == "," and depth == 0:
            parts.append("".join(buffer)); buffer = []; i += 1; continue
        buffer.append(ch); i += 1

    if buffer:
        parts.append("".join(buffer))
    return [p.strip() for p in parts if p.strip()]


def strip_leading_comments(statement: str) -> str:
    out = statement.strip()
    while out.startswith("--") or out.startswith("/*"):
        if out.startswith("--"):
            nl = out.find("\n")
            if nl < 0:
                return ""
            out = out[nl + 1:].strip()
        else:
            end = out.find("*/")
            if end < 0:
                return ""
            out = out[end + 2:].strip()
    return out


# --------------------------------------------------------------------------
# 类型与约束
# --------------------------------------------------------------------------

def enum_columns() -> set[str]:
    """读取 policy.json 里声明的「tinyint(1) 但实际是枚举」的列。

    ⛔ MySQL 的 tinyint(1) 有歧义：GORM 用它表示 bool，但也常被当成小枚举写。
    本仓 `sys_menu.type` 就是后者 —— 值域是 1/2/3（目录/菜单/按钮），不是 0/1。
    ⛔ 若不读这份声明就按 bool 生成 CHECK，建库时会直接被
    `CHECK constraint failed: type` 拦下，而且报错完全指不到"值域判错"这件事。
    ⭐口径真源在 policy.json 的 tinyint1_enum_columns，与 MySQL 基线生成器共用同一份。
    """
    if not POLICY.exists():
        return set()
    return set(json.loads(POLICY.read_text(encoding="utf-8")).get("tinyint1_enum_columns", []))


ENUM_COLUMNS = enum_columns()


def q(name: str) -> str:
    """标识符加双引号（SQLite 里双引号是标识符引用，单引号是字符串）。"""
    return '"' + name.replace('"', '""') + '"'


def sql_str(text: str) -> str:
    return "'" + text.replace("'", "''") + "'"


def map_type(raw: str) -> str:
    """MySQL 列类型 → SQLite 声明类型。

    SQLite 用动态类型，声明类型更多是文档性质；真正干活的是 CHECK。
    所以这里只保证"看起来对"，语义靠 column_checks 补。
    """
    low = raw.strip().lower()
    base = re.sub(r"\(.*\)", "", low).replace("unsigned", "").strip()
    if base in {"tinyint", "smallint", "mediumint", "int", "bigint", "integer"}:
        return "INTEGER"
    if base in {"bool", "boolean"}:
        return "INTEGER"
    if base in {"decimal", "numeric"}:
        return "NUMERIC"
    if base in {"float", "double", "real"}:
        return "REAL"
    if base in {"date", "datetime", "timestamp"}:
        return "DATETIME"
    if base == "time":
        return "TIME"
    # char/varchar/text/*blob/*text/enum/set/json 都落到 TEXT
    return "TEXT"


def column_checks(name: str, raw_type: str, table: str = "") -> list[str]:
    """把 MySQL 的类型约束翻译成 SQLite CHECK。

    ⛔ 这些 CHECK 不是装饰：不加的话，MySQL 里放不下的值在 SQLite 里能写进去，
    等客户切回 MySQL 时才炸 —— 那时候数据已经脏了。
    """
    checks: list[str] = []
    ref = q(name)
    low = raw_type.strip().lower()
    base = re.sub(r"\(.*\)", "", low).replace("unsigned", "").strip()
    numeric = base in {"tinyint", "smallint", "mediumint", "int", "bigint", "integer",
                       "bool", "boolean"}

    if numeric:
        if "unsigned" in low:
            checks.append(f'CHECK ({ref} IS NULL OR (typeof({ref}) = \'integer\' AND {ref} >= 0))')
        else:
            checks.append(f"CHECK ({ref} IS NULL OR typeof({ref}) = 'integer')")

    if re.fullmatch(r"tinyint\s*\(\s*1\s*\)", low):
        if f"{table}.{name}" in ENUM_COLUMNS:
            # policy.json 声明过：这列是**枚举**不是 bool。
            # ⛔ 不能套 0/1 约束 —— 值域不同会把种子数据全拦下。
            # 这里只约束「是整数」，具体值域交给应用层校验。
            return checks
        # GORM 用 tinyint(1) 表示 bool；SQLite 没有 bool，靠 CHECK 回语义
        checks.append(f"CHECK ({ref} IS NULL OR {ref} IN (0, 1))")

    length = re.search(r"\((\d+)\)", low)
    if length and base in {"char", "varchar"}:
        checks.append(f'CHECK ({ref} IS NULL OR length({ref}) <= {length.group(1)})')

    if base == "json":
        # ⛔ json_valid 对非法值返回 0 而不报错，所以是 CHECK 而不是 NOT NULL
        checks.append(f"CHECK ({ref} IS NULL OR json_valid({ref}))")

    return checks


# --------------------------------------------------------------------------
# 正则
# --------------------------------------------------------------------------

CREATE_TABLE_RE = re.compile(
    r"^CREATE TABLE\s+`?(?P<name>\w+)`?\s*\((?P<body>.*)\)\s*(?:ENGINE|;|$)",
    re.S | re.I,
)
COLUMN_RE = re.compile(
    r"^`(?P<name>\w+)`\s+(?P<type>[a-zA-Z]+(?:\s*\([^)]*\))?(?:\s+unsigned)?)"
    r"(?P<rest>.*)$",
    re.S | re.I,
)
PRIMARY_RE = re.compile(r"^PRIMARY KEY \((?P<cols>[^)]*)\)$", re.I)
KEY_RE = re.compile(
    r"^(?:(?P<unique>UNIQUE)\s+)?KEY\s+`(?P<name>\w+)`\s*\((?P<cols>[^)]*)\)$", re.I
)
FK_RE = re.compile(
    r"^(?:CONSTRAINT\s+`?(?P<fk>\w+)`?\s+)?FOREIGN KEY \((?P<cols>[^)]*)\)\s*(?P<ref>.+)$", re.S | re.I
)
CHECK_RE = re.compile(r"^CHECK\s*\((?P<body>.*)\)$", re.S | re.I)
DEFAULT_RE = re.compile(r"\bDEFAULT\s+(?P<value>'(?:[^']|'')*'|[\w.+-]+)", re.I)
COMMENT_RE = re.compile(r"\bCOMMENT\s+'(?P<text>(?:[^']|'')*)'", re.I)


def column_list(raw: str) -> str:
    return ", ".join(q(c.strip().strip("`")) for c in raw.split(","))


# --------------------------------------------------------------------------
# 表转换
# --------------------------------------------------------------------------

def convert_table(stmt: str, inline_fks: list[str] | None = None) -> tuple[str, list[str], dict]:
    """转换一张表，返回 (CREATE TABLE 语句, 表外语句, 统计)。

    ⛔ **索引与外键必须放在 CREATE TABLE 之外**：
    SQLite 的表体内**不允许**出现 `CREATE INDEX`，把它塞进括号里会报
    `near "<col>": syntax error`，而且报错位置指向列名、看不出真正原因。
    MySQL 那种「索引写在表定义里」的写法必须拆出来。
    """
    m = CREATE_TABLE_RE.match(stmt.strip().rstrip(";"))
    if not m:
        raise ValueError(f"无法解析 CREATE TABLE:\n{stmt[:200]}")
    table, body = m.group("name"), m.group("body")

    lines: list[str] = []
    outside: list[str] = []      # 表外语句：CREATE INDEX / ALTER TABLE ADD FOREIGN KEY
    notes: list[str] = []        # 表外注释：SQLite 不支持列级 COMMENT，转成行注释保留信息
    primary: list[str] = []
    auto_col: str | None = None
    kept_fk = 0

    for item in split_top_level(body):
        if PRIMARY_RE.match(item):
            primary = [c.strip().strip("`") for c in PRIMARY_RE.match(item).group("cols").split(",")]
            continue
        if KEY_RE.match(item) and not COLUMN_RE.match(item):
            km = KEY_RE.match(item)
            kind = "UNIQUE INDEX" if km.group("unique") else "INDEX"
            outside.append(
                f'CREATE {kind} IF NOT EXISTS {q(km.group("name"))} '
                f'ON {q(table)} ({column_list(km.group("cols"))});'
            )
            continue
        if FK_RE.match(item):
            fm = FK_RE.match(item)
            ref = fm.group("ref").strip().rstrip(";").replace("`", '"')
            outside.append(f'ALTER TABLE {q(table)} ADD FOREIGN KEY ({column_list(fm.group("cols"))}) {ref};')
            kept_fk += 1
            continue
        if CHECK_RE.match(item):
            # MySQL 的 CHECK 用反引号与函数，语法与 SQLite 不同；本仓实际没有 CHECK，
            # 真遇到必须人工确认而不是无脑搬运。
            outside.append(f"-- CHECK 原样保留需人工确认: {item[:120]}")
            continue

        cm = COLUMN_RE.match(item)
        if not cm:
            continue

        name, raw_type, rest = cm.group("name"), cm.group("type").strip(), cm.group("rest") or ""
        stype = map_type(raw_type)
        auto = "auto_increment" in rest.lower()
        if auto:
            auto_col = name

        # ⛔ CHECK 必须**内联在列定义后面**，不能作为独立的表级约束。
        # SQLite 要求约束紧跟它所约束的列；写成「列,CHECK,列,CHECK」会报
        # `near "<col>": syntax error`，而且报错指向列名、看不出真正原因（实测踩过）。
        parts = [q(name), stype]
        # 单列整数主键的自增由 PRIMARY KEY 那一行统一表达
        is_single_int_pk = len(primary) == 1 and primary[0] == name and stype == "INTEGER"
        if not (auto and is_single_int_pk):
            parts.append("NOT NULL" if re.search(r"\bNOT NULL\b", rest, re.I) else "NULL")
        for check in column_checks(name, raw_type, table):
            parts.append(check)
        dm = DEFAULT_RE.search(rest)
        if dm:
            parts.append(f"DEFAULT {dm.group('value')}")
        # ⛔ SQLite **不支持列级 COMMENT**（MySQL 专有语法），照搬会报
        # `near "COMMENT": syntax error`。保留列注释的信息价值，改写成 SQL 行注释。
        cmn = COMMENT_RE.search(rest)
        if cmn:
            comment_sql = f"  -- {q(name)}: {cmn.group('text')}"

        lines.append("  " + " ".join(parts))
        if cmn:
            notes.append(comment_sql)

    # 来自 ALTER TABLE 的外键内联进表体（SQLite 不支持 ADD CONSTRAINT）
    for fk in inline_fks or []:
        lines.append(f"  {fk}")

    if len(primary) == 1 and primary[0] == auto_col:
        # 单列整数主键 + 自增 → 真正的 AUTOINCREMENT（SQLite 只有这种能自增）。
        # 注意要把这一列原先的 CHECK 一并搬到新行上，否则约束会随旧行一起被丢掉。
        pk_checks = []
        kept = []
        for line in lines:
            bare = line.strip()
            if bare.startswith(q(primary[0]) + " INTEGER") and "PRIMARY KEY" not in bare:
                m = re.match(rf'^\s*{re.escape(q(primary[0]))} INTEGER.*?(CHECK \(.*\))$', bare)
                if m:
                    pk_checks.append(m.group(1))
                continue
            kept.append(line)
        lines = kept
        head = f"  {q(primary[0])} INTEGER PRIMARY KEY AUTOINCREMENT"
        lines.insert(0, head + ((" " + pk_checks[0]) if pk_checks else ""))
    elif primary:
        lines.append(f"  PRIMARY KEY ({column_list(', '.join(primary))})")
        if auto_col:
            lines.append(
                f"  -- AUTOINCREMENT 已省略：本表是复合/非整数主键"
                f"（{', '.join(primary)}），SQLite 只允许单列 INTEGER 主键自增"
            )

    ddl = f"CREATE TABLE IF NOT EXISTS {q(table)} (\n" + ",\n".join(lines) + "\n);"
    if notes:
        # 列注释紧跟表定义之后 —— ⛔ 绝不能放进表体内（那是语法错误），
        # 也不能混进要执行的语句流（会以 `near "FOREIGN"` 之类的方式炸）。
        ddl += "\n" + "\n".join(notes)
    return ddl, outside, {"table": table, "foreign_keys_kept": kept_fk, "autoincrement": auto_col}

def convert_insert(stmt: str) -> str:
    """INSERT 转换：去反引号 + 把 MySQL 反斜杠转义换成 SQLite 的 char() 拼接。

    ⛔⛔ **MySQL 用反斜杠转义，SQLite 完全不认**：``\\n`` 在 SQLite 里是"两个字符"。
    最容易写错的方向是"把它换成真换行" —— 那会让单行字符串跨行，
    直接把 INSERT 拆成语法错误的片段（实测踩过，报 near "INSERT"）。

    ✅ 正确做法：换成 SQLite 的 ``char(10)`` 拼接，它是**字面量拼接、不产生换行**：
    ``'a\\nb'`` → ``'a' || char(10) || 'b'``
    ⛔ 其余函数/字面量一律不改写：出现 MySQL 专有写法时静默改写比转换失败危险得多 ——
    失败会被人看见，静默改写不会。
    """
    out = stmt.replace("`", '"')

    # MySQL 转义 → SQLite 可用的字面量片段
    escapes = {
        "n": "\n", "t": "\t", "r": "\r",
        "0": "\x00", "b": "\b", "Z": "\x1a",
    }

    def fix_string(text: str) -> str:
        """把一个单引号字符串的内容（不含两端引号）转成 SQLite 表达式片段。"""
        parts: list[str] = []
        buf: list[str] = []
        i, n = 0, len(text)
        while i < n:
            ch = text[i]
            if ch == "\\" and i + 1 < n:
                nxt = text[i + 1]
                if nxt in escapes:
                    if buf:
                        parts.append("'" + "".join(buf).replace("'", "''") + "'")
                        buf = []
                    parts.append(f"char({ord(escapes[nxt])})")
                    i += 2
                    continue
                if nxt in ("'", '"', "\\"):
                    buf.append(nxt)
                    i += 2
                    continue
                buf.append(nxt)          # 未知转义：原样保留，不猜
                i += 2
                continue
            buf.append(ch)
            i += 1
        if buf:
            parts.append("'" + "".join(buf).replace("'", "''") + "'")
        return " || ".join(parts) if parts else "''"

    result: list[str] = []
    i, n = 0, len(out)
    while i < n:
        ch = out[i]
        if ch != "'":
            result.append(ch); i += 1; continue
        j = i + 1
        buf: list[str] = []
        while j < n:
            if out[j] == "'" and j + 1 < n and out[j + 1] == "'":
                buf.append("''"); j += 2; continue
            if out[j] == "'":
                break
            buf.append(out[j]); j += 1
        content = "".join(buf)
        # 只在「内容里真的含反斜杠转义」时才改写，避免无谓地把字符串变成表达式
        if "\\" in content:
            result.append(fix_string(content))
        else:
            result.append("'" + content + "'")
        i = j + 1
    # ⛔ 必须补回分号：split_sql 切分时把分号吃掉了，而下面用 "\n".join(output)
    #   拼装 —— 不补的话所有 INSERT 会连成一条语句，执行时报
    #   `near "INSERT": syntax error`，且报错完全指不到真正原因（实测踩过）。
    text = "".join(result).strip()
    return text if text.endswith(";") else text + ";"


# --------------------------------------------------------------------------
# 主流程
# --------------------------------------------------------------------------

ALTER_FK_RE = re.compile(
    r"^ALTER TABLE\s+`?(?P<table>\w+)`?\s+ADD CONSTRAINT\s+`?(?P<fk>\w+)`?\s+"
    r"(?P<body>FOREIGN KEY\s*\(.*)$",
    re.S | re.I,
)


def parse_alter_foreign_key(stmt: str) -> tuple[str, str] | None:
    """从 `ALTER TABLE t ADD CONSTRAINT ... FOREIGN KEY (...)` 提取 (表名, 表级约束文本)。

    ⛔ MySQL 把外键写成独立的 ALTER 语句，而 **SQLite 没有实现 `ALTER TABLE ADD CONSTRAINT`**
    （实测 → `near "FOREIGN": syntax error`）。所以外键必须**内联进 CREATE TABLE 的表体**。

    ⛔ 只扫 CREATE TABLE 会静默丢掉全部外键（本仓 3 处属这类），
    表现为「引用完整性悄悄没了」，开发库与客户库行为不一致却没人发现。
    """
    m = ALTER_FK_RE.match(stmt.strip().rstrip(";"))
    if not m:
        return None
    # body 形如: FOREIGN KEY (`grant_id`) REFERENCES `gb_openapi_play_grant` (`grant_id`) ON DELETE ...
    # ⛔ 保留反引号原样，只把**列名**单独取引号再统一由 q() 输出。
    #   若先整体 replace(反引号→双引号) 再过 q()，列名会变成 """grant_id"""（双重包裹），
    #   报 unknown column ""grant_id"" in foreign key definition（实测踩过）。
    body = m.group("body").strip().rstrip(";")
    fk_cols, sep, ref_part = body.partition("REFERENCES")
    if not sep:
        return None
    inner = fk_cols[fk_cols.index("(") + 1: fk_cols.rindex(")")]
    columns = ", ".join(q(c.strip().strip("`")) for c in inner.split(",") if c.strip())
    # ref_part 里的引用表与列也要去反引号→ 双引号
    target = ref_part.replace("`", '"')
    return m.group("table"), f"FOREIGN KEY ({columns}) REFERENCES{target}"


def main() -> None:
    if not SOURCE.exists():
        raise SystemExit(f"找不到源基线: {SOURCE}")

    text = SOURCE.read_text(encoding="utf-8")
    creates: dict[str, str] = []
    table_statements: list[str] = []
    stats = {"foreign_keys": 0, "autoincrement": 0, "tables_without_autoincrement": []}
    seeds: list[str] = []
    alters: list[str] = []
    skipped: dict[str, int] = {}

    # 第一遍：先收集外键。⛔ MySQL 把外键放在独立的 ALTER TABLE 语句里，
    # 而 SQLite 不支持 ADD CONSTRAINT，只能内联进表体 —— 所以必须先扫一遍再转表。
    pending_fks: dict[str, list[str]] = {}
    for raw in split_sql(text):
        stmt = strip_leading_comments(raw)
        if stmt and stmt.split(None, 1)[0].upper() == "ALTER":
            parsed = parse_alter_foreign_key(stmt)
            if parsed is not None:
                table, constraint = parsed
                pending_fks.setdefault(table, []).append(constraint)
    stats["foreign_keys"] = sum(len(v) for v in pending_fks.values())

    for raw in split_sql(text):
        stmt = strip_leading_comments(raw)
        if not stmt:
            continue
        head = stmt.split(None, 1)[0].upper()

        if head == "CREATE":
            name_match = CREATE_TABLE_RE.match(stmt.strip().rstrip(";"))
            fks = pending_fks.get(name_match.group("name")) if name_match else None
            ddl, outside, info = convert_table(stmt, fks)
            creates.append(ddl)
            table_statements.extend(outside)
            stats["autoincrement"] += 1 if info["autoincrement"] else 0
            if not info["autoincrement"]:
                stats["tables_without_autoincrement"].append(info["table"])
        elif head == "INSERT":
            seeds.append(convert_insert(stmt))
        elif head == "ALTER":
            # 外键已在上文收集并内联；这里只统计无法识别的 ALTER。
            if parse_alter_foreign_key(stmt) is None:
                skipped[head] = skipped.get(head, 0) + 1
        elif head in {"SET", "USE", "PRAGMA", "LOCK", "UNLOCK", "START", "DROP"}:
            continue
        elif stmt.startswith("/*!"):
            continue          # MySQL 版本条件注释块，SQLite 不认
        else:
            skipped[head] = skipped.get(head, 0) + 1

    output: list[str] = [
        "-- SQLite baseline for the UVP standalone green package.",
        f"-- Generated from {SOURCE.name}; DO NOT EDIT (run generate.py instead).",
        "--",
        "-- Type mapping: auto_increment(single int pk) -> INTEGER PRIMARY KEY AUTOINCREMENT;",
        "--   text/blob/json -> TEXT; datetime/date/timestamp -> DATETIME; decimal -> NUMERIC.",
        "--   unsigned / varchar(n) / tinyint(1) / json semantics restored via CHECK.",
        "-- ⚠️ SQLite has foreign_keys OFF by default. This file turns it ON, and the",
        "--    application's DSN must also carry _pragma=foreign_keys(1) — otherwise",
        "--    every REFERENCES is a no-op and referential integrity silently degrades.",
        "PRAGMA foreign_keys = ON;",
        "PRAGMA busy_timeout = 5000;",
        "",
    ]
    # ⛔⛔ 顺序必须是「全部建表 → 全部索引」。
    #   索引依赖目标表已存在，而表名是排序输出的、索引是按发现顺序输出的 ——
    #   两者顺序不一致就会出现 "no such table: main.xxx"（实测踩过）。
    output.extend(creates)
    output.append("")
    if table_statements:
        output.append("-- 索引与表级约束（SQLite 不允许写在表定义内部；必须在建表之后执行）")
        output.extend(table_statements)
        output.append("")
    output.append("-- Deterministic system seed and permission metadata.")
    output.extend(seeds)
    output.append("")

    baseline = "\n".join(output)
    (ROOT / "baseline.sql").write_text(baseline, encoding="utf-8")
    sha256 = hashlib.sha256(baseline.encode()).hexdigest()

    manifest = {
        "version": VERSION,
        "source": SOURCE.name,
        "source_sha256": hashlib.sha256(SOURCE.read_bytes()).hexdigest(),
        "sha256": sha256,
        "tables": len(creates),
        "seed_statements": len(seeds),
        "foreign_keys_kept": stats["foreign_keys"],
        "autoincrement_tables": stats["autoincrement"],
        "tables_without_autoincrement": sorted(stats["tables_without_autoincrement"]),
        "skipped_statement_heads": skipped,
        "notes": [
            "sys_civil_code 不在本文件：3348 行区划数据由应用侧 civilcode.SeedIfEmpty 在同一事务内灌。",
            "复合主键表保留主键约束、放弃 AUTOINCREMENT（SQLite 只允许单列 INTEGER 主键自增）。",
        ],
    }
    (ROOT / "manifest.json").write_text(
        json.dumps(manifest, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
    )
    print(f"✅ {len(creates)} 表 / {len(seeds)} 条种子 → baseline.sql（{len(baseline)} 字节）")
    print(f"   自增表 {stats['autoincrement']} 张，外键保留 {stats['foreign_keys']} 处")
    if skipped:
        print(f"   ⚠️ 跳过的语句头: {skipped}")
    if stats["tables_without_autoincrement"]:
        print(f"   无自增的表 {len(stats['tables_without_autoincrement'])} 张（复合主键等）")


if __name__ == "__main__":
    main()
