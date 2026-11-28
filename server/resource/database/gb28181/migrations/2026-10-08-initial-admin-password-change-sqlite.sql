-- Existing users default to no forced password change; only the fresh-install seed is true.
-- SQLite has no ADD COLUMN IF NOT EXISTS; the migration ledger applies this file once.
ALTER TABLE "sys_users"
  ADD COLUMN "must_change_password" INTEGER NOT NULL DEFAULT 0
  CHECK ("must_change_password" IN (0, 1));
