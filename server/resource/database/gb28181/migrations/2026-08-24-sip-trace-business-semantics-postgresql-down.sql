DROP INDEX IF EXISTS idx_gb_sip_trace_business_occurred;
ALTER TABLE gb_sip_trace_message
  DROP COLUMN IF EXISTS business_confidence, DROP COLUMN IF EXISTS business_type,
  DROP COLUMN IF EXISTS business_code, DROP COLUMN IF EXISTS to_id, DROP COLUMN IF EXISTS from_id;
