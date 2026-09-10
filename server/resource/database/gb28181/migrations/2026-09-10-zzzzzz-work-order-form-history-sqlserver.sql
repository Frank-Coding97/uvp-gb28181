-- 作业单表单历史值（SQL Server 2017+）。
IF NOT EXISTS (SELECT 1 FROM sys.tables WHERE name = 'gb_work_order_form_history')
BEGIN
  CREATE TABLE gb_work_order_form_history (
    id BIGINT IDENTITY(1,1) PRIMARY KEY,
    field_key NVARCHAR(64) NOT NULL,
    value NVARCHAR(256) NOT NULL,
    use_count INT NOT NULL DEFAULT 1,
    last_used_at DATETIME NOT NULL,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
  );
END;
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = 'uk_gb_work_order_form_history_field_value')
  CREATE UNIQUE INDEX uk_gb_work_order_form_history_field_value
    ON gb_work_order_form_history (field_key, value);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = 'idx_gb_work_order_form_history_field_used')
  CREATE INDEX idx_gb_work_order_form_history_field_used
    ON gb_work_order_form_history (field_key, last_used_at);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = 'idx_gb_work_order_form_history_field_count')
  CREATE INDEX idx_gb_work_order_form_history_field_count
    ON gb_work_order_form_history (field_key, use_count);
