-- 录像缓存任务：补「分段续播」状态列（cursor_at / segments）
-- Migration: 2026-10-03-record-cache-segment-state
--
-- ⚠️ 本文件只用于把新改动补进**已有的开发库**；全新环境由三方言全量脚本建库
--    （uvp-gb28181.sql / postgresql_converted.sql / sqlserver_converted.sql 已包含同样内容）。
--    与 2026-10-03-record-cache.sql 同一形态：仅 MySQL 方言、可重复执行。
--
-- 为什么需要这两列：GB28181 下载模式单会话有墙钟时限（默认 30 分钟），
-- 超过时限的录像段必须「拉一段 → 落一片 → 从断点续拉下一段」：
--   cursor_at 记录断点（下一片会话的起始时间），服务重启后能接着拉；
--   segments  记录每一片的产出文件清单（JSON 数组，含 ZLM 上的 stream 名），
--             因为每片是**独立会话 ⇒ 独立 stream ⇒ 独立目录**，
--             不记下来就再也找不回前几片落的盘。
-- 只有 cursor_at 没有 segments 的表现是「下载到的永远只有最后一片」。

-- ---- cursor_at ----
SET @table_exists := (
  SELECT COUNT(*) FROM information_schema.TABLES
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gb_record_cache_task'
);
SET @cursor_exists := (
  SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gb_record_cache_task' AND COLUMN_NAME = 'cursor_at'
);
-- ⛔ MySQL 不支持 `ADD COLUMN IF NOT EXISTS`（那是 MariaDB 扩展），必须自己判存在性后动态执行。
SET @ddl := IF(@table_exists = 1 AND @cursor_exists = 0,
  'ALTER TABLE `gb_record_cache_task` ADD COLUMN `cursor_at` datetime NULL COMMENT ''分段续播游标（下一分片会话的起始时间）'' AFTER `request_id`',
  'SELECT 1');
PREPARE record_cache_cursor_stmt FROM @ddl;
EXECUTE record_cache_cursor_stmt;
DEALLOCATE PREPARE record_cache_cursor_stmt;

-- ---- segments ----
SET @segments_exists := (
  SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gb_record_cache_task' AND COLUMN_NAME = 'segments'
);
SET @ddl := IF(@table_exists = 1 AND @segments_exists = 0,
  'ALTER TABLE `gb_record_cache_task` ADD COLUMN `segments` text COLLATE utf8mb4_general_ci NOT NULL COMMENT ''分片产出清单（JSON 数组）''',
  'SELECT 1');
PREPARE record_cache_segments_stmt FROM @ddl;
EXECUTE record_cache_segments_stmt;
DEALLOCATE PREPARE record_cache_segments_stmt;

-- ---- record_key ----
-- 录像段标识（受签快照句柄）。分片续播的每一片都要用它重新向设备发起 download 回放，
-- 不是"创建时用一次就丢"的参数；不落库的表现是「第一片拉完就再也拉不动」。
SET @record_key_exists := (
  SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gb_record_cache_task' AND COLUMN_NAME = 'record_key'
);
SET @ddl := IF(@table_exists = 1 AND @record_key_exists = 0,
  'ALTER TABLE `gb_record_cache_task` ADD COLUMN `record_key` varchar(255) COLLATE utf8mb4_general_ci NOT NULL DEFAULT '''' COMMENT ''录像段标识（续播分片要复用同一个）'' AFTER `record_type`',
  'SELECT 1');
PREPARE record_cache_key_stmt FROM @ddl;
EXECUTE record_cache_key_stmt;
DEALLOCATE PREPARE record_cache_key_stmt;

-- ---- session_id 类型修正 ----
-- 回放会话 ID 是 playback registry 生成的 `pb-<随机>`（字符型），不是自增主键。
-- 原设计写成 bigint unsigned 会把 `pb-xxxx` 落成 0，表现是「取消任务找不到会话，
-- 通道被占满 30 分钟才释放」——一个纯类型错误伪装成超时问题。
SET @legacy_int := (
  SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gb_record_cache_task'
    AND COLUMN_NAME = 'session_id' AND DATA_TYPE = 'bigint'
);
SET @ddl := IF(@table_exists = 1 AND @legacy_int = 1,
  'ALTER TABLE `gb_record_cache_task` MODIFY COLUMN `session_id` varchar(64) COLLATE utf8mb4_general_ci NULL COMMENT ''回放会话 ID（形如 pb-xxxx，字符型）''',
  'SELECT 1');
PREPARE record_cache_session_stmt FROM @ddl;
EXECUTE record_cache_session_stmt;
DEALLOCATE PREPARE record_cache_session_stmt;

-- ⛔ down 不可自动执行：删列/改类型会丢掉在途任务的续播进度与分片清单。