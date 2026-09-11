-- ZLM 媒体节点表(M1 新增)
-- 多节点集群管理的元数据源,启动时由 application loaded 入内存 Registry。
DROP TABLE IF EXISTS `meta_node`;
CREATE TABLE `meta_node` (
  `id` int unsigned NOT NULL AUTO_INCREMENT,
  `revision` bigint unsigned NOT NULL DEFAULT '1' COMMENT 'full-row CAS revision',
  `name` varchar(64) NOT NULL DEFAULT '' COMMENT '显示名,如 zlm-bj-1',
  `host` varchar(64) NOT NULL DEFAULT '' COMMENT 'ZLM API host',
  `receive_host` varchar(255) NOT NULL DEFAULT '' COMMENT '设备收流地址,写入 SDP 的 c= 地址',
  `playback_host` varchar(255) NOT NULL DEFAULT '' COMMENT '播放访问地址,返回给浏览器/客户端',
  `api_port` int NOT NULL DEFAULT '18080' COMMENT 'ZLM API port',
  `api_secret` varchar(128) NOT NULL DEFAULT '' COMMENT 'ZLM api.secret',
  `media_server_uuid` varchar(64) NOT NULL DEFAULT '' COMMENT '业务侧 UUID,启动时写入 ZLM general.mediaServerId',
  `weight` int NOT NULL DEFAULT '50' COMMENT '加权轮询用 0-100,默认 50',
  `tags_json` text COMMENT '任意标签 JSON 字典',
  `state` varchar(16) NOT NULL DEFAULT 'active' COMMENT 'active/maintenance/offline',
  `recovery_required` tinyint(1) NOT NULL DEFAULT '0' COMMENT '外部配置不确定时禁止调度',
  `recovery_reason` varchar(255) NOT NULL DEFAULT '' COMMENT '安全、有限长的恢复原因',
  `recovery_fingerprint` char(64) NOT NULL DEFAULT '' COMMENT 'opaque recovery operation marker',
  `rtp_port_start` int NOT NULL DEFAULT '30000' COMMENT 'rtp_proxy.port_range 起',
  `rtp_port_end` int NOT NULL DEFAULT '35000' COMMENT 'rtp_proxy.port_range 止',
  `created_at` datetime DEFAULT NULL,
  `updated_at` datetime DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_media_server_uuid` (`media_server_uuid`),
  KEY `idx_state` (`state`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='ZLM 媒体节点表';
