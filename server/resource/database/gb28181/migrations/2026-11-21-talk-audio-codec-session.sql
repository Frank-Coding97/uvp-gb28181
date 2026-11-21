-- 对讲会话使用的 G.711 变体：创建时从设备配置快照下来。
-- 广播的设备 offer 也按它校验「这台设备能不能收」，不再假设一定是 PCMA。
SET @ddl := IF((SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gb_talk_session' AND COLUMN_NAME = 'audio_codec') = 0,
  'ALTER TABLE `gb_talk_session` ADD COLUMN `audio_codec` VARCHAR(16) NOT NULL DEFAULT ''pcma''', 'SELECT 1');
PREPARE audio_codec_session_stmt FROM @ddl;
EXECUTE audio_codec_session_stmt;
DEALLOCATE PREPARE audio_codec_session_stmt;
