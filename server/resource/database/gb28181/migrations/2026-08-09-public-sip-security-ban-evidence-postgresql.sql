-- Evidence snapshot for automatic bans and sampled source metadata (PostgreSQL).
ALTER TABLE gb_sip_security_event ADD COLUMN IF NOT EXISTS user_agent VARCHAR(255) NOT NULL DEFAULT '';
ALTER TABLE gb_sip_security_ban ADD COLUMN IF NOT EXISTS trigger_method VARCHAR(16) NOT NULL DEFAULT '';
ALTER TABLE gb_sip_security_ban ADD COLUMN IF NOT EXISTS trigger_count INT NOT NULL DEFAULT 0;
ALTER TABLE gb_sip_security_ban ADD COLUMN IF NOT EXISTS trigger_threshold INT NOT NULL DEFAULT 0;
ALTER TABLE gb_sip_security_ban ADD COLUMN IF NOT EXISTS window_seconds INT NOT NULL DEFAULT 0;
ALTER TABLE gb_sip_security_ban ADD COLUMN IF NOT EXISTS policy_mode VARCHAR(16) NOT NULL DEFAULT '';
ALTER TABLE gb_sip_security_ban ADD COLUMN IF NOT EXISTS firewall_applied_at TIMESTAMP NULL;
ALTER TABLE gb_sip_security_ban ADD COLUMN IF NOT EXISTS blocked_count_after_ban BIGINT NOT NULL DEFAULT 0;
ALTER TABLE gb_sip_security_ban ADD COLUMN IF NOT EXISTS last_blocked_at TIMESTAMP NULL;
