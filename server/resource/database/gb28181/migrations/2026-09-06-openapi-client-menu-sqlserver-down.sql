-- openapi-client-menu:down-begin
-- Remove only the page owned by this migration. Button API links and API rows
-- are intentionally retained; buttons are restored to their original root.

DELETE FROM [sys_role_menu]
WHERE [menu_id] IN (
  SELECT p.[id] FROM [sys_menu] p
  WHERE p.[path]=N'/gb28181/openapi-client' AND p.[name]=N'gb28181-openapi-client'
    AND p.[component]=N'gb28181/openapi-client/index' AND p.[deleted_at] IS NULL
);

UPDATE b
SET [parent_id]=0
FROM [sys_menu] b
WHERE b.[permission] IN (N'gb28181:openapi:client:read',N'gb28181:openapi:client:create',N'gb28181:openapi:client:grant',N'gb28181:openapi:client:rotate',N'gb28181:openapi:client:status',N'gb28181:openapi:client:audit')
  AND b.[deleted_at] IS NULL
  AND b.[parent_id] IN (
    SELECT p.[id] FROM [sys_menu] p
    WHERE p.[path]=N'/gb28181/openapi-client' AND p.[name]=N'gb28181-openapi-client'
      AND p.[component]=N'gb28181/openapi-client/index'
  );

UPDATE [sys_menu]
SET [deleted_at]=GETDATE(),[updated_at]=GETDATE()
WHERE [path]=N'/gb28181/openapi-client' AND [name]=N'gb28181-openapi-client'
  AND [component]=N'gb28181/openapi-client/index' AND [deleted_at] IS NULL;

-- openapi-client-menu:down-end
