-- Fail closed: work-recording form permission metadata must be retained; automatic schema rollback is disabled.
DO $$ BEGIN RAISE EXCEPTION 'Work-recording form permission metadata must be retained; automatic schema rollback is disabled'; END $$;
