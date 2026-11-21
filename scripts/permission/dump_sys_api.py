#!/usr/bin/env python3
"""导出 ``sys_api`` 权限快照，供 reconcile_routes.py 对账。

为什么需要它
------------
对账必须以**目标环境的真实库**为准，而不是仓库里的种子 ——
种子可能被手改过、活库可能有历史脏数据，两边不一致时以活库为准。

用法
----
    # SQLite（绿色包）
    python3 dump_sys_api.py sqlite /path/to/uvp.db > /tmp/permlive.json

    # MySQL（开发库），DSN 形如 user:pass@host:port/dbname
    python3 dump_sys_api.py mysql 'root:pwd@127.0.0.1:3306/uvp_gb28181' > /tmp/permlive.json

输出：``[[path, method], ...]``（已去重、method 大写）
"""

from __future__ import annotations

import json
import sqlite3
import sys


def from_sqlite(db_path: str) -> list[tuple[str, str]]:
    con = sqlite3.connect(db_path)
    try:
        rows = con.execute(
            "SELECT path, method FROM sys_api WHERE deleted_at IS NULL"
        ).fetchall()
    finally:
        con.close()
    return [(r[0], str(r[1]).upper()) for r in rows]


def from_mysql(dsn: str) -> list[tuple[str, str]]:
    #⛔ 不引第三方驱动做能力探测：缺 pymysql 时给出可执行的安装提示，
    #   而不是抛一堆 ImportError 让人自己猜（记忆里的教训：报错误导人方向）。
    try:
        import pymysql  # type: ignore
    except ImportError as exc:  # pragma: no cover
        raise SystemExit(
            "需要 pymysql 才能读 MySQL：\n"
            "  /Users/menglulu/.workbuddy/binaries/python/envs/default/bin/pip install pymysql\n"
            "或改用 sqlite 源（绿色包的库就是 SQLite）。"
        ) from exc

    # user:pass@host:port/dbname —— 密码含 @ 时请改用 socket 或自行改代码
    user, _, rest = dsn.partition(":")
    passwd, _, rest2 = rest.partition("@")
    hostport, _, db = rest2.partition("/")
    host, _, port = hostport.partition(":")
    con = pymysql.connect(host=host or "127.0.0.1",
                          port=int(port or 3306),
                          user=user, password=passwd, database=db, charset="utf8mb4")
    try:
        with con.cursor() as cur:
            cur.execute("SELECT path, method FROM sys_api WHERE deleted_at IS NULL")
            rows = cur.fetchall()
    finally:
        con.close()
    return [(r[0], str(r[1]).upper()) for r in rows]


def main() -> int:
    if len(sys.argv) != 3:
        print(__doc__)
        return 2
    kind, target = sys.argv[1], sys.argv[2]
    rows = {"sqlite": from_sqlite, "mysql": from_mysql}.get(kind)
    if rows is None:
        raise SystemExit(f"不支持的源类型：{kind}（可选 sqlite / mysql）")
    data = sorted(set(rows(target)))
    json.dump([list(x) for x in data], sys.stdout, ensure_ascii=False)
    print(f"  ← {len(data)} 条 sys_api 登记（{kind}）", file=sys.stderr)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
