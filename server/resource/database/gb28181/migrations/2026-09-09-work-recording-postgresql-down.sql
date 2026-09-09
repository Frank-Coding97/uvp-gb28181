-- Fail closed: do not discard durable ownership or recording history.
DO $$ BEGIN RAISE EXCEPTION 'Work recording history and claims must be retained; automatic schema rollback is disabled'; END $$;
