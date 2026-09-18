-- Make new and currently active automatic bans explicit permanent rules.
ALTER TABLE gb_sip_security_ban ALTER COLUMN expires_at DATETIME2 NULL;
UPDATE gb_sip_security_ban SET expires_at = NULL WHERE origin = N'auto' AND status IN (N'active', N'agent_failed');
UPDATE gb_sip_security_policy SET ban_ttl_steps = CONVERT(NVARCHAR(20), ban_score) + N':0', updated_at = SYSUTCDATETIME() WHERE scope_key = N'global';
