-- Fail closed: work-order permission metadata must stay granted; automatic schema rollback is disabled.
THROW 51000, 'Work-order permission metadata must stay granted; automatic schema rollback is disabled', 1;
