-- 回滚 2026-09-15-reorder-top-level-menus-postgresql.sql：把一级菜单 sort 恢复为重排前的值（PostgreSQL 方言）。
-- 定位同样用 path。原状态里 /media 与 /gb28181/device-assignment 都是 9（撞号）—— 如实回滚。
UPDATE sys_menu SET sort=1, updated_at=CURRENT_TIMESTAMP WHERE (parent_id=0 OR parent_id IS NULL) AND path='/home' AND deleted_at IS NULL AND (sort IS NULL OR sort<>1);
UPDATE sys_menu SET sort=2, updated_at=CURRENT_TIMESTAMP WHERE (parent_id=0 OR parent_id IS NULL) AND path IN ('/gb28181/device-mgmt/index','/gb28181/device-mgmt') AND deleted_at IS NULL AND (sort IS NULL OR sort<>2);
UPDATE sys_menu SET sort=3, updated_at=CURRENT_TIMESTAMP WHERE (parent_id=0 OR parent_id IS NULL) AND path='/gb28181/multi-screen-playback' AND deleted_at IS NULL AND (sort IS NULL OR sort<>3);
UPDATE sys_menu SET sort=4, updated_at=CURRENT_TIMESTAMP WHERE (parent_id=0 OR parent_id IS NULL) AND path='/gb28181/alarm-management' AND deleted_at IS NULL AND (sort IS NULL OR sort<>4);
UPDATE sys_menu SET sort=5, updated_at=CURRENT_TIMESTAMP WHERE (parent_id=0 OR parent_id IS NULL) AND path='/security-preview' AND deleted_at IS NULL AND (sort IS NULL OR sort<>5);
UPDATE sys_menu SET sort=6, updated_at=CURRENT_TIMESTAMP WHERE (parent_id=0 OR parent_id IS NULL) AND path='/gb28181/sip/platform' AND deleted_at IS NULL AND (sort IS NULL OR sort<>6);
UPDATE sys_menu SET sort=7, updated_at=CURRENT_TIMESTAMP WHERE (parent_id=0 OR parent_id IS NULL) AND path='/gb28181/sip/config' AND deleted_at IS NULL AND (sort IS NULL OR sort<>7);
UPDATE sys_menu SET sort=8, updated_at=CURRENT_TIMESTAMP WHERE (parent_id=0 OR parent_id IS NULL) AND path='/gb28181/sip-traces' AND deleted_at IS NULL AND (sort IS NULL OR sort<>8);
UPDATE sys_menu SET sort=9, updated_at=CURRENT_TIMESTAMP WHERE (parent_id=0 OR parent_id IS NULL) AND path='/media' AND deleted_at IS NULL AND (sort IS NULL OR sort<>9);
UPDATE sys_menu SET sort=9, updated_at=CURRENT_TIMESTAMP WHERE (parent_id=0 OR parent_id IS NULL) AND path='/gb28181/device-assignment' AND deleted_at IS NULL AND (sort IS NULL OR sort<>9);
UPDATE sys_menu SET sort=13, updated_at=CURRENT_TIMESTAMP WHERE (parent_id=0 OR parent_id IS NULL) AND path='/gb28181/cascade' AND deleted_at IS NULL AND (sort IS NULL OR sort<>13);
UPDATE sys_menu SET sort=15, updated_at=CURRENT_TIMESTAMP WHERE (parent_id=0 OR parent_id IS NULL) AND path='/gb28181/openapi-client' AND deleted_at IS NULL AND (sort IS NULL OR sort<>15);
UPDATE sys_menu SET sort=35, updated_at=CURRENT_TIMESTAMP WHERE (parent_id=0 OR parent_id IS NULL) AND path='/gb28181/cloud-recordings' AND deleted_at IS NULL AND (sort IS NULL OR sort<>35);
UPDATE sys_menu SET sort=36, updated_at=CURRENT_TIMESTAMP WHERE (parent_id=0 OR parent_id IS NULL) AND path='/gb28181/recording-schedules' AND deleted_at IS NULL AND (sort IS NULL OR sort<>36);
UPDATE sys_menu SET sort=99, updated_at=CURRENT_TIMESTAMP WHERE (parent_id=0 OR parent_id IS NULL) AND path='/gb28181/device-record-playback/:channelId' AND deleted_at IS NULL AND (sort IS NULL OR sort<>99);
UPDATE sys_menu SET sort=0, updated_at=CURRENT_TIMESTAMP WHERE (parent_id=0 OR parent_id IS NULL) AND path='/system' AND deleted_at IS NULL AND (sort IS NULL OR sort<>0);
UPDATE sys_menu SET sort=0, updated_at=CURRENT_TIMESTAMP WHERE (parent_id=0 OR parent_id IS NULL) AND path='/sysjobs' AND deleted_at IS NULL AND (sort IS NULL OR sort<>0);
UPDATE sys_menu SET sort=0, updated_at=CURRENT_TIMESTAMP WHERE (parent_id=0 OR parent_id IS NULL) AND path='/demo' AND deleted_at IS NULL AND (sort IS NULL OR sort<>0);
