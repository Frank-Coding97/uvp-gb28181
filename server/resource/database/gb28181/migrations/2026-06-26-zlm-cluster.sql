-- 2026-06-26 ZLM 多节点集群管理:新增 meta_node + scheduler_setting
-- 对已存在环境跑此增量;新环境直接用 meta_node.sql + scheduler_setting.sql。
-- 应用层启动时若 meta_node 空表,会用 yaml cfg.gb28181.zlm.* 自动 seed 第一节点(应用层做,不在此处)。

CREATE TABLE IF NOT EXISTS `meta_node` (
  `id` int unsigned NOT NULL AUTO_INCREMENT,
  `name` varchar(64) NOT NULL DEFAULT '',
  `host` varchar(64) NOT NULL DEFAULT '',
  `api_port` int NOT NULL DEFAULT '18080',
  `api_secret` varchar(128) NOT NULL DEFAULT '',
  `media_server_uuid` varchar(64) NOT NULL DEFAULT '',
  `weight` int NOT NULL DEFAULT '50',
  `tags_json` text,
  `state` varchar(16) NOT NULL DEFAULT 'active',
  `rtp_port_start` int NOT NULL DEFAULT '30000',
  `rtp_port_end` int NOT NULL DEFAULT '35000',
  `created_at` datetime DEFAULT NULL,
  `updated_at` datetime DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_media_server_uuid` (`media_server_uuid`),
  KEY `idx_state` (`state`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='ZLM 媒体节点表';

CREATE TABLE IF NOT EXISTS `scheduler_setting` (
  `id` int unsigned NOT NULL AUTO_INCREMENT,
  `algorithm` varchar(32) NOT NULL DEFAULT 'roundrobin',
  `config_json` text,
  `created_at` datetime DEFAULT NULL,
  `updated_at` datetime DEFAULT NULL,
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='ZLM 调度器全局设置(单行)';

INSERT INTO `scheduler_setting` (`id`, `algorithm`, `created_at`, `updated_at`)
SELECT 1, 'roundrobin', NOW(), NOW()
WHERE NOT EXISTS (SELECT 1 FROM `scheduler_setting` WHERE `id` = 1);
