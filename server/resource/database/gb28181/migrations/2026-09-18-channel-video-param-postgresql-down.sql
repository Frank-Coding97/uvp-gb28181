-- 回退 2026-09-18-channel-video-param-postgresql.sql —— PostgreSQL 方言
-- ⛔ 与既有 down 一致使用**软删除**语义（deleted_at），不是物理删 sys_api 行。
-- ⚠️ 回滚会丢弃已回读的视频参数与通道码流清单（后者可随下次目录刷新重建）。
DELETE FROM sys_casbin_rule WHERE v1='/api/gb28181/device-mgmt/channel/:id/video-params' AND v2 IN ('GET','POST');
DELETE FROM sys_menu_api WHERE api_id IN (SELECT id FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/video-params' AND method IN ('GET','POST'));
UPDATE sys_api SET deleted_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP
WHERE path='/api/gb28181/device-mgmt/channel/:id/video-params' AND method IN ('GET','POST') AND deleted_at IS NULL;

DROP TABLE IF EXISTS gb_device_video_param;

ALTER TABLE gb_channel DROP COLUMN IF EXISTS stream_number_list;
