DO $$ BEGIN IF EXISTS (SELECT 1 FROM gb_channel_favorite_item LIMIT 1) OR EXISTS (SELECT 1 FROM gb_channel_favorite_group LIMIT 1) THEN RAISE EXCEPTION 'channel favorite tables are not empty'; END IF; END $$;
DROP TABLE IF EXISTS gb_channel_favorite_item;
DROP TABLE IF EXISTS gb_channel_favorite_group;
