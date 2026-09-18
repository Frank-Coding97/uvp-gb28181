-- Preserve old rows without fabricating a process-retirement certificate.
SET @ptz_retire_exists = (SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name='gb_ptz_operation_attempt' AND column_name='retired_by_process_id');
SET @ptz_retire_sql = IF(@ptz_retire_exists=0, 'ALTER TABLE gb_ptz_operation_attempt ADD COLUMN retired_by_process_id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NULL', 'SELECT 1');
PREPARE ptz_retire_stmt FROM @ptz_retire_sql;
EXECUTE ptz_retire_stmt;
DEALLOCATE PREPARE ptz_retire_stmt;
SET @ptz_retire_exists = (SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name='gb_ptz_operation_attempt' AND column_name='retired_at');
SET @ptz_retire_sql = IF(@ptz_retire_exists=0, 'ALTER TABLE gb_ptz_operation_attempt ADD COLUMN retired_at DATETIME(3) NULL', 'SELECT 1');
PREPARE ptz_retire_stmt FROM @ptz_retire_sql;
EXECUTE ptz_retire_stmt;
DEALLOCATE PREPARE ptz_retire_stmt;
SET @ptz_retire_exists = (SELECT COUNT(*) FROM information_schema.table_constraints WHERE constraint_schema=DATABASE() AND table_name='gb_ptz_operation_attempt' AND constraint_name='ck_ptz_attempt_retirement');
SET @ptz_retire_sql = IF(@ptz_retire_exists=0, 'ALTER TABLE gb_ptz_operation_attempt ADD CONSTRAINT ck_ptz_attempt_retirement CHECK ((retired_by_process_id IS NULL AND retired_at IS NULL) OR (retired_by_process_id IS NOT NULL AND retired_at IS NOT NULL AND owner_process_id IS NOT NULL AND owner_run_id IS NOT NULL AND retired_by_process_id <> owner_process_id AND local_quiesced_at IS NULL))', 'SELECT 1');
PREPARE ptz_retire_stmt FROM @ptz_retire_sql;
EXECUTE ptz_retire_stmt;
DEALLOCATE PREPARE ptz_retire_stmt;
