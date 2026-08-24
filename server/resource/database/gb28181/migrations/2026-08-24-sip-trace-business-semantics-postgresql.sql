ALTER TABLE gb_sip_trace_message
  ADD COLUMN IF NOT EXISTS from_id VARCHAR(64) NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS to_id VARCHAR(64) NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS business_code VARCHAR(64) NOT NULL DEFAULT 'unknown',
  ADD COLUMN IF NOT EXISTS business_type VARCHAR(64) NOT NULL DEFAULT '未知业务',
  ADD COLUMN IF NOT EXISTS business_confidence VARCHAR(16) NOT NULL DEFAULT 'none';
CREATE INDEX IF NOT EXISTS idx_gb_sip_trace_business_occurred ON gb_sip_trace_message (business_code, occurred_at);
