-- Fail closed: form history permission metadata must stay applied; automatic schema rollback is disabled.
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'Work-order form history permission metadata must stay applied; automatic schema rollback is disabled';
