-- Media node single-port RTP receive mode (see the MySQL variant for context).
IF COL_LENGTH(N'dbo.meta_node', N'rtp_receive_mode') IS NULL
BEGIN
  ALTER TABLE [dbo].[meta_node] ADD [rtp_receive_mode] VARCHAR(8) NOT NULL CONSTRAINT [df_meta_node_rtp_receive_mode] DEFAULT ('multi');
  ALTER TABLE [dbo].[meta_node] ADD [rtp_proxy_port] BIGINT NOT NULL CONSTRAINT [df_meta_node_rtp_proxy_port] DEFAULT (10000);
END;