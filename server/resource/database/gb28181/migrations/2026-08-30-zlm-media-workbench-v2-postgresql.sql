-- media-workbench-v2:start
-- Flatten media management into six visible workspaces while preserving legacy permission anchors (PostgreSQL).
INSERT INTO sys_menu (parent_id,path,name,redirect,component,title,icon,sort,type,permission,hide,keep_alive,created_at,updated_at,created_by)
SELECT 0,'/media','Media','','gb28181/zlm/workbench/MediaEntry','流媒体管理','lucide:Clapperboard',9,1,'',false,true,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/media' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=0,name='Media',redirect='',component='gb28181/zlm/workbench/MediaEntry',title='流媒体管理',icon='lucide:Clapperboard',sort=9,type=1,permission='',hide=false,keep_alive=true,updated_at=CURRENT_TIMESTAMP
WHERE path='/media' AND deleted_at IS NULL;

INSERT INTO sys_menu (parent_id,path,name,redirect,component,title,icon,sort,type,permission,hide,keep_alive,created_at,updated_at,created_by)
SELECT p.id,'/media/overview','media-overview','','gb28181/zlm/workbench/MediaOverview','媒体总览','lucide:LayoutDashboard',10,2,'',false,true,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM sys_menu p
WHERE p.path='/media' AND p.deleted_at IS NULL
AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/media/overview' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),name='media-overview',redirect='',component='gb28181/zlm/workbench/MediaOverview',title='媒体总览',icon='lucide:LayoutDashboard',sort=10,type=2,permission='',hide=false,keep_alive=true,updated_at=CURRENT_TIMESTAMP
WHERE path='/media/overview' AND deleted_at IS NULL;

INSERT INTO sys_menu (parent_id,path,name,redirect,component,title,icon,sort,type,permission,hide,keep_alive,created_at,updated_at,created_by)
SELECT p.id,'/media/monitoring','media-monitoring','','gb28181/zlm/workbench/MediaMonitoring','媒体监控','lucide:Activity',20,2,'',false,true,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM sys_menu p
WHERE p.path='/media' AND p.deleted_at IS NULL
AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/media/monitoring' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),name='media-monitoring',redirect='',component='gb28181/zlm/workbench/MediaMonitoring',title='媒体监控',icon='lucide:Activity',sort=20,type=2,permission='',hide=false,keep_alive=true,updated_at=CURRENT_TIMESTAMP
WHERE path='/media/monitoring' AND deleted_at IS NULL;

INSERT INTO sys_menu (parent_id,path,name,redirect,component,title,icon,sort,type,permission,hide,keep_alive,created_at,updated_at,created_by)
SELECT p.id,'/media/ingress','media-ingress','','gb28181/zlm/workbench/IngressManagement','接入管理','lucide:RadioTower',30,2,'',false,true,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM sys_menu p
WHERE p.path='/media' AND p.deleted_at IS NULL
AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/media/ingress' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),name='media-ingress',redirect='',component='gb28181/zlm/workbench/IngressManagement',title='接入管理',icon='lucide:RadioTower',sort=30,type=2,permission='',hide=false,keep_alive=true,updated_at=CURRENT_TIMESTAMP
WHERE path='/media/ingress' AND deleted_at IS NULL;

INSERT INTO sys_menu (parent_id,path,name,redirect,component,title,icon,sort,type,permission,hide,keep_alive,created_at,updated_at,created_by)
SELECT p.id,'/media/recordings','media-recordings','','gb28181/zlm/workbench/RecordingCenter','录制中心','lucide:Cloud',40,2,'',false,true,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM sys_menu p
WHERE p.path='/media' AND p.deleted_at IS NULL
AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/media/recordings' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),name='media-recordings',redirect='',component='gb28181/zlm/workbench/RecordingCenter',title='录制中心',icon='lucide:Cloud',sort=40,type=2,permission='',hide=false,keep_alive=true,updated_at=CURRENT_TIMESTAMP
WHERE path='/media/recordings' AND deleted_at IS NULL;

INSERT INTO sys_menu (parent_id,path,name,redirect,component,title,icon,sort,type,permission,hide,keep_alive,created_at,updated_at,created_by)
SELECT p.id,'/media/nodes','media-nodes','','gb28181/zlm/workbench/NodeManagement','节点管理','lucide:Server',50,2,'',false,true,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM sys_menu p
WHERE p.path='/media' AND p.deleted_at IS NULL
AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/media/nodes' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),name='media-nodes',redirect='',component='gb28181/zlm/workbench/NodeManagement',title='节点管理',icon='lucide:Server',sort=50,type=2,permission='',hide=false,keep_alive=true,updated_at=CURRENT_TIMESTAMP
WHERE path='/media/nodes' AND deleted_at IS NULL;

