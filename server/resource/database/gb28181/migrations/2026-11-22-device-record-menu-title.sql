-- 旧隐藏路由（/gb28181/device-record-playback/:channelId）与「录像中心 / 设备录像」统一叫法。
--
-- 这条隐藏菜单不是死代码：通道列表点「录像」过去就按 name 跳到这里，页签标题取的
-- 就是它的 title ⇒ 同一页一会儿叫「设备录像回放」、一会儿叫「设备录像」。
-- 本次只改 title：path / name / component / permission 一律不动 ——
--   老书签、`gb28181-device-record-playback` 这个 route name、以及一批按路径断言的测试
--   都挂在它们身上。
-- ⛔ hide=1 的菜单**仍然进路由表**，「藏起来」不等于「不出现在页签上」。
UPDATE sys_menu SET title='设备录像', updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/device-record-playback/:channelId' AND title='设备录像回放' AND deleted_at IS NULL;
