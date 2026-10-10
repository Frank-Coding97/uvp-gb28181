-- Existing users default to no forced password change; only the fresh-install seed is true.
ALTER TABLE sys_users
  ADD COLUMN IF NOT EXISTS must_change_password BOOLEAN NOT NULL DEFAULT FALSE;
