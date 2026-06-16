-- GB28181 国标设备表(注册/心跳主体)
-- 沿用 ginfast 公共字段约定(id/created_at/updated_at/deleted_at/created_by/tenant_id)
DROP TABLE IF EXISTS `gb_device`;
CREATE TABLE `gb_device` (
  `id` int unsigned NOT NULL AUTO_INCREMENT,
  `device_id` varchar(20) NOT NULL DEFAULT '' COMMENT '20位国标编码',
  `name` varchar(255) NOT NULL DEFAULT '' COMMENT '设备名称',
  `password` varchar(255) NOT NULL DEFAULT '' COMMENT '按设备独立密码(本期用统一密码,留空)',
  `transport` varchar(8) NOT NULL DEFAULT '' COMMENT '传输模式 UDP/TCP',
  `manufacturer` varchar(255) NOT NULL DEFAULT '' COMMENT '厂商',
  `model` varchar(255) NOT NULL DEFAULT '' COMMENT '型号',
  `firmware` varchar(255) NOT NULL DEFAULT '' COMMENT '固件版本',
  `ip` varchar(64) NOT NULL DEFAULT '' COMMENT '设备来源IP',
  `port` int DEFAULT '0' COMMENT '设备来源端口',
  `register_time` datetime DEFAULT NULL COMMENT '最近注册成功时间',
  `register_expire_at` datetime DEFAULT NULL COMMENT '注册到期时刻',
  `keepalive_time` datetime DEFAULT NULL COMMENT '【事实】最后心跳时间=在线判定唯一真相',
  `keepalive_interval` int DEFAULT '60' COMMENT '【事实】该设备期望心跳周期(秒)',
  `expires` int DEFAULT '0' COMMENT '注册有效期(秒)',
  `status` tinyint(1) DEFAULT '0' COMMENT '【物化缓存】在线状态 0离线 1在线,由事实派生',
  `offline_at` datetime DEFAULT NULL COMMENT '最近被判离线的时刻',
  `created_at` datetime DEFAULT NULL,
  `updated_at` datetime DEFAULT NULL,
  `deleted_at` datetime DEFAULT NULL,
  `created_by` int unsigned DEFAULT '0' COMMENT '创建人',
  `tenant_id` int unsigned DEFAULT '0' COMMENT '租户ID',
  PRIMARY KEY (`id`) USING BTREE,
  UNIQUE KEY `uk_device_id` (`device_id`) USING BTREE,
  KEY `idx_deleted_at` (`deleted_at`),
  KEY `idx_status_keepalive` (`status`, `keepalive_time`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='GB28181国标设备表';
