-- Fill only the untouched global default. Operator-managed allowlists are preserved.
UPDATE gb_sip_security_policy
SET allowlist_text = N'127.0.0.0/8' + CHAR(10) + N'10.0.0.0/8' + CHAR(10) + N'172.16.0.0/12' + CHAR(10) + N'192.168.0.0/16',
    updated_at = SYSUTCDATETIME()
WHERE scope_key = N'global' AND LTRIM(RTRIM(allowlist_text)) = N'';
