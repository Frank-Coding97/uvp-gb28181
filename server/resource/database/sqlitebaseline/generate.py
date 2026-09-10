#!/usr/bin/env python3
"""Build the checked-in SQLite baseline from the release SQL sources.

This generator is intentionally kept beside the generated artifact.  It does
not mutate any release source file; it reads the MySQL release schema and the
four post-release DDL/permission inputs and writes baseline.sql.
"""

from __future__ import annotations

import hashlib
import json
import re
from collections import Counter
from pathlib import Path


ROOT = Path(__file__).resolve().parent
SOURCE = ROOT.parent / "uvp-gb28181.sql"
MIGRATIONS = [
    ROOT.parent / "gb28181" / "migrations" / "2026-08-15-device-grant-table.sql",
    ROOT.parent / "gb28181" / "migrations" / "2026-08-15-device-traffic.sql",
    ROOT.parent / "gb28181" / "migrations" / "2026-08-21-device-traffic-hourly.sql",
    ROOT.parent / "gb28181" / "migrations" / "2026-09-02-home-dashboard.sql",
]
BASELINE_TIMESTAMP = "2026-09-07 00:00:00"
VERSION = "sqlite-baseline-20260910-ren-r4"
SOURCE_COMMIT = "fdcb63da29c320b0962157574e98f851a751ff41"


def split_sql(text: str) -> list[str]:
    """Split SQL at semicolons outside quoted strings and comments."""
    result: list[str] = []
    start = 0
    i = 0
    quote: str | None = None
    line_comment = False
    block_comment = False
    while i < len(text):
        char = text[i]
        next_char = text[i + 1] if i + 1 < len(text) else ""
        if line_comment:
            if char == "\n":
                line_comment = False
        elif block_comment:
            if char == "*" and next_char == "/":
                block_comment = False
                i += 1
        elif quote:
            if char == "\\":
                i += 1
            elif char == quote:
                if i + 1 < len(text) and text[i + 1] == quote:
                    i += 1
                else:
                    quote = None
        else:
            if char in "'\"`":
                quote = char
            elif char == "-" and next_char == "-":
                line_comment = True
                i += 1
            elif char == "/" and next_char == "*":
                block_comment = True
                i += 1
            elif char == ";":
                statement = text[start:i].strip()
                if statement:
                    result.append(statement)
                start = i + 1
        i += 1
    statement = text[start:].strip()
    if statement:
        result.append(statement)
    return result


def without_leading_comments(statement: str) -> str:
    while True:
        updated = re.sub(r"\A\s*(?:--[^\n]*(?:\n|$)|/\*.*?\*/\s*)", "", statement, count=1, flags=re.S)
        if updated == statement:
            return statement.strip()
        statement = updated


def strip_comments(statement: str) -> str:
    statement = re.sub(r"--[^\n]*(?:\n|$)", "\n", statement)
    return re.sub(r"/\*.*?\*/", " ", statement, flags=re.S)


def split_top_level(text: str) -> list[str]:
    result: list[str] = []
    start = 0
    depth = 0
    quote: str | None = None
    i = 0
    while i < len(text):
        char = text[i]
        if quote:
            if char == "\\":
                i += 1
            elif char == quote:
                if i + 1 < len(text) and text[i + 1] == quote:
                    i += 1
                else:
                    quote = None
        else:
            if char in "'\"`":
                quote = char
            elif char == "(":
                depth += 1
            elif char == ")":
                depth -= 1
            elif char == "," and depth == 0:
                part = text[start:i].strip()
                if part:
                    result.append(part)
                start = i + 1
        i += 1
    part = text[start:].strip()
    if part:
        result.append(part)
    return result


def matching_close(text: str, open_index: int) -> int:
    depth = 0
    quote: str | None = None
    i = open_index
    while i < len(text):
        char = text[i]
        if quote:
            if char == "\\":
                i += 1
            elif char == quote:
                if i + 1 < len(text) and text[i + 1] == quote:
                    i += 1
                else:
                    quote = None
        else:
            if char in "'\"`":
                quote = char
            elif char == "(":
                depth += 1
            elif char == ")":
                depth -= 1
                if depth == 0:
                    return i
        i += 1
    raise ValueError("unbalanced SQL parentheses")


