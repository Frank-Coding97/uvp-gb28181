-- Refuse to discard current media ownership while any live state is present.
DO $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_schema = current_schema()
      AND table_name = 'gb_channel'
      AND column_name = 'current_ssrc'
  ) THEN
    IF EXISTS (SELECT 1 FROM gb_channel WHERE stream_id <> '' OR current_ssrc <> '') THEN
      RAISE EXCEPTION 'current_ssrc rollback blocked: active stream state exists';
    END IF;
    ALTER TABLE gb_channel DROP COLUMN current_ssrc;
  END IF;
END $$;
