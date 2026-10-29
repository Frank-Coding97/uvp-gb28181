IF COL_LENGTH(N'dbo.meta_node', N'hook_ip') IS NULL
BEGIN
  ALTER TABLE [dbo].[meta_node] ADD [hook_ip] VARCHAR(45) NOT NULL CONSTRAINT [df_meta_node_hook_ip] DEFAULT ('');
END;
