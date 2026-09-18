-- Cloud recording download task control APIs (SQL Server 2019+).
-- The content route is intentionally absent: it is authorized by a one-time HttpOnly cookie.
INSERT INTO [sys_api] ([title],[path],[method],[api_group],[created_at],[updated_at],[created_by])
SELECT v.title,v.path,v.method,N'GB28181 云端录像下载',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM (VALUES
  (N'创建云端录像下载','/api/gb28181/cloud-recordings/files/:id/downloads','POST'),
  (N'查询云端录像下载','/api/gb28181/cloud-recordings/downloads/:taskId','GET'),
  (N'取消云端录像下载','/api/gb28181/cloud-recordings/downloads/:taskId','DELETE')
) v(title,path,method)
WHERE NOT EXISTS (
  SELECT 1 FROM [sys_api] a
  WHERE a.[path]=v.path AND a.[method]=v.method AND a.[deleted_at] IS NULL
);

INSERT INTO [sys_menu_api] ([menu_id],[api_id])
SELECT m.[id],a.[id] FROM [sys_menu] m CROSS JOIN [sys_api] a
WHERE m.[path]='/gb28181/cloud-recordings' AND m.[deleted_at] IS NULL
  AND a.[api_group]=N'GB28181 云端录像下载'
  AND (
    (a.[path]='/api/gb28181/cloud-recordings/files/:id/downloads' AND a.[method]='POST')
    OR (a.[path]='/api/gb28181/cloud-recordings/downloads/:taskId' AND a.[method] IN ('GET','DELETE'))
  )
  AND a.[deleted_at] IS NULL
  AND NOT EXISTS (SELECT 1 FROM [sys_menu_api] x WHERE x.[menu_id]=m.[id] AND x.[api_id]=a.[id]);

INSERT INTO [sys_casbin_rule] ([ptype],[v0],[v1],[v2],[v3],[v4],[v5])
SELECT 'p','role_' + CAST(rm.[role_id] AS varchar(20)),a.[path],a.[method],'*','',''
FROM [sys_role_menu] rm
JOIN [sys_menu] m ON m.[id]=rm.[menu_id] CROSS JOIN [sys_api] a
WHERE m.[path]='/gb28181/cloud-recordings' AND m.[deleted_at] IS NULL
  AND a.[api_group]=N'GB28181 云端录像下载' AND a.[deleted_at] IS NULL
  AND (
    (a.[path]='/api/gb28181/cloud-recordings/files/:id/downloads' AND a.[method]='POST')
    OR (a.[path]='/api/gb28181/cloud-recordings/downloads/:taskId' AND a.[method] IN ('GET','DELETE'))
  )
  AND NOT EXISTS (
    SELECT 1 FROM [sys_casbin_rule] c
    WHERE c.[ptype]='p' AND c.[v0]='role_' + CAST(rm.[role_id] AS varchar(20))
      AND c.[v1]=a.[path] AND c.[v2]=a.[method] AND c.[v3]='*'
  );
