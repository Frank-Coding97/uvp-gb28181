-- Evidence snapshot for automatic bans and sampled source metadata (SQL Server).
IF COL_LENGTH('gb_sip_security_event','user_agent') IS NULL ALTER TABLE gb_sip_security_event ADD user_agent NVARCHAR(255) NOT NULL CONSTRAINT df_gb_sip_security_event_user_agent DEFAULT '';
IF COL_LENGTH('gb_sip_security_ban','trigger_method') IS NULL ALTER TABLE gb_sip_security_ban ADD trigger_method NVARCHAR(16) NOT NULL CONSTRAINT df_gb_sip_security_ban_trigger_method DEFAULT '';
IF COL_LENGTH('gb_sip_security_ban','trigger_count') IS NULL ALTER TABLE gb_sip_security_ban ADD trigger_count INT NOT NULL CONSTRAINT df_gb_sip_security_ban_trigger_count DEFAULT 0;
IF COL_LENGTH('gb_sip_security_ban','trigger_threshold') IS NULL ALTER TABLE gb_sip_security_ban ADD trigger_threshold INT NOT NULL CONSTRAINT df_gb_sip_security_ban_trigger_threshold DEFAULT 0;
IF COL_LENGTH('gb_sip_security_ban','window_seconds') IS NULL ALTER TABLE gb_sip_security_ban ADD window_seconds INT NOT NULL CONSTRAINT df_gb_sip_security_ban_window_seconds DEFAULT 0;
IF COL_LENGTH('gb_sip_security_ban','policy_mode') IS NULL ALTER TABLE gb_sip_security_ban ADD policy_mode NVARCHAR(16) NOT NULL CONSTRAINT df_gb_sip_security_ban_policy_mode DEFAULT '';
IF COL_LENGTH('gb_sip_security_ban','firewall_applied_at') IS NULL ALTER TABLE gb_sip_security_ban ADD firewall_applied_at DATETIME2 NULL;
IF COL_LENGTH('gb_sip_security_ban','blocked_count_after_ban') IS NULL ALTER TABLE gb_sip_security_ban ADD blocked_count_after_ban BIGINT NOT NULL CONSTRAINT df_gb_sip_security_ban_blocked_count DEFAULT 0;
IF COL_LENGTH('gb_sip_security_ban','last_blocked_at') IS NULL ALTER TABLE gb_sip_security_ban ADD last_blocked_at DATETIME2 NULL;
