-- SIP 首次安装引导（SQL Server 增量迁移）
-- 重要：增量升级只补 legacy，绝不能改成 pending；重复执行不得覆盖既有安装状态。

IF OBJECT_ID(N'system_installation', N'U') IS NULL
BEGIN
    CREATE TABLE [system_installation] (
        [id] SMALLINT NOT NULL,
        [instance_id] NVARCHAR(36) NOT NULL CONSTRAINT [df_system_installation_instance_id] DEFAULT '',
        [onboarding_version] INT NOT NULL CONSTRAINT [df_system_installation_version] DEFAULT 1,
        [sip_onboarding_status] NVARCHAR(16) NOT NULL CONSTRAINT [df_system_installation_status] DEFAULT 'legacy',
        [sip_onboarding_finished_at] DATETIME NULL,
        [created_at] DATETIME NOT NULL CONSTRAINT [df_system_installation_created_at] DEFAULT GETDATE(),
        [updated_at] DATETIME NOT NULL CONSTRAINT [df_system_installation_updated_at] DEFAULT GETDATE(),
        CONSTRAINT [pk_system_installation] PRIMARY KEY ([id]),
        CONSTRAINT [chk_system_installation_singleton] CHECK ([id] = 1),
        CONSTRAINT [chk_system_installation_sip_status] CHECK (
            [sip_onboarding_status] IN ('pending', 'completed', 'skipped', 'legacy')
        )
    );
END;

IF OBJECT_ID(N'gb_sip_config', N'U') IS NULL
BEGIN
    CREATE TABLE [gb_sip_config] (
        [id] SMALLINT NOT NULL,
        [deployment_mode] NVARCHAR(8) NOT NULL,
        [listen_ip] NVARCHAR(45) NOT NULL,
        [advertise_ip] NVARCHAR(45) NOT NULL,
        [advertise_ip_inferred] BIT NOT NULL CONSTRAINT [df_gb_sip_config_advertise_ip_inferred] DEFAULT 0,
        [port] INT NOT NULL,
        [domain] NVARCHAR(10) NOT NULL,
        [server_id] NVARCHAR(20) NOT NULL,
        [password] NVARCHAR(255) NOT NULL,
        [created_at] DATETIME NOT NULL CONSTRAINT [df_gb_sip_config_created_at] DEFAULT GETDATE(),
        [updated_at] DATETIME NOT NULL CONSTRAINT [df_gb_sip_config_updated_at] DEFAULT GETDATE(),
        CONSTRAINT [pk_gb_sip_config] PRIMARY KEY ([id]),
        CONSTRAINT [chk_gb_sip_config_singleton] CHECK ([id] = 1),
        CONSTRAINT [chk_gb_sip_config_deployment_mode] CHECK ([deployment_mode] IN ('lan', 'public')),
        CONSTRAINT [chk_gb_sip_config_port] CHECK ([port] BETWEEN 1 AND 65535)
    );
END;

IF NOT EXISTS (SELECT 1 FROM [system_installation] WHERE [id] = 1)
BEGIN
    INSERT INTO [system_installation] (
        [id], [instance_id], [onboarding_version], [sip_onboarding_status],
        [sip_onboarding_finished_at], [created_at], [updated_at]
    )
    VALUES (1, '', 1, 'legacy', NULL, GETDATE(), GETDATE());
END;
