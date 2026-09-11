-- UVP 国标公网安全防护一期安全聚合/封禁/策略/审计 (PostgreSQL)
CREATE TABLE IF NOT EXISTS gb_sip_security_event (
  id BIGSERIAL PRIMARY KEY, bucket_at TIMESTAMP NOT NULL, source_ip VARCHAR(64) NOT NULL,
  address_family VARCHAR(8) NOT NULL, transport VARCHAR(8) NOT NULL, method VARCHAR(16) NOT NULL, user_agent VARCHAR(255) NOT NULL DEFAULT '',
  reason VARCHAR(32) NOT NULL, action VARCHAR(16) NOT NULL, count BIGINT NOT NULL DEFAULT 0,
  score_delta BIGINT NOT NULL DEFAULT 0, first_seen_at TIMESTAMP NOT NULL, last_seen_at TIMESTAMP NOT NULL,
  sample_event_id VARCHAR(64) NOT NULL DEFAULT '',
  CONSTRAINT uk_gb_sip_security_event UNIQUE (bucket_at,source_ip,transport,method,reason,action)
);
CREATE INDEX IF NOT EXISTS idx_gb_sip_security_event_source_time ON gb_sip_security_event(source_ip,last_seen_at);
CREATE INDEX IF NOT EXISTS idx_gb_sip_security_event_reason_time ON gb_sip_security_event(reason,last_seen_at);

CREATE TABLE IF NOT EXISTS gb_sip_security_ban (
  id BIGSERIAL PRIMARY KEY, source_ip VARCHAR(64) NOT NULL, address_family VARCHAR(8) NOT NULL,
  status VARCHAR(16) NOT NULL, reason VARCHAR(32) NOT NULL, rule_id VARCHAR(64) NOT NULL,
  score INT NOT NULL DEFAULT 0, created_at TIMESTAMP NOT NULL, expires_at TIMESTAMP NULL,
  unbanned_at TIMESTAMP NULL, unbanned_by VARCHAR(64) NOT NULL DEFAULT '', origin VARCHAR(16) NOT NULL,
  agent_state VARCHAR(16) NOT NULL, decision_id VARCHAR(64) NOT NULL UNIQUE, last_error VARCHAR(512) NOT NULL DEFAULT '', trigger_method VARCHAR(16) NOT NULL DEFAULT '', trigger_count INT NOT NULL DEFAULT 0, trigger_threshold INT NOT NULL DEFAULT 0, window_seconds INT NOT NULL DEFAULT 0, policy_mode VARCHAR(16) NOT NULL DEFAULT '', firewall_applied_at TIMESTAMP NULL, blocked_count_after_ban BIGINT NOT NULL DEFAULT 0, last_blocked_at TIMESTAMP NULL
);
CREATE INDEX IF NOT EXISTS idx_gb_sip_security_ban_source_status ON gb_sip_security_ban(source_ip,status);
CREATE INDEX IF NOT EXISTS idx_gb_sip_security_ban_expiry ON gb_sip_security_ban(expires_at);

CREATE TABLE IF NOT EXISTS gb_sip_security_policy (
  id BIGSERIAL PRIMARY KEY, scope_key VARCHAR(32) NOT NULL UNIQUE, mode VARCHAR(16) NOT NULL,
  window_seconds INT NOT NULL, ban_score INT NOT NULL, max_packet_bytes INT NOT NULL,
  max_udp_per_window INT NOT NULL, max_tcp_connections INT NOT NULL, sample_per_source INT NOT NULL,
  nonce_ttl_seconds INT NOT NULL, ban_ttl_steps VARCHAR(1024) NOT NULL, allowlist_text VARCHAR(4096) NOT NULL,
  updated_by BIGINT NOT NULL DEFAULT 0, updated_at TIMESTAMP NOT NULL
);
CREATE TABLE IF NOT EXISTS gb_sip_security_audit (
  id BIGSERIAL PRIMARY KEY, actor VARCHAR(64) NOT NULL, action VARCHAR(32) NOT NULL,
  target VARCHAR(128) NOT NULL, reason VARCHAR(255) NOT NULL, decision_id VARCHAR(64) NOT NULL DEFAULT '',
  created_at TIMESTAMP NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_gb_sip_security_audit_time ON gb_sip_security_audit(created_at);
INSERT INTO gb_sip_security_policy (scope_key,mode,window_seconds,ban_score,max_packet_bytes,max_udp_per_window,max_tcp_connections,sample_per_source,nonce_ttl_seconds,ban_ttl_steps,allowlist_text,updated_at)
VALUES ('global','protect',10,100,65536,120,32,3,60,'100:0','',CURRENT_TIMESTAMP)
ON CONFLICT (scope_key) DO NOTHING;
