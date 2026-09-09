-- Fail closed: ownership metadata is required to safely reconcile live recordings.
DO $$ BEGIN RAISE EXCEPTION 'Recorder ownership metadata must be retained; automatic schema rollback is disabled'; END $$;
