IF OBJECT_ID('gb_recording_plan_gap','U') IS NOT NULL DROP TABLE gb_recording_plan_gap;
IF OBJECT_ID('gb_recording_plan_execution','U') IS NOT NULL DROP TABLE gb_recording_plan_execution;
IF OBJECT_ID('gb_recording_plan_channel_state','U') IS NOT NULL DROP TABLE gb_recording_plan_channel_state;
IF OBJECT_ID('gb_recording_plan_binding','U') IS NOT NULL DROP TABLE gb_recording_plan_binding;
IF OBJECT_ID('gb_recording_plan_period','U') IS NOT NULL DROP TABLE gb_recording_plan_period;
IF OBJECT_ID('gb_recording_plan','U') IS NOT NULL DROP TABLE gb_recording_plan;
IF COL_LENGTH('gb_channel','recording_mode') IS NOT NULL ALTER TABLE gb_channel DROP CONSTRAINT df_gb_channel_recording_mode;
IF COL_LENGTH('gb_channel','recording_mode') IS NOT NULL ALTER TABLE gb_channel DROP COLUMN recording_mode;
