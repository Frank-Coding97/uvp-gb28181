-- media-management-baseline:start
-- Media management menus and exact backend permission bindings (postgresql, idempotent).
INSERT INTO sys_menu (parent_id,path,name,redirect,component,title,icon,sort,type,permission,keep_alive,created_at,updated_at,created_by)
SELECT 0,'/media','Media','/gb28181/zlm/overview','','流媒体管理','lucide:Clapperboard',9,1,'',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/media' AND deleted_at IS NULL);
UPDATE sys_menu SET redirect='/gb28181/zlm/overview',title='流媒体管理',icon='lucide:Clapperboard',sort=9,updated_at=CURRENT_TIMESTAMP
WHERE path='/media' AND deleted_at IS NULL;

UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),redirect='',title='集群概览',icon='lucide:LayoutDashboard',sort=10,updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/zlm/overview' AND deleted_at IS NULL;
INSERT INTO sys_menu (parent_id,path,name,redirect,component,title,icon,sort,type,permission,hide,created_at,updated_at,created_by)
SELECT (SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),'/gb28181/zlm/overview','gb28181-zlm-overview','','gb28181/zlm/ClusterOverview','集群概览','lucide:LayoutDashboard',10,2,'',0,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE (SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/gb28181/zlm/overview' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),redirect='',title='节点管理',icon='lucide:Server',sort=11,updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/zlm/nodes' AND deleted_at IS NULL;
INSERT INTO sys_menu (parent_id,path,name,redirect,component,title,icon,sort,type,permission,hide,created_at,updated_at,created_by)
SELECT (SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),'/gb28181/zlm/nodes','gb28181-zlm-nodes','','gb28181/zlm/NodeList','节点管理','lucide:Server',11,2,'',0,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE (SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/gb28181/zlm/nodes' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),redirect='',title='调度策略',icon='lucide:Workflow',sort=12,updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/zlm/scheduler' AND deleted_at IS NULL;
INSERT INTO sys_menu (parent_id,path,name,redirect,component,title,icon,sort,type,permission,hide,created_at,updated_at,created_by)
SELECT (SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),'/gb28181/zlm/scheduler','gb28181-zlm-scheduler-strategy','','gb28181/zlm/SchedulerStrategy','调度策略','lucide:Workflow',12,2,'',0,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE (SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/gb28181/zlm/scheduler' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),redirect='',title='调度日志',icon='lucide:History',sort=13,updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/zlm/scheduler/logs' AND deleted_at IS NULL;
INSERT INTO sys_menu (parent_id,path,name,redirect,component,title,icon,sort,type,permission,hide,created_at,updated_at,created_by)
SELECT (SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),'/gb28181/zlm/scheduler/logs','gb28181-zlm-scheduler-log','','gb28181/zlm/SchedulerLog','调度日志','lucide:History',13,2,'',0,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE (SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/gb28181/zlm/scheduler/logs' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),redirect='',title='运行监控',icon='lucide:Activity',sort=20,updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/zlm/runtime' AND deleted_at IS NULL;
INSERT INTO sys_menu (parent_id,path,name,redirect,component,title,icon,sort,type,permission,hide,created_at,updated_at,created_by)
SELECT (SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),'/gb28181/zlm/runtime','gb28181-zlm-runtime','','gb28181/zlm/RuntimeOverview','运行监控','lucide:Activity',20,2,'',0,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE (SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/gb28181/zlm/runtime' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),redirect='',title='流媒体',icon='lucide:RadioTower',sort=21,updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/zlm/streams' AND deleted_at IS NULL;
INSERT INTO sys_menu (parent_id,path,name,redirect,component,title,icon,sort,type,permission,hide,created_at,updated_at,created_by)
SELECT (SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),'/gb28181/zlm/streams','gb28181-zlm-streams','','gb28181/zlm/StreamManagement','流媒体','lucide:RadioTower',21,2,'',0,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE (SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/gb28181/zlm/streams' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),redirect='',title='会话管理',icon='lucide:Users',sort=22,updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/zlm/sessions' AND deleted_at IS NULL;
INSERT INTO sys_menu (parent_id,path,name,redirect,component,title,icon,sort,type,permission,hide,created_at,updated_at,created_by)
SELECT (SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),'/gb28181/zlm/sessions','gb28181-zlm-sessions','','gb28181/zlm/SessionManagement','会话管理','lucide:Users',22,2,'',0,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE (SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/gb28181/zlm/sessions' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),redirect='',title='拉流代理',icon='lucide:Network',sort=30,updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/zlm/proxies' AND deleted_at IS NULL;
INSERT INTO sys_menu (parent_id,path,name,redirect,component,title,icon,sort,type,permission,hide,created_at,updated_at,created_by)
SELECT (SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),'/gb28181/zlm/proxies','gb28181-zlm-proxies','','gb28181/zlm/ProxyManagement','拉流代理','lucide:Network',30,2,'',0,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE (SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/gb28181/zlm/proxies' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),redirect='',title='FFmpeg 源',icon='lucide:Clapperboard',sort=31,updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/zlm/ffmpeg-sources' AND deleted_at IS NULL;
INSERT INTO sys_menu (parent_id,path,name,redirect,component,title,icon,sort,type,permission,hide,created_at,updated_at,created_by)
SELECT (SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),'/gb28181/zlm/ffmpeg-sources','gb28181-zlm-ffmpeg-sources','','gb28181/zlm/FFmpegSources','FFmpeg 源','lucide:Clapperboard',31,2,'',0,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE (SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/gb28181/zlm/ffmpeg-sources' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),redirect='',title='RTP 服务',icon='lucide:Waypoints',sort=32,updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/zlm/rtp-servers' AND deleted_at IS NULL;
INSERT INTO sys_menu (parent_id,path,name,redirect,component,title,icon,sort,type,permission,hide,created_at,updated_at,created_by)
SELECT (SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),'/gb28181/zlm/rtp-servers','gb28181-zlm-rtp-servers','','gb28181/zlm/RTPServices','RTP 服务','lucide:Waypoints',32,2,'',0,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE (SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/gb28181/zlm/rtp-servers' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),redirect='',title='录制管理',icon='lucide:Cloud',sort=40,updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/cloud-recordings' AND deleted_at IS NULL;
INSERT INTO sys_menu (parent_id,path,name,redirect,component,title,icon,sort,type,permission,hide,created_at,updated_at,created_by)
SELECT (SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),'/gb28181/cloud-recordings','gb28181-cloud-recordings','','gb28181/cloud-recordings/index','录制管理','lucide:Cloud',40,2,'gb28181:recording:view',0,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE (SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),redirect='',title='录像计划',icon='lucide:CalendarClock',sort=41,updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/recording-schedules' AND deleted_at IS NULL;
INSERT INTO sys_menu (parent_id,path,name,redirect,component,title,icon,sort,type,permission,hide,created_at,updated_at,created_by)
SELECT (SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),'/gb28181/recording-schedules','gb28181-recording-schedules','','gb28181/recording-schedules/index','录像计划','lucide:CalendarClock',41,2,'gb28181:recording-plan:view',0,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE (SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/gb28181/recording-schedules' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),redirect='',title='服务配置',icon='lucide:Settings2',sort=42,updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/zlm/config' AND deleted_at IS NULL;
INSERT INTO sys_menu (parent_id,path,name,redirect,component,title,icon,sort,type,permission,hide,created_at,updated_at,created_by)
SELECT (SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),'/gb28181/zlm/config','gb28181-zlm-config','','gb28181/zlm/ServerConfig','服务配置','lucide:Settings2',42,2,'',0,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE (SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/gb28181/zlm/config' AND deleted_at IS NULL);

INSERT INTO sys_menu (parent_id,path,name,component,title,type,permission,hide,created_at,updated_at,created_by)
SELECT p.id,'','','','管理节点',3,'gb28181:zlm:node:manage',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM sys_menu p
WHERE p.path='/gb28181/zlm/nodes' AND p.deleted_at IS NULL
AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:zlm:node:manage' AND deleted_at IS NULL);

INSERT INTO sys_menu (parent_id,path,name,component,title,type,permission,hide,created_at,updated_at,created_by)
SELECT p.id,'','','','踢除节点会话',3,'gb28181:zlm:node:kick',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM sys_menu p
WHERE p.path='/gb28181/zlm/nodes' AND p.deleted_at IS NULL
AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:zlm:node:kick' AND deleted_at IS NULL);

INSERT INTO sys_menu (parent_id,path,name,component,title,type,permission,hide,created_at,updated_at,created_by)
SELECT p.id,'','','','切换调度策略',3,'gb28181:zlm:scheduler:manage',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM sys_menu p
WHERE p.path='/gb28181/zlm/scheduler' AND p.deleted_at IS NULL
AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:zlm:scheduler:manage' AND deleted_at IS NULL);

INSERT INTO sys_menu (parent_id,path,name,component,title,type,permission,hide,created_at,updated_at,created_by)
SELECT p.id,'','','','预览与截图',3,'gb28181:zlm:stream:preview',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM sys_menu p
WHERE p.path='/gb28181/zlm/streams' AND p.deleted_at IS NULL
AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:zlm:stream:preview' AND deleted_at IS NULL);

INSERT INTO sys_menu (parent_id,path,name,component,title,type,permission,hide,created_at,updated_at,created_by)
SELECT p.id,'','','','关闭流',3,'gb28181:zlm:stream:close',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM sys_menu p
WHERE p.path='/gb28181/zlm/streams' AND p.deleted_at IS NULL
AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:zlm:stream:close' AND deleted_at IS NULL);

INSERT INTO sys_menu (parent_id,path,name,component,title,type,permission,hide,created_at,updated_at,created_by)
SELECT p.id,'','','','强制关闭流',3,'gb28181:zlm:stream:force-close',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM sys_menu p
WHERE p.path='/gb28181/zlm/streams' AND p.deleted_at IS NULL
AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:zlm:stream:force-close' AND deleted_at IS NULL);

INSERT INTO sys_menu (parent_id,path,name,component,title,type,permission,hide,created_at,updated_at,created_by)
SELECT p.id,'','','','踢除会话',3,'gb28181:zlm:session:kick',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM sys_menu p
WHERE p.path='/gb28181/zlm/sessions' AND p.deleted_at IS NULL
AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:zlm:session:kick' AND deleted_at IS NULL);

INSERT INTO sys_menu (parent_id,path,name,component,title,type,permission,hide,created_at,updated_at,created_by)
SELECT p.id,'','','','管理代理',3,'gb28181:zlm:proxy:manage',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM sys_menu p
WHERE p.path='/gb28181/zlm/proxies' AND p.deleted_at IS NULL
AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:zlm:proxy:manage' AND deleted_at IS NULL);

INSERT INTO sys_menu (parent_id,path,name,component,title,type,permission,hide,created_at,updated_at,created_by)
SELECT p.id,'','','','管理 FFmpeg 源',3,'gb28181:zlm:ffmpeg:manage',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM sys_menu p
WHERE p.path='/gb28181/zlm/ffmpeg-sources' AND p.deleted_at IS NULL
AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:zlm:ffmpeg:manage' AND deleted_at IS NULL);

INSERT INTO sys_menu (parent_id,path,name,component,title,type,permission,hide,created_at,updated_at,created_by)
SELECT p.id,'','','','管理 RTP 服务',3,'gb28181:zlm:rtp:manage',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM sys_menu p
WHERE p.path='/gb28181/zlm/rtp-servers' AND p.deleted_at IS NULL
AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:zlm:rtp:manage' AND deleted_at IS NULL);

INSERT INTO sys_menu (parent_id,path,name,component,title,type,permission,hide,created_at,updated_at,created_by)
SELECT p.id,'','','','强制关闭 RTP 服务',3,'gb28181:zlm:rtp:force-close',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM sys_menu p
WHERE p.path='/gb28181/zlm/rtp-servers' AND p.deleted_at IS NULL
AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:zlm:rtp:force-close' AND deleted_at IS NULL);

INSERT INTO sys_menu (parent_id,path,name,component,title,type,permission,hide,created_at,updated_at,created_by)
SELECT p.id,'','','','手工录制控制',3,'gb28181:recording:control',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM sys_menu p
WHERE p.path='/gb28181/cloud-recordings' AND p.deleted_at IS NULL
AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:recording:control' AND deleted_at IS NULL);

INSERT INTO sys_menu (parent_id,path,name,component,title,type,permission,hide,created_at,updated_at,created_by)
SELECT p.id,'','','','强制停止录制',3,'gb28181:recording:force-stop',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM sys_menu p
WHERE p.path='/gb28181/cloud-recordings' AND p.deleted_at IS NULL
AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:recording:force-stop' AND deleted_at IS NULL);

INSERT INTO sys_menu (parent_id,path,name,component,title,type,permission,hide,created_at,updated_at,created_by)
SELECT p.id,'','','','更新服务配置',3,'gb28181:zlm:config:update',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM sys_menu p
WHERE p.path='/gb28181/zlm/config' AND p.deleted_at IS NULL
AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:zlm:config:update' AND deleted_at IS NULL);

INSERT INTO sys_menu (parent_id,path,name,component,title,type,permission,hide,created_at,updated_at,created_by)
SELECT p.id,'','','','重启媒体服务',3,'gb28181:zlm:restart',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM sys_menu p
WHERE p.path='/gb28181/zlm/config' AND p.deleted_at IS NULL
AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:zlm:restart' AND deleted_at IS NULL);

INSERT INTO sys_role_menu (role_id,menu_id)
SELECT 1,m.id FROM sys_menu m
WHERE m.deleted_at IS NULL AND (m.path IN ('/media','/gb28181/zlm/overview','/gb28181/zlm/nodes','/gb28181/zlm/scheduler','/gb28181/zlm/scheduler/logs','/gb28181/zlm/runtime','/gb28181/zlm/streams','/gb28181/zlm/sessions','/gb28181/zlm/proxies','/gb28181/zlm/ffmpeg-sources','/gb28181/zlm/rtp-servers','/gb28181/cloud-recordings','/gb28181/recording-schedules','/gb28181/zlm/config')
OR m.permission IN ('gb28181:zlm:node:manage','gb28181:zlm:node:kick','gb28181:zlm:scheduler:manage','gb28181:zlm:stream:preview','gb28181:zlm:stream:close','gb28181:zlm:stream:force-close','gb28181:zlm:session:kick','gb28181:zlm:proxy:manage','gb28181:zlm:ffmpeg:manage','gb28181:zlm:rtp:manage','gb28181:zlm:rtp:force-close','gb28181:recording:control','gb28181:recording:force-stop','gb28181:zlm:config:update','gb28181:zlm:restart','gb28181:recording:view','gb28181:recording:reconcile','gb28181:recording:delete','gb28181:recording:stop','gb28181:recording-plan:view','gb28181:recording-plan:maintain','gb28181:recording-plan:assign'))
AND NOT EXISTS (SELECT 1 FROM sys_role_menu x WHERE x.role_id=1 AND x.menu_id=m.id);

INSERT INTO sys_api (title,path,method,api_group,created_at,updated_at,created_by)
SELECT v.title,v.path,v.method,'GB28181 媒体管理',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM (VALUES
  ('媒体管理 GET zlm/overview','/api/gb28181/zlm/overview','GET'),
  ('媒体管理 GET zlm/nodes','/api/gb28181/zlm/nodes','GET'),
  ('媒体管理 GET zlm/nodes/:id','/api/gb28181/zlm/nodes/:id','GET'),
  ('媒体管理 GET zlm/nodes/:id/config','/api/gb28181/zlm/nodes/:id/config','GET'),
  ('媒体管理 GET zlm/scheduler','/api/gb28181/zlm/scheduler','GET'),
  ('媒体管理 GET zlm/scheduler/logs','/api/gb28181/zlm/scheduler/logs','GET'),
  ('媒体管理 GET zlm/nodes/:id/runtime','/api/gb28181/zlm/nodes/:id/runtime','GET'),
  ('媒体管理 GET zlm/streams','/api/gb28181/zlm/streams','GET'),
  ('媒体管理 GET zlm/nodes/:id/streams','/api/gb28181/zlm/nodes/:id/streams','GET'),
  ('媒体管理 GET zlm/nodes/:id/streams/detail','/api/gb28181/zlm/nodes/:id/streams/detail','GET'),
  ('媒体管理 GET zlm/nodes/:id/streams/viewers','/api/gb28181/zlm/nodes/:id/streams/viewers','GET'),
  ('媒体管理 GET zlm/nodes/:id/sessions/network','/api/gb28181/zlm/nodes/:id/sessions/network','GET'),
  ('媒体管理 GET zlm/nodes/:id/sessions/viewers','/api/gb28181/zlm/nodes/:id/sessions/viewers','GET'),
  ('媒体管理 GET zlm/nodes/:id/proxies/pull','/api/gb28181/zlm/nodes/:id/proxies/pull','GET'),
  ('媒体管理 GET zlm/nodes/:id/proxies/pull/:key','/api/gb28181/zlm/nodes/:id/proxies/pull/:key','GET'),
  ('媒体管理 GET zlm/nodes/:id/proxies/push','/api/gb28181/zlm/nodes/:id/proxies/push','GET'),
  ('媒体管理 GET zlm/nodes/:id/proxies/push/:key','/api/gb28181/zlm/nodes/:id/proxies/push/:key','GET'),
  ('媒体管理 GET zlm/nodes/:id/ffmpeg-sources','/api/gb28181/zlm/nodes/:id/ffmpeg-sources','GET'),
  ('媒体管理 GET zlm/nodes/:id/rtp-servers','/api/gb28181/zlm/nodes/:id/rtp-servers','GET'),
  ('媒体管理 GET cloud-recordings/files','/api/gb28181/cloud-recordings/files','GET'),
  ('媒体管理 GET cloud-recordings/files/options','/api/gb28181/cloud-recordings/files/options','GET'),
  ('媒体管理 GET cloud-recordings/files/:id','/api/gb28181/cloud-recordings/files/:id','GET'),
  ('媒体管理 POST cloud-recordings/files/:id/access','/api/gb28181/cloud-recordings/files/:id/access','POST'),
  ('媒体管理 POST cloud-recordings/files/:id/downloads','/api/gb28181/cloud-recordings/files/:id/downloads','POST'),
  ('媒体管理 GET cloud-recordings/downloads/:taskId','/api/gb28181/cloud-recordings/downloads/:taskId','GET'),
  ('媒体管理 DELETE cloud-recordings/downloads/:taskId','/api/gb28181/cloud-recordings/downloads/:taskId','DELETE'),
  ('媒体管理 GET cloud-recordings/active','/api/gb28181/cloud-recordings/active','GET'),
  ('媒体管理 GET zlm/nodes/:id/recordings/runtime/status','/api/gb28181/zlm/nodes/:id/recordings/runtime/status','GET'),
  ('媒体管理 GET cloud-recordings/reconciliations','/api/gb28181/cloud-recordings/reconciliations','GET'),
  ('媒体管理 POST cloud-recordings/reconciliations','/api/gb28181/cloud-recordings/reconciliations','POST'),
  ('媒体管理 POST cloud-recordings/files/batch-delete','/api/gb28181/cloud-recordings/files/batch-delete','POST'),
  ('媒体管理 DELETE cloud-recordings/files/:id','/api/gb28181/cloud-recordings/files/:id','DELETE'),
  ('媒体管理 POST cloud-recordings/active/:id/stop','/api/gb28181/cloud-recordings/active/:id/stop','POST'),
  ('媒体管理 GET recording-plans','/api/gb28181/recording-plans','GET'),
  ('媒体管理 GET recording-plans/:id','/api/gb28181/recording-plans/:id','GET'),
  ('媒体管理 GET recording-plans/:id/channels','/api/gb28181/recording-plans/:id/channels','GET'),
  ('媒体管理 GET recording-plans/channels/:channelId/diagnosis','/api/gb28181/recording-plans/channels/:channelId/diagnosis','GET'),
  ('媒体管理 GET recording-plans/channels/:channelId/timeline','/api/gb28181/recording-plans/channels/:channelId/timeline','GET'),
  ('媒体管理 POST recording-plans','/api/gb28181/recording-plans','POST'),
  ('媒体管理 PUT recording-plans/:id','/api/gb28181/recording-plans/:id','PUT'),
  ('媒体管理 DELETE recording-plans/:id','/api/gb28181/recording-plans/:id','DELETE'),
  ('媒体管理 PATCH recording-plans/:id/status','/api/gb28181/recording-plans/:id/status','PATCH'),
  ('媒体管理 PATCH recording-plans/channels/:channelId/recording-mode','/api/gb28181/recording-plans/channels/:channelId/recording-mode','PATCH'),
  ('媒体管理 GET recording-plans/:id/assignment-options/devices','/api/gb28181/recording-plans/:id/assignment-options/devices','GET'),
  ('媒体管理 GET recording-plans/:id/assignment-options/channels','/api/gb28181/recording-plans/:id/assignment-options/channels','GET'),
  ('媒体管理 POST recording-plans/:id/assignments','/api/gb28181/recording-plans/:id/assignments','POST'),
  ('媒体管理 POST zlm/nodes','/api/gb28181/zlm/nodes','POST'),
  ('媒体管理 PUT zlm/nodes/:id','/api/gb28181/zlm/nodes/:id','PUT'),
  ('媒体管理 DELETE zlm/nodes/:id','/api/gb28181/zlm/nodes/:id','DELETE'),
  ('媒体管理 POST zlm/nodes/:id/maintenance','/api/gb28181/zlm/nodes/:id/maintenance','POST'),
  ('媒体管理 POST zlm/nodes/:id/activate','/api/gb28181/zlm/nodes/:id/activate','POST'),
  ('媒体管理 POST zlm/nodes/:id/kick','/api/gb28181/zlm/nodes/:id/kick','POST'),
  ('媒体管理 PUT zlm/scheduler','/api/gb28181/zlm/scheduler','PUT'),
  ('媒体管理 POST zlm/nodes/:id/streams/playback-grant','/api/gb28181/zlm/nodes/:id/streams/playback-grant','POST'),
  ('媒体管理 GET zlm/nodes/:id/streams/snapshot','/api/gb28181/zlm/nodes/:id/streams/snapshot','GET'),
  ('媒体管理 POST zlm/nodes/:id/streams/close/preflight','/api/gb28181/zlm/nodes/:id/streams/close/preflight','POST'),
  ('媒体管理 POST zlm/nodes/:id/streams/close','/api/gb28181/zlm/nodes/:id/streams/close','POST'),
  ('媒体管理 POST zlm/nodes/:id/streams/close/batch/preflight','/api/gb28181/zlm/nodes/:id/streams/close/batch/preflight','POST'),
  ('媒体管理 POST zlm/nodes/:id/streams/close/batch','/api/gb28181/zlm/nodes/:id/streams/close/batch','POST'),
  ('媒体管理 POST zlm/nodes/:id/streams/force-close','/api/gb28181/zlm/nodes/:id/streams/force-close','POST'),
  ('媒体管理 POST zlm/nodes/:id/sessions/kick','/api/gb28181/zlm/nodes/:id/sessions/kick','POST'),
  ('媒体管理 POST zlm/nodes/:id/proxies/pull','/api/gb28181/zlm/nodes/:id/proxies/pull','POST'),
  ('媒体管理 POST zlm/nodes/:id/proxies/pull/:key/preflight','/api/gb28181/zlm/nodes/:id/proxies/pull/:key/preflight','POST'),
  ('媒体管理 DELETE zlm/nodes/:id/proxies/pull/:key','/api/gb28181/zlm/nodes/:id/proxies/pull/:key','DELETE'),
  ('媒体管理 POST zlm/nodes/:id/proxies/push','/api/gb28181/zlm/nodes/:id/proxies/push','POST'),
  ('媒体管理 POST zlm/nodes/:id/proxies/push/:key/preflight','/api/gb28181/zlm/nodes/:id/proxies/push/:key/preflight','POST'),
  ('媒体管理 DELETE zlm/nodes/:id/proxies/push/:key','/api/gb28181/zlm/nodes/:id/proxies/push/:key','DELETE'),
  ('媒体管理 POST zlm/nodes/:id/ffmpeg-sources','/api/gb28181/zlm/nodes/:id/ffmpeg-sources','POST'),
  ('媒体管理 POST zlm/nodes/:id/ffmpeg-sources/:key/preflight','/api/gb28181/zlm/nodes/:id/ffmpeg-sources/:key/preflight','POST'),
  ('媒体管理 DELETE zlm/nodes/:id/ffmpeg-sources/:key','/api/gb28181/zlm/nodes/:id/ffmpeg-sources/:key','DELETE'),
  ('媒体管理 POST zlm/nodes/:id/rtp-servers','/api/gb28181/zlm/nodes/:id/rtp-servers','POST'),
  ('媒体管理 POST zlm/nodes/:id/rtp-servers/close/preflight','/api/gb28181/zlm/nodes/:id/rtp-servers/close/preflight','POST'),
  ('媒体管理 POST zlm/nodes/:id/rtp-servers/close','/api/gb28181/zlm/nodes/:id/rtp-servers/close','POST'),
  ('媒体管理 POST zlm/nodes/:id/rtp-servers/force-close','/api/gb28181/zlm/nodes/:id/rtp-servers/force-close','POST'),
  ('媒体管理 POST zlm/nodes/:id/recordings/runtime/start/preflight','/api/gb28181/zlm/nodes/:id/recordings/runtime/start/preflight','POST'),
  ('媒体管理 POST zlm/nodes/:id/recordings/runtime/start','/api/gb28181/zlm/nodes/:id/recordings/runtime/start','POST'),
  ('媒体管理 POST zlm/nodes/:id/recordings/runtime/stop/preflight','/api/gb28181/zlm/nodes/:id/recordings/runtime/stop/preflight','POST'),
  ('媒体管理 POST zlm/nodes/:id/recordings/runtime/stop','/api/gb28181/zlm/nodes/:id/recordings/runtime/stop','POST'),
  ('媒体管理 POST zlm/nodes/:id/recordings/runtime/force-stop/preflight','/api/gb28181/zlm/nodes/:id/recordings/runtime/force-stop/preflight','POST'),
  ('媒体管理 POST zlm/nodes/:id/recordings/runtime/force-stop','/api/gb28181/zlm/nodes/:id/recordings/runtime/force-stop','POST'),
  ('媒体管理 PUT zlm/nodes/:id/config','/api/gb28181/zlm/nodes/:id/config','PUT'),
  ('媒体管理 POST zlm/nodes/:id/config/test-connection','/api/gb28181/zlm/nodes/:id/config/test-connection','POST'),
  ('媒体管理 GET zlm/nodes/:id/restart','/api/gb28181/zlm/nodes/:id/restart','GET'),
  ('媒体管理 POST zlm/nodes/:id/restart','/api/gb28181/zlm/nodes/:id/restart','POST')
) AS v(title,path,method)
WHERE NOT EXISTS (SELECT 1 FROM sys_api a WHERE a.path=v.path AND a.method=v.method AND a.deleted_at IS NULL);

INSERT INTO sys_menu_api (menu_id,api_id)
SELECT DISTINCT m.id,a.id FROM (VALUES
  ('path','/gb28181/zlm/overview','/api/gb28181/zlm/overview','GET'),
  ('path','/gb28181/zlm/nodes','/api/gb28181/zlm/nodes','GET'),
  ('path','/gb28181/zlm/nodes','/api/gb28181/zlm/nodes/:id','GET'),
  ('path','/gb28181/zlm/nodes','/api/gb28181/zlm/nodes/:id/config','GET'),
  ('path','/gb28181/zlm/scheduler','/api/gb28181/zlm/scheduler','GET'),
  ('path','/gb28181/zlm/scheduler/logs','/api/gb28181/zlm/scheduler/logs','GET'),
  ('path','/gb28181/zlm/scheduler/logs','/api/gb28181/zlm/nodes','GET'),
  ('path','/gb28181/zlm/runtime','/api/gb28181/zlm/nodes','GET'),
  ('path','/gb28181/zlm/runtime','/api/gb28181/zlm/nodes/:id/runtime','GET'),
  ('path','/gb28181/zlm/streams','/api/gb28181/zlm/nodes','GET'),
  ('path','/gb28181/zlm/streams','/api/gb28181/zlm/streams','GET'),
  ('path','/gb28181/zlm/streams','/api/gb28181/zlm/nodes/:id/streams','GET'),
  ('path','/gb28181/zlm/streams','/api/gb28181/zlm/nodes/:id/streams/detail','GET'),
  ('path','/gb28181/zlm/streams','/api/gb28181/zlm/nodes/:id/streams/viewers','GET'),
  ('path','/gb28181/zlm/sessions','/api/gb28181/zlm/nodes','GET'),
  ('path','/gb28181/zlm/sessions','/api/gb28181/zlm/nodes/:id/sessions/network','GET'),
  ('path','/gb28181/zlm/sessions','/api/gb28181/zlm/nodes/:id/sessions/viewers','GET'),
  ('path','/gb28181/zlm/proxies','/api/gb28181/zlm/nodes','GET'),
  ('path','/gb28181/zlm/proxies','/api/gb28181/zlm/nodes/:id/proxies/pull','GET'),
  ('path','/gb28181/zlm/proxies','/api/gb28181/zlm/nodes/:id/proxies/pull/:key','GET'),
  ('path','/gb28181/zlm/proxies','/api/gb28181/zlm/nodes/:id/proxies/push','GET'),
  ('path','/gb28181/zlm/proxies','/api/gb28181/zlm/nodes/:id/proxies/push/:key','GET'),
  ('path','/gb28181/zlm/ffmpeg-sources','/api/gb28181/zlm/nodes','GET'),
  ('path','/gb28181/zlm/ffmpeg-sources','/api/gb28181/zlm/nodes/:id/ffmpeg-sources','GET'),
  ('path','/gb28181/zlm/rtp-servers','/api/gb28181/zlm/nodes','GET'),
  ('path','/gb28181/zlm/rtp-servers','/api/gb28181/zlm/nodes/:id/rtp-servers','GET'),
  ('path','/gb28181/zlm/config','/api/gb28181/zlm/nodes','GET'),
  ('path','/gb28181/zlm/config','/api/gb28181/zlm/nodes/:id/config','GET'),
  ('permission','gb28181:recording:view','/api/gb28181/cloud-recordings/files','GET'),
  ('permission','gb28181:recording:view','/api/gb28181/cloud-recordings/files/options','GET'),
  ('permission','gb28181:recording:view','/api/gb28181/cloud-recordings/files/:id','GET'),
  ('permission','gb28181:recording:view','/api/gb28181/cloud-recordings/files/:id/access','POST'),
  ('permission','gb28181:recording:view','/api/gb28181/cloud-recordings/files/:id/downloads','POST'),
  ('permission','gb28181:recording:view','/api/gb28181/cloud-recordings/downloads/:taskId','GET'),
  ('permission','gb28181:recording:view','/api/gb28181/cloud-recordings/downloads/:taskId','DELETE'),
  ('permission','gb28181:recording:view','/api/gb28181/cloud-recordings/active','GET'),
  ('permission','gb28181:recording:view','/api/gb28181/zlm/nodes','GET'),
  ('permission','gb28181:recording:view','/api/gb28181/zlm/nodes/:id/recordings/runtime/status','GET'),
  ('permission','gb28181:recording:reconcile','/api/gb28181/cloud-recordings/reconciliations','GET'),
  ('permission','gb28181:recording:reconcile','/api/gb28181/cloud-recordings/reconciliations','POST'),
  ('permission','gb28181:recording:delete','/api/gb28181/cloud-recordings/files/batch-delete','POST'),
  ('permission','gb28181:recording:delete','/api/gb28181/cloud-recordings/files/:id','DELETE'),
  ('permission','gb28181:recording:stop','/api/gb28181/cloud-recordings/active/:id/stop','POST'),
  ('permission','gb28181:recording-plan:view','/api/gb28181/recording-plans','GET'),
  ('permission','gb28181:recording-plan:view','/api/gb28181/recording-plans/:id','GET'),
  ('permission','gb28181:recording-plan:view','/api/gb28181/recording-plans/:id/channels','GET'),
  ('permission','gb28181:recording-plan:view','/api/gb28181/recording-plans/channels/:channelId/diagnosis','GET'),
  ('permission','gb28181:recording-plan:view','/api/gb28181/recording-plans/channels/:channelId/timeline','GET'),
  ('permission','gb28181:recording-plan:maintain','/api/gb28181/recording-plans','POST'),
  ('permission','gb28181:recording-plan:maintain','/api/gb28181/recording-plans/:id','PUT'),
  ('permission','gb28181:recording-plan:maintain','/api/gb28181/recording-plans/:id','DELETE'),
  ('permission','gb28181:recording-plan:maintain','/api/gb28181/recording-plans/:id/status','PATCH'),
  ('permission','gb28181:recording-plan:assign','/api/gb28181/recording-plans/channels/:channelId/recording-mode','PATCH'),
  ('permission','gb28181:recording-plan:assign','/api/gb28181/recording-plans/:id/assignment-options/devices','GET'),
  ('permission','gb28181:recording-plan:assign','/api/gb28181/recording-plans/:id/assignment-options/channels','GET'),
  ('permission','gb28181:recording-plan:assign','/api/gb28181/recording-plans/:id/assignments','POST'),
  ('permission','gb28181:zlm:node:manage','/api/gb28181/zlm/nodes','POST'),
  ('permission','gb28181:zlm:node:manage','/api/gb28181/zlm/nodes/:id','PUT'),
  ('permission','gb28181:zlm:node:manage','/api/gb28181/zlm/nodes/:id','DELETE'),
  ('permission','gb28181:zlm:node:manage','/api/gb28181/zlm/nodes/:id/maintenance','POST'),
  ('permission','gb28181:zlm:node:manage','/api/gb28181/zlm/nodes/:id/activate','POST'),
  ('permission','gb28181:zlm:node:kick','/api/gb28181/zlm/nodes/:id/kick','POST'),
  ('permission','gb28181:zlm:scheduler:manage','/api/gb28181/zlm/scheduler','PUT'),
  ('permission','gb28181:zlm:stream:preview','/api/gb28181/zlm/nodes/:id/streams/playback-grant','POST'),
  ('permission','gb28181:zlm:stream:preview','/api/gb28181/zlm/nodes/:id/streams/snapshot','GET'),
  ('permission','gb28181:zlm:stream:close','/api/gb28181/zlm/nodes/:id/streams/close/preflight','POST'),
  ('permission','gb28181:zlm:stream:close','/api/gb28181/zlm/nodes/:id/streams/close','POST'),
  ('permission','gb28181:zlm:stream:close','/api/gb28181/zlm/nodes/:id/streams/close/batch/preflight','POST'),
  ('permission','gb28181:zlm:stream:close','/api/gb28181/zlm/nodes/:id/streams/close/batch','POST'),
  ('permission','gb28181:zlm:stream:force-close','/api/gb28181/zlm/nodes/:id/streams/force-close','POST'),
  ('permission','gb28181:zlm:session:kick','/api/gb28181/zlm/nodes/:id/sessions/kick','POST'),
  ('permission','gb28181:zlm:proxy:manage','/api/gb28181/zlm/nodes/:id/proxies/pull','POST'),
  ('permission','gb28181:zlm:proxy:manage','/api/gb28181/zlm/nodes/:id/proxies/pull/:key/preflight','POST'),
  ('permission','gb28181:zlm:proxy:manage','/api/gb28181/zlm/nodes/:id/proxies/pull/:key','DELETE'),
  ('permission','gb28181:zlm:proxy:manage','/api/gb28181/zlm/nodes/:id/proxies/push','POST'),
  ('permission','gb28181:zlm:proxy:manage','/api/gb28181/zlm/nodes/:id/proxies/push/:key/preflight','POST'),
  ('permission','gb28181:zlm:proxy:manage','/api/gb28181/zlm/nodes/:id/proxies/push/:key','DELETE'),
  ('permission','gb28181:zlm:ffmpeg:manage','/api/gb28181/zlm/nodes/:id/ffmpeg-sources','POST'),
  ('permission','gb28181:zlm:ffmpeg:manage','/api/gb28181/zlm/nodes/:id/ffmpeg-sources/:key/preflight','POST'),
  ('permission','gb28181:zlm:ffmpeg:manage','/api/gb28181/zlm/nodes/:id/ffmpeg-sources/:key','DELETE'),
  ('permission','gb28181:zlm:rtp:manage','/api/gb28181/zlm/nodes/:id/rtp-servers','POST'),
  ('permission','gb28181:zlm:rtp:manage','/api/gb28181/zlm/nodes/:id/rtp-servers/close/preflight','POST'),
  ('permission','gb28181:zlm:rtp:manage','/api/gb28181/zlm/nodes/:id/rtp-servers/close','POST'),
  ('permission','gb28181:zlm:rtp:force-close','/api/gb28181/zlm/nodes/:id/rtp-servers/force-close','POST'),
  ('permission','gb28181:recording:control','/api/gb28181/zlm/nodes/:id/recordings/runtime/start/preflight','POST'),
  ('permission','gb28181:recording:control','/api/gb28181/zlm/nodes/:id/recordings/runtime/start','POST'),
  ('permission','gb28181:recording:control','/api/gb28181/zlm/nodes/:id/recordings/runtime/stop/preflight','POST'),
  ('permission','gb28181:recording:control','/api/gb28181/zlm/nodes/:id/recordings/runtime/stop','POST'),
  ('permission','gb28181:recording:force-stop','/api/gb28181/zlm/nodes/:id/recordings/runtime/force-stop/preflight','POST'),
  ('permission','gb28181:recording:force-stop','/api/gb28181/zlm/nodes/:id/recordings/runtime/force-stop','POST'),
  ('permission','gb28181:zlm:config:update','/api/gb28181/zlm/nodes/:id/config','PUT'),
  ('permission','gb28181:zlm:config:update','/api/gb28181/zlm/nodes/:id/config/test-connection','POST'),
  ('permission','gb28181:zlm:restart','/api/gb28181/zlm/nodes/:id/restart','GET'),
  ('permission','gb28181:zlm:restart','/api/gb28181/zlm/nodes/:id/restart','POST')
) AS b(selector_type,selector,api_path,method)
JOIN sys_menu m ON ((b.selector_type='path' AND m.path=b.selector) OR (b.selector_type='permission' AND m.permission=b.selector)) AND m.deleted_at IS NULL
JOIN sys_api a ON a.path=b.api_path AND a.method=b.method AND a.deleted_at IS NULL
WHERE NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);

INSERT INTO sys_casbin_rule (ptype,v0,v1,v2,v3,v4,v5)
SELECT DISTINCT 'p','role_'||CAST(rm.role_id AS varchar(20)),a.path,a.method,'*','','' FROM sys_role_menu rm
JOIN sys_menu m ON m.id=rm.menu_id
JOIN sys_menu_api ma ON ma.menu_id=m.id
JOIN sys_api a ON a.id=ma.api_id
WHERE m.deleted_at IS NULL AND a.deleted_at IS NULL
AND (m.path IN ('/gb28181/zlm/overview','/gb28181/zlm/nodes','/gb28181/zlm/scheduler','/gb28181/zlm/scheduler/logs','/gb28181/zlm/runtime','/gb28181/zlm/streams','/gb28181/zlm/sessions','/gb28181/zlm/proxies','/gb28181/zlm/ffmpeg-sources','/gb28181/zlm/rtp-servers','/gb28181/cloud-recordings','/gb28181/recording-schedules','/gb28181/zlm/config') OR m.permission IN ('gb28181:zlm:node:manage','gb28181:zlm:node:kick','gb28181:zlm:scheduler:manage','gb28181:zlm:stream:preview','gb28181:zlm:stream:close','gb28181:zlm:stream:force-close','gb28181:zlm:session:kick','gb28181:zlm:proxy:manage','gb28181:zlm:ffmpeg:manage','gb28181:zlm:rtp:manage','gb28181:zlm:rtp:force-close','gb28181:recording:control','gb28181:recording:force-stop','gb28181:zlm:config:update','gb28181:zlm:restart','gb28181:recording:view','gb28181:recording:reconcile','gb28181:recording:delete','gb28181:recording:stop','gb28181:recording-plan:view','gb28181:recording-plan:maintain','gb28181:recording-plan:assign'))
AND (a.path LIKE '/api/gb28181/zlm/%' OR a.path LIKE '/api/gb28181/cloud-recordings/%' OR a.path LIKE '/api/gb28181/recording-plans%')
AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule c WHERE c.ptype='p' AND c.v0='role_'||CAST(rm.role_id AS varchar(20)) AND c.v1=a.path AND c.v2=a.method AND c.v3='*');
-- media-management-baseline:end
