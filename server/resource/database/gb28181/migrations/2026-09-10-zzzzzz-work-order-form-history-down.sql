-- Fail closed: work-order form history metadata must stay applied; automatic schema rollback is disabled.
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'Work-order form history must stay applied; automatic schema rollback is disabled';
