-- Persist permanent automatic-ban policy without rewriting existing decisions (PostgreSQL).
UPDATE gb_sip_security_policy
SET ban_ttl_steps = ban_score::text || ':0',
    updated_at = CURRENT_TIMESTAMP
WHERE scope_key = 'global';
