-- openapi-client-menu:begin
-- T14 dynamic menu entry. This migration owns one page row and only reparents
-- the six pre-existing OpenAPI management buttons; it never creates APIs.
-- The temporary duplicate-key guard makes a foreign page/button collision fail
-- before any persistent row is changed.

IF OBJECT_ID('tempdb..#openapi_client_menu_guard') IS NOT NULL DROP TABLE #openapi_client_menu_guard;
CREATE TABLE #openapi_client_menu_guard ([id] TINYINT NOT NULL PRIMARY KEY);
INSERT INTO #openapi_client_menu_guard ([id])
SELECT 1 WHERE NOT EXISTS (SELECT 1 FROM #openapi_client_menu_guard WHERE [id]=1);
INSERT INTO #openapi_client_menu_guard ([id])
SELECT 1
WHERE
  EXISTS (
    SELECT 1 FROM [sys_menu]
    WHERE [deleted_at] IS NULL AND [path]=N'/gb28181/openapi-client'
      AND ([parent_id]<>0 OR COALESCE([name],N'')<>N'gb28181-openapi-client'
        OR COALESCE([component],N'')<>N'gb28181/openapi-client/index'
        OR COALESCE([title],N'')<>N'OpenAPI 客户端' OR COALESCE([redirect],N'')<>N''
        OR COALESCE([is_full],0)<>0 OR COALESCE([hide],0)<>0 OR COALESCE([disable],0)<>0
        OR COALESCE([keep_alive],0)<>0 OR COALESCE([affix],0)<>0 OR COALESCE([link],N'')<>N''
        OR COALESCE([iframe],0)<>0 OR COALESCE([svg_icon],N'')<>N'' OR COALESCE([icon],N'')<>N'lucide:KeyRound'
        OR COALESCE([sort],0)<>15 OR COALESCE([type],0)<>2 OR COALESCE([is_link],0)<>0 OR COALESCE([permission],N'')<>N'')
  )
  OR EXISTS (
    SELECT 1 FROM [sys_menu]
    WHERE [deleted_at] IS NULL AND [name]=N'gb28181-openapi-client'
      AND [path]<>N'/gb28181/openapi-client'
  )
  OR (SELECT COUNT(*) FROM [sys_menu] WHERE [deleted_at] IS NULL AND [path]=N'/gb28181/openapi-client')>1
  OR (SELECT COUNT(*) FROM [sys_menu] WHERE [deleted_at] IS NULL AND [permission]=N'gb28181:openapi:client:read')<>1
  OR (SELECT COUNT(*) FROM [sys_menu] WHERE [deleted_at] IS NULL AND [permission]=N'gb28181:openapi:client:create')<>1
  OR (SELECT COUNT(*) FROM [sys_menu] WHERE [deleted_at] IS NULL AND [permission]=N'gb28181:openapi:client:grant')<>1
  OR (SELECT COUNT(*) FROM [sys_menu] WHERE [deleted_at] IS NULL AND [permission]=N'gb28181:openapi:client:rotate')<>1
  OR (SELECT COUNT(*) FROM [sys_menu] WHERE [deleted_at] IS NULL AND [permission]=N'gb28181:openapi:client:status')<>1
  OR (SELECT COUNT(*) FROM [sys_menu] WHERE [deleted_at] IS NULL AND [permission]=N'gb28181:openapi:client:audit')<>1
  OR EXISTS (SELECT 1 FROM [sys_menu] WHERE [deleted_at] IS NULL AND [permission]=N'gb28181:openapi:client:read' AND (COALESCE([name],N'')<>N'Permission_gb28181_openapi_client_read' OR COALESCE([path],N'')<>N'' OR COALESCE([redirect],N'')<>N'' OR COALESCE([component],N'')<>N'' OR COALESCE([title],N'')<>N'查看 OpenAPI 客户端' OR COALESCE([is_full],0)<>0 OR COALESCE([hide],0)<>1 OR COALESCE([disable],0)<>0 OR COALESCE([keep_alive],0)<>0 OR COALESCE([affix],0)<>0 OR COALESCE([link],N'')<>N'' OR COALESCE([iframe],0)<>0 OR COALESCE([svg_icon],N'')<>N'' OR COALESCE([icon],N'')<>N'' OR COALESCE([sort],0)<>100 OR COALESCE([type],0)<>3 OR COALESCE([is_link],0)<>0))
  OR EXISTS (SELECT 1 FROM [sys_menu] WHERE [deleted_at] IS NULL AND [permission]=N'gb28181:openapi:client:create' AND (COALESCE([name],N'')<>N'Permission_gb28181_openapi_client_create' OR COALESCE([path],N'')<>N'' OR COALESCE([redirect],N'')<>N'' OR COALESCE([component],N'')<>N'' OR COALESCE([title],N'')<>N'创建 OpenAPI 客户端' OR COALESCE([is_full],0)<>0 OR COALESCE([hide],0)<>1 OR COALESCE([disable],0)<>0 OR COALESCE([keep_alive],0)<>0 OR COALESCE([affix],0)<>0 OR COALESCE([link],N'')<>N'' OR COALESCE([iframe],0)<>0 OR COALESCE([svg_icon],N'')<>N'' OR COALESCE([icon],N'')<>N'' OR COALESCE([sort],0)<>100 OR COALESCE([type],0)<>3 OR COALESCE([is_link],0)<>0))
  OR EXISTS (SELECT 1 FROM [sys_menu] WHERE [deleted_at] IS NULL AND [permission]=N'gb28181:openapi:client:grant' AND (COALESCE([name],N'')<>N'Permission_gb28181_openapi_client_grant' OR COALESCE([path],N'')<>N'' OR COALESCE([redirect],N'')<>N'' OR COALESCE([component],N'')<>N'' OR COALESCE([title],N'')<>N'分配 OpenAPI 客户端能力' OR COALESCE([is_full],0)<>0 OR COALESCE([hide],0)<>1 OR COALESCE([disable],0)<>0 OR COALESCE([keep_alive],0)<>0 OR COALESCE([affix],0)<>0 OR COALESCE([link],N'')<>N'' OR COALESCE([iframe],0)<>0 OR COALESCE([svg_icon],N'')<>N'' OR COALESCE([icon],N'')<>N'' OR COALESCE([sort],0)<>100 OR COALESCE([type],0)<>3 OR COALESCE([is_link],0)<>0))
  OR EXISTS (SELECT 1 FROM [sys_menu] WHERE [deleted_at] IS NULL AND [permission]=N'gb28181:openapi:client:rotate' AND (COALESCE([name],N'')<>N'Permission_gb28181_openapi_client_rotate' OR COALESCE([path],N'')<>N'' OR COALESCE([redirect],N'')<>N'' OR COALESCE([component],N'')<>N'' OR COALESCE([title],N'')<>N'轮换 OpenAPI 客户端密钥' OR COALESCE([is_full],0)<>0 OR COALESCE([hide],0)<>1 OR COALESCE([disable],0)<>0 OR COALESCE([keep_alive],0)<>0 OR COALESCE([affix],0)<>0 OR COALESCE([link],N'')<>N'' OR COALESCE([iframe],0)<>0 OR COALESCE([svg_icon],N'')<>N'' OR COALESCE([icon],N'')<>N'' OR COALESCE([sort],0)<>100 OR COALESCE([type],0)<>3 OR COALESCE([is_link],0)<>0))
  OR EXISTS (SELECT 1 FROM [sys_menu] WHERE [deleted_at] IS NULL AND [permission]=N'gb28181:openapi:client:status' AND (COALESCE([name],N'')<>N'Permission_gb28181_openapi_client_status' OR COALESCE([path],N'')<>N'' OR COALESCE([redirect],N'')<>N'' OR COALESCE([component],N'')<>N'' OR COALESCE([title],N'')<>N'启停或撤销 OpenAPI 客户端' OR COALESCE([is_full],0)<>0 OR COALESCE([hide],0)<>1 OR COALESCE([disable],0)<>0 OR COALESCE([keep_alive],0)<>0 OR COALESCE([affix],0)<>0 OR COALESCE([link],N'')<>N'' OR COALESCE([iframe],0)<>0 OR COALESCE([svg_icon],N'')<>N'' OR COALESCE([icon],N'')<>N'' OR COALESCE([sort],0)<>100 OR COALESCE([type],0)<>3 OR COALESCE([is_link],0)<>0))
  OR EXISTS (SELECT 1 FROM [sys_menu] WHERE [deleted_at] IS NULL AND [permission]=N'gb28181:openapi:client:audit' AND (COALESCE([name],N'')<>N'Permission_gb28181_openapi_client_audit' OR COALESCE([path],N'')<>N'' OR COALESCE([redirect],N'')<>N'' OR COALESCE([component],N'')<>N'' OR COALESCE([title],N'')<>N'查看 OpenAPI 客户端审计' OR COALESCE([is_full],0)<>0 OR COALESCE([hide],0)<>1 OR COALESCE([disable],0)<>0 OR COALESCE([keep_alive],0)<>0 OR COALESCE([affix],0)<>0 OR COALESCE([link],N'')<>N'' OR COALESCE([iframe],0)<>0 OR COALESCE([svg_icon],N'')<>N'' OR COALESCE([icon],N'')<>N'' OR COALESCE([sort],0)<>100 OR COALESCE([type],0)<>3 OR COALESCE([is_link],0)<>0))
  OR EXISTS (
    SELECT 1 FROM [sys_menu] b
    WHERE b.[deleted_at] IS NULL
      AND b.[permission] IN (N'gb28181:openapi:client:read',N'gb28181:openapi:client:create',N'gb28181:openapi:client:grant',N'gb28181:openapi:client:rotate',N'gb28181:openapi:client:status',N'gb28181:openapi:client:audit')
      AND b.[parent_id]<>0
      AND (NOT EXISTS (SELECT 1 FROM [sys_menu] p WHERE p.[deleted_at] IS NULL AND p.[path]=N'/gb28181/openapi-client' AND p.[name]=N'gb28181-openapi-client' AND p.[component]=N'gb28181/openapi-client/index' AND p.[parent_id]=0 AND p.[type]=2)
        OR b.[parent_id]<>(SELECT MIN(p.[id]) FROM [sys_menu] p WHERE p.[deleted_at] IS NULL AND p.[path]=N'/gb28181/openapi-client' AND p.[name]=N'gb28181-openapi-client' AND p.[component]=N'gb28181/openapi-client/index' AND p.[parent_id]=0 AND p.[type]=2))
  );
DROP TABLE #openapi_client_menu_guard;

INSERT INTO [sys_menu] ([parent_id],[path],[name],[redirect],[component],[title],[is_full],[hide],[disable],[keep_alive],[affix],[link],[iframe],[svg_icon],[icon],[sort],[type],[is_link],[permission],[created_at],[updated_at],[created_by])
SELECT 0,N'/gb28181/openapi-client',N'gb28181-openapi-client',N'',N'gb28181/openapi-client/index',N'OpenAPI 客户端',0,0,0,0,0,N'',0,N'',N'lucide:KeyRound',15,2,0,N'',GETDATE(),GETDATE(),1
WHERE NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [path]=N'/gb28181/openapi-client' AND [deleted_at] IS NULL);

