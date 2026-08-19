DELIMITER $$
CREATE PROCEDURE uvp_drop_channel_favorites_if_empty()
BEGIN
  IF EXISTS (SELECT 1 FROM gb_channel_favorite_item LIMIT 1)
     OR EXISTS (SELECT 1 FROM gb_channel_favorite_group LIMIT 1) THEN
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'channel favorite tables are not empty';
  END IF;
  DROP TABLE IF EXISTS gb_channel_favorite_item;
  DROP TABLE IF EXISTS gb_channel_favorite_group;
END$$
DELIMITER ;
CALL uvp_drop_channel_favorites_if_empty();
DROP PROCEDURE uvp_drop_channel_favorites_if_empty;
