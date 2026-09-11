DROP TABLE IF EXISTS `gb_recording_plan_gap`;
DROP TABLE IF EXISTS `gb_recording_plan_execution`;
DROP TABLE IF EXISTS `gb_recording_plan_channel_state`;
DROP TABLE IF EXISTS `gb_recording_plan_binding`;
DROP TABLE IF EXISTS `gb_recording_plan_period`;
DROP TABLE IF EXISTS `gb_recording_plan`;
ALTER TABLE `gb_channel` DROP COLUMN `recording_mode`;
