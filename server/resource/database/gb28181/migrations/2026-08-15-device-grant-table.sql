-- 设备共享授权表(MySQL 5.7+,幂等)。
-- 语义:设备级可见性共享(额外授权,不改变 owner_dept_id 归属)。
-- 软删 + 唯一键(device_id, target_type, target_id);重复授权由 repo 层"恢复或跳过"处理。
-- 存量 owner_dept_id=0 设备的回填走 Go 侧 backfill(需要读配置默认部门),见 device/service.go。

CREATE TABLE IF NOT EXISTS `gb_device_grant` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `device_id` int unsigned NOT NULL COMMENT '设备ID(gb_device.id)',
  `target_type` varchar(16) NOT NULL DEFAULT '' COMMENT '共享目标类型 dept/user',
  `target_id` int unsigned NOT NULL DEFAULT 0 COMMENT '部门ID或用户ID',
  `created_by` int unsigned DEFAULT 0 COMMENT '操作人',
  `created_at` datetime DEFAULT NULL,
  `updated_at` datetime DEFAULT NULL,
  `deleted_at` datetime DEFAULT NULL,
  PRIMARY KEY (`id`) USING BTREE,
  UNIQUE KEY `uk_device_target` (`device_id`,`target_type`,`target_id`) USING BTREE,
  KEY `idx_target` (`target_type`,`target_id`) USING BTREE,
  KEY `idx_device` (`device_id`) USING BTREE,
  KEY `idx_deleted_at` (`deleted_at`) USING BTREE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='设备共享授权表(设备级可见性共享)';
