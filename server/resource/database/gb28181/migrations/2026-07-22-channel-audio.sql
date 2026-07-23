-- 通道级点播音频开关
-- 2026-07-22: ZLM openRtpServer only_track=0 音视频,2 仅视频
ALTER TABLE `gb_channel`
  ADD COLUMN `audio_enabled` tinyint(1) NOT NULL DEFAULT '1' COMMENT '点播是否接收音频' AFTER `stream_id`;
