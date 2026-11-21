ALTER TABLE gb_cascade_snapshot_session ADD COLUMN IF NOT EXISTS downstream_session_id varchar(128) NULL;
