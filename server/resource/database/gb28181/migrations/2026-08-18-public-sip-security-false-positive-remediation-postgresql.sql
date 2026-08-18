-- Repair SIP security false positives and restore finite automatic bans (PostgreSQL).
ALTER TABLE gb_sip_security_event ADD COLUMN IF NOT EXISTS device_id VARCHAR(64) NULL;
ALTER TABLE gb_sip_security_event ADD COLUMN IF NOT EXISTS risk_scope VARCHAR(16) NULL;
ALTER TABLE gb_sip_security_ban ADD COLUMN IF NOT EXISTS device_id VARCHAR(64) NULL;
ALTER TABLE gb_sip_security_ban ADD COLUMN IF NOT EXISTS risk_scope VARCHAR(16) NULL;

UPDATE gb_sip_security_event
SET device_id = COALESCE(device_id, ''),
    risk_scope = COALESCE(NULLIF(risk_scope, ''), CASE WHEN COALESCE(device_id, '') = '' THEN 'source' ELSE 'device' END);

ALTER TABLE gb_sip_security_event DROP CONSTRAINT IF EXISTS uk_gb_sip_security_event;
ALTER TABLE gb_sip_security_event
  ADD CONSTRAINT uk_gb_sip_security_event UNIQUE (bucket_at,source_ip,device_id,risk_scope,transport,method,reason,action);
CREATE INDEX IF NOT EXISTS idx_gb_sip_security_event_attribution_time
  ON gb_sip_security_event(risk_scope,device_id,last_seen_at);
CREATE INDEX IF NOT EXISTS idx_gb_sip_security_ban_attribution_status
  ON gb_sip_security_ban(risk_scope,device_id,status);

UPDATE gb_sip_security_policy
SET ban_ttl_steps = '100:60;200:600;500:3600', updated_at = CURRENT_TIMESTAMP
WHERE scope_key = 'global';

UPDATE gb_sip_security_ban
SET expires_at = CURRENT_TIMESTAMP + INTERVAL '60 seconds',
    risk_scope = COALESCE(NULLIF(risk_scope, ''), 'source')
WHERE origin = 'auto'
  AND expires_at IS NULL
  AND status IN ('active', 'agent_failed');

INSERT INTO gb_sip_security_audit (actor,action,target,reason,decision_id,created_at)
SELECT 'system','policy.auto-ban-remediation','global','finite TTL restored; legacy automatic permanent bans expire after 60 seconds','',CURRENT_TIMESTAMP
WHERE NOT EXISTS (
  SELECT 1 FROM gb_sip_security_audit
  WHERE action = 'policy.auto-ban-remediation' AND target = 'global'
);
