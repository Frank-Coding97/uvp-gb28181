-- 默认播放协议字典（SQL Server，幂等）。

IF NOT EXISTS (
  SELECT 1 FROM [sys_dict] WHERE [code]='gb28181_playback_protocol' AND [deleted_at] IS NULL
)
BEGIN
  INSERT INTO [sys_dict] ([name],[code],[status],[description],[created_by],[created_at],[updated_at])
  VALUES (N'GB28181 默认播放协议','gb28181_playback_protocol',1,N'国标播放入口支持的默认协议选项',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
END;

DECLARE @gb_playback_protocol_dict_id BIGINT;
SELECT TOP (1) @gb_playback_protocol_dict_id=[id]
FROM [sys_dict]
WHERE [code]='gb28181_playback_protocol' AND [deleted_at] IS NULL
ORDER BY [id];

IF @gb_playback_protocol_dict_id IS NOT NULL
BEGIN
  UPDATE [sys_dict]
  SET [name]=N'GB28181 默认播放协议',[status]=1,[description]=N'国标播放入口支持的默认协议选项',[updated_at]=CURRENT_TIMESTAMP
  WHERE [id]=@gb_playback_protocol_dict_id;

  IF NOT EXISTS (SELECT 1 FROM [sys_dict_item] WHERE [dict_id]=@gb_playback_protocol_dict_id AND [value]='ws-flv')
    INSERT INTO [sys_dict_item] ([name],[value],[status],[dict_id]) VALUES (N'WS-FLV','ws-flv',1,@gb_playback_protocol_dict_id);
  IF NOT EXISTS (SELECT 1 FROM [sys_dict_item] WHERE [dict_id]=@gb_playback_protocol_dict_id AND [value]='http-flv')
    INSERT INTO [sys_dict_item] ([name],[value],[status],[dict_id]) VALUES (N'HTTP-FLV','http-flv',1,@gb_playback_protocol_dict_id);
  IF NOT EXISTS (SELECT 1 FROM [sys_dict_item] WHERE [dict_id]=@gb_playback_protocol_dict_id AND [value]='hls')
    INSERT INTO [sys_dict_item] ([name],[value],[status],[dict_id]) VALUES (N'HLS','hls',1,@gb_playback_protocol_dict_id);
  IF NOT EXISTS (SELECT 1 FROM [sys_dict_item] WHERE [dict_id]=@gb_playback_protocol_dict_id AND [value]='webrtc')
    INSERT INTO [sys_dict_item] ([name],[value],[status],[dict_id]) VALUES (N'WebRTC','webrtc',1,@gb_playback_protocol_dict_id);

  UPDATE [sys_dict_item] SET [name]=N'WS-FLV',[status]=1 WHERE [dict_id]=@gb_playback_protocol_dict_id AND [value]='ws-flv';
  UPDATE [sys_dict_item] SET [name]=N'HTTP-FLV',[status]=1 WHERE [dict_id]=@gb_playback_protocol_dict_id AND [value]='http-flv';
  UPDATE [sys_dict_item] SET [name]=N'HLS',[status]=1 WHERE [dict_id]=@gb_playback_protocol_dict_id AND [value]='hls';
  UPDATE [sys_dict_item] SET [name]=N'WebRTC',[status]=1 WHERE [dict_id]=@gb_playback_protocol_dict_id AND [value]='webrtc';
END;
