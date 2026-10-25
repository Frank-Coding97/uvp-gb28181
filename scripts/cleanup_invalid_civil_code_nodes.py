#!/usr/bin/env python3
"""清理 gb_catalog_node 里「行政区编码不合法」的脏节点（SQLite 单机库）。

为什么有这东西：某台验收环境上报过 `civil_code = 'Ci'` 这种厂商私有值，早期实现
不校验就建了链，于是在目录树根上留了一个叫「行政区 Ci」的节点（`code='Ci'`），
而且它下面可能挂着一串通道/设备目录项。

代码侧已经修好（`normalizeCivilCodeChain` 对非纯数字直接返回空串，不再建节点，
见 `catalog/civil_code_length_test.go`），但**存量那条还在库里**，本脚本负责清掉它。

清理动作（`--apply` 才真写）：
  1. 把脏节点的子树整体提升一层：`parent_id` 置空、`path` 剥掉 `/脏id/` 前缀、
     `depth` 减一 —— 不连坐删掉挂在它下面的真实资源；
  2. 软删脏节点本身（`deleted_at`），不做物理删除，留痕可回滚。

用法：
    python3 cleanup_invalid_civil_code_nodes.py /path/to/uvp.db            # 只盘点
    python3 cleanup_invalid_civil_code_nodes.py /path/to/uvp.db --apply    # 真清理
可选：
    --clear-channel-civil-codes   顺带把 gb_channel.civil_code 里的同类非法值清空
                                  （不清也安全：走 ProjectCivilCode 会落到「未知行政区」兜底，
                                  清空只是让脏值不再出现在导出/排查里）
"""
from __future__ import annotations

import argparse
import datetime
import re
import sqlite3
import sys

# GB/T 28181-2022 附录 J：行政区域目录项允许 2 / 4 / 6 / 8 位纯数字。
LEGAL_CIVIL_CODE = re.compile(r"^(?:\d{2}|\d{4}|\d{6}|\d{8})$")
TABLE = "gb_catalog_node"


def invalid_reason(code: str, civil_code: str) -> str:
    """返回空串表示合法；否则给出人读的原因。"""
    raw = (code or "").strip()
    wide = (civil_code or "").strip()
    if raw and not LEGAL_CIVIL_CODE.match(raw):
        return f"code={raw!r} 不是 2/4/6/8 位纯数字"
    if wide and not LEGAL_CIVIL_CODE.match(wide):
        return f"civil_code={wide!r} 不是 2/4/6/8 位纯数字"
    if not raw and not wide:
        return "code 与 civil_code 都为空"
    return ""


def has_column(cur: sqlite3.Cursor, table: str, column: str) -> bool:
    return column in {row[1] for row in cur.execute(f'PRAGMA table_info("{table}")')}


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("db")
    parser.add_argument("--apply", action="store_true", help="真写库；缺省只盘点")
    parser.add_argument("--clear-channel-civil-codes", action="store_true")
    args = parser.parse_args()

    con = sqlite3.connect(args.db)
    con.row_factory = sqlite3.Row
    cur = con.cursor()
    cur.execute("SELECT name FROM sqlite_master WHERE type='table' AND name=?", (TABLE,))
    if cur.fetchone() is None:
        print(f"✗ 库里没有 {TABLE}，路径对吗？")
        return 1

    rows = list(
        cur.execute(
            f"SELECT id, owner_dept_id, parent_id, code, civil_code, name, path, depth "
            f"FROM {TABLE} WHERE node_type='civil_code' AND deleted_at IS NULL ORDER BY id"
        )
    )
    dirty = []
    for row in rows:
        reason = invalid_reason(row["code"], row["civil_code"])
        if reason:
            dirty.append((row, reason))

    print(f"· 行政区节点共 {len(rows)} 个，其中不合法 {len(dirty)} 个")
    for row, reason in dirty:
        prefix = f"/{row['id']}/"
        # 脏节点的父级路径段：子树要提升到这一层，不能一律当根（可能挂在别的目录下）。
        parent_prefix = (row["path"] or "").rstrip("/")
        if parent_prefix.endswith(prefix.rstrip("/")):
            parent_prefix = parent_prefix[: len(parent_prefix) - len(str(row["id"])) - 1]
        parent_prefix = parent_prefix.rstrip("/") + "/" if parent_prefix not in ("", "/") else "/"
        children = list(
            cur.execute(
                f"SELECT id, name, node_type FROM {TABLE} "
                f"WHERE owner_dept_id=? AND parent_id=? AND deleted_at IS NULL",
                (row["owner_dept_id"], row["id"]),
            )
        )
        subtree = list(
            cur.execute(
                f"SELECT id, path, depth FROM {TABLE} "
                f"WHERE owner_dept_id=? AND id<>? AND path LIKE ? AND deleted_at IS NULL",
                (row["owner_dept_id"], row["id"], prefix + "%"),
            )
        )
        print(f"  ✗ id={row['id']} name={row['name']!r} {reason}")
        print(f"    直接子节点 {len(children)} 个，整棵子树 {len(subtree)} 个")
        for child in children[:5]:
            print(f"      - [{child['node_type']}] {child['name']}")

        if not args.apply:
            continue

        # 子树整体提升一层：先改 path/depth（按行算），再把直接子节点改挂到脏节点的父级。
        for node in subtree:
            old = node["path"]
            if not old.startswith(prefix):
                continue
            new_path = parent_prefix + old[len(prefix):]
            cur.execute(
                f"UPDATE {TABLE} SET path=?, depth=? WHERE id=?",
                (new_path, max(0, int(node["depth"]) - 1), node["id"]),
            )
        cur.execute(
            f"UPDATE {TABLE} SET parent_id=?, depth=? WHERE owner_dept_id=? AND parent_id=?",
            (
                row["parent_id"],
                max(0, int(row["depth"])),
                row["owner_dept_id"],
                row["id"],
            ),
        )
        cur.execute(
            f"UPDATE {TABLE} SET deleted_at=? WHERE id=?",
            (datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%d %H:%M:%S.%f"), row["id"]),
        )
        print(f"    ✓ 已提升子树并软删 id={row['id']}")

    if args.clear_channel_civil_codes and has_column(cur, "gb_channel", "civil_code"):
        bad_channels = list(
            cur.execute("SELECT id, channel_id, civil_code FROM gb_channel WHERE civil_code IS NOT NULL AND civil_code<>''")
        )
        bad_channels = [c for c in bad_channels if not LEGAL_CIVIL_CODE.match(c["civil_code"].strip())]
        print(f"· gb_channel 中 civil_code 不合法的通道 {len(bad_channels)} 条")
        for channel in bad_channels[:10]:
            print(f"  - id={channel['id']} {channel['channel_id']} civil_code={channel['civil_code']!r}")
        if args.apply:
            for channel in bad_channels:
                cur.execute("UPDATE gb_channel SET civil_code='' WHERE id=?", (channel["id"],))
            print("  ✓ 已清空这些通道的 civil_code（目录树会落到「未知行政区」兜底）")

    if args.apply:
        con.commit()
        print("✓ 已提交")
    else:
        print("· 未写入（加 --apply 才真清理）")
    con.close()
    return 0


if __name__ == "__main__":
    sys.exit(main())
