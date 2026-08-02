DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM gb_custom_group_device LIMIT 1)
     OR EXISTS (SELECT 1 FROM gb_custom_group LIMIT 1) THEN
    RAISE EXCEPTION 'custom group tables are not empty';
  END IF;
END $$;
DROP TABLE IF EXISTS gb_custom_group_device;
DROP TABLE IF EXISTS gb_custom_group;
