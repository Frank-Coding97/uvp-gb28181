-- Persist the standalone first-run lifecycle outside the frozen baseline.
-- The row is a durable latch: deleting the administrator cannot reopen setup.
CREATE TABLE "standalone_installation" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "phase" TEXT NOT NULL,
  "admin_user_id" INTEGER DEFAULT NULL,
  "completed_at" DATETIME DEFAULT NULL,
  "created_at" DATETIME NOT NULL,
  "updated_at" DATETIME NOT NULL,
  CHECK ("id" = 1),
  CHECK ("phase" IN ('pending_admin', 'pending_sip', 'complete')),
  CHECK ("admin_user_id" IS NULL OR (typeof("admin_user_id") = 'integer' AND "admin_user_id" BETWEEN 0 AND 4294967295)),
  CHECK (
    ("phase" = 'pending_admin' AND "admin_user_id" IS NULL AND "completed_at" IS NULL)
    OR ("phase" = 'pending_sip' AND "admin_user_id" IS NOT NULL AND "completed_at" IS NULL)
    OR ("phase" = 'complete' AND "completed_at" IS NOT NULL AND ("admin_user_id" IS NULL OR "admin_user_id" > 0))
  )
);

-- The frozen seed contains a logical user_1 -> role_1 relation without a
-- corresponding sys_users row. Remove exactly that orphan on fresh SQLite
-- installations; preserve it if an existing user_1 is present.
DELETE FROM "sys_casbin_rule"
 WHERE "ptype" = 'g'
   AND "v0" = 'user_1'
   AND "v1" = 'role_1'
   AND "v2" = '*'
   AND "v3" = ''
   AND "v4" = ''
   AND "v5" = ''
   AND NOT EXISTS (SELECT 1 FROM "sys_users" WHERE "id" = 1);

INSERT INTO "standalone_installation" ("id", "phase", "admin_user_id", "completed_at", "created_at", "updated_at")
VALUES (1, 'pending_admin', NULL, NULL, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);
