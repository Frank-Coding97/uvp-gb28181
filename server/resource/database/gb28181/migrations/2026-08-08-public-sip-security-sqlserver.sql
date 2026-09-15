-- UVP 国标公网安全防护一期安全聚合/封禁/策略/审计 (SQL Server)
IF OBJECT_ID(N'gb_sip_security_event', N'U') IS NULL
BEGIN
CREATE TABLE gb_sip_security_event (
 id BIGINT IDENTITY(1,1) PRIMARY KEY, bucket_at DATETIME2 NOT NULL, source_ip NVARCHAR(64) NOT NULL,
 address_family NVARCHAR(8) NOT NULL, transport NVARCHAR(8) NOT NULL, method NVARCHAR(16) NOT NULL, user_agent NVARCHAR(255) NOT NULL DEFAULT '',
 reason NVARCHAR(32) NOT NULL, action NVARCHAR(16) NOT NULL, count BIGINT NOT NULL DEFAULT 0,
 score_delta BIGINT NOT NULL DEFAULT 0, first_seen_at DATETIME2 NOT NULL, last_seen_at DATETIME2 NOT NULL,
 sample_event_id NVARCHAR(64) NOT NULL DEFAULT '', CONSTRAINT uk_gb_sip_security_event UNIQUE (bucket_at,source_ip,transport,method,reason,action));
CREATE INDEX idx_gb_sip_security_event_source_time ON gb_sip_security_event(source_ip,last_seen_at);
CREATE INDEX idx_gb_sip_security_event_reason_time ON gb_sip_security_event(reason,last_seen_at);
END;
IF OBJECT_ID(N'gb_sip_security_ban', N'U') IS NULL
BEGIN
CREATE TABLE gb_sip_security_ban (
 id BIGINT IDENTITY(1,1) PRIMARY KEY, source_ip NVARCHAR(64) NOT NULL, address_family NVARCHAR(8) NOT NULL,
 status NVARCHAR(16) NOT NULL, reason NVARCHAR(32) NOT NULL, rule_id NVARCHAR(64) NOT NULL,
 score INT NOT NULL DEFAULT 0, created_at DATETIME2 NOT NULL, expires_at DATETIME2 NULL,
 unbanned_at DATETIME2 NULL, unbanned_by NVARCHAR(64) NOT NULL DEFAULT '', origin NVARCHAR(16) NOT NULL,
 agent_state NVARCHAR(16) NOT NULL, decision_id NVARCHAR(64) NOT NULL UNIQUE, last_error NVARCHAR(512) NOT NULL DEFAULT '', trigger_method NVARCHAR(16) NOT NULL DEFAULT '', trigger_count INT NOT NULL DEFAULT 0, trigger_threshold INT NOT NULL DEFAULT 0, window_seconds INT NOT NULL DEFAULT 0, policy_mode NVARCHAR(16) NOT NULL DEFAULT '', firewall_applied_at DATETIME2 NULL, blocked_count_after_ban BIGINT NOT NULL DEFAULT 0, last_blocked_at DATETIME2 NULL);
CREATE INDEX idx_gb_sip_security_ban_source_status ON gb_sip_security_ban(source_ip,status);
CREATE INDEX idx_gb_sip_security_ban_expiry ON gb_sip_security_ban(expires_at);
END;
IF OBJECT_ID(N'gb_sip_security_policy', N'U') IS NULL
BEGIN
CREATE TABLE gb_sip_security_policy (
 id BIGINT IDENTITY(1,1) PRIMARY KEY, scope_key NVARCHAR(32) NOT NULL UNIQUE, mode NVARCHAR(16) NOT NULL,
 window_seconds INT NOT NULL, ban_score INT NOT NULL, max_packet_bytes INT NOT NULL, max_udp_per_window INT NOT NULL,
 max_tcp_connections INT NOT NULL, sample_per_source INT NOT NULL, nonce_ttl_seconds INT NOT NULL,
 ban_ttl_steps NVARCHAR(1024) NOT NULL, allowlist_text NVARCHAR(4096) NOT NULL, updated_by BIGINT NOT NULL DEFAULT 0, updated_at DATETIME2 NOT NULL);
END;
IF OBJECT_ID(N'gb_sip_security_audit', N'U') IS NULL
BEGIN
CREATE TABLE gb_sip_security_audit (
 id BIGINT IDENTITY(1,1) PRIMARY KEY, actor NVARCHAR(64) NOT NULL, action NVARCHAR(32) NOT NULL,
 target NVARCHAR(128) NOT NULL, reason NVARCHAR(255) NOT NULL, decision_id NVARCHAR(64) NOT NULL DEFAULT '', created_at DATETIME2 NOT NULL);
CREATE INDEX idx_gb_sip_security_audit_time ON gb_sip_security_audit(created_at);
END;
IF NOT EXISTS (SELECT 1 FROM gb_sip_security_policy WHERE scope_key = 'global')
INSERT INTO gb_sip_security_policy (scope_key,mode,window_seconds,ban_score,max_packet_bytes,max_udp_per_window,max_tcp_connections,sample_per_source,nonce_ttl_seconds,ban_ttl_steps,allowlist_text,updated_at)
VALUES ('global','protect',10,100,65536,120,32,3,60,'100:0','',SYSUTCDATETIME());
