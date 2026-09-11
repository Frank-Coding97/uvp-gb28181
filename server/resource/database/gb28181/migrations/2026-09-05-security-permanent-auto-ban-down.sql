-- Restore the previous finite automatic-ban policy (MySQL).
UPDATE `gb_sip_security_policy`
SET `ban_ttl_steps` = '100:60;200:600;500:3600',
    `updated_at` = NOW()
WHERE `scope_key` = 'global';
