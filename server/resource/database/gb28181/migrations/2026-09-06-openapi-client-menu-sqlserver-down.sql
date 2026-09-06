-- openapi-client-menu:down-begin
-- Forward-only migration: ownership is not persisted, so rollback cannot
-- distinguish this migration's rows from pre-existing identical rows.
-- Keep page, button-parent, role-menu, and API data intact.
SELECT 1;

-- openapi-client-menu:down-end
