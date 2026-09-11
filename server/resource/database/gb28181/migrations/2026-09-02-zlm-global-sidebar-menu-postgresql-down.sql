-- zlm-global-sidebar-menu:down:start
-- Restore the single-entry workbench presentation (PostgreSQL).
UPDATE sys_menu SET hide=1,updated_at=CURRENT_TIMESTAMP WHERE path IN ('/media/overview','/media/monitoring','/media/ingress','/media/nodes','/media/scheduling','/media/nodes/:id') AND deleted_at IS NULL;
-- zlm-global-sidebar-menu:down:end
