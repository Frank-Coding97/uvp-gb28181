-- 回滚 2026-09-21-channel-position-source.sql:移除坐标来源两列。
--
-- ⚠️ 回滚会让"这个坐标是谁写的"这段信息永久丢失，且此后目录刷新会重新变成
--    无门禁覆盖（回到本次修复前的行为）。执行前确认确实要退回旧语义。
-- ⛔ 不动 longitude/latitude 本身 —— 它们是本次改动之前就存在的列。

ALTER TABLE `gb_channel`
  DROP COLUMN `position_updated_at`,
  DROP COLUMN `position_source`;
