-- Fail closed: retired permission metadata must stay retired; automatic schema rollback is disabled.
DO $$ BEGIN RAISE EXCEPTION 'Retired cascade reconnect permission metadata must stay retired; automatic schema rollback is disabled'; END $$;