def ident(name: str) -> str:
    return '"' + name.replace('"', '""') + '"'


def remove_mysql_quoting(text: str) -> str:
    return text.replace("`", '"')


def replace_role_concat(text: str) -> str:
    """Translate the release dump's nested CONCAT('role_', expr) calls."""
    offset = 0
    while True:
        match = re.search(r"(?i)\bCONCAT\s*\(", text[offset:])
        if match is None:
            return text
        start = offset + match.start()
        open_index = offset + match.end() - 1
        close_index = matching_close(text, open_index)
        arguments = split_top_level(text[open_index + 1 : close_index])
        if len(arguments) == 2 and arguments[0].strip() == "'role_'":
            replacement = "('role_' || " + arguments[1].strip() + ")"
            text = text[:start] + replacement + text[close_index + 1 :]
            offset = start + len(replacement)
        else:
            # Only the role-key form is present in the accepted input.  Leave
            # any future unrelated CONCAT expression visible for review.
            offset = close_index + 1


def normalize_scalar_sql(text: str) -> str:
    text = remove_mysql_quoting(text)
    text = re.sub(r"(?i)(?<![A-Za-z0-9_])_utf8mb4(?=')", "", text)
    text = re.sub(r"(?i)\bNOW\(\)", "'" + BASELINE_TIMESTAMP + "'", text)
    text = re.sub(r"(?i)\bCURRENT_TIMESTAMP(?:\(\d+\))?", "'" + BASELINE_TIMESTAMP + "'", text)
    text = text.replace("\\'", "''")
    return replace_role_concat(text)


def source_column_type(source_column: str) -> tuple[str, str | None]:
    match = re.match(
        r"[`\"]?[A-Za-z0-9_]+[`\"]?\s+([A-Za-z]+)(?:\s*\(([^)]*)\))?",
        source_column,
    )
    if match is None:
        raise ValueError(f"cannot identify column type: {source_column}")
    return match.group(1).upper(), match.group(2)


def integer_check(column_name: str, source_column: str) -> str | None:
    """Keep integer storage class and the representable MySQL range."""
    comment_start = re.search(r"(?i)\s+COMMENT\s+", source_column)
    type_part = source_column[: comment_start.start() if comment_start else len(source_column)]
    base, _ = source_column_type(type_part)
    ranges = {
        "TINYINT": (-128, 127, 255),
        "SMALLINT": (-32768, 32767, 65535),
        "MEDIUMINT": (-8388608, 8388607, 16777215),
        "INT": (-2147483648, 2147483647, 4294967295),
        "INTEGER": (-2147483648, 2147483647, 4294967295),
        "BIT": (0, 1, 1),
    }
    if base not in {"BIGINT", "TINYINT", "SMALLINT", "MEDIUMINT", "INT", "INTEGER", "BIT", "BOOLEAN", "BOOL"}:
        return None
    unsigned = bool(re.search(r"(?i)\bUNSIGNED\b", type_part))
    if base in ranges:
        signed_min, signed_max, unsigned_max = ranges[base]
        minimum, maximum = (0, unsigned_max) if unsigned else (signed_min, signed_max)
    elif base == "BIGINT":
        # SQLite INTEGER is signed 64-bit.  BIGINT UNSIGNED therefore has a
        # deliberate representable range of 0..MaxInt64; signed BIGINT already
        # has the same storage range and needs only the storage-class check.
        minimum, maximum = (0, 9223372036854775807) if unsigned else (None, None)
    else:
        # MySQL BOOLEAN/BOOL has no narrower range in this authority schema.
        minimum, maximum = None, None
    terms = [f"typeof({ident(column_name)}) = 'integer'"]
    if minimum is not None:
        terms.append(f"{ident(column_name)} BETWEEN {minimum} AND {maximum}")
    return f"CHECK ({ident(column_name)} IS NULL OR ({' AND '.join(terms)}))"


def length_check(column_name: str, source_column: str) -> str | None:
    """Keep source VARCHAR/CHAR/VARBINARY length limits after SQLite mapping."""
    base, arguments = source_column_type(source_column)
    if base not in {"VARCHAR", "CHAR", "VARBINARY"} or arguments is None or not arguments.isdigit():
        return None
    limit = int(arguments)
    value = ident(column_name)
    if base == "VARBINARY":
        value = f"CAST({value} AS BLOB)"
    return f"CHECK ({ident(column_name)} IS NULL OR length({value}) <= {limit})"


