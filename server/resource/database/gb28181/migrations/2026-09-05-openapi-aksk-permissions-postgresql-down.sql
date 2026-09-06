-- OpenAPI permission rows can pre-exist under the same natural keys and may
-- have been granted to operators after installation. Keep rollback forward-only
-- so a down step cannot remove unrelated or explicitly managed authorization.
SELECT 1;
