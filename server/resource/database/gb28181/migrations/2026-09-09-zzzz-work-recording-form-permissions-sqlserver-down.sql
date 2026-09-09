-- Fail closed: work-recording form permission metadata must be retained; automatic schema rollback is disabled.
THROW 51000, 'Work-recording form permission metadata must be retained; automatic schema rollback is disabled', 1;
