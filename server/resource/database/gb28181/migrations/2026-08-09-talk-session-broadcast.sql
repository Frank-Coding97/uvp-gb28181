SET @schema_name = DATABASE();
ALTER TABLE `gb_talk_session`
  ADD COLUMN `target_id` varchar(20) NOT NULL DEFAULT '' AFTER `device_id`,
  ADD COLUMN `broadcast_sn` bigint unsigned NOT NULL DEFAULT 0 AFTER `mode`,
  ADD COLUMN `broadcast_reply_status` varchar(32) NOT NULL DEFAULT '' AFTER `broadcast_sn`,
  ADD COLUMN `signal_phase` varchar(40) NOT NULL DEFAULT '' AFTER `broadcast_reply_status`,
  ADD COLUMN `remote_media_ip` varchar(64) NOT NULL DEFAULT '' AFTER `local_port`,
  ADD COLUMN `remote_media_port` int NOT NULL DEFAULT 0 AFTER `remote_media_ip`,
  ADD COLUMN `media_transport` varchar(16) NOT NULL DEFAULT '' AFTER `remote_media_port`,
  ADD COLUMN `sender_mode` varchar(24) NOT NULL DEFAULT '' AFTER `media_transport`,
  ADD INDEX `idx_talk_session_broadcast_match` (`device_id`, `target_id`, `state`),
  ADD INDEX `idx_talk_session_broadcast_sn` (`broadcast_sn`);
