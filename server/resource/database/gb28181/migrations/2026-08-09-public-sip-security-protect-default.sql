-- Upgrade only the original untouched observe defaults. Operator-customized policies are preserved.
UPDATE `gb_sip_security_policy`
SET `mode` = 'protect', `window_seconds` = 10, `ban_ttl_steps` = CONCAT(CAST(`ban_score` AS CHAR), ':0'), `updated_at` = NOW()
WHERE `scope_key` = 'global'
  AND `mode` = 'observe'
  AND `window_seconds` = 60
  AND `ban_score` = 100
  AND `max_udp_per_window` = 120;
