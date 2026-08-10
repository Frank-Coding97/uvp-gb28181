-- 默认播放协议字典（PostgreSQL，幂等）。

INSERT INTO sys_dict (name,code,status,description,created_by,created_at,updated_at)
SELECT 'GB28181 默认播放协议','gb28181_playback_protocol',TRUE,'国标播放入口支持的默认协议选项',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP
WHERE NOT EXISTS (
  SELECT 1 FROM sys_dict WHERE code='gb28181_playback_protocol' AND deleted_at IS NULL
);

UPDATE sys_dict
SET name='GB28181 默认播放协议',status=TRUE,description='国标播放入口支持的默认协议选项',updated_at=CURRENT_TIMESTAMP
WHERE id=(SELECT id FROM sys_dict WHERE code='gb28181_playback_protocol' AND deleted_at IS NULL ORDER BY id LIMIT 1);

INSERT INTO sys_dict_item (name,value,status,dict_id)
SELECT 'WS-FLV','ws-flv',TRUE,d.id
FROM sys_dict d
WHERE d.code='gb28181_playback_protocol' AND d.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_dict_item i WHERE i.dict_id=d.id AND i.value='ws-flv')
ORDER BY d.id LIMIT 1;
INSERT INTO sys_dict_item (name,value,status,dict_id)
SELECT 'HTTP-FLV','http-flv',TRUE,d.id
FROM sys_dict d
WHERE d.code='gb28181_playback_protocol' AND d.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_dict_item i WHERE i.dict_id=d.id AND i.value='http-flv')
ORDER BY d.id LIMIT 1;
INSERT INTO sys_dict_item (name,value,status,dict_id)
SELECT 'HLS','hls',TRUE,d.id
FROM sys_dict d
WHERE d.code='gb28181_playback_protocol' AND d.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_dict_item i WHERE i.dict_id=d.id AND i.value='hls')
ORDER BY d.id LIMIT 1;
INSERT INTO sys_dict_item (name,value,status,dict_id)
SELECT 'WebRTC','webrtc',TRUE,d.id
FROM sys_dict d
WHERE d.code='gb28181_playback_protocol' AND d.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_dict_item i WHERE i.dict_id=d.id AND i.value='webrtc')
ORDER BY d.id LIMIT 1;

UPDATE sys_dict_item SET name='WS-FLV',status=TRUE
WHERE dict_id=(SELECT id FROM sys_dict WHERE code='gb28181_playback_protocol' AND deleted_at IS NULL ORDER BY id LIMIT 1) AND value='ws-flv';
UPDATE sys_dict_item SET name='HTTP-FLV',status=TRUE
WHERE dict_id=(SELECT id FROM sys_dict WHERE code='gb28181_playback_protocol' AND deleted_at IS NULL ORDER BY id LIMIT 1) AND value='http-flv';
UPDATE sys_dict_item SET name='HLS',status=TRUE
WHERE dict_id=(SELECT id FROM sys_dict WHERE code='gb28181_playback_protocol' AND deleted_at IS NULL ORDER BY id LIMIT 1) AND value='hls';
UPDATE sys_dict_item SET name='WebRTC',status=TRUE
WHERE dict_id=(SELECT id FROM sys_dict WHERE code='gb28181_playback_protocol' AND deleted_at IS NULL ORDER BY id LIMIT 1) AND value='webrtc';
