DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM gb_playback_scheme_slot LIMIT 1)
     OR EXISTS (SELECT 1 FROM gb_playback_scheme LIMIT 1) THEN
    RAISE EXCEPTION 'playback scheme tables are not empty';
  END IF;
END $$;
DROP TABLE IF EXISTS gb_playback_scheme_slot;
DROP TABLE IF EXISTS gb_playback_scheme;
