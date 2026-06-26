-- ZLM 媒体节点表(M1 新增)
-- 多节点集群管理的元数据源,启动时由 application loaded 入内存 Registry。
DROP TABLE IF EXISTS `meta_node`;
CREATE TABLE `meta_node` (
  `id` int unsigned NOT NULL AUTO_INCREMENT,
  `name` varchar(64) NOT NULL DEFAULT '' COMMENT '显示名,如 zlm-bj-1',
  `host` varchar(64) NOT NULL DEFAULT '' COMMENT 'ZLM API host',
  `api_port` int NOT NULL DEFAULT '18080' COMMENT 'ZLM API port',
  `api_secret` varchar(128) NOT NULL DEFAULT '' COMMENT 'ZLM api.secret',
  `media_server_uuid` varchar(64) NOT NULL DEFAULT '' COMMENT '业务侧 UUID,启动时写入 ZLM general.mediaServerId',
  `weight` int NOT NULL DEFAULT '50' COMMENT '加权轮询用 0-100,默认 50',
  `tags_json` text COMMENT '任意标签 JSON 字典',
  `state` varchar(16) NOT NULL DEFAULT 'active' COMMENT 'active/maintenance/offline',
  `rtp_port_start` int NOT NULL DEFAULT '30000' COMMENT 'rtp_proxy.port_range 起',
  `rtp_port_end` int NOT NULL DEFAULT '35000' COMMENT 'rtp_proxy.port_range 止',
  `created_at` datetime DEFAULT NULL,
  `updated_at` datetime DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_media_server_uuid` (`media_server_uuid`),
  KEY `idx_state` (`state`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='ZLM 媒体节点表';
