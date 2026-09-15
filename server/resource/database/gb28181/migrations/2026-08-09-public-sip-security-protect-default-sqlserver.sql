-- Upgrade only the original untouched observe defaults. Operator-customized policies are preserved.
UPDATE gb_sip_security_policy
SET mode = N'protect', window_seconds = 10, ban_ttl_steps = CONVERT(NVARCHAR(20), ban_score) + N':0', updated_at = SYSUTCDATETIME()
WHERE scope_key = N'global'
  AND mode = N'observe'
  AND window_seconds = 60
  AND ban_score = 100
  AND max_udp_per_window = 120;
