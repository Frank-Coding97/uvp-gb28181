-- SIP 首次安装引导（PostgreSQL 增量迁移）
-- 重要：增量升级只补 legacy，绝不能改成 pending；重复执行不得覆盖既有安装状态。

CREATE TABLE IF NOT EXISTS system_installation (
    id SMALLINT NOT NULL,
    instance_id VARCHAR(36) NOT NULL DEFAULT '',
    onboarding_version INTEGER NOT NULL DEFAULT 1,
    sip_onboarding_status VARCHAR(16) NOT NULL DEFAULT 'legacy',
    sip_onboarding_finished_at TIMESTAMP NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    CONSTRAINT chk_system_installation_singleton CHECK (id = 1),
    CONSTRAINT chk_system_installation_sip_status CHECK (
        sip_onboarding_status IN ('pending', 'completed', 'skipped', 'legacy')
    )
);

COMMENT ON TABLE system_installation IS '平台安装与引导状态';
COMMENT ON COLUMN system_installation.id IS '单例主键，固定为 1';
COMMENT ON COLUMN system_installation.instance_id IS '平台实例 UUID，首次启动时补齐';

CREATE TABLE IF NOT EXISTS gb_sip_config (
    id SMALLINT NOT NULL,
    deployment_mode VARCHAR(8) NOT NULL,
    listen_ip VARCHAR(45) NOT NULL,
    advertise_ip VARCHAR(45) NOT NULL,
    advertise_ip_inferred BOOLEAN NOT NULL DEFAULT FALSE,
    port INTEGER NOT NULL,
    domain VARCHAR(10) NOT NULL,
    server_id VARCHAR(20) NOT NULL,
    password VARCHAR(255) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    CONSTRAINT chk_gb_sip_config_singleton CHECK (id = 1),
    CONSTRAINT chk_gb_sip_config_deployment_mode CHECK (deployment_mode IN ('lan', 'public')),
    CONSTRAINT chk_gb_sip_config_port CHECK (port BETWEEN 1 AND 65535)
);

COMMENT ON TABLE gb_sip_config IS 'GB28181 SIP 运行时配置';
COMMENT ON COLUMN gb_sip_config.advertise_ip_inferred IS '宣告地址是否由系统推断';
COMMENT ON COLUMN gb_sip_config.password IS 'SIP Digest 原始凭据';

INSERT INTO system_installation (
    id, instance_id, onboarding_version, sip_onboarding_status,
    sip_onboarding_finished_at, created_at, updated_at
)
VALUES (1, '', 1, 'legacy', NULL, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
ON CONFLICT (id) DO NOTHING;
