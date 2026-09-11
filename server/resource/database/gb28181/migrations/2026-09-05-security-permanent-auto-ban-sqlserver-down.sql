-- Restore the previous finite automatic-ban policy (SQL Server).
UPDATE gb_sip_security_policy
SET ban_ttl_steps = N'100:60;200:600;500:3600',
    updated_at = SYSUTCDATETIME()
WHERE scope_key = N'global';
