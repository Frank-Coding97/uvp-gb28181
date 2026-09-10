-- 作业单表单历史值（PostgreSQL 12+）。
CREATE TABLE IF NOT EXISTS gb_work_order_form_history (
  id BIGSERIAL PRIMARY KEY,
  field_key VARCHAR(64) NOT NULL,
  value VARCHAR(256) NOT NULL,
  use_count INTEGER NOT NULL DEFAULT 1,
  last_used_at TIMESTAMP NOT NULL,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS uk_gb_work_order_form_history_field_value
  ON gb_work_order_form_history (field_key, value);
CREATE INDEX IF NOT EXISTS idx_gb_work_order_form_history_field_used
  ON gb_work_order_form_history (field_key, last_used_at);
CREATE INDEX IF NOT EXISTS idx_gb_work_order_form_history_field_count
  ON gb_work_order_form_history (field_key, use_count);
