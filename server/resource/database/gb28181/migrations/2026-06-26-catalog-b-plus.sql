-- 2026-06-26 国标多级目录 + 设备管理页 数据模型(plan §3 落地)
-- 4 张新表 + 2 张表加字段;思路 B+:物理对象 + 独立目录树 + N:N 挂载
-- 非破坏性 ADD COLUMN / CREATE TABLE,可重入(IF NOT EXISTS / IF EXISTS)
-- 既有 gb_channel.parent_id / civil_code 字段暂留(A5 backfill 后再删,见 plan §3.3)

-- ========= 1. gb_device 加 subscribe_* 字段(Q4 决议) =========
ALTER TABLE `gb_device`
  ADD COLUMN `subscribe_capability` varchar(16) NOT NULL DEFAULT 'unknown' COMMENT '订阅能力 unknown/subscribed/fallback' AFTER `offline_at`,
  ADD COLUMN `subscribe_last_test` datetime DEFAULT NULL COMMENT '最近一次 SUBSCRIBE 尝试' AFTER `subscribe_capability`,
  ADD COLUMN `subscribe_expires_at` datetime DEFAULT NULL COMMENT '订阅过期时刻(提前续订)' AFTER `subscribe_last_test`,
  ADD INDEX `idx_subscribe_capability` (`subscribe_capability`, `subscribe_last_test`);

-- ========= 2. gb_channel 加 capabilities JSON =========
ALTER TABLE `gb_channel`
  ADD COLUMN `capabilities` json DEFAULT NULL COMMENT '通道能力 {audio,h265,night_vision,alarm_io,recording}' AFTER `stream_id`;

-- ========= 3. gb_catalog_node 独立目录树(思路 B+ 核心) =========
CREATE TABLE IF NOT EXISTS `gb_catalog_node` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `tenant_id` bigint unsigned NOT NULL DEFAULT 0 COMMENT '多租户',
  `node_type` varchar(16) NOT NULL COMMENT 'civil_code/biz_group/virtual_org/device/channel',
  `parent_id` bigint unsigned DEFAULT NULL COMMENT '父节点;根 NULL',
  `path` varchar(512) NOT NULL DEFAULT '/' COMMENT '物化路径 /12/47/189/',
  `depth` tinyint unsigned NOT NULL DEFAULT 0 COMMENT '深度,根=0',
  `name` varchar(128) NOT NULL COMMENT '节点显示名',
  `code` varchar(32) DEFAULT NULL COMMENT '节点编码(行政区 6/设备 20/通道 20)',
  `civil_code` varchar(6) DEFAULT NULL COMMENT '关联行政区(冗余加速过滤)',
  `device_id` bigint unsigned DEFAULT NULL COMMENT 'node_type=device 时引用',
  `channel_id` bigint unsigned DEFAULT NULL COMMENT 'node_type=channel 主挂载时引用',
  `source` varchar(16) NOT NULL DEFAULT 'catalog' COMMENT 'catalog/manual/auto',
  `sort_order` int NOT NULL DEFAULT 0 COMMENT '同级排序',
  `anomaly` tinyint(1) NOT NULL DEFAULT 0 COMMENT 'Q1:1=识别失败被兜底',
  `anomaly_reason` varchar(255) DEFAULT NULL COMMENT '兜底原因',
  `raw_code` varchar(64) DEFAULT NULL COMMENT '兜底前原始编码',
  `created_at` datetime DEFAULT NULL,
  `updated_at` datetime DEFAULT NULL,
  `deleted_at` datetime DEFAULT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_tenant_parent` (`tenant_id`, `parent_id`),
  KEY `idx_tenant_path` (`tenant_id`, `path`(128)),
  KEY `idx_tenant_type` (`tenant_id`, `node_type`),
  KEY `idx_tenant_anomaly` (`tenant_id`, `anomaly`),
  KEY `idx_civil_code` (`tenant_id`, `civil_code`),
  KEY `idx_code` (`code`),
  KEY `idx_deleted_at` (`deleted_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='国标多级目录树节点(思路 B+ 核心)';

-- ========= 4. gb_channel_mount 通道与节点的 N:N 挂载 =========
CREATE TABLE IF NOT EXISTS `gb_channel_mount` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `tenant_id` bigint unsigned NOT NULL DEFAULT 0,
  `channel_id` bigint unsigned NOT NULL COMMENT '物理通道',
  `parent_node_id` bigint unsigned NOT NULL COMMENT '挂在哪个目录节点',
  `display_name` varchar(128) DEFAULT NULL COMMENT '挂载点别名,NULL=继承通道名',
  `is_primary` tinyint(1) NOT NULL DEFAULT 0 COMMENT '1=主挂载,每个 channel 必有且仅有一个',
  `mount_source` varchar(16) NOT NULL DEFAULT 'catalog' COMMENT 'catalog/manual',
  `sort_order` int NOT NULL DEFAULT 0,
  `created_at` datetime DEFAULT NULL,
  `updated_at` datetime DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_channel_parent` (`channel_id`, `parent_node_id`),
  KEY `idx_parent_sort` (`parent_node_id`, `sort_order`),
  KEY `idx_tenant` (`tenant_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='通道与目录树节点的 N:N 挂载关系';

-- ========= 5. gb_anomaly_record 目录异常审计(Q1 治理) =========
CREATE TABLE IF NOT EXISTS `gb_anomaly_record` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `tenant_id` bigint unsigned NOT NULL DEFAULT 0,
  `catalog_node_id` bigint unsigned NOT NULL COMMENT '被兜底的节点',
  `raw_code` varchar(64) NOT NULL COMMENT '原始上报编码',
  `guessed_type` varchar(32) DEFAULT NULL COMMENT '推测类型',
  `fallback_type` varchar(16) NOT NULL COMMENT 'virtual_org/channel/device',
  `source_device_id` bigint unsigned DEFAULT NULL COMMENT '来源下级平台/设备',
  `reason` varchar(255) DEFAULT NULL,
  `resolved` tinyint(1) NOT NULL DEFAULT 0 COMMENT '0=待处理 1=已处理',
  `resolved_by` bigint unsigned DEFAULT NULL,
  `resolved_at` datetime DEFAULT NULL,
  `resolved_action` varchar(64) DEFAULT NULL COMMENT '动作摘要',
  `created_at` datetime NOT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_tenant_resolved` (`tenant_id`, `resolved`, `created_at`),
  KEY `idx_node` (`catalog_node_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='目录异常审计:Q1 决议';

-- ========= 6. sys_civil_code 行政区划字典(Q2:Go embed seed) =========
CREATE TABLE IF NOT EXISTS `sys_civil_code` (
  `code` varchar(6) NOT NULL COMMENT '6 位 GB/T 2260 行政区码',
  `name` varchar(64) NOT NULL COMMENT '完整名称',
  `short_name` varchar(32) DEFAULT NULL,
  `parent_code` varchar(6) DEFAULT NULL,
  `level` tinyint NOT NULL COMMENT '1=省 2=市 3=区县',
  `pinyin` varchar(64) DEFAULT NULL COMMENT '拼音(Cmd+K 搜索)',
  `created_at` datetime DEFAULT NULL,
  `updated_at` datetime DEFAULT NULL,
  PRIMARY KEY (`code`),
  KEY `idx_parent_code` (`parent_code`, `level`),
  KEY `idx_pinyin` (`pinyin`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='行政区划字典 GB/T 2260';
