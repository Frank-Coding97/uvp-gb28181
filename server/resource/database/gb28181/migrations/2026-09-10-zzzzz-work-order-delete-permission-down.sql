-- Fail closed: work-order permission metadata must stay granted; automatic schema rollback is disabled.
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'Work-order permission metadata must stay granted; automatic schema rollback is disabled';
