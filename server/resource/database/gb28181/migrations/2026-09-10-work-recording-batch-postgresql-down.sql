DO $$ BEGIN RAISE EXCEPTION 'Work recording batch history must be retained; automatic schema rollback is disabled'; END $$;
