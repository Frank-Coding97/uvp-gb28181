-- Persist permanent automatic-ban policy without rewriting existing decisions (MySQL).
UPDATE `gb_sip_security_policy`
SET `ban_ttl_steps` = CONCAT(CAST(`ban_score` AS CHAR), ':0'),
    `updated_at` = NOW()
WHERE `scope_key` = 'global';
