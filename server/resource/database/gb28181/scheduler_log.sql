-- 2026-06-27 ZLM 调度日志表(M3 T3.3)
-- 完整建表 SQL,新环境直接用;已存在环境跑 migrations/2026-06-27-scheduler-log.sql。
--
-- 记录每次 Scheduler.Pick 的结果(命中节点 + 错误),供运维事后审计 / 复盘。
-- 异步写入(LogService buffered channel + worker),失败 drop。
-- 保留 7 天,bootstrap 起 24h ticker prune。

CREATE TABLE IF NOT EXISTS `scheduler_log` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `happened_at` datetime(3) NOT NULL,
  `algorithm` varchar(32) NOT NULL DEFAULT '',
  `node_id` bigint NOT NULL DEFAULT '0',
  `node_name` varchar(64) NOT NULL DEFAULT '',
  `stream_id` varchar(64) NOT NULL DEFAULT '',
  `device_id` varchar(64) NOT NULL DEFAULT '',
  `channel_id` varchar(64) NOT NULL DEFAULT '',
  `error_message` varchar(255) NOT NULL DEFAULT '',
  PRIMARY KEY (`id`),
  KEY `idx_happened_at` (`happened_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='ZLM 调度日志(异步写,7d 保留)';
