-- 回退「视频参数属性」：软删 API 与菜单绑定，再删掉落库表与新增列。
-- ⛔ 与既有 down 一致使用**软删除**语义（deleted_at），不是物理删 sys_api 行 ——
--    物理删会让"这条 API 曾经存在过"这段历史消失，而 sys_menu_api /
--    sys_casbin_rule 残留行会变成指向不存在 API 的孤儿规则。
-- ⚠️ 回滚会丢弃已回读的视频参数与通道码流清单（后者可随下次目录刷新重建）。
DELETE FROM sys_casbin_rule WHERE v1='/api/gb28181/device-mgmt/channel/:id/video-params' AND v2 IN ('GET','POST');
DELETE FROM sys_menu_api WHERE api_id IN (SELECT id FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/video-params' AND method IN ('GET','POST'));
UPDATE sys_api SET deleted_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP
WHERE path='/api/gb28181/device-mgmt/channel/:id/video-params' AND method IN ('GET','POST') AND deleted_at IS NULL;

DROP TABLE IF EXISTS `gb_device_video_param`;

-- ⛔ MySQL 不支持 `DROP COLUMN IF EXISTS`（那是 MariaDB 语法），故按既有 down 的写法直接删。
ALTER TABLE `gb_channel` DROP COLUMN `stream_number_list`;
