-- In-place rollback is intentionally blocked.
-- The migration changes existing policy and ban expiry values; restore the
-- pre-migration database snapshot instead of guessing historical values.
DO $$
BEGIN
  RAISE EXCEPTION 'public SIP security remediation rollback requires restoring the pre-migration backup';
END $$;
