-- 设备对讲/广播的音频编码（pcma=G.711A 默认 / pcmu=G.711U），由用户在设备编辑里维护。
-- 必须按设备维护：平台上行源在推流前就要定编码，ZLM 的 startSendRtp 只按 PT 打包、不转码。
-- 见 models/gb_device.go 的 TalkAudioCodec 与 sdp/codec.go。
SET @ddl := IF((SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gb_device' AND COLUMN_NAME = 'talk_audio_codec') = 0,
  'ALTER TABLE `gb_device` ADD COLUMN `talk_audio_codec` VARCHAR(16) NOT NULL DEFAULT ''pcma'' COMMENT ''对讲音频编码 pcma/pcmu(用户维护)''', 'SELECT 1');
PREPARE talk_audio_codec_device_stmt FROM @ddl;
EXECUTE talk_audio_codec_device_stmt;
DEALLOCATE PREPARE talk_audio_codec_device_stmt;
