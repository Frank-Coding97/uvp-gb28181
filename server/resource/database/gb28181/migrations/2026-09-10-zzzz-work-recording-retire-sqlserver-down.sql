-- Fail closed: retired permission metadata must stay retired; automatic schema rollback is disabled.
THROW 51000, 'Retired work-recording permission metadata must stay retired; automatic schema rollback is disabled', 1;
