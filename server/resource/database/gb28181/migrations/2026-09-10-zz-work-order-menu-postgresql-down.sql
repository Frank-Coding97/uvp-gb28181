-- Fail closed: work-order menu and permission metadata must be retained; automatic schema rollback is disabled.
DO $$ BEGIN RAISE EXCEPTION 'Work-order menu and permission metadata must be retained; automatic schema rollback is disabled'; END $$;