INSERT INTO sys_menu (parent_id,path,name,redirect,component,title,icon,sort,type,permission,hide,keep_alive,created_at,updated_at,created_by)
SELECT p.id,'/media/scheduling','media-scheduling','','gb28181/zlm/workbench/SchedulingManagement','调度管理','lucide:Workflow',60,2,'',false,true,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM sys_menu p
WHERE p.path='/media' AND p.deleted_at IS NULL
AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/media/scheduling' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),name='media-scheduling',redirect='',component='gb28181/zlm/workbench/SchedulingManagement',title='调度管理',icon='lucide:Workflow',sort=60,type=2,permission='',hide=false,keep_alive=true,updated_at=CURRENT_TIMESTAMP
WHERE path='/media/scheduling' AND deleted_at IS NULL;

INSERT INTO sys_menu (parent_id,path,name,redirect,component,title,icon,sort,type,permission,hide,keep_alive,created_at,updated_at,created_by)
SELECT p.id,'/media/nodes/:id','media-node-detail','','gb28181/zlm/workbench/nodes/NodeDetail','节点详情','lucide:Server',99,2,'',true,true,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM sys_menu p
WHERE p.path='/media/nodes' AND p.deleted_at IS NULL
AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/media/nodes/:id' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media/nodes' AND deleted_at IS NULL),name='media-node-detail',redirect='',component='gb28181/zlm/workbench/nodes/NodeDetail',title='节点详情',icon='lucide:Server',sort=99,type=2,permission='',hide=true,keep_alive=true,updated_at=CURRENT_TIMESTAMP
WHERE path='/media/nodes/:id' AND deleted_at IS NULL;

UPDATE sys_menu SET component='gb28181/zlm/workbench/LegacyMediaRoute',hide=true,updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/zlm/overview' AND deleted_at IS NULL;

UPDATE sys_menu SET component='gb28181/zlm/workbench/LegacyMediaRoute',hide=true,updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/zlm/runtime' AND deleted_at IS NULL;

UPDATE sys_menu SET component='gb28181/zlm/workbench/LegacyMediaRoute',hide=true,updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/zlm/streams' AND deleted_at IS NULL;

UPDATE sys_menu SET component='gb28181/zlm/workbench/LegacyMediaRoute',hide=true,updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/zlm/sessions' AND deleted_at IS NULL;

UPDATE sys_menu SET component='gb28181/zlm/workbench/LegacyMediaRoute',hide=true,updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/zlm/proxies' AND deleted_at IS NULL;

UPDATE sys_menu SET component='gb28181/zlm/workbench/LegacyMediaRoute',hide=true,updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/zlm/ffmpeg-sources' AND deleted_at IS NULL;

UPDATE sys_menu SET component='gb28181/zlm/workbench/LegacyMediaRoute',hide=true,updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/zlm/rtp-servers' AND deleted_at IS NULL;

UPDATE sys_menu SET component='gb28181/zlm/workbench/LegacyMediaRoute',hide=true,updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/cloud-recordings' AND deleted_at IS NULL;

UPDATE sys_menu SET component='gb28181/zlm/workbench/LegacyMediaRoute',hide=true,updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/recording-schedules' AND deleted_at IS NULL;

UPDATE sys_menu SET component='gb28181/zlm/workbench/LegacyMediaRoute',hide=true,updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/zlm/nodes' AND deleted_at IS NULL;

UPDATE sys_menu SET component='gb28181/zlm/workbench/LegacyMediaRoute',hide=true,updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/zlm/nodes/:id' AND deleted_at IS NULL;

UPDATE sys_menu SET component='gb28181/zlm/workbench/LegacyMediaRoute',hide=true,updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/zlm/config' AND deleted_at IS NULL;

UPDATE sys_menu SET component='gb28181/zlm/workbench/LegacyMediaRoute',hide=true,updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/zlm/scheduler' AND deleted_at IS NULL;

UPDATE sys_menu SET component='gb28181/zlm/workbench/LegacyMediaRoute',hide=true,updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/zlm/scheduler/logs' AND deleted_at IS NULL;

-- workspace-role-union:/media
INSERT INTO sys_role_menu (role_id,menu_id)
SELECT DISTINCT rm.role_id,target.id
FROM sys_role_menu rm
JOIN sys_menu source ON source.id=rm.menu_id AND source.deleted_at IS NULL
JOIN sys_menu target ON target.path='/media' AND target.deleted_at IS NULL
WHERE source.path IN ('/gb28181/zlm/overview','/gb28181/zlm/runtime','/gb28181/zlm/streams','/gb28181/zlm/sessions','/gb28181/zlm/proxies','/gb28181/zlm/ffmpeg-sources','/gb28181/zlm/rtp-servers','/gb28181/cloud-recordings','/gb28181/recording-schedules','/gb28181/zlm/nodes','/gb28181/zlm/nodes/:id','/gb28181/zlm/config','/gb28181/zlm/scheduler','/gb28181/zlm/scheduler/logs')
AND NOT EXISTS (SELECT 1 FROM sys_role_menu existing WHERE existing.role_id=rm.role_id AND existing.menu_id=target.id);