def json_check(column_name: str, source_column: str) -> str | None:
    """Keep JSON validity while allowing the source column's NULL default."""
    base, _ = source_column_type(source_column)
    if base != "JSON":
        return None
    return f'CHECK ({ident(column_name)} IS NULL OR json_valid({ident(column_name)}))'


def normalize_column(column: str) -> tuple[str, bool]:
    column = remove_mysql_quoting(column.strip())
    column = re.sub(r"(?i)(?<![A-Za-z0-9_])_utf8mb4(?=')", "", column)
    column = column.replace("\\'", "''")
    column = re.sub(r"(?i)\bCURRENT_TIMESTAMP\(\d+\)", "CURRENT_TIMESTAMP", column)
    auto_increment = bool(re.search(r"(?i)\bAUTO_INCREMENT\b", column))
    inline_primary = bool(re.search(r"(?i)\bPRIMARY\s+KEY\b", column))
    column = re.sub(r"(?i)\bAUTO_INCREMENT\b", "", column)
    column = re.sub(r"(?i)\bUNSIGNED\b", "", column)
    column = re.sub(r"(?i)\s+COMMENT\s+'(?:''|[^'])*'", "", column)
    column = re.sub(r"(?i)\s+CHARACTER\s+SET\s+\w+", "", column)
    column = re.sub(r"(?i)\s+COLLATE\s+\w+", "", column)
    column = re.sub(r"(?i)\s+ON\s+UPDATE\s+'[^']*'", "", column)
    column = re.sub(r"(?i)\s+ON\s+UPDATE\s+CURRENT_TIMESTAMP", "", column)
    replacements = [
        (r"(?i)\b(?:BIGINT|INT|INTEGER|MEDIUMINT|SMALLINT|TINYINT)(?:\(\d+\))?(?![A-Za-z0-9_])", "INTEGER"),
        (r"(?i)\b(?:DECIMAL|NUMERIC)\s*\(\s*\d+\s*,\s*\d+\s*\)(?![A-Za-z0-9_])", "NUMERIC"),
        (r"(?i)\b(?:DOUBLE|REAL|FLOAT)(?:\s*\(\s*\d+\s*,\s*\d+\s*\))?(?![A-Za-z0-9_])", "REAL"),
        (r"(?i)\b(?:BOOLEAN|BOOL|BIT)(?:\(\d+\))?(?![A-Za-z0-9_])", "INTEGER"),
        (r"(?i)\bVARBINARY(?:\(\d+\))?(?![A-Za-z0-9_])", "BLOB"),
        (r"(?i)\b(?:VARCHAR|CHAR|TINYTEXT|MEDIUMTEXT|LONGTEXT|JSON|TEXT)(?:\(\d+\))?(?![A-Za-z0-9_])", "TEXT"),
        (r"(?i)\b(?:DATETIME|TIMESTAMP|DATE)(?:\(\d+\))?(?![A-Za-z0-9_])", lambda match: match.group(0).split("(", 1)[0].upper()),
        # modernc.org/sqlite parses DATE, DATETIME and TIMESTAMP as time.Time
        # when _time_format=sqlite is enabled.  TIME is deliberately TEXT:
        # the driver returns a clock value as a string for that declared type.
        (r"(?i)\bTIME(?:\(\d+\))?(?![A-Za-z0-9_])", "TEXT"),
    ]
    for pattern, replacement in replacements:
        column = re.sub(pattern, replacement, column)
    if auto_increment and inline_primary:
        column = re.sub(r"(?i)\s+PRIMARY\s+KEY\b", "", column)
        column = re.sub(r"(?i)\s+NOT\s+NULL\b", "", column)
        column += " PRIMARY KEY AUTOINCREMENT"
    column = re.sub(r"\s+", " ", column).strip()
    return column, auto_increment


def normalize_index_columns(columns: str) -> str:
    columns = normalize_scalar_sql(columns)
    columns = re.sub(r"(\"[A-Za-z0-9_]+\")\s*\(\s*\d+\s*\)", r"\1", columns)
    return columns


