-- Refuse destructive rollback while user data exists.
DELIMITER $$
CREATE PROCEDURE uvp_drop_custom_groups_if_empty()
BEGIN
  IF EXISTS (SELECT 1 FROM gb_custom_group_device LIMIT 1)
     OR EXISTS (SELECT 1 FROM gb_custom_group LIMIT 1) THEN
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'custom group tables are not empty';
  END IF;
  DROP TABLE IF EXISTS gb_custom_group_device;
  DROP TABLE IF EXISTS gb_custom_group;
END$$
DELIMITER ;
CALL uvp_drop_custom_groups_if_empty();
DROP PROCEDURE uvp_drop_custom_groups_if_empty;
