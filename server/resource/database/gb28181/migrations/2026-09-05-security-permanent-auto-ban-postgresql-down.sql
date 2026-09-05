-- Restore the previous finite automatic-ban policy (PostgreSQL).
UPDATE gb_sip_security_policy
SET ban_ttl_steps = '100:60;200:600;500:3600',
    updated_at = CURRENT_TIMESTAMP
WHERE scope_key = 'global';