def parse_create(statement: str) -> tuple[str, str, list[str], list[tuple[str, str, bool]]]:
    clean = without_leading_comments(statement)
    match = re.match(
        r"(?is)CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?[`\"]?([A-Za-z0-9_]+)[`\"]?\s*\(",
        clean,
    )
    if not match:
        raise ValueError(f"not a CREATE TABLE statement: {clean[:100]}")
    name = match.group(1)
    open_index = clean.find("(", match.start())
    close_index = matching_close(clean, open_index)
    body = clean[open_index + 1 : close_index]
    items = split_top_level(body)
    columns: list[str] = []
    indexes: list[tuple[str, str, bool]] = []
    primary_columns: list[str] | None = None
    auto_columns: list[str] = []
    constraints: list[str] = []
    for item in items:
        stripped = item.strip()
        index_match = re.match(
            r"(?is)(UNIQUE\s+)?KEY\s+[`\"]?([A-Za-z0-9_]+)[`\"]?\s*\((.*?)\)(?:\s+USING\s+BTREE)?\s*$",
            stripped,
        )
        if index_match:
            indexes.append((index_match.group(2), normalize_index_columns(index_match.group(3)), bool(index_match.group(1))))
            continue
        primary_match = re.match(r"(?is)PRIMARY\s+KEY\s*\((.*?)\)(?:\s+USING\s+BTREE)?\s*$", stripped)
        if primary_match:
            primary_columns = [c.strip().strip('`"') for c in split_top_level(primary_match.group(1))]
            constraints.append("PRIMARY KEY (" + normalize_index_columns(primary_match.group(1)) + ")")
            continue
        if re.match(r"(?is)(?:CONSTRAINT\s+[`\"]?\w+[`\"]?\s+)?(?:FOREIGN\s+KEY|CHECK)", stripped):
            normalized = normalize_column(stripped)[0]
            constraints.append(normalized)
            continue
        column_match = re.match(r"[`\"]?([A-Za-z0-9_]+)[`\"]?\s+", stripped)
        if column_match is None:
            raise ValueError(f"cannot identify column definition: {stripped}")
        column_name = column_match.group(1)
        normalized, auto = normalize_column(stripped)
        columns.append(normalized)
        for check in (
            integer_check(column_name, stripped),
            length_check(column_name, stripped),
            json_check(column_name, stripped),
        ):
            if check is not None:
                constraints.append(check)
        if auto:
            auto_columns.append(column_name)
    if primary_columns and len(primary_columns) == 1 and primary_columns[0] in auto_columns:
        primary = primary_columns[0]
        updated_columns: list[str] = []
        for column in columns:
            if re.match(rf"(?is)[`\"]?{re.escape(primary)}[`\"]?\s+", column):
                column = re.sub(r"(?i)\s+NOT\s+NULL\b", "", column)
                column = re.sub(r"(?i)\s+DEFAULT\s+[^ ]+", "", column)
                column = re.sub(rf"(?is)^([`\"]?{re.escape(primary)}[`\"]?)\s+", r"\1 ", column)
                column += " PRIMARY KEY AUTOINCREMENT"
            updated_columns.append(column)
        columns = updated_columns
        constraints = [c for c in constraints if not re.match(r"(?is)PRIMARY\s+KEY", c)]
    return name, ",\n  ".join(columns + constraints), indexes, [(c, "", False) for c in auto_columns]


