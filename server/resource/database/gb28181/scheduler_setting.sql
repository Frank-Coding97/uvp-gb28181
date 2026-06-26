-- ZLM 调度器全局设置表(单行,id=1)(M1 占位,M3 用)
DROP TABLE IF EXISTS `scheduler_setting`;
CREATE TABLE `scheduler_setting` (
  `id` int unsigned NOT NULL AUTO_INCREMENT,
  `algorithm` varchar(32) NOT NULL DEFAULT 'roundrobin' COMMENT 'roundrobin/weighted/leastload',
  `config_json` text COMMENT '算法相关配置 JSON',
  `created_at` datetime DEFAULT NULL,
  `updated_at` datetime DEFAULT NULL,
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='ZLM 调度器全局设置(单行)';
-- seed
INSERT INTO `scheduler_setting` (`id`, `algorithm`, `created_at`, `updated_at`)
SELECT 1, 'roundrobin', NOW(), NOW()
WHERE NOT EXISTS (SELECT 1 FROM `scheduler_setting` WHERE `id` = 1);
