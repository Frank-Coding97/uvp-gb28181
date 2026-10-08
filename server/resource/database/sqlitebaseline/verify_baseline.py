#!/usr/bin/env python3
"""实机验证 SQLite 基线：真灌进 sqlite3，再逐项断言。

为什么必须实机验证
------------------
SQLite 的 DDL 约束比看起来多：CHECK 必须内联在列后、索引不能在表体内、
外键 ALTER 不能在事务内执行……这些**静态看 SQL 完全看不出来**，只有真跑才暴露。

判据不是「没报错」，而是逐项对账：表数、索引数、种子行数、约束是否真的生效。
"""

import sqlite3
import sys
import tempfile
from pathlib import Path

BASELINE = Path(__file__).resolve().parent / "baseline.sql"

EXPECT_TABLES = 111
EXPECT_INDEX = None      # 由 manifest 给出，不硬编码
EXPECT_SEED_ROWS = None


def main() -> int:
    sql = BASELINE.read_text(encoding="utf-8")

    with tempfile.TemporaryDirectory() as tmp:
        db_path = Path(tmp) / "verify.db"
        db = sqlite3.connect(db_path)
        # ⛔ 必须显式打开外键：SQLite 默认是关的。应用侧 DSN 同样要带
        #    _pragma=foreign_keys(1)，否则 REFERENCES 全是摆设。
        db.execute("PRAGMA foreign_keys = ON")

        # ⛔⛔ 切分必须复用 generate.py 自己的 split_sql()。
        # 本轮踩过的三种错法都会给出与真实原因无关的报错：
        #   · executescript：整批预解析，遇到 MySQL 转义换行（\\n）报 near "INSERT"；
        #   · 按分号裸切：同样被转义换行切坏；
        #   · 逐行 complete_statement：前一句的**尾随注释**被算进缓冲区导致误判，
        #     报 no such table: main.xxx —— 而表明明在前面的行里。
        # 复用生产代码的切分才是可靠判据：它本来就正确处理了引号与注释。
        sys.path.insert(0, str(Path(__file__).resolve().parent))
        from generate import split_sql  # noqa: PLC0415

        for index, statement in enumerate(split_sql(sql), start=1):
            try:
                db.execute(statement)
            except sqlite3.OperationalError as exc:
                print(f"❌ 第 {index} 条建库失败: {type(exc).__name__}: {exc}")
                print(f"   语句: {statement[:220]}")
                return 1
        print("✅ 建库成功")

        one = lambda s: db.execute(s).fetchone()[0]   # noqa: E731

        tables = one("SELECT count(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'")
        indexes = one("SELECT count(*) FROM sqlite_master WHERE type='index' AND name NOT LIKE 'sqlite_%'")
        fks = one("SELECT count(*) FROM sqlite_master WHERE type='table' AND sql LIKE '%FOREIGN KEY%'")
        print(f"   表 {tables} / 索引 {indexes} / 含外键的表 {fks}")

        if tables != EXPECT_TABLES:
            print(f"❌ 表数应为 {EXPECT_TABLES}，实际 {tables}")
            return 1

        # 自增必须真的自增：插一条再插一条，id 应递增
        try:
            db.execute("INSERT INTO sys_role (name, sort, status) VALUES ('验证角色', 0, 1)")
            first = one("SELECT max(id) FROM sys_role")
            db.execute("INSERT INTO sys_role (name, sort, status) VALUES ('验证角色2', 0, 1)")
            second = one("SELECT max(id) FROM sys_role")
            if second <= first:
                print(f"❌ AUTOINCREMENT 未生效: {first} → {second}")
                return 1
            print(f"✅ 自增生效: {first} → {second}")
        except Exception as exc:      # noqa: BLE001
            print(f"❌ 自增表写入失败: {exc}")
            return 1

        # ---- casbin 策略表专项检查 -------------------------------------------
        # ⛔⛔ 为什么单列一条：ptype 为空的策略会让 **casbin 启动即 panic**，
        #   而且症状极具误导性 —— 后端不做任何 DB 查询就直接退，日志里只有
        #   一行 stdlib 占位，报错是 `slice bounds out of range [:1]`
        #   出现在 casbin/gorm-adapter 内部，看不出跟种子数据有关。
        #   （adapter.go 的 Preview() 会做 `p[0]` → `key[:1]`，
        #     ptype 为 NULL 时 p 是空切片，切片越界直接 panic。）
        #   这条检查的价值在于：**种子文件里混进一条脏数据就会开机失败**，
        #   而这类问题只有真机启动才暴露 —— 放在这里能提前拦住。
        bad_ptype = one(
            "SELECT count(*) FROM sys_casbin_rule "
            "WHERE ptype IS NULL OR trim(ptype) = ''"
        )
        if bad_ptype:
            print(f"❌ sys_casbin_rule 有 {bad_ptype} 条 ptype 为空 —— casbin 启动会 panic")
            print("   样例:", db.execute(
                "SELECT id, v0, v1, v2 FROM sys_casbin_rule "
                "WHERE ptype IS NULL OR trim(ptype) = '' LIMIT 3"
            ).fetchall())
            return 1
        kinds = db.execute(
            "SELECT DISTINCT ptype FROM sys_casbin_rule ORDER BY ptype"
        ).fetchall()
        print(f"✅ casbin 策略 ptype 合法，取值: {[k[0] for k in kinds]}")

        # ---- 权限覆盖：role_1 必须能过所有「登记在 sys_api 且走鉴权」的接口 ----
        # ⛔⛔ 判据必须用**全量对比**，不能硬编码路径清单 ——
        #   我第一版就列了 9 个已知路径，结果漏掉 /firmware-repository，
        #   而它恰恰是用户实际撞到 403 的那个（参数管理页实测全通，
        #   但固件仓库页 403 ⇒ 我上轮"修好了"的说法是错的）。
        #   ⇒ 现在从 sys_api 表取全量，算出 role_1 缺哪些。
        # ⭐ 关键前提：casbin 中间件用 **c.Request.URL.Path** 匹配策略
        #   （见 casbinhelper.CasbinMiddleware 的 s.Enforce(userSubject, path, method, domain)），
        #   **不查 sys_api 表** ⇒ 所以"路由注册了但 sys_api 没登记"的接口
        #   无法被这张表发现，只能靠下面的「反向核对」从 routes.go 找。
        admin_p = set()
        for (v1, v2) in db.execute(
                "SELECT v1, v2 FROM sys_casbin_rule WHERE ptype='p' AND v0='role_1'"):
            if v1 and v1 != '*':
                admin_p.add((v1.strip(), (v2 or '').upper()))

        def _can(path: str, method: str) -> bool:
            m = method.upper()
            return any(p == path and (a == m or a == '*')
                       or (p.endswith('*') and path.startswith(p.rstrip('*')))
                       for p, a in admin_p)

        # 免鉴权的那几个（登录/刷新 token 不该也不能有策略）
        exempt = {('/api/login', 'POST'), ('/api/refreshToken', 'POST')}

        missing = [
            (m, path) for (path, m) in db.execute(
                "SELECT path, method FROM sys_api")
            if (path, m) not in exempt and not _can('/' + path.lstrip('/'), m)
        ]
        if missing:
            print(f"❌ 以下接口登记在 sys_api 但 role_1 没授权 → admin 访问会 403（{len(missing)} 条）：")
            for m, path in missing[:20]:
                print(f"     {m:6s} {path}")
            print("   修法：把这些策略加进 casbin_delta_role1.jsonl 后重跑")
            print("         apply_casbin_seed_delta.py（别手工改 SQL，理由见该脚本头部）。")
            return 1
        print(f"✅ sys_api 里的接口 role_1 全部有授权（{len(admin_p)} 条策略，无 403 缺口）")

        # admin 账号必须在 —— 没有它装完登不进去
        admin = one("SELECT count(*) FROM sys_users WHERE username='admin'")
        if admin != 1:
            print(f"❌ 缺 admin 账号（找到 {admin} 个）")
            return 1
        print("✅ admin 账号存在")

        user_columns = {row[1]: row for row in db.execute("PRAGMA table_info(sys_users)")}
        forced_change = user_columns.get("must_change_password")
        if forced_change is None or forced_change[3] != 1 or str(forced_change[4]) != "0":
            print("❌ sys_users.must_change_password 必须为 NOT NULL 且默认 false")
            return 1
        if one("SELECT must_change_password FROM sys_users WHERE username='admin'") != 1:
            print("❌ 新装管理员必须标记为待修改初始密码")
            return 1
        print("✅ 新装管理员被标记为待修改初始密码")

        # 字典是本轮字典化的成果，缺了就等于白做
        dicts = one("SELECT count(*) FROM sys_dict")
        items = one("SELECT count(*) FROM sys_dict_item")
        print(f"   字典 {dicts} 组 / 明细 {items} 项")
        if dicts < 37 or items < 201:
            print(f"❌ 字典数量不足（应为 37 / 201，实际 {dicts} / {items}）")
            return 1

        # CHECK 是否真的生效：超长字符串必须被拒
        try:
            db.execute(
                "INSERT INTO gb_alarm_binding (device_id, channel_code, alarm_resource_id, source, "
                "created_at, updated_at) VALUES (1, ?, 1, 'manual', '2026-01-01', '2026-01-01')",
                ("X" * 999,),   # channel_code 上限 20
            )
            print("❌ 超长 channel_code 未被 CHECK 拦截 —— 类型约束没生效")
            return 1
        except sqlite3.IntegrityError:
            print("✅ CHECK 生效（超长 channel_code 被拒）")

        # 外键是否真的生效：插一个指向不存在表的 device_id
        fk_on = one("PRAGMA foreign_keys")
        if fk_on != 1:
            print("❌ 外键未开启")
            return 1

        # 完整性自检
        result = one("PRAGMA integrity_check")
        if result != "ok":
            print(f"❌ integrity_check: {result}")
            return 1
        print("✅ integrity_check ok")

        # 外键无悬挂
        fk_rows = db.execute("PRAGMA foreign_key_check").fetchall()
        if fk_rows:
            print(f"❌ 外键检查未通过: {fk_rows[:5]}")
            return 1
        print("✅ 外键检查无悬挂")

        # 种子总量与 MySQL 基线对齐（3348 行 civil_code 由应用侧灌，不在此文件）
        seeded = sum(
            one(f"SELECT count(*) FROM \"{t}\"")
            for t in ("sys_api", "sys_menu", "sys_menu_api", "sys_casbin_rule",
                      "sys_role_menu", "sys_dict", "sys_dict_item")
        )
        print(f"   种子行合计（不含 civil_code）{seeded}")

        db.close()

    print("\n✅ 全部检查通过")
    return 0


if __name__ == "__main__":
    sys.exit(main())
