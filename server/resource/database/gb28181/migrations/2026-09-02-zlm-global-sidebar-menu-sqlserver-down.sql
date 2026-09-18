-- zlm-global-sidebar-menu:down:start
-- Restore the single-entry workbench presentation (SQL Server).
UPDATE [sys_menu] SET [hide]=1,[updated_at]=GETDATE() WHERE [path] IN ('/media/overview','/media/monitoring','/media/ingress','/media/nodes','/media/scheduling','/media/nodes/:id') AND [deleted_at] IS NULL;
-- zlm-global-sidebar-menu:down:end
