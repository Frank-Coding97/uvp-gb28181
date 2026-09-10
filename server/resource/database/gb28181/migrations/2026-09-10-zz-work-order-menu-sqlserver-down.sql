-- Fail closed: work-order menu and permission metadata must be retained; automatic schema rollback is disabled.
THROW 51000, 'Work-order menu and permission metadata must be retained; automatic schema rollback is disabled', 1;
