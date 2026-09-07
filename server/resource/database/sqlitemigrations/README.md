# SQLite 增量迁移

当前白名单包含 `2026-09-07-zlm-media-identity-sqlite.sql`，补齐冻结基线缺失的媒体身份字段及七列唯一键；已有身份保持空值，不伪造回填。启动时必须先由
`sqlitebaseline` 写入匹配的基线版本和 SHA-256 marker；空库、基线前数据库、未知版本和
已应用 checksum 不一致都会被拒绝，不能自动初始化或猜测迁移历史。

后续增量迁移必须提供明确的 `<version>-sqlite.sql` 文件，并在
`server/internal/sqlitebootstrap` 的有序白名单中登记独立 SHA-256。迁移脚本由
SQLite 驱动一次执行完整 batch，事务由引擎控制；脚本不能包含 `BEGIN`、`COMMIT`、
`ROLLBACK`、`SAVEPOINT`、`PRAGMA`、`ATTACH`、`DETACH` 或 `VACUUM`。

当前实现不支持 trigger。包含 trigger 的脚本会明确拒绝，不能通过拆分语句静默执行；若
未来需要 trigger，必须先补充专门的迁移能力和审计合同。
