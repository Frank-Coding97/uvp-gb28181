#!/usr/bin/env python3
"""Generate the release baseline SQL for every supported dialect.

Input  : ``schema.ir.json`` + ``seeds/*.jsonl`` (see capture_schema.py)
Output : the three checked-in release scripts

    mysql       ../uvp-gb28181.sql
    postgresql  ../postgresql_converted.sql
    sqlserver   ../sqlserver_converted.sql

SQL Server 的来龙去脉：2026-09-15 曾被从交付集摘除（当时判断该库淘汰），
2026-09-22 按用户要求恢复。SQLServerProfile 因此是后补的，**没有实机验收**
（导入只在 MySQL / PostgreSQL 两侧跑过），最低保障是 initialization_contract_test.go
的列级跨方言一致断言。别把它当成「跑过验收」。
⚠️ 仓里仍有 `gorm.io/driver/sqlserver` 依赖、`app/gb28181/migration/dialect.go`
的 SQL Server 分支、以及 144 个 `-sqlserver` 迁移变体 —— 那是**另一件更大的事**
（全仓方言层清除），不在这里处理。

Adding a domestic database (达梦 / 人大金仓 / OceanBase / GaussDB) means adding
one entry to ``PROFILES``; the pipeline, the IR and the seed data do not change.
Most of those speak a PostgreSQL-compatible dialect, so a profile can often
inherit from ``postgresql``.

Design notes
------------
* Deterministic: the same IR always produces byte-identical output, so CI can
  assert ``generate -> git diff --exit-code``.
* Styles deliberately match the conventions already in use, because these files
  are consumed by existing deployments and by initialization_contract_test.go:
  MySQL keeps ``-- Table structure for ``...`` section markers and inline KEY
  clauses, PostgreSQL keeps unquoted identifiers, SQLite keeps double-quoted
  identifiers plus the range/length CHECK constraints, and every dialect emits
  a table's indexes immediately after that table.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import re
import sys
from collections import Counter
from pathlib import Path

ROOT = Path(__file__).resolve().parent
DATABASE_DIR = ROOT.parent
REPO_ROOT = DATABASE_DIR.parents[2]

IR_FILE = ROOT / "schema.ir.json"
SEEDS_DIR = ROOT / "seeds"
POLICY_FILE = ROOT / "policy.json"

INTEGER_BASES = {"tinyint", "smallint", "mediumint", "int", "integer", "bigint"}
STRING_BASES = {"varchar", "char", "tinytext", "text", "mediumtext", "longtext", "json", "enum", "set"}
BLOB_BASES = {"blob", "mediumblob", "longblob", "tinyblob", "binary", "varbinary"}


def normalize_sql_text(value: str) -> str:
    """Keep embedded SQL literals deterministic across platform line endings."""
    return value.replace("\r\n", "\n").replace("\r", "\n")


# --------------------------------------------------------------------------
# MySQL type parsing
# --------------------------------------------------------------------------

class ColumnType:
    __slots__ = ("base", "args", "unsigned", "raw")

    def __init__(self, raw: str):
        self.raw = raw.strip()
        text = self.raw.lower()
        self.unsigned = text.endswith("unsigned")
        if self.unsigned:
            text = text[: -len("unsigned")].strip()
        match = re.match(r"^([a-z]+)\s*(?:\(([^)]*)\))?$", text)
        if not match:
            raise ValueError(f"无法解析 MySQL 列类型: {raw!r}")
        self.base = match.group(1)
        args = match.group(2) or ""
        self.args = [a.strip() for a in args.split(",") if a.strip()]

    @property
    def is_bool(self) -> bool:
        return self.base == "tinyint" and self.args == ["1"] and not self.unsigned

    @property
    def is_integer(self) -> bool:
        return self.base in INTEGER_BASES

    @property
    def length(self) -> int | None:
        if self.base in STRING_BASES or self.base in BLOB_BASES:
            if self.args and self.args[0].isdigit():
                return int(self.args[0])
        return None

    def __str__(self) -> str:
        return self.raw


# --------------------------------------------------------------------------
# dialect profiles
# --------------------------------------------------------------------------

class Profile:
    """Base profile: how one dialect spells things."""

    name = ""
    header: list[str] = []
    footer: list[str] = []
    emits_comments = False
    statement_separator = "\n\n"

    # -- identifiers -------------------------------------------------------
    def quote(self, name: str) -> str:
        return name

    # -- types -------------------------------------------------------------
    def map_type(self, col: dict, ctype: ColumnType, auto_increment: bool) -> str:
        raise NotImplementedError

    def auto_increment_column(self, col: dict, ctype: ColumnType, is_pk: bool) -> str:
        return self.map_type(col, ctype, is_pk)

    def column_collation(self, col: dict, ctype: ColumnType) -> str | None:
        """Return a dialect-specific column collation, if one is required."""
        return None

    # -- literals ----------------------------------------------------------
    def quote_string(self, value: str) -> str:
        value = normalize_sql_text(value)
        return "'" + value.replace("'", "''") + "'"

    def literal(self, value) -> str:
        if value is None:
            return "NULL"
        if isinstance(value, bool):
            return "1" if value else "0"
        if isinstance(value, (int, float)):
            return repr(value) if isinstance(value, float) else str(value)
        if isinstance(value, str):
            return self.quote_string(value)
        if isinstance(value, dict):
            if "$dec" in value:
                return value["$dec"]
            if "$hex" in value:
                return self.hex_literal(value["$hex"])
            if "$ts" in value:
                return self.quote_string(value["$ts"])
            if "$date" in value:
                return self.quote_string(value["$date"])
            if "$dur" in value:
                total = float(value["$dur"])
                sign = "-" if total < 0 else ""
                total = abs(total)
                hours = int(total // 3600)
                minutes = int((total % 3600) // 60)
                seconds = total % 60
                return self.quote_string(f"{sign}{hours:02d}:{minutes:02d}:{seconds:09.6f}")
            if "$raw" in value:
                return self.quote_string(value["$raw"])
        raise ValueError(f"无法渲染字面量: {value!r}")

    def hex_literal(self, hex_text: str) -> str:
        return "X'" + hex_text + "'"

    # -- defaults ----------------------------------------------------------
    def bool_literal(self, value) -> str:
        """布尔列的默认值字面量。默认按 0/1 渲染（MySQL/SQL Server/SQLite 都收）。"""
        return "1" if str(value).strip() in {"1", "true", "TRUE", "True"} else "0"

    def bool_sql(self, value) -> str:
        """布尔列的取值字面量（种子行用）。"""
        if value is None:
            return "NULL"
        return self.bool_literal(value)

    def default_clause(self, col: dict, ctype: ColumnType) -> str:
        default = col.get("default")
        if default is None:
            return ""
        generated = "DEFAULT_GENERATED" in col.get("extra", "")
        if generated:
            expression = str(default)
            if self.name == "mysql":
                return f" DEFAULT {expression}"
            return f" DEFAULT {self.expression_default(expression)}"
        # 布尔必须先判：GORM 把 Go 的 bool 建成 tinyint(1)，走下面的整数分支会渲染成
        # `DEFAULT 0`。PostgreSQL 的 BOOLEAN 列拒绝整数默认值
        # （column "resolved" is of type boolean but default expression is of type integer），
        # 所以这里交给各方言给出自己的字面量。
        if col.get("boolean"):
            return f" DEFAULT {self.bool_literal(default)}"
        if ctype.is_integer or ctype.base in {"decimal", "numeric", "double", "float", "bit"}:
            return f" DEFAULT {default}"
        if ctype.base in {"datetime", "timestamp", "date", "time"}:
            return f" DEFAULT {self.quote_string(str(default))}"
        if ctype.base in BLOB_BASES:
            return f" DEFAULT {self.hex_literal(str(default).encode().hex())}"
        return f" DEFAULT {self.quote_string(str(default))}"

    def expression_default(self, expression: str) -> str:
        return expression

    # -- comments ----------------------------------------------------------
    # MySQL 把注释写在列/表定义里，PostgreSQL 用独立的 COMMENT ON 语句，
    # SQL Server 要 sp_addextendedproperty。基类默认「不支持」，由各方言自行覆盖 ——
    # 沉默地丢掉注释比报错更糟：脚本照样导入成功，只是字段含义没了。
    def comment_literal(self, text: str) -> str:
        text = normalize_sql_text(text)
        return "'" + text.replace("'", "''") + "'"

    def column_comment_suffix(self, col: dict) -> str:
        return ""

    def table_comment_suffix(self, table: dict) -> str:
        return ""

    def table_comment_statements(self, table: dict) -> list[str]:
        return []

    # -- constraints -------------------------------------------------------
    def referential_action(self, rule: str) -> str:
        """把 MySQL 的级联动作名映射到本方言。

        默认同名直传；SQL Server 没有 ``RESTRICT``（用 ``NO ACTION`` 表达同一
        语义），所以它要覆盖这一个。
        """
        return rule

    def translate_check(self, clause: str) -> str:
        """把 MySQL 的 CHECK 表达式转成本方言。

        MySQL 的原文里有三类本方言不认的东西，逐个消掉：
        ``_utf8mb4'x'`` 字面量前缀、反引号标识符、``regexp_like`` 与
        ``length`` 这类函数（后者是**字节**长度，别错译成字符长度）。
        """
        return clause

    def render_foreign_key(self, table: dict, fk: dict) -> str:
        q = self.quote
        columns = ", ".join(q(c) for c in fk["columns"])
        referenced = ", ".join(q(c) for c in fk["referenced_columns"])
        sql = (f"  CONSTRAINT {q(fk['name'])} FOREIGN KEY ({columns}) "
               f"REFERENCES {q(fk['referenced_table'])} ({referenced})")
        delete = self.referential_action(fk.get("delete_rule", "NO ACTION"))
        update = self.referential_action(fk.get("update_rule", "NO ACTION"))
        if delete != "NO ACTION":
            sql += f" ON DELETE {delete}"
        if update != "NO ACTION":
            sql += f" ON UPDATE {update}"
        return sql

    def render_check(self, table: dict, check: dict) -> str:
        clause = normalize_check_clause(check["clause"])
        return f"  CONSTRAINT {self.quote(check['name'])} CHECK ({self.translate_check(clause)})"


class MySQLProfile(Profile):
    name = "mysql"
    emits_comments = True

    def quote(self, name: str) -> str:
        return "`" + name.replace("`", "``") + "`"

    def map_type(self, col, ctype, auto_increment):
        return ctype.raw

    def quote_string(self, value: str) -> str:
        value = normalize_sql_text(value)
        out = value.replace("\\", "\\\\").replace("'", "\\'")
        out = out.replace("\n", "\\n").replace("\r", "\\r")
        return "'" + out + "'"

    def column_sql(self, col, ctype, auto_increment, is_pk, pk_columns):
        parts = [self.quote(col["name"]), ctype.raw]
        collation = col.get("collation")
        if collation and collation.startswith("utf8mb4") and collation != "utf8mb4_general_ci":
            parts.append(f"COLLATE {collation}")
        parts.append("NOT NULL" if not col["nullable"] else "NULL")
        default = self.default_clause(col, ctype)
        if default:
            parts.append(default.strip())
        extra = col.get("extra", "")
        if "auto_increment" in extra:
            parts.append("AUTO_INCREMENT")
        match = re.search(r"on update (CURRENT_TIMESTAMP(?:\(\d+\))?)", extra, re.I)
        if match:
            parts.append("ON UPDATE " + match.group(1))
        if col.get("comment"):
            parts.append("COMMENT " + self.quote_string(col["comment"]))
        return "  " + " ".join(parts)

    def table_comment_suffix(self, table: dict) -> str:
        """MySQL 的表注释挂在建表语句尾部（``) ENGINE=... COMMENT='...'``）。

        注意默认值和表注释共用 ``COMMENT`` 关键字但位置不同：列注释在列定义里，
        表注释在括号之后。放到括号里会被 MySQL 当成列定义解析失败。
        """
        comment = table.get("comment")
        if not comment:
            return ""
        return " COMMENT=" + self.quote_string(comment)


class PostgresProfile(Profile):
    name = "postgresql"
    emits_comments = True

    def quote(self, name: str) -> str:
        """PostgreSQL 一律加双引号。

        不能"按需加引号"：本库里存在保留字做列名的情况（sys_jobs.group），
        不引号就 `syntax error at or near "group"`；而且连索引定义一起挂掉，
        表现成"少了一张表 + 少几个索引"。其它方言的 profile 本来就总是带引号
        （MySQL 反引号 / SQL Server 方括号 / SQLite 双引号），这里对齐即可。
        所有标识符都是小写 snake_case，加引号不改变 PostgreSQL 的标识符解析结果。
        """
        return '"' + name.replace('"', '""') + '"'

    def bool_literal(self, value) -> str:
        return "true" if str(value).strip() in {"1", "true", "TRUE", "True"} else "false"

    def map_type(self, col, ctype, auto_increment):
        base, args, _unsigned = ctype.base, ctype.args, ctype.unsigned
        if col.get("boolean"):
            return "BOOLEAN"
        if base in {"tinyint", "smallint", "mediumint"}:
            return "SMALLINT"
        if base in {"int", "integer"}:
            return "SERIAL" if auto_increment else "INTEGER"
        if base == "bigint":
            return "BIGSERIAL" if auto_increment else "BIGINT"
        if base in {"decimal", "numeric"}:
            return "NUMERIC(" + ",".join(args) + ")" if args else "NUMERIC"
        if base in {"double", "double precision"}:
            return "DOUBLE PRECISION"
        if base in {"float", "real"}:
            return "REAL"
        if base in {"decimal"}:
            return "NUMERIC"
        if base == "date":
            return "DATE"
        if base in {"time"}:
            return "TIME"
        if base == "year":
            return "SMALLINT"
        if base in {"datetime", "timestamp"}:
            return "TIMESTAMP(" + args[0] + ")" if args else "TIMESTAMP"
        if base in {"varchar", "char"}:
            return base.upper() + "(" + (args[0] if args else "255") + ")"
        if base in {"text", "tinytext", "mediumtext", "longtext"}:
            return "TEXT"
        if base == "json":
            return "JSONB"
        if base in BLOB_BASES:
            return "BYTEA"
        if base == "bit":
            return "BOOLEAN" if args == ["1"] else "BIT(" + ",".join(args) + ")"
        if base in {"enum", "set"}:
            return "TEXT"
        raise ValueError(f"PostgreSQL 无映射: {ctype.raw}")

    def expression_default(self, expression: str) -> str:
        return re.sub(r"(?i)CURRENT_TIMESTAMP(?:\(\d+\))?", "CURRENT_TIMESTAMP", expression)

    def table_comment_statements(self, table: dict) -> list[str]:
        """PostgreSQL 的注释是独立语句，不能写在 CREATE TABLE 里。

        表注释和列注释都要发；列注释按建表顺序逐列发，方便与 MySQL 侧对照。
        """
        q = self.quote
        name = table["name"]
        out: list[str] = []
        if table.get("comment"):
            out.append(f"COMMENT ON TABLE {q(name)} IS {self.comment_literal(table['comment'])};")
        for col in table["columns"]:
            if col.get("comment"):
                out.append(
                    f"COMMENT ON COLUMN {q(name)}.{q(col['name'])} IS "
                    f"{self.comment_literal(col['comment'])};"
                )
        return out

    def translate_check(self, clause: str) -> str:
        """MySQL CHECK 表达式 → PostgreSQL。

        三处必须转：
        * ``_utf8mb4'x'`` / ``_ascii'x'`` —— MySQL 的字符集字面量前缀，PG 不认；
        * ``regexp_like(a, b[, flags])`` —— PG 的正则匹配操作符是 ``~``；
        * ``length(x)`` —— **MySQL 的 length 是字节数**，PG 同名函数是字符数。
          直接照抄会让「json 列不超过 32768」这类长度约束在中文内容上放宽三倍，
          所以译成 ``octet_length``。``char_length`` 两边同义，保持不动。
        * 反引号标识符 → 双引号。
        """
        text = re.sub(r"_(?:utf8mb4|utf8|ascii|binary|latin1|ucs2|utf16|utf32|gbk)(?=')", "", clause)
        text = rewrite_calls(text, "regexp_like", lambda args: f"{args[0]} ~ {args[1]}")
        text = re.sub(r"(?<![A-Za-z0-9_])length\s*\(", "octet_length(", text, flags=re.I)
        text = re.sub(r"`([^`]+)`", r'"\1"', text)
        return text


class SQLServerProfile(Profile):
    """SQL Server 方言。

    与其它方言的三处硬差异，全部在 profile 内部消化，调用方不做特判：

    * **前缀索引没有等价语法**。MySQL 的 ``path(191)`` 在 PostgreSQL / SQLite
      可以改写成 ``left(path,191)`` / ``substr(path,1,191)``；SQL Server 只能靠
      计算列（改结构）—— 所以 ``index_column`` 返回 ``None``，由调用方把该列
      从索引/唯一约束里摘掉并留 NOTE 注释。
    * **显式 id 插入必须先 ``SET IDENTITY_INSERT``**。IDENTITY 列默认拒绝显式值，
      而基线种子全是显式 id（``Cannot insert explicit value for identity column``）。
      插入完成后 SQL Server 会把 identity 当前值顶到本次插入的最大值，所以
      **不需要** PG 那样的 setval 补水位语句。
    * **字符串字面量带 N 前缀**，否则非 ASCII 会按服务器排序规则丢字。

    类型映射的两个刻意选择（不是照搬 MySQL）：
    * ``datetime``（MySQL 秒精度）→ ``DATETIME2(3)``：与仓里既有的转换文件一致，
      避免落库后亚秒被截断导致与既有环境的观测口径不一致。
    * ``ON UPDATE CURRENT_TIMESTAMP`` **丢弃** —— SQL Server 无等价 DDL，
      要保留语义只能建触发器，属改结构，不在基线范围内（文件头有显式说明）。
    """

    name = "sqlserver"
    emits_comments = True

    def quote(self, name: str) -> str:
        return "[" + name.replace("]", "]]") + "]"

    def quote_string(self, value: str) -> str:
        value = normalize_sql_text(value)
        return "N'" + value.replace("'", "''") + "'"

    def column_collation(self, col, ctype):
        """Preserve MySQL's binary string comparison semantics.

        MySQL ``*_bin`` collations are case-sensitive, while SQL Server's
        database default is commonly case-insensitive.  The incremental
        SQL Server migrations use BIN2 for these columns, so a fresh baseline
        must make the same mapping or unique keys will behave differently.
        """
        source = str(col.get("collation") or "").lower()
        if source.endswith("_bin") and ctype.base in STRING_BASES:
            return "Latin1_General_100_BIN2"
        return None

    def hex_literal(self, hex_text: str) -> str:
        return "0x" + hex_text

    def bool_literal(self, value) -> str:
        return "1" if str(value).strip() in {"1", "true", "TRUE", "True"} else "0"

    def expression_default(self, expression: str) -> str:
        # MySQL 的 CURRENT_TIMESTAMP(n) → SYSDATETIME()（datetime2 精度，
        # 才能喂给 DATETIME2(3)/(6) 列；裸 CURRENT_TIMESTAMP 是 datetime 类型）。
        if re.search(r"(?i)CURRENT_TIMESTAMP\s*\(", expression):
            return re.sub(r"(?i)CURRENT_TIMESTAMP\s*\(\d*\)", "SYSDATETIME()", expression)
        return expression

    def referential_action(self, rule: str) -> str:
        """SQL Server 没有 RESTRICT 关键字，等义动作是 NO ACTION。"""
        return "NO ACTION" if rule.upper() == "RESTRICT" else rule

    def translate_check(self, clause: str) -> str:
        """MySQL CHECK 表达式 → Transact-SQL。

        * 字符集字面量前缀（``_utf8mb4'x'``）去掉；
        * ``length()`` 在 MySQL 是**字节**数，T-SQL 的对应物是 ``DATALENGTH``
          （``LEN`` 按字符数算且忽略尾空格，语义不同）；
        * 反引号标识符 → 方括号。

        这里**故意不处理** ``regexp_like``：T-SQL 没有等价函数。静默丢掉这条
        CHECK 会让约束在三方言之间悄悄失效（同一份脏数据 MySQL/PG 拒收、
        SQL Server 收下），所以宁可生成阶段直接失败，逼调用方在 policy.json
        的 ``check_overrides`` 里给出显式写法。
        """
        if re.search(r"(?i)\bregexp_like\s*\(", clause):
            raise ValueError(
                "SQL Server 无 regexp_like 等价函数；请在本条 CHECK 的 "
                "policy.json check_overrides 里显式给出 sqlserver 写法"
            )
        text = re.sub(r"_(?:utf8mb4|utf8|ascii|binary|latin1|ucs2|utf16|utf32|gbk)(?=')", "", clause)
        text = re.sub(r"(?<![A-Za-z0-9_])length\s*\(", "DATALENGTH(", text, flags=re.I)
        text = re.sub(r"`([^`]+)`", r"[\1]", text)
        return text

    def map_type(self, col, ctype, auto_increment):
        base, args = ctype.base, ctype.args
        identity = " IDENTITY(1,1)" if auto_increment else ""
        if col.get("boolean"):
            return "BIT"
        if base == "tinyint":
            return "TINYINT" + identity
        if base in {"smallint", "mediumint", "year"}:
            return "SMALLINT" + identity
        if base in {"int", "integer"}:
            return "INT" + identity
        if base == "bigint":
            return "BIGINT" + identity
        if base in {"decimal", "numeric"}:
            return "DECIMAL(" + ",".join(args) + ")" if args else "DECIMAL(20,6)"
        if base == "double":
            return "FLOAT"
        if base in {"float", "real"}:
            return "REAL"
        if base == "date":
            return "DATE"
        if base == "time":
            return "TIME"
        if base in {"datetime", "timestamp"}:
            return "DATETIME2(" + (args[0] if args else "3") + ")"
        if base in {"char", "varchar"}:
            length = int(args[0]) if args and args[0].isdigit() else (1 if base == "char" else 255)
            if length > 4000:
                return "NVARCHAR(MAX)"
            return ("NCHAR(" if base == "char" else "NVARCHAR(") + str(length) + ")"
        if base in {"text", "tinytext", "mediumtext", "longtext", "json", "enum", "set"}:
            return "NVARCHAR(MAX)"
        if base in {"binary", "varbinary"}:
            length = int(args[0]) if args and args[0].isdigit() else None
            return "VARBINARY(" + str(length) + ")" if length else "VARBINARY(MAX)"
        if base in {"blob", "tinyblob", "mediumblob", "longblob"}:
            return "VARBINARY(MAX)"
        if base == "bit":
            if args == ["1"]:
                return "BIT"
            bits = int(args[0]) if args and args[0].isdigit() else 1
            return "VARBINARY(" + str((bits + 7) // 8) + ")"
        raise ValueError(f"SQL Server 无映射: {ctype.raw}")


class SQLiteProfile(Profile):
    name = "sqlite"

    def quote(self, name: str) -> str:
        return '"' + name.replace('"', '""') + '"'

    def map_type(self, col, ctype, auto_increment):
        base = ctype.base
        if ctype.is_integer:
            return "INTEGER"
        if base in {"decimal", "numeric"}:
            return "NUMERIC"
        if base in {"double", "float", "real"}:
            return "REAL"
        if base in STRING_BASES:
            return "TEXT"
        if base in BLOB_BASES:
            return "BLOB"
        if base in {"date", "datetime", "timestamp"}:
            return base.upper()
        if base == "time":
            return "TEXT"
        if base == "bit":
            return "INTEGER"
        raise ValueError(f"SQLite 无映射: {ctype.raw}")

    def default_clause(self, col, ctype):
        """SQLite keeps declared defaults, but rejects function defaults."""
        default = col.get("default")
        if default is None:
            return ""
        if "DEFAULT_GENERATED" in col.get("extra", ""):
            return ""
        return super().default_clause(col, ctype)


PROFILES: dict[str, Profile] = {
    "mysql": MySQLProfile(),
    "postgresql": PostgresProfile(),
    "sqlserver": SQLServerProfile(),
    "sqlite": SQLiteProfile(),
}


# --------------------------------------------------------------------------
# CHECK expression rewriting
# --------------------------------------------------------------------------

def normalize_check_clause(clause: str) -> str:
    """还原 ``check_clause`` 的 ``\\'`` 转义（幂等）。

    capture_schema.py 已经还原过一次；这里再做一遍是因为 IR 是检查入库的
    文件，手改或从旧版本取回时可能带着未还原的形态。带着 ``\\'`` 的表达式
    生成出来是坏的 SQL（反斜杠会原样进文件），而症状很隐蔽：
    字符集前缀/引号替换的正则全都匹配不上，看起来像「转换器没生效」。
    """
    if "\\'" not in clause and '\\"' not in clause and "\\\\" not in clause:
        return clause
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


def split_call_arguments(text: str) -> list[str]:
    """按顶层逗号切分函数实参，忽略嵌套调用里的逗号。"""
    args, depth, current = [], 0, []
    for ch in text:
        if ch in "(":
            depth += 1
        elif ch == ")":
            depth -= 1
        if ch == "," and depth == 0:
            args.append("".join(current).strip())
            current = []
            continue
        current.append(ch)
    tail = "".join(current).strip()
    if tail:
        args.append(tail)
    return args


def rewrite_calls(text: str, function: str, render) -> str:
    """把 ``function(...)`` 逐个替换成 ``render(args)``。

    必须按括号配平扫描而不是正则替换：``regexp_like(a, b, 'c')`` 的实参里
    可以有括号（``not(regexp_like(...))``、嵌套的 ``length(...)``），
    非贪婪正则会切在第一个 ``)`` 上，把表达式切碎。
    """
    pattern = re.compile(r"(?i)\b" + re.escape(function) + r"\s*\(")
    out, index = [], 0
    while True:
        match = pattern.search(text, index)
        if not match:
            out.append(text[index:])
            break
        out.append(text[index:match.start()])
        depth, cursor = 0, match.end() - 1
        while cursor < len(text):
            if text[cursor] == "(":
                depth += 1
            elif text[cursor] == ")":
                depth -= 1
                if depth == 0:
                    break
            cursor += 1
        out.append(render(split_call_arguments(text[match.end():cursor])))
        index = cursor + 1
    return "".join(out)


# --------------------------------------------------------------------------
# rendering
# --------------------------------------------------------------------------

def primary_key_columns(table: dict) -> list[str]:
    return [c["name"] for c in table.get("primary_key", [])]


def is_auto_increment(col: dict, table: dict) -> bool:
    if "auto_increment" not in col.get("extra", ""):
        return False
    return col["name"] in primary_key_columns(table)


def sequence_watermarks(profile: Profile, tables: list[dict],
                        seeds: dict[str, list[dict]]) -> list[str]:
    """补齐自增水位。

    基线用**显式 id** 插入种子行。三种方言对此的反应不一样：

    * MySQL  —— 显式 id 插入会把 AUTO_INCREMENT 推到 max(id)+1，无需处理。
    * SQLite —— INTEGER PRIMARY KEY 就是 rowid，取 max+1，无需处理。
    * PostgreSQL —— **SERIAL / BIGSERIAL 的序列不前进**。不补 setval，
      全新装环境一旦运行，第一次 INSERT 就会撞上已存在的 id（`duplicate key
      value violates unique constraint`）。这曾经靠手工在生成文件尾部加
      `SELECT setval(...)` 兜住，是纯人工知识、没有落到任何可校验的地方。

    所以这里按种子数据的实际 max(id) 逐个生成水位语句。
    """
    if profile.name != "postgresql":
        return []
    out: list[str] = []
    for table in tables:
        rows = seeds.get(table["name"])
        if not rows:
            continue
        # primary_key 是 [{"name": ..., "prefix": ...}] 形态，不能当字符串列表用。
        pk = primary_key_columns(table)
        if len(pk) != 1:
            continue
        column = next((c for c in table["columns"] if c["name"] == pk[0]), None)
        if column is None or not is_auto_increment(column, table):
            continue
        ids = [r.get(pk[0]) for r in rows]
        if any(not isinstance(v, int) for v in ids):
            continue
        # 不照 `<表>_<列>_seq` 约定名硬写：PostgreSQL 把标识符截到 63 字节，
        # 超长表名的序列名就不再是约定值。pg_get_serial_sequence 返回真实序列名，
        # 且对 serial / bigserial 都成立（本基线自增列渲染成 BIGSERIAL）。
        out.append(
            "SELECT setval(pg_get_serial_sequence('{t}', '{c}'), {m}, true);".format(
                t=table["name"], c=pk[0], m=max(ids)
            )
        )
    return out


def render_column(profile: Profile, table: dict, col: dict, pk: list[str]) -> str:
    ctype = ColumnType(col["type"])
    auto = is_auto_increment(col, table)

    if profile.name == "mysql":
        return profile.column_sql(col, ctype, auto, col["name"] in pk, pk)

    parts = [profile.quote(col["name"]), profile.map_type(col, ctype, auto)]
    collation = profile.column_collation(col, ctype)
    if collation:
        parts.append("COLLATE " + collation)
    if not col["nullable"]:
        parts.append("NOT NULL")
    if not auto:
        # SERIAL / IDENTITY already carry the generated value.
        default = profile.default_clause(col, ctype)
        if default:
            parts.append(default.strip())
    return "  " + " ".join(parts)


def sqlite_checks(col: dict, ctype: ColumnType, pk: list[str]) -> list[str]:
    """Preserve MySQL integer ranges and text lengths that SQLite would drop."""
    name = '"' + col["name"].replace('"', '""') + '"'
    checks = []
    if col.get("boolean"):
        checks.append(f"{name} IS NULL OR {name} IN (0, 1)")
    elif ctype.is_integer:
        if ctype.unsigned:
            maximum = {"tinyint": 255, "smallint": 65535, "mediumint": 16777215,
                       "int": 4294967295, "integer": 4294967295,
                       "bigint": 9223372036854775807}[ctype.base]
            minimum = 0
        else:
            minimum, maximum = {
                "tinyint": (-128, 127), "smallint": (-32768, 32767),
                "mediumint": (-8388608, 8388607),
                "int": (-2147483648, 2147483647),
                "integer": (-2147483648, 2147483647),
                "bigint": (-9223372036854775808, 9223372036854775807),
            }[ctype.base]
        if col["nullable"]:
            checks.append(
                f'{name} IS NULL OR (typeof({name}) = \'integer\' AND {name} '
                f"BETWEEN {minimum} AND {maximum})"
            )
        else:
            checks.append(
                f'typeof({name}) = \'integer\' AND {name} BETWEEN {minimum} AND {maximum}'
            )
    if ctype.base == "json":
        # MySQL 建成 json、PostgreSQL 建成 JSONB —— 两者都会在写入时拒绝非法 JSON。
        # SQLite 只有 TEXT，不补这条 CHECK 就是跨方言的语义退化（非法 JSON 静默入库）。
        # json_valid 是 deterministic 函数，可以出现在 CHECK 里。
        if col["nullable"]:
            checks.append(f"{name} IS NULL OR json_valid({name})")
        else:
            checks.append(f"json_valid({name})")
    length = ctype.length
    if length is not None and ctype.base in STRING_BASES and ctype.base != "json":
        if col["nullable"]:
            checks.append(f"{name} IS NULL OR length({name}) <= {length}")
        else:
            checks.append(f"length({name}) <= {length}")
    return checks


def index_column(profile: Profile, col: dict) -> str | None:
    """Render one index column, honouring MySQL key prefix lengths.

    MySQL lets an index cover only the first ``n`` characters (``path(191)``)
    because a full utf8mb4 column can exceed the key size limit.  PostgreSQL and
    SQLite have no such syntax but do have expression indexes, so the same
    prefix becomes ``left(col, n)`` / ``substr(col, 1, n)``.  SQL Server has
    neither without a computed column, so the column is dropped from the index
    and ``None`` is returned for the caller to record.
    """
    name = col["name"]
    prefix = col.get("prefix")
    if not prefix:
        return profile.quote(name)
    if profile.name == "mysql":
        return f"{profile.quote(name)}({prefix})"
    if profile.name == "postgresql":
        return f"left({profile.quote(name)}, {prefix})"
    if profile.name == "sqlite":
        return f"substr({profile.quote(name)}, 1, {prefix})"
    return None


def index_columns(profile: Profile, columns: list[dict]) -> tuple[list[str], list[str]]:
    rendered, dropped = [], []
    for col in columns:
        text = index_column(profile, col)
        if text is None:
            dropped.append(col["name"])
        else:
            rendered.append(text)
    return rendered, dropped


# PostgreSQL keeps index names per schema and SQLite per database, while MySQL
# and SQL Server scope them per table.  A name reused across tables therefore
# works on MySQL/SQL Server but collides elsewhere -- and with
# "CREATE INDEX IF NOT EXISTS" the collision is silent, leaving the later tables
# without their index (and, for unique indexes, without their uniqueness).
SCHEMA_WIDE_INDEX_NAMES = {"postgresql", "sqlite"}
PG_IDENTIFIER_LIMIT = 63


def resolve_index_names(dialect: str, tables: list[dict]) -> dict[tuple[str, str], str]:
    if dialect not in SCHEMA_WIDE_INDEX_NAMES:
        return {}

    counts = Counter(
        entry["name"] for table in tables for entry in table["uniques"] + table["indexes"]
    )
    used: set[str] = set()
    mapping: dict[tuple[str, str], str] = {}
    for table in tables:
        for entry in table["uniques"] + table["indexes"]:
            name = entry["name"]
            if counts[name] == 1 and name not in used:
                used.add(name)
                mapping[(table["name"], name)] = name
                continue
            candidate = f"{table['name']}__{name}"
            if dialect == "postgresql" and len(candidate) > PG_IDENTIFIER_LIMIT:
                digest = hashlib.sha1(candidate.encode("utf-8")).hexdigest()[:8]
                candidate = candidate[: PG_IDENTIFIER_LIMIT - 9] + "_" + digest
            while candidate in used:
                candidate += "_x"
            used.add(candidate)
            mapping[(table["name"], name)] = candidate
    return mapping


def render_table(profile: Profile, table: dict,
                 index_names: dict[tuple[str, str], str] | None = None) -> str:
    index_names = index_names or {}

    def named(entry: dict) -> str:
        return index_names.get((table["name"], entry["name"]), entry["name"])

    name = table["name"]
    pk = primary_key_columns(table)
    lines: list[str] = []

    if profile.name == "mysql":
        lines.append(f"-- Table structure for `{name}`")
        lines.append(f"DROP TABLE IF EXISTS `{name}`;")
        lines.append(f"CREATE TABLE `{name}` (")
        body = [render_column(profile, table, col, pk) for col in table["columns"]]
        if pk:
            body.append("  PRIMARY KEY (" + ",".join(f"`{c}`" for c in pk) + ")")
        for unique in table["uniques"]:
            cols, _ = index_columns(profile, unique["columns"])
            body.append(f"  UNIQUE KEY `{unique['name']}` ({','.join(cols)})")
        for index in table["indexes"]:
            cols, _ = index_columns(profile, index["columns"])
            body.append(f"  KEY `{index['name']}` ({','.join(cols)})")
        for check in table.get("checks", []):
            body.append(profile.render_check(table, check))
        lines.append(",\n".join(body))
        lines.append(") ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci"
                     + profile.table_comment_suffix(table) + ";")
        return "\n".join(lines)

    if profile.name == "sqlserver":
        q = profile.quote
        lines.append(f"IF OBJECT_ID(N'{name}', N'U') IS NOT NULL DROP TABLE {q(name)};")
        lines.append(f"IF OBJECT_ID(N'{name}', N'U') IS NULL")
        lines.append("BEGIN")
        lines.append(f"  CREATE TABLE {q(name)} (")
        inline_identity_pk = (
            len(pk) == 1
            and any(c["name"] == pk[0] and is_auto_increment(c, table) for c in table["columns"])
        )
        body = []
        for col in table["columns"]:
            rendered = render_column(profile, table, col, pk)
            if inline_identity_pk and col["name"] == pk[0]:
                rendered += " PRIMARY KEY"
            body.append(rendered)
        # JSON 列的原生校验：MySQL 用 json 类型、PostgreSQL 用 JSONB，写非法 JSON
        # 直接报错；SQL Server 没有 JSON 类型（只能 NVARCHAR(MAX)），必须靠
        # ISJSON 的 CHECK 把同等约束补回来 —— 否则同一份非法数据在这里静默入库。
        # 与 SQLite 的 json_valid 是同一条教训的两个方言版本。
        for col in table["columns"]:
            if ColumnType(col["type"]).base != "json":
                continue
            expr = f"ISJSON({q(col['name'])}) = 1"
            if col["nullable"]:
                expr = f"{q(col['name'])} IS NULL OR {expr}"
            body.append("  CHECK (" + expr + ")")
        if pk and not inline_identity_pk:
            body.append("  PRIMARY KEY (" + ", ".join(q(c) for c in pk) + ")")
        dropped_notes = []
        for unique in table["uniques"]:
            cols, dropped = index_columns(profile, unique["columns"])
            if dropped:
                dropped_notes.append(f"-- NOTE: {unique['name']} 的 {', '.join(dropped)} 使用前缀索引，"
                                     f"SQL Server 无等价语法，已从唯一约束中移除。")
            body.append(f"  CONSTRAINT {q(named(unique))} UNIQUE ({', '.join(cols)})")
        for check in table.get("checks", []):
            body.append(profile.render_check(table, check))
        # CREATE TABLE 本体嵌在 IF ... BEGIN 里（缩 2 空格），列定义再缩一级对齐。
        lines.append(",\n".join("  " + line for line in body))
        lines.append("  );")
        lines.append("END;")
        lines.extend(dropped_notes)
        for index in table["indexes"]:
            cols, dropped = index_columns(profile, index["columns"])
            if dropped:
                lines.append(f"-- NOTE: {index['name']} 的 {', '.join(dropped)} 使用前缀索引，"
                             f"SQL Server 无等价语法，已从索引中移除。")
            index_name = named(index)
            lines.append(
                f"IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id=OBJECT_ID(N'{name}') "
                f"AND name=N'{index_name}')"
            )
            lines.append(f"  CREATE INDEX {q(index_name)} ON {q(name)} ({', '.join(cols)});")
        return "\n".join(lines)

    if profile.name == "sqlite":
        # Deliberately no DROP TABLE: this baseline is applied by the standalone
        # bootstrap on first start, and a stray DROP would destroy a live file.
        lines.append(f'CREATE TABLE IF NOT EXISTS "{name}" (')
        body = []
        for col in table["columns"]:
            rendered = render_column(profile, table, col, pk)
            if col["name"] in pk and len(pk) == 1:
                if is_auto_increment(col, table):
                    rendered = f'  "{col["name"]}" INTEGER PRIMARY KEY AUTOINCREMENT'
                else:
                    rendered += " PRIMARY KEY"
            body.append(rendered)
        if len(pk) > 1:
            body.append("  PRIMARY KEY (" + ", ".join(f'"{c}"' for c in pk) + ")")
        for col in table["columns"]:
            for check in sqlite_checks(col, ColumnType(col["type"]), pk):
                body.append("  CHECK (" + check + ")")
        lines.append(",\n".join(body))
        lines.append(");")
        for unique in table["uniques"]:
            cols, _ = index_columns(profile, unique["columns"])
            lines.append(
                f'CREATE UNIQUE INDEX IF NOT EXISTS "{named(unique)}" ON "{name}" '
                f'({", ".join(cols)});'
            )
        for index in table["indexes"]:
            cols, _ = index_columns(profile, index["columns"])
            lines.append(
                f'CREATE INDEX IF NOT EXISTS "{named(index)}" ON "{name}" '
                f'({", ".join(cols)});'
            )
        return "\n".join(lines)

    # postgresql
    prefix = profile.quote(name)
    lines.append(f"DROP TABLE IF EXISTS {prefix};")
    lines.append(f"CREATE TABLE {prefix} (")

    # A single auto-increment primary key is spelled inline for PostgreSQL so it
    # reads as "id BIGSERIAL PRIMARY KEY"; every other primary key becomes a
    # table level constraint.
    inline_serial_pk = (
        profile.name == "postgresql"
        and len(pk) == 1
        and any(c["name"] == pk[0] and is_auto_increment(c, table) for c in table["columns"])
    )

    body = []
    for col in table["columns"]:
        if inline_serial_pk and col["name"] == pk[0]:
            ctype = ColumnType(col["type"])
            serial = "BIGSERIAL" if ctype.base == "bigint" else "SERIAL"
            body.append(f"  {profile.quote(col['name'])} {serial} PRIMARY KEY")
            continue
        body.append(render_column(profile, table, col, pk))
    if pk and not inline_serial_pk:
        cols = ", ".join(profile.quote(c) for c in pk)
        body.append(f"  PRIMARY KEY ({cols})")
    for unique in table["uniques"]:
        # A named UNIQUE constraint already creates the index, so no separate
        # CREATE UNIQUE INDEX is emitted (that would collide on the same name).
        cols, dropped = index_columns(profile, unique["columns"])
        if dropped:
            lines.append(
                f"-- NOTE: {unique['name']} 的 {', '.join(dropped)} 使用前缀索引，"
                f"该方言无等价语法，已从唯一约束中移除。"
            )
        body.append(f"  CONSTRAINT {named(unique)} UNIQUE ({', '.join(cols)})")
    for check in table.get("checks", []):
        body.append(profile.render_check(table, check))
    lines.append(",\n".join(body))
    lines.append(");")
    # 注释紧跟建表：PostgreSQL 的 COMMENT 是独立语句，放这里读起来与
    # MySQL 的 inline 注释位置一致，也避免全部堆到文件尾部。
    lines.extend(profile.table_comment_statements(table))
    for index in table["indexes"]:
        cols, dropped = index_columns(profile, index["columns"])
        if dropped:
            lines.append(
                f"-- NOTE: {index['name']} 的 {', '.join(dropped)} 使用前缀索引，"
                f"该方言无等价语法，已从索引中移除。"
            )
        lines.append(f"CREATE INDEX {named(index)} ON {prefix} ({', '.join(cols)});")
    return "\n".join(lines)


def apply_check_overrides(tables: list[dict], dialect: str,
                          overrides: dict) -> list[dict]:
    """把 policy.json 里为该方言写好的 CHECK 子句替换进去。

    只替换 clause，不动约束名与归属表 —— 这样渲染路径完全不变，override
    只是「这条的表达换成本方言的写法」。缺失某些方言的 override 不算错误
    （大多数约束三方言都能自动转）。
    """
    if not overrides:
        return tables
    out: list[dict] = []
    for table in tables:
        checks = table.get("checks") or []
        if not checks:
            out.append(table)
            continue
        replaced = []
        for check in checks:
            spec = overrides.get(check["name"])
            if spec and spec.get(dialect):
                replaced.append({**check, "clause": spec[dialect]})
            else:
                replaced.append(check)
        out.append({**table, "checks": replaced})
    return out


def render_foreign_key_statements(profile: Profile, tables: list[dict]) -> list[str]:
    """把所有外键渲染成建表之后的 ``ALTER TABLE ... ADD CONSTRAINT``。

    不能内联进 CREATE TABLE：表按表名字母序输出，而本库里
    ``sys_job_results`` 的外键指向字母序排在它**之后**的 ``sys_jobs``。
    MySQL 能在脚本头用 ``SET FOREIGN_KEY_CHECKS=0`` 兜住，PostgreSQL 与
    SQL Server 没有这个开关 —— 内联会在建表阶段直接失败
    （``relation "sys_jobs" does not exist``）。改成建表之后再 ADD，
    顺序就无关了。旧基线里的 ``DO $$ ... ALTER TABLE ... ADD CONSTRAINT``
    正是同一个原因。
    """
    out: list[str] = []
    for table in tables:
        for fk in table.get("foreign_keys", []):
            # render_foreign_key 产出的是 CREATE TABLE 内部片段（带缩进和
            # CONSTRAINT 前缀），改写成 ALTER 形态即可。
            definition = profile.render_foreign_key(table, fk).strip()
            out.append(f"ALTER TABLE {profile.quote(table['name'])} ADD {definition};")
    return out


def render_seed(profile: Profile, table: dict, rows: list[dict], batch: int = 200) -> str:
    if not rows:
        return ""
    name = table["name"]
    columns = [c["name"] for c in table["columns"]]
    # 布尔列要单独渲染：MySQL 里存的是 0/1，PostgreSQL 的 BOOLEAN 收不了整数
    # （column "..." is of type boolean but expression is of type integer）。
    bool_columns = {c["name"] for c in table["columns"] if c.get("boolean")}
    for row in rows:
        for key in row:
            if key not in columns:
                raise ValueError(f"{name}: 种子行含未知列 {key}")
    header = ", ".join(profile.quote(c) for c in columns)
    statements = []
    for start in range(0, len(rows), batch):
        chunk = rows[start : start + batch]
        values = []
        for row in chunk:
            cells = ", ".join(
                profile.bool_sql(row.get(c)) if c in bool_columns
                else profile.literal(row.get(c))
                for c in columns
            )
            values.append("(" + cells + ")")
        statements.append(
            f"INSERT INTO {profile.quote(name)} ({header}) VALUES\n" + ",\n".join(values) + ";"
        )
    if profile.name == "sqlserver":
        # IDENTITY 列默认拒绝显式值。基线种子全部用显式 id（跨方言一致），
        # 所以必须整段包在 SET IDENTITY_INSERT 里，否则第一次插入就报
        # "Cannot insert explicit value for identity column"。
        # 插完不需要补水位：SQL Server 会把 identity 当前值顶到本次插入的最大值。
        pk = primary_key_columns(table)
        identity = len(pk) == 1 and any(
            c["name"] == pk[0] and is_auto_increment(c, table) for c in table["columns"]
        )
        if identity:
            return (
                f"SET IDENTITY_INSERT {profile.quote(name)} ON;\n"
                + "\n\n".join(statements)
                + f"\nSET IDENTITY_INSERT {profile.quote(name)} OFF;"
            )
    return "\n\n".join(statements)


def build_document(dialect: str, ir: dict, seeds: dict[str, list[dict]],
                   check_overrides: dict | None = None) -> str:
    profile = PROFILES[dialect]
    tables = apply_check_overrides(ir["tables"], dialect, check_overrides or {})
    seeded = {t["name"]: t for t in tables if t["name"] in seeds}
    fingerprint = ir["meta"]["schema_fingerprint"]

    out: list[str] = []
    server_version = ir["meta"]["source_server_version"]
    if dialect == "mysql":
        out.append("-- UVP-GB28181 MySQL 8.0 release initialization script")
        out.append(f"-- Generated from the development schema ({server_version}) by")
        out.append("-- server/resource/database/baseline/generate_sql.py. Do not edit by hand.")
        out.append(f"-- Schema fingerprint: {fingerprint}")
        out.append("-- Contains production table structures and release baseline data only.")
        out.append("-- Excludes demo content and all environment-specific device, media-node,")
        out.append("-- SIP, cascade, alarm, trace, and operation data.")
        out.append("")
        out.append("SET NAMES utf8mb4;")
        out.append("SET time_zone = '+08:00';")
        out.append("SET FOREIGN_KEY_CHECKS = 0;")
        out.append("SET UNIQUE_CHECKS = 0;")
    elif dialect == "postgresql":
        out.append("-- UVP-GB28181 PostgreSQL release initialization script")
        out.append("-- Generated from the development schema by")
        out.append("-- server/resource/database/baseline/generate_sql.py. Do not edit by hand.")
        out.append(f"-- Schema fingerprint: {fingerprint}")
        out.append("-- Contains production table structures and release baseline data only.")
        out.append("")
        out.append("SET client_min_messages TO WARNING;")
    elif dialect == "sqlserver":
        out.append("-- UVP-GB28181 SQL Server release initialization script")
        out.append("-- Generated from the development schema by")
        out.append("-- server/resource/database/baseline/generate_sql.py. Do not edit by hand.")
        out.append(f"-- Schema fingerprint: {fingerprint}")
        out.append("-- Contains production table structures and release baseline data only.")
        out.append("")
        out.append("-- 方言差异（由 profile 消化，阅读时注意）：")
        out.append("--   * 自增主键渲染成 IDENTITY(1,1)；种子带显式 id，故整段包在")
        out.append("--     SET IDENTITY_INSERT ... ON/OFF 之间（IDENTITY 列拒绝显式值）。")
        out.append("--   * 前缀索引（MySQL 的 col(n)）无等价 DDL，该列已从索引中摘除，")
        out.append("--     每处都留了 -- NOTE 注释，看那里即可知道哪张表少了哪一列。")
        out.append("--   * MySQL 的 ON UPDATE CURRENT_TIMESTAMP 已丢弃：SQL Server 无等价")
        out.append("--     DDL，保留语义只能建触发器，属改结构，不在基线范围内。")
        out.append("--   * 字符串字面量与默认值一律带 N 前缀；文本/json 映射 NVARCHAR(MAX)。")
        out.append("")
        out.append("SET NOCOUNT ON;")
    else:
        out.append("-- UVP-GB28181 SQLite standalone baseline.")
        out.append("-- Generated from the development schema by")
        out.append("-- server/resource/database/baseline/generate_sql.py. Do not edit by hand.")
        out.append(f"-- Schema fingerprint: {fingerprint}")
        out.append("-- SQLite storage mapping: integer ids/counters -> INTEGER; DATE/DATETIME/TIMESTAMP")
        out.append("-- keep their declared type; JSON -> TEXT; decimal -> NUMERIC.")
        out.append("-- MySQL numeric ranges and VARCHAR/CHAR lengths are preserved by explicit checks.")
        out.append("")
        out.append("PRAGMA foreign_keys = ON;")
        out.append("PRAGMA busy_timeout = 5000;")

    out.append("")
    index_names = resolve_index_names(dialect, tables)
    # Keep the six catalog tables contiguous.  They are interleaved with the
    # older AK/SK tables in the captured alphabetical IR; placing markers from
    # the first to the last catalog table would accidentally include
    # sys_openapi_client_scope in the contract block.
    regular_tables = [
        table for table in tables
        if table["name"] not in OPENAPI_CAPABILITY_CATALOG_TABLES
    ]
    catalog_tables = [
        table for table in tables
        if table["name"] in OPENAPI_CAPABILITY_CATALOG_TABLES
    ]
    for table in regular_tables:
        out.append(render_table(profile, table, index_names))
        out.append("")
    if catalog_tables:
        out.append("-- openapi-capability-catalog:begin")
        out.append("")
        for table in catalog_tables:
            out.append(render_table(profile, table, index_names))
            out.append("")
        out.append("-- openapi-capability-catalog:end")
        out.append("")

    foreign_keys = render_foreign_key_statements(profile, tables)
    if foreign_keys:
        out.append("-- Foreign keys（建表之后统一添加：表按名字母序输出，"
                   "被引用的表可能排在后面）")
        out.append("")
        out.extend(foreign_keys)
        out.append("")

    seed_sections = []
    for name in sorted(seeds):
        table = seeded.get(name)
        if table is None:
            continue
        text = render_seed(profile, table, seeds[name])
        if text:
            seed_sections.append(text)
    if seed_sections:
        out.append("")
        out.append("-- Release baseline data")
        out.append("")
        out.extend(seed_sections)

    watermarks = sequence_watermarks(profile, tables, seeds)
    if watermarks:
        out.append("")
        out.append("-- 自增水位：种子行用的是显式 id，PostgreSQL 的序列不会自己前进，")
        out.append("-- 不补这一步，全新装环境第一次 INSERT 就会撞主键。")
        out.extend(watermarks)

    out.append("")
    return "\n".join(out).rstrip("\n") + "\n"


# --------------------------------------------------------------------------
# entry point
# --------------------------------------------------------------------------

OUTPUTS = {
    "mysql": DATABASE_DIR / "uvp-gb28181.sql",
    "postgresql": DATABASE_DIR / "postgresql_converted.sql",
    "sqlserver": DATABASE_DIR / "sqlserver_converted.sql",
}

# These tables form the durable OpenAPI capability publication projection.
# Keep the block markers in generated baselines so contract tests and release
# tooling can locate the schema without depending on dialect-specific quoting.
OPENAPI_CAPABILITY_CATALOG_TABLES = {
    "sys_openapi_capability",
    "sys_openapi_capability_group",
    "sys_openapi_operation",
    "sys_openapi_release",
    "sys_openapi_release_item",
    "sys_openapi_runtime_state",
}
# `sqlite` 有 Profile 但**不在交付集**：develop 线上没有 SQLite 运行时底座
# （无 internal/sqlitebootstrap、无 sqlitebaseline/ 目录），产物无处落。
# 保留 Profile 是为了将来接国产库/单机版时能直接复用，别再误以为它是交付件。

# SQLite 基线还要配一个 Go 包：运行时按「版本号 + 整脚本 SHA-256」决定是否落库。
# 版本号由脚本内容派生 —— 内容一变版本就变，已初始化的库不会被判成"篡改"而拒绝启动，
# 增量由 sqlitebootstrap.Migrate 负责。
SQLITE_BASELINE_GO = DATABASE_DIR / "sqlitebaseline" / "baseline.go"


def build_sqlite_baseline_go(sql_text: str) -> str:
    digest = hashlib.sha256(sql_text.encode("utf-8")).hexdigest()
    return f'''// Code generated by server/resource/database/baseline/generate_sql.py; DO NOT EDIT.
//
// 本文件与同目录 baseline.sql 同源：SQL 变则 Version 与 SHA256 一起变。

// Package sqlitebaseline 提供内置 SQLite 库的建库脚本与它的身份标识。
package sqlitebaseline

import _ "embed"

// Version 是基线版本号，取自 baseline.sql 的 SHA-256 前 12 位。
const Version = "sqlite-baseline-{digest[:12]}"

// SHA256 是 baseline.sql 的 SHA-256（十六进制小写）。
const SHA256 = "{digest}"

// SQL 是完整的建库 + 系统种子批次。调用方负责外层事务与迁移标记。
//
//go:embed baseline.sql
var SQL string
'''


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--check", action="store_true",
                        help="report drift instead of writing")
    parser.add_argument("--only", action="append", choices=sorted(OUTPUTS),
                        help="regenerate only the named dialect(s)")
    args = parser.parse_args()

    ir = json.loads(IR_FILE.read_text(encoding="utf-8"))
    seeds = {}
    for path in sorted(SEEDS_DIR.glob("*.jsonl")):
        seeds[path.stem] = [
            json.loads(line) for line in path.read_text(encoding="utf-8").splitlines() if line.strip()
        ]

    policy = json.loads(POLICY_FILE.read_text(encoding="utf-8"))
    check_overrides = policy.get("check_overrides", {})

    dialects = args.only or sorted(OUTPUTS)
    outputs = []
    for dialect in dialects:
        text = build_document(dialect, ir, seeds, check_overrides)
        outputs.append((dialect, OUTPUTS[dialect], text))
        if dialect == "sqlite":
            outputs.append(("sqlite(go)", SQLITE_BASELINE_GO, build_sqlite_baseline_go(text)))

    drift = []
    for dialect, target, text in outputs:
        if args.check:
            if not target.exists() or target.read_text(encoding="utf-8") != text:
                drift.append(f"{target.relative_to(REPO_ROOT)} 与 IR 不一致")
        else:
            target.write_text(text, encoding="utf-8")
        relative = target.relative_to(REPO_ROOT)
        if target.suffix == ".go":
            print(f"   {dialect:12s} 基线版本与校验和 → {relative}")
        else:
            tables = len(ir["tables"])
            rows = sum(len(v) for v in seeds.values())
            print(f"   {dialect:12s} {tables} 表 / {rows} 种子行 → {relative}"
                  f"  ({len(text.encode('utf-8')):>9,} bytes)")

    if args.check:
        if drift:
            print("\n⛔ 生成物与 IR 不一致:")
            for line in drift:
                print("   ", line)
            return 1
        print("\n✅ 生成物与 IR 一致")
    return 0


if __name__ == "__main__":
    sys.exit(main())