UPDATE b
SET [parent_id]=(SELECT MIN(p.[id]) FROM [sys_menu] p WHERE p.[path]=N'/gb28181/openapi-client' AND p.[name]=N'gb28181-openapi-client' AND p.[component]=N'gb28181/openapi-client/index' AND p.[deleted_at] IS NULL)
FROM [sys_menu] b
WHERE b.[permission] IN (N'gb28181:openapi:client:read',N'gb28181:openapi:client:create',N'gb28181:openapi:client:grant',N'gb28181:openapi:client:rotate',N'gb28181:openapi:client:status',N'gb28181:openapi:client:audit')
  AND b.[deleted_at] IS NULL AND b.[parent_id]=0;

INSERT INTO [sys_role_menu] ([role_id],[menu_id])
SELECT r.[id],m.[id]
FROM [sys_role] r CROSS JOIN [sys_menu] m
WHERE r.[id]=1 AND r.[status]=1 AND r.[deleted_at] IS NULL
  AND m.[path]=N'/gb28181/openapi-client' AND m.[name]=N'gb28181-openapi-client' AND m.[component]=N'gb28181/openapi-client/index' AND m.[deleted_at] IS NULL
  AND NOT EXISTS (SELECT 1 FROM [sys_role_menu] x WHERE x.[role_id]=r.[id] AND x.[menu_id]=m.[id]);

-- openapi-client-menu:end
