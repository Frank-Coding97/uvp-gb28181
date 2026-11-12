#!/usr/bin/env python3
"""幂等补齐 gb_talk_session 的 3 个 OpenAPI 列与复合索引（SQLite 单机库）。

为什么需要它：发布基线（sqlitebaseline/baseline.sql）只在**建新库**时生效，
已经在跑的单机版库不会自动补列。而这两件事必须同时成立：
  1. 代码（models.GbTalkSession）按 43 列读写，repo 走 `Create(&row)`
     ⇒ 少一列，写对讲会话直接报 `no such column: openapi_client_id`；
  2. 生产代码里**没有 AutoMigrate**，没人替它补。

用法：
    python3 backfill_talk_openapi_columns.py /path/to/uvp.db [--dry-run]
"""
from __future__ import annotations

import argparse
import sqlite3
import sys

COLUMNS = [
    ("openapi_client_id", "INTEGER NOT NULL DEFAULT 0"),
    ("openapi_idempotency_key", "TEXT"),
    ("openapi_request_hash", "TEXT NOT NULL DEFAULT ''"),
]
INDEX = "idx_talk_session_openapi_idempotency"
TABLE = "gb_talk_session"


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("db")
    parser.add_argument("--dry-run", action="store_true")
    args = parser.parse_args()

    con = sqlite3.connect(args.db)
    cur = con.cursor()
    cur.execute("SELECT name FROM sqlite_master WHERE type='table' AND name=?", (TABLE,))
    if cur.fetchone() is None:
        print(f"✗ 库里没有 {TABLE}，路径对吗？")
        return 1

    existing = {row[1] for row in cur.execute(f'PRAGMA table_info("{TABLE}")')}
    added = []
    for name, ddl in COLUMNS:
        if name in existing:
            print(f"· 已存在，跳过 {name}")
            continue
        if args.dry_run:
            print(f"[dry-run] ALTER TABLE {TABLE} ADD COLUMN {name} {ddl}")
        else:
            cur.execute(f'ALTER TABLE "{TABLE}" ADD COLUMN "{name}" {ddl}')
        added.append(name)

    cur.execute("SELECT name FROM sqlite_master WHERE type='index' AND name=?", (INDEX,))
    has_index = cur.fetchone() is not None
    if has_index:
        print(f"· 已存在，跳过索引 {INDEX}")
    elif args.dry_run:
        print(f"[dry-run] CREATE INDEX {INDEX}")
    else:
        cur.execute(
            f'CREATE INDEX IF NOT EXISTS "{INDEX}" ON "{TABLE}" ("openapi_client_id", "openapi_idempotency_key")'
        )
        added.append(f"index:{INDEX}")

    if not args.dry_run:
        con.commit()
    total = len(list(cur.execute(f'PRAGMA table_info("{TABLE}")')))
    print(f"✓ {TABLE} 现有 {total} 列；本次新增 {added or '无'}")
    con.close()
    return 0


if __name__ == "__main__":
    sys.exit(main())
