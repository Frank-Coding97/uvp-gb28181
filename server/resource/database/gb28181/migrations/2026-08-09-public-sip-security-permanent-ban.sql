-- Make new and currently active automatic bans explicit permanent rules.
ALTER TABLE `gb_sip_security_ban` MODIFY COLUMN `expires_at` DATETIME NULL;
UPDATE `gb_sip_security_ban` SET `expires_at` = NULL WHERE `origin` = 'auto' AND `status` IN ('active', 'agent_failed');
UPDATE `gb_sip_security_policy` SET `ban_ttl_steps` = CONCAT(CAST(`ban_score` AS CHAR), ':0'), `updated_at` = NOW() WHERE `scope_key` = 'global';