def normalize_update_delete(statement: str) -> str:
    statement = normalize_scalar_sql(statement)
    delete_match = re.match(
        r"(?is)\bDELETE\s+([A-Za-z0-9_]+)\s+FROM\s+([`\"]?[A-Za-z0-9_]+[`\"]?)\s+\1\s+JOIN\s+([`\"]?[A-Za-z0-9_]+[`\"]?)\s+([A-Za-z0-9_]+)\s+ON\s+(.*?)\s+WHERE\s+(.*)",
        statement,
    )
    if delete_match:
        statement = (
            f"DELETE FROM {delete_match.group(2)} AS {delete_match.group(1)} "
            f"WHERE EXISTS (SELECT 1 FROM {delete_match.group(3)} {delete_match.group(4)} "
            f"WHERE {delete_match.group(5)} AND {delete_match.group(6)})"
        )
    update_match = re.match(
        r"(?is)\bUPDATE\s+([`\"]?[A-Za-z0-9_]+[`\"]?)\s+([A-Za-z0-9_]+)\s+JOIN\s+([`\"]?[A-Za-z0-9_]+[`\"]?)\s+([A-Za-z0-9_]+)\s+ON\s+(.*?)\s+SET\s+(.*?)\s+WHERE\s+(.*)",
        statement,
    )
    if update_match:
        target_alias = update_match.group(2)
        joined_table = update_match.group(3)
        joined_alias = update_match.group(4)
        join_condition = update_match.group(5)
        assignments = re.sub(rf"(?i)([`\"]?{re.escape(target_alias)}[`\"]?)\s*\.\s*", "", update_match.group(6))
        joined_value = (
            f"(SELECT {ident(joined_alias)}.{ident('id')} FROM {joined_table} AS {ident(joined_alias)} "
            f"WHERE {join_condition} LIMIT 1)"
        )
        assignments = re.sub(
            rf"(?i)([`\"]?{re.escape(joined_alias)}[`\"]?)\s*\.\s*([`\"]?[A-Za-z0-9_]+[`\"]?)",
            joined_value,
            assignments,
        )
        where_clause = re.sub(rf"(?i)([`\"]?{re.escape(target_alias)}[`\"]?)\s*\.\s*", "", update_match.group(7))
        statement = (
            f"UPDATE {update_match.group(1)} SET {assignments} "
            f"WHERE {where_clause} AND EXISTS (SELECT 1 FROM {joined_table} {joined_alias} "
            f"WHERE {join_condition})"
        )
    return statement


def classify(statement: str) -> str:
    clean = without_leading_comments(statement)
    match = re.match(r"(?i)(CREATE|DROP|INSERT|UPDATE|DELETE|SET)", clean)
    return match.group(1).upper() if match else "OTHER"


def insert_table(statement: str) -> str | None:
    match = re.match(r"(?is)INSERT\s+(?:OR\s+\w+\s+)?INTO\s+[`\"]?([A-Za-z0-9_]+)[`\"]?", without_leading_comments(statement))
    return match.group(1) if match else None


