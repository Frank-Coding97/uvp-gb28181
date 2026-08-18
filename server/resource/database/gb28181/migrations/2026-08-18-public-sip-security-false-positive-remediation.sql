-- Repair SIP security false positives and restore finite automatic bans (MySQL).
ALTER TABLE `gb_sip_security_event`
  ADD COLUMN IF NOT EXISTS `device_id` VARCHAR(64) NULL AFTER `source_ip`,
  ADD COLUMN IF NOT EXISTS `risk_scope` VARCHAR(16) NULL AFTER `device_id`;
ALTER TABLE `gb_sip_security_ban`
  ADD COLUMN IF NOT EXISTS `device_id` VARCHAR(64) NULL AFTER `source_ip`,
  ADD COLUMN IF NOT EXISTS `risk_scope` VARCHAR(16) NULL AFTER `device_id`;

UPDATE `gb_sip_security_event`
SET `device_id` = COALESCE(`device_id`, ''),
    `risk_scope` = COALESCE(NULLIF(`risk_scope`, ''), CASE WHEN COALESCE(`device_id`, '') = '' THEN 'source' ELSE 'device' END);

SET @drop_event_unique = IF(
  EXISTS(SELECT 1 FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = 'gb_sip_security_event' AND index_name = 'uk_gb_sip_security_event'),
  'ALTER TABLE `gb_sip_security_event` DROP INDEX `uk_gb_sip_security_event`',
  'SELECT 1'
);
PREPARE stmt FROM @drop_event_unique;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
ALTER TABLE `gb_sip_security_event`
  ADD UNIQUE KEY `uk_gb_sip_security_event` (`bucket_at`,`source_ip`,`device_id`,`risk_scope`,`transport`,`method`,`reason`,`action`);

SET @add_event_attribution_index = IF(
  NOT EXISTS(SELECT 1 FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = 'gb_sip_security_event' AND index_name = 'idx_gb_sip_security_event_attribution_time'),
  'CREATE INDEX `idx_gb_sip_security_event_attribution_time` ON `gb_sip_security_event` (`risk_scope`,`device_id`,`last_seen_at`)',
  'SELECT 1'
);
PREPARE stmt FROM @add_event_attribution_index;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @add_ban_attribution_index = IF(
  NOT EXISTS(SELECT 1 FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = 'gb_sip_security_ban' AND index_name = 'idx_gb_sip_security_ban_attribution_status'),
  'CREATE INDEX `idx_gb_sip_security_ban_attribution_status` ON `gb_sip_security_ban` (`risk_scope`,`device_id`,`status`)',
  'SELECT 1'
);
PREPARE stmt FROM @add_ban_attribution_index;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

UPDATE `gb_sip_security_policy`
SET `ban_ttl_steps` = '100:60;200:600;500:3600', `updated_at` = NOW()
WHERE `scope_key` = 'global';

UPDATE `gb_sip_security_ban`
SET `expires_at` = DATE_ADD(NOW(), INTERVAL 60 SECOND),
    `risk_scope` = COALESCE(NULLIF(`risk_scope`, ''), 'source')
WHERE `origin` = 'auto'
  AND `expires_at` IS NULL
  AND `status` IN ('active', 'agent_failed');

INSERT INTO `gb_sip_security_audit` (`actor`,`action`,`target`,`reason`,`decision_id`,`created_at`)
SELECT 'system','policy.auto-ban-remediation','global','finite TTL restored; legacy automatic permanent bans expire after 60 seconds','',NOW()
WHERE NOT EXISTS (
  SELECT 1 FROM `gb_sip_security_audit`
  WHERE `action` = 'policy.auto-ban-remediation' AND `target` = 'global'
);
