-- 回退 2026-09-18-channel-video-param-sqlserver.sql —— SQL Server 方言
-- ⛔ 与既有 down 一致使用**软删除**语义（deleted_at），不是物理删 sys_api 行。
-- ⛔ SQL Server 不允许直接 DROP 带默认值约束的列（报 "is dependent on column"），
--    必须先 DROP CONSTRAINT 再 DROP COLUMN。
-- ⚠️ 回滚会丢弃已回读的视频参数与通道码流清单（后者可随下次目录刷新重建）。
DELETE FROM sys_casbin_rule WHERE v1=N'/api/gb28181/device-mgmt/channel/:id/video-params' AND v2 IN (N'GET',N'POST');
DELETE FROM sys_menu_api WHERE api_id IN (SELECT id FROM sys_api WHERE path=N'/api/gb28181/device-mgmt/channel/:id/video-params' AND method IN (N'GET',N'POST'));
UPDATE sys_api SET deleted_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP
WHERE path=N'/api/gb28181/device-mgmt/channel/:id/video-params' AND method IN (N'GET',N'POST') AND deleted_at IS NULL;

IF OBJECT_ID(N'gb_device_video_param', N'U') IS NOT NULL
    DROP TABLE [gb_device_video_param];

IF OBJECT_ID(N'df_gb_channel_stream_number_list', N'D') IS NOT NULL
    ALTER TABLE [gb_channel] DROP CONSTRAINT [df_gb_channel_stream_number_list];
IF COL_LENGTH(N'gb_channel', N'stream_number_list') IS NOT NULL
    ALTER TABLE [gb_channel] DROP COLUMN [stream_number_list];
