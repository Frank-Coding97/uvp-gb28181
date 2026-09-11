-- Make new and currently active automatic bans explicit permanent rules.
ALTER TABLE gb_sip_security_ban ALTER COLUMN expires_at DROP NOT NULL;
UPDATE gb_sip_security_ban SET expires_at = NULL WHERE origin = 'auto' AND status IN ('active', 'agent_failed');
UPDATE gb_sip_security_policy SET ban_ttl_steps = ban_score::text || ':0', updated_at = CURRENT_TIMESTAMP WHERE scope_key = 'global';
