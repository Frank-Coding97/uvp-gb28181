-- Fail closed: work-recording permission metadata must be retained; automatic schema rollback is disabled.
THROW 51000, 'Work-recording permission metadata must be retained; automatic schema rollback is disabled', 1;
