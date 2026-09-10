-- Fail closed: work-order form history metadata must stay applied; automatic schema rollback is disabled.
DO $$ BEGIN RAISE EXCEPTION 'Work-order form history must stay applied; automatic schema rollback is disabled'; END $$;
