IF EXISTS (SELECT 1 FROM gb_channel_favorite_item) OR EXISTS (SELECT 1 FROM gb_channel_favorite_group) THROW 51000, 'channel favorite tables are not empty', 1;
DROP TABLE IF EXISTS gb_channel_favorite_item;
DROP TABLE IF EXISTS gb_channel_favorite_group;
