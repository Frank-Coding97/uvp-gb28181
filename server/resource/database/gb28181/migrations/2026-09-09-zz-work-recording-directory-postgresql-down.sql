-- Fail closed: directory and claim-version metadata are required to safely stop a job.
DO $$ BEGIN RAISE EXCEPTION 'Work-recording directory metadata must be retained; automatic schema rollback is disabled'; END $$;
