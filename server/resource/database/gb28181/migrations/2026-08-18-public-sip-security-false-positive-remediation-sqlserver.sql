-- Repair SIP security false positives and restore finite automatic bans (SQL Server).
IF COL_LENGTH('gb_sip_security_event','device_id') IS NULL
  ALTER TABLE gb_sip_security_event ADD device_id NVARCHAR(64) NULL;
IF COL_LENGTH('gb_sip_security_event','risk_scope') IS NULL
  ALTER TABLE gb_sip_security_event ADD risk_scope NVARCHAR(16) NULL;
IF COL_LENGTH('gb_sip_security_ban','device_id') IS NULL
  ALTER TABLE gb_sip_security_ban ADD device_id NVARCHAR(64) NULL;
IF COL_LENGTH('gb_sip_security_ban','risk_scope') IS NULL
  ALTER TABLE gb_sip_security_ban ADD risk_scope NVARCHAR(16) NULL;

UPDATE gb_sip_security_event
SET device_id = COALESCE(device_id, N''),
    risk_scope = COALESCE(NULLIF(risk_scope, N''), CASE WHEN COALESCE(device_id, N'') = N'' THEN N'source' ELSE N'device' END);

IF EXISTS (SELECT 1 FROM sys.key_constraints WHERE name = N'uk_gb_sip_security_event' AND parent_object_id = OBJECT_ID(N'gb_sip_security_event'))
  ALTER TABLE gb_sip_security_event DROP CONSTRAINT uk_gb_sip_security_event;
ALTER TABLE gb_sip_security_event ADD CONSTRAINT uk_gb_sip_security_event
  UNIQUE (bucket_at,source_ip,device_id,risk_scope,transport,method,reason,action);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'idx_gb_sip_security_event_attribution_time' AND object_id = OBJECT_ID(N'gb_sip_security_event'))
  CREATE INDEX idx_gb_sip_security_event_attribution_time ON gb_sip_security_event(risk_scope,device_id,last_seen_at);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'idx_gb_sip_security_ban_attribution_status' AND object_id = OBJECT_ID(N'gb_sip_security_ban'))
  CREATE INDEX idx_gb_sip_security_ban_attribution_status ON gb_sip_security_ban(risk_scope,device_id,status);

UPDATE gb_sip_security_policy
SET ban_ttl_steps = N'100:60;200:600;500:3600', updated_at = SYSUTCDATETIME()
WHERE scope_key = N'global';

UPDATE gb_sip_security_ban
SET expires_at = DATEADD(SECOND, 60, SYSUTCDATETIME()),
    risk_scope = COALESCE(NULLIF(risk_scope, N''), N'source')
WHERE origin = N'auto'
  AND expires_at IS NULL
  AND status IN (N'active', N'agent_failed');

IF NOT EXISTS (
  SELECT 1 FROM gb_sip_security_audit
  WHERE action = N'policy.auto-ban-remediation' AND target = N'global'
)
INSERT INTO gb_sip_security_audit (actor,action,target,reason,decision_id,created_at)
VALUES (N'system',N'policy.auto-ban-remediation',N'global',N'finite TTL restored; legacy automatic permanent bans expire after 60 seconds',N'',SYSUTCDATETIME());
