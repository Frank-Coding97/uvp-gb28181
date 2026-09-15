-- 回滚全局播放配置 API 权限（PostgreSQL）。
DELETE FROM sys_casbin_rule WHERE v1='/api/gb28181/sip/service-config/playback-settings' AND v2 IN ('GET','PUT');
DELETE FROM sys_menu_api ma USING sys_api a WHERE a.id=ma.api_id AND a.path='/api/gb28181/sip/service-config/playback-settings';
DELETE FROM sys_api WHERE path='/api/gb28181/sip/service-config/playback-settings' AND method IN ('GET','PUT');
