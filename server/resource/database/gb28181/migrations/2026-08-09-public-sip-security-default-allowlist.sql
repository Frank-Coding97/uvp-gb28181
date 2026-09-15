-- Fill only the untouched global default. Operator-managed allowlists are preserved.
UPDATE `gb_sip_security_policy`
SET `allowlist_text` = CONCAT('127.0.0.0/8', CHAR(10), '10.0.0.0/8', CHAR(10), '172.16.0.0/12', CHAR(10), '192.168.0.0/16'),
    `updated_at` = NOW()
WHERE `scope_key` = 'global' AND TRIM(`allowlist_text`) = '';
