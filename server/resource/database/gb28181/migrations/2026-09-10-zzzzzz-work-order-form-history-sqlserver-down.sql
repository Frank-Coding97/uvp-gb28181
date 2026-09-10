-- Fail closed: work-order form history metadata must stay applied; automatic schema rollback is disabled.
THROW 51000, 'Work-order form history must stay applied; automatic schema rollback is disabled', 1;
