-- Fill only the untouched global default. Operator-managed allowlists are preserved.
UPDATE gb_sip_security_policy
SET allowlist_text = E'127.0.0.0/8\n10.0.0.0/8\n172.16.0.0/12\n192.168.0.0/16',
    updated_at = CURRENT_TIMESTAMP
WHERE scope_key = 'global' AND BTRIM(allowlist_text) = '';
