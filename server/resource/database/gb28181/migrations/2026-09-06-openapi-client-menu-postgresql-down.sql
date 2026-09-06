-- openapi-client-menu:down-begin
-- Remove only the page owned by this migration. Button API links and API rows
-- are intentionally retained; buttons are restored to their original root.

DELETE FROM sys_role_menu
WHERE menu_id IN (
  SELECT id FROM (
    SELECT id FROM sys_menu
    WHERE path='/gb28181/openapi-client' AND name='gb28181-openapi-client'
      AND component='gb28181/openapi-client/index' AND deleted_at IS NULL
  ) AS page
);

UPDATE sys_menu b
SET parent_id=0
WHERE b.permission IN ('gb28181:openapi:client:read','gb28181:openapi:client:create','gb28181:openapi:client:grant','gb28181:openapi:client:rotate','gb28181:openapi:client:status','gb28181:openapi:client:audit')
  AND b.deleted_at IS NULL
  AND b.parent_id IN (
    SELECT id FROM (
      SELECT id FROM sys_menu
      WHERE path='/gb28181/openapi-client' AND name='gb28181-openapi-client'
        AND component='gb28181/openapi-client/index'
    ) AS page
  );

UPDATE sys_menu
SET deleted_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/openapi-client' AND name='gb28181-openapi-client'
  AND component='gb28181/openapi-client/index' AND deleted_at IS NULL;

-- openapi-client-menu:down-end
