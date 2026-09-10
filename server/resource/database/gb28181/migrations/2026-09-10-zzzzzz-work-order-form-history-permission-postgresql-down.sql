-- Fail closed: form history permission metadata must stay applied; automatic schema rollback is disabled.
DO $$ BEGIN RAISE EXCEPTION 'Work-order form history permission metadata must stay applied; automatic schema rollback is disabled'; END $$;
