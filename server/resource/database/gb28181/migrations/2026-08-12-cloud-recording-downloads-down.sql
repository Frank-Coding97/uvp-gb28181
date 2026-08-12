-- Safe non-destructive rollback for MySQL.
-- The up migration may reuse existing download API, menu and Casbin records.
-- sys_casbin_rule has no migration ownership metadata, so deleting by API group,
-- path or method could remove pre-existing or administrator-added permissions.
-- Keep this additive metadata on code rollback; remove it only through a reviewed,
-- targeted administrative cleanup with a backup.
SELECT 1;