def main() -> None:
    source_statements: list[tuple[str, str]] = [(str(SOURCE), statement) for statement in split_sql(SOURCE.read_text())]
    for migration in MIGRATIONS:
        source_statements.extend((str(migration), statement) for statement in split_sql(migration.read_text()))

    create_statements: list[tuple[str, str, list[tuple[str, str, bool]]]] = []
    for filename, statement in source_statements:
        if classify(statement) != "CREATE":
            continue
        clean = without_leading_comments(statement)
        if not re.match(r"(?is)CREATE\s+TABLE\b", clean):
            continue
        name, body, indexes, _ = parse_create(statement)
        create_statements.append((name, body, indexes))
    # A few post-release migration files repeat a table already present in the
    # release dump.  Keep the first definition and reject conflicting copies.
    unique_creates: dict[str, tuple[str, list[tuple[str, str, bool]]]] = {}
    for name, body, indexes in create_statements:
        if name in unique_creates:
            old_body, old_indexes = unique_creates[name]
            if (old_body, old_indexes) != (body, indexes):
                raise ValueError(f"conflicting CREATE TABLE definitions for {name}")
            continue
        unique_creates[name] = (body, indexes)
    # The release dump predates the recovery scheduler guard.  Keep the
    # column from that dump and add the model-declared lookup index explicitly.
    meta_body, meta_indexes = unique_creates["meta_node"]
    if not any(index_name == "idx_recovery_required" for index_name, _, _ in meta_indexes):
        unique_creates["meta_node"] = (meta_body, [*meta_indexes, ("idx_recovery_required", '"recovery_required"', False)])
    index_occurrences = Counter(index_name for _, (_, indexes) in unique_creates.items() for index_name, _, _ in indexes)
    used_index_names: set[str] = set()
    index_names: dict[tuple[str, str], str] = {}
    for table, (_, indexes) in unique_creates.items():
        for original, _, _ in indexes:
            candidate = original if index_occurrences[original] == 1 else f"{table}__{original}"
            suffix = 2
            while candidate in used_index_names:
                candidate = f"{table}__{original}_{suffix}"
                suffix += 1
            used_index_names.add(candidate)
            index_names[(table, original)] = candidate

    variable_expressions: dict[str, str] = {}
    seed_statements: list[str] = []
    for _, raw in source_statements:
        kind = classify(raw)
        clean = without_leading_comments(raw)
        if kind == "SET":
            variable_match = re.match(r"(?is)SET\s+@([A-Za-z0-9_]+)\s*:=\s*(.*)", clean)
            if variable_match:
                expression = normalize_scalar_sql(variable_match.group(2)).strip()
                for variable, value in variable_expressions.items():
                    expression = re.sub(rf"@{re.escape(variable)}\b", f"({value})", expression)
                variable_expressions[variable_match.group(1)] = expression
            continue
        if kind in {"DROP", "OTHER"}:
            continue
        if kind == "CREATE":
            continue
        table = insert_table(clean)
        if table in {"sys_users", "sys_user_role", "sys_civil_code"} and kind == "INSERT":
            continue
        if kind in {"INSERT", "UPDATE", "DELETE"}:
            statement = normalize_update_delete(clean)
            statement = normalize_scalar_sql(statement)
            for variable, value in variable_expressions.items():
                statement = re.sub(rf"@{re.escape(variable)}\b", f"({value})", statement)
            statement = statement.replace("DELETE ma FROM", "DELETE FROM")
            seed_statements.append(statement)

    output: list[str] = [
        f"-- SQLite standalone baseline generated from release source {SOURCE_COMMIT[:8]}.",
        "-- SQLite storage mapping: integer ids/counters -> INTEGER; DATE/DATETIME/TIMESTAMP keep their declared type; JSON -> TEXT with json_valid checks; decimal -> NUMERIC.",
        "-- MySQL numeric ranges and VARCHAR/CHAR/VARBINARY lengths are represented by explicit checks where SQLite can preserve them.",
        "-- Application code owns timestamp updates; no environment rows or credentials are seeded here.",
        "PRAGMA foreign_keys = ON;",
        "PRAGMA busy_timeout = 5000;",
        "",
    ]
    for table, (body, indexes) in unique_creates.items():
        output.append(f"CREATE TABLE IF NOT EXISTS {ident(table)} (\n  {body}\n);")
        for original, columns, unique in indexes:
            index_name = index_names[(table, original)]
            unique_keyword = "UNIQUE " if unique else ""
            output.append(f"CREATE {unique_keyword}INDEX IF NOT EXISTS {ident(index_name)} ON {ident(table)} ({columns});")
        output.append("")
    output.append("-- Deterministic system seed and permission metadata. Civil-code rows are seeded by the Go initializer.")
    for statement in seed_statements:
        statement = remove_mysql_quoting(statement)
        output.append(statement + ";")
    output.append("")
    baseline = "\n".join(output)
    (ROOT / "baseline.sql").write_text(baseline, encoding="utf-8")
    sha256 = hashlib.sha256(baseline.encode()).hexdigest()
    tables = list(unique_creates)
    index_manifest = [
        {
            "table": table,
            "name": index_names[(table, original)],
            "source_name": original,
            "columns": columns,
            "unique": unique,
        }
        for table, (_, indexes) in unique_creates.items()
        for original, columns, unique in indexes
    ]
    index_count = len(index_manifest)
    seed_counts = Counter(classify(statement) for statement in seed_statements)
    seed_count = sum(seed_counts.values())
    manifest = {
        "version": VERSION,
        "source_commit": SOURCE_COMMIT,
        "sha256": sha256,
        "tables": len(tables),
        "table_names": tables,
        "indexes": index_count,
        "index_manifest": index_manifest,
        "seed_statements": seed_count,
        "seed_inserts": seed_counts["INSERT"],
        "seed_updates": seed_counts["UPDATE"],
        "seed_deletes": seed_counts["DELETE"],
        "seeded_tables": sorted({table for statement in seed_statements if (table := insert_table(statement))}),
        "inputs": {
            "uvp-gb28181.sql": hashlib.sha256(SOURCE.read_bytes()).hexdigest(),
            **{migration.name: hashlib.sha256(migration.read_bytes()).hexdigest() for migration in MIGRATIONS},
        },
    }
    (ROOT / "manifest.json").write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


if __name__ == "__main__":
    main()
