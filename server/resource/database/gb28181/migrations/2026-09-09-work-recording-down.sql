-- Fail closed: do not discard durable ownership or recording history.
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'Work recording history and claims must be retained; automatic schema rollback is disabled';
