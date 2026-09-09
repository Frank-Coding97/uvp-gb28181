-- Fail closed: work-recording form permission metadata must be retained; automatic schema rollback is disabled.
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'Work-recording form permission metadata must be retained; automatic schema rollback is disabled';
