-- Forward-only: this down migration is intentionally a no-op.
-- The up migration has no source field identifying the rows it created.
-- Automatic cleanup could affect pre-existing or administrator-managed permissions.
SELECT 1;
