-- SIP 首次安装引导（MySQL 5.7+ 增量迁移）
-- 重要：增量升级只补 legacy，绝不能改成 pending；重复执行不得覆盖既有安装状态。

CREATE TABLE IF NOT EXISTS `system_installation` (
  `id` tinyint unsigned NOT NULL COMMENT '单例主键，固定为 1',
  `instance_id` varchar(36) NOT NULL DEFAULT '' COMMENT '平台实例 UUID，首次启动时补齐',
  `onboarding_version` int unsigned NOT NULL DEFAULT 1 COMMENT '引导协议版本',
  `sip_onboarding_status` varchar(16) NOT NULL DEFAULT 'legacy' COMMENT 'pending/completed/skipped/legacy',
  `sip_onboarding_finished_at` datetime DEFAULT NULL COMMENT '完成或跳过时间',
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
  PRIMARY KEY (`id`),
  CONSTRAINT `chk_system_installation_singleton` CHECK (`id` = 1),
  CONSTRAINT `chk_system_installation_sip_status` CHECK (`sip_onboarding_status` IN ('pending', 'completed', 'skipped', 'legacy'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='平台安装与引导状态';

CREATE TABLE IF NOT EXISTS `gb_sip_config` (
  `id` tinyint unsigned NOT NULL COMMENT '单例主键，固定为 1',
  `deployment_mode` varchar(8) NOT NULL COMMENT 'lan/public',
  `listen_ip` varchar(45) NOT NULL COMMENT 'SIP 监听地址',
  `advertise_ip` varchar(45) NOT NULL COMMENT 'SIP 对外宣告地址',
  `advertise_ip_inferred` tinyint(1) NOT NULL DEFAULT 0 COMMENT '宣告地址是否由系统推断',
  `port` int unsigned NOT NULL COMMENT 'SIP 端口',
  `domain` varchar(10) NOT NULL COMMENT 'SIP 域',
  `server_id` varchar(20) NOT NULL COMMENT '平台国标编码',
  `password` varchar(255) NOT NULL COMMENT 'SIP Digest 原始凭据',
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
  PRIMARY KEY (`id`),
  CONSTRAINT `chk_gb_sip_config_singleton` CHECK (`id` = 1),
  CONSTRAINT `chk_gb_sip_config_deployment_mode` CHECK (`deployment_mode` IN ('lan', 'public')),
  CONSTRAINT `chk_gb_sip_config_port` CHECK (`port` BETWEEN 1 AND 65535)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='GB28181 SIP 运行时配置';

-- MySQL 5.7 解析但不执行 CHECK，触发器用于真正保证 id=1。
DROP TRIGGER IF EXISTS `trg_system_installation_singleton_insert`;
DROP TRIGGER IF EXISTS `trg_system_installation_singleton_update`;
DROP TRIGGER IF EXISTS `trg_gb_sip_config_singleton_insert`;
DROP TRIGGER IF EXISTS `trg_gb_sip_config_singleton_update`;
DELIMITER $$
CREATE TRIGGER `trg_system_installation_singleton_insert`
BEFORE INSERT ON `system_installation`
FOR EACH ROW
BEGIN
  IF NEW.`id` <> 1 THEN
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'system_installation only accepts id=1';
  END IF;
END$$
CREATE TRIGGER `trg_system_installation_singleton_update`
BEFORE UPDATE ON `system_installation`
FOR EACH ROW
BEGIN
  IF NEW.`id` <> 1 THEN
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'system_installation only accepts id=1';
  END IF;
END$$
CREATE TRIGGER `trg_gb_sip_config_singleton_insert`
BEFORE INSERT ON `gb_sip_config`
FOR EACH ROW
BEGIN
  IF NEW.`id` <> 1 THEN
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'gb_sip_config only accepts id=1';
  END IF;
END$$
CREATE TRIGGER `trg_gb_sip_config_singleton_update`
BEFORE UPDATE ON `gb_sip_config`
FOR EACH ROW
BEGIN
  IF NEW.`id` <> 1 THEN
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'gb_sip_config only accepts id=1';
  END IF;
END$$
DELIMITER ;

INSERT INTO `system_installation` (
  `id`, `instance_id`, `onboarding_version`, `sip_onboarding_status`,
  `sip_onboarding_finished_at`, `created_at`, `updated_at`
)
SELECT 1, '', 1, 'legacy', NULL, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
FROM DUAL
WHERE NOT EXISTS (
  SELECT 1 FROM `system_installation` WHERE `id` = 1
);
