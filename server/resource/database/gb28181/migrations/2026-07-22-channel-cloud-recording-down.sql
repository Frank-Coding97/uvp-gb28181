DROP TABLE IF EXISTS `gb_recording_file`;
DROP TABLE IF EXISTS `gb_recording_session`;

ALTER TABLE `gb_channel`
  DROP COLUMN `cloud_recording_updated_at`,
  DROP COLUMN `cloud_recording_error`,
  DROP COLUMN `cloud_recording_state`,
  DROP COLUMN `cloud_recording_enabled`;
