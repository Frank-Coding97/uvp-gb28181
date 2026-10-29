IF COL_LENGTH(N'dbo.gb_cascade_platform', N'rtcp_keepalive') IS NOT NULL
BEGIN
  DECLARE @cascade_rtcp_keepalive_default NVARCHAR(128);
  SELECT @cascade_rtcp_keepalive_default = dc.name FROM sys.default_constraints dc
  JOIN sys.columns c ON c.object_id = dc.parent_object_id AND c.column_id = dc.parent_column_id
  WHERE dc.parent_object_id = OBJECT_ID(N'dbo.gb_cascade_platform') AND c.name = N'rtcp_keepalive';
  IF @cascade_rtcp_keepalive_default IS NOT NULL
    EXEC(N'ALTER TABLE [dbo].[gb_cascade_platform] DROP CONSTRAINT ' + QUOTENAME(@cascade_rtcp_keepalive_default));
  ALTER TABLE [dbo].[gb_cascade_platform] DROP COLUMN [rtcp_keepalive];
END;
