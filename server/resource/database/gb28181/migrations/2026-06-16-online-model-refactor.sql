-- 2026-06-16 在线状态模型重构:keepalive_time 作唯一真相,status 降为派生缓存
-- 对已存在的 gb_device 表加列(新建环境用 gb_device.sql 即含这些列)
ALTER TABLE `gb_device`
  ADD COLUMN `register_expire_at` datetime DEFAULT NULL COMMENT '注册到期时刻' AFTER `register_time`,
  ADD COLUMN `keepalive_interval` int DEFAULT 60 COMMENT '【事实】该设备期望心跳周期(秒)' AFTER `keepalive_time`,
  ADD COLUMN `offline_at` datetime DEFAULT NULL COMMENT '最近被判离线的时刻' AFTER `status`,
  ADD INDEX `idx_status_keepalive` (`status`, `keepalive_time`);
