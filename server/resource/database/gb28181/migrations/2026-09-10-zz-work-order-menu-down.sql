-- Fail closed: work-order menu and permission metadata must be retained; automatic schema rollback is disabled.
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'Work-order menu and permission metadata must be retained; automatic schema rollback is disabled';
