-- Refuse destructive rollback while user data exists.
DELIMITER $$
CREATE PROCEDURE uvp_drop_playback_schemes_if_empty()
BEGIN
  IF EXISTS (SELECT 1 FROM gb_playback_scheme_slot LIMIT 1)
     OR EXISTS (SELECT 1 FROM gb_playback_scheme LIMIT 1) THEN
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'playback scheme tables are not empty';
  END IF;
  DROP TABLE IF EXISTS gb_playback_scheme_slot;
  DROP TABLE IF EXISTS gb_playback_scheme;
END$$
DELIMITER ;
CALL uvp_drop_playback_schemes_if_empty();
DROP PROCEDURE uvp_drop_playback_schemes_if_empty;
