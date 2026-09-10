-- Fail closed: retired permission metadata must stay retired; automatic schema rollback is disabled.
DO $$ BEGIN RAISE EXCEPTION 'Retired work-recording permission metadata must stay retired; automatic schema rollback is disabled'; END $$;
