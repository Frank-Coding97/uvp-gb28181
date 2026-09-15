-- Fail closed: retired permission metadata must stay retired; automatic schema rollback is disabled.
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'Retired cascade reconnect permission metadata must stay retired; automatic schema rollback is disabled';