-- workspace-role-union:/media/overview
INSERT INTO sys_role_menu (role_id,menu_id)
SELECT DISTINCT rm.role_id,target.id
FROM sys_role_menu rm
JOIN sys_menu source ON source.id=rm.menu_id AND source.deleted_at IS NULL
JOIN sys_menu target ON target.path='/media/overview' AND target.deleted_at IS NULL
WHERE source.path IN ('/gb28181/zlm/overview')
AND NOT EXISTS (SELECT 1 FROM sys_role_menu existing WHERE existing.role_id=rm.role_id AND existing.menu_id=target.id);

-- workspace-role-union:/media/monitoring
INSERT INTO sys_role_menu (role_id,menu_id)
SELECT DISTINCT rm.role_id,target.id
FROM sys_role_menu rm
JOIN sys_menu source ON source.id=rm.menu_id AND source.deleted_at IS NULL
JOIN sys_menu target ON target.path='/media/monitoring' AND target.deleted_at IS NULL
WHERE source.path IN ('/gb28181/zlm/runtime','/gb28181/zlm/streams','/gb28181/zlm/sessions')
AND NOT EXISTS (SELECT 1 FROM sys_role_menu existing WHERE existing.role_id=rm.role_id AND existing.menu_id=target.id);

-- workspace-role-union:/media/ingress
INSERT INTO sys_role_menu (role_id,menu_id)
SELECT DISTINCT rm.role_id,target.id
FROM sys_role_menu rm
JOIN sys_menu source ON source.id=rm.menu_id AND source.deleted_at IS NULL
JOIN sys_menu target ON target.path='/media/ingress' AND target.deleted_at IS NULL
WHERE source.path IN ('/gb28181/zlm/proxies','/gb28181/zlm/ffmpeg-sources','/gb28181/zlm/rtp-servers')
AND NOT EXISTS (SELECT 1 FROM sys_role_menu existing WHERE existing.role_id=rm.role_id AND existing.menu_id=target.id);

-- workspace-role-union:/media/recordings
INSERT INTO sys_role_menu (role_id,menu_id)
SELECT DISTINCT rm.role_id,target.id
FROM sys_role_menu rm
JOIN sys_menu source ON source.id=rm.menu_id AND source.deleted_at IS NULL
JOIN sys_menu target ON target.path='/media/recordings' AND target.deleted_at IS NULL
WHERE source.path IN ('/gb28181/cloud-recordings','/gb28181/recording-schedules')
AND NOT EXISTS (SELECT 1 FROM sys_role_menu existing WHERE existing.role_id=rm.role_id AND existing.menu_id=target.id);

-- workspace-role-union:/media/nodes
INSERT INTO sys_role_menu (role_id,menu_id)
SELECT DISTINCT rm.role_id,target.id
FROM sys_role_menu rm
JOIN sys_menu source ON source.id=rm.menu_id AND source.deleted_at IS NULL
JOIN sys_menu target ON target.path='/media/nodes' AND target.deleted_at IS NULL
WHERE source.path IN ('/gb28181/zlm/nodes','/gb28181/zlm/nodes/:id','/gb28181/zlm/config')
AND NOT EXISTS (SELECT 1 FROM sys_role_menu existing WHERE existing.role_id=rm.role_id AND existing.menu_id=target.id);

-- workspace-role-union:/media/nodes/:id
INSERT INTO sys_role_menu (role_id,menu_id)
SELECT DISTINCT rm.role_id,target.id
FROM sys_role_menu rm
JOIN sys_menu source ON source.id=rm.menu_id AND source.deleted_at IS NULL
JOIN sys_menu target ON target.path='/media/nodes/:id' AND target.deleted_at IS NULL
WHERE source.path IN ('/gb28181/zlm/nodes','/gb28181/zlm/nodes/:id','/gb28181/zlm/config')
AND NOT EXISTS (SELECT 1 FROM sys_role_menu existing WHERE existing.role_id=rm.role_id AND existing.menu_id=target.id);

-- workspace-role-union:/media/scheduling
INSERT INTO sys_role_menu (role_id,menu_id)
SELECT DISTINCT rm.role_id,target.id
FROM sys_role_menu rm
JOIN sys_menu source ON source.id=rm.menu_id AND source.deleted_at IS NULL
JOIN sys_menu target ON target.path='/media/scheduling' AND target.deleted_at IS NULL
WHERE source.path IN ('/gb28181/zlm/scheduler','/gb28181/zlm/scheduler/logs')
AND NOT EXISTS (SELECT 1 FROM sys_role_menu existing WHERE existing.role_id=rm.role_id AND existing.menu_id=target.id);

-- media-workbench-v2:end
