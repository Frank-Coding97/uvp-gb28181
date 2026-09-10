-- Fail closed: work-order permission metadata must stay granted; automatic schema rollback is disabled.
DO $$ BEGIN RAISE EXCEPTION 'Work-order permission metadata must stay granted; automatic schema rollback is disabled'; END $$;
