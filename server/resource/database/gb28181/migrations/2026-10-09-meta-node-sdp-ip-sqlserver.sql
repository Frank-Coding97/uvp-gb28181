IF COL_LENGTH(N'dbo.meta_node', N'sdp_ip') IS NULL
BEGIN
  ALTER TABLE [dbo].[meta_node] ADD [sdp_ip] VARCHAR(253) NOT NULL CONSTRAINT [df_meta_node_sdp_ip] DEFAULT ('');
END;