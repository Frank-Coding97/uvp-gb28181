-- Fail closed: form history permission metadata must stay applied; automatic schema rollback is disabled.
THROW 51000, 'Work-order form history permission metadata must stay applied; automatic schema rollback is disabled', 1;
