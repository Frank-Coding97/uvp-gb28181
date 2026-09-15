-- drop-catalog-anomaly-menu:start
-- Physically drop the "catalog anomaly" menu (SQL Server, idempotent).
-- Its page gb28181/device-mgmt/anomaly/index only re-exports the device list page,
-- so the extra entry opens exactly the same view as /gb28181/device-mgmt/index.
-- Only the menu and its permission bindings are removed; gb_anomaly_record and the
-- catalog ingest pipeline stay untouched.
DELETE FROM [sys_casbin_rule] WHERE [v1] IN (N'/api/gb28181/device-mgmt/anomaly',N'/api/gb28181/device-mgmt/anomaly/:id/resolve',N'/api/gb28181/device-mgmt/anomaly/batch-resolve');
DELETE FROM [sys_menu_api] WHERE [menu_id] IN (SELECT [id] FROM [sys_menu] WHERE [path]=N'/gb28181/device-mgmt/anomaly');
DELETE FROM [sys_role_menu] WHERE [menu_id] IN (SELECT [id] FROM [sys_menu] WHERE [path]=N'/gb28181/device-mgmt/anomaly');
DELETE FROM [sys_menu] WHERE [path]=N'/gb28181/device-mgmt/anomaly';
-- drop-catalog-anomaly-menu:end
