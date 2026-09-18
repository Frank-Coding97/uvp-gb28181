-- Persist permanent automatic-ban policy without rewriting existing decisions (SQL Server).
UPDATE gb_sip_security_policy
SET ban_ttl_steps = CONVERT(NVARCHAR(20), ban_score) + N':0',
    updated_at = SYSUTCDATETIME()
WHERE scope_key = N'global';
