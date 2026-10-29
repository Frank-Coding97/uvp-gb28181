-- 合并重复字段：`receive_host` 与 `sdp_ip` 原本都只是「写入 SDP 的 c= 地址」，
-- 同一件事的两个名字（兜底链里 sdp_ip 优先、receive_host 仅作第二顺位）。
-- 2026-10-20 起统一由 `sdp_ip` 承载，删掉旧列，避免继续出现「两个框填同一件事」。
--
-- ⛔ 不可逆。执行前必须确认该库已升级到不再读写 receive_host 的版本，
--    否则旧版本进程会因为 `SELECT ... receive_host` 失败而启动不了。
-- ⛔ 本迁移**不搬运数据**：sdp_ip 在 2026-10-09 已单独建列并成为优先字段，
--    现场若确实只在 receive_host 里填过值，请在删列前先按需
--    `UPDATE meta_node SET sdp_ip = receive_host WHERE sdp_ip = '' AND receive_host <> ''`。
--    本次开发库按老板口径「不考虑老数据兼容」直接删。
SET @ddl := IF((SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'meta_node' AND COLUMN_NAME = 'receive_host') > 0,
  'ALTER TABLE `meta_node` DROP COLUMN `receive_host`', 'SELECT 1');
PREPARE meta_node_drop_receive_host_stmt FROM @ddl;
EXECUTE meta_node_drop_receive_host_stmt;
DEALLOCATE PREPARE meta_node_drop_receive_host_stmt;
