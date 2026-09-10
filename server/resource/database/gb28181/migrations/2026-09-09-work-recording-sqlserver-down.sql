-- Fail closed: do not discard durable ownership or recording history.
THROW 51000, 'Work recording history and claims must be retained; automatic schema rollback is disabled', 1;
