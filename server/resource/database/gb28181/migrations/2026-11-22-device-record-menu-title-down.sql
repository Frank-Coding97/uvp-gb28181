-- 回滚：把标题改回「设备录像回放」。
-- 只认自己写进去的那个值（title='设备录像'），管理员手工改过的名字不会被覆盖。
UPDATE sys_menu SET title='设备录像回放', updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/device-record-playback/:channelId' AND title='设备录像' AND deleted_at IS NULL;
