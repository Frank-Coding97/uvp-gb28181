-- Fail closed: work-recording permission metadata must be retained; automatic schema rollback is disabled.
DO $$ BEGIN RAISE EXCEPTION 'Work-recording permission metadata must be retained; automatic schema rollback is disabled'; END $$;
